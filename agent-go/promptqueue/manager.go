package promptqueue

import (
	"errors"
	"log"
	"sync"
	"time"

	"github.com/obot-platform/discobot/agent-go/agent"
	"github.com/obot-platform/discobot/agent-go/message"
)

const restoreRetryDelay = 15 * time.Second

// Conversation starts and observes thread turns for queued prompts.
type Conversation interface {
	ActiveCompletionID(threadID string) string
	Chat(threadID string, req agent.PromptRequest) (string, error)
	Resume(threadID string, req agent.PromptRequest) (string, error)
	HasInterruptedTurn(threadID string) (bool, error)
	PendingQuestion(threadID string) (*agent.PendingQuestion, error)
	ListThreads() ([]string, error)
}

// ChangeFunc observes durable queue changes. It is called after the queue has
// changed, outside the manager lock.
type ChangeFunc func(threadID string, queue []Prompt)

// Manager owns prompt queue scheduling and prompt start decisions.
type Manager struct {
	store         *Store
	conversations Conversation
	onChange      ChangeFunc

	mu          sync.Mutex
	timers      map[string]*time.Timer
	timersReady bool
}

// NewManager creates a prompt queue manager.
func NewManager(store *Store, conversations Conversation, onChange ChangeFunc) *Manager {
	return &Manager{
		store:         store,
		conversations: conversations,
		onChange:      onChange,
		timers:        make(map[string]*time.Timer),
	}
}

// SetChangeFunc updates the queue change callback.
func (m *Manager) SetChangeFunc(onChange ChangeFunc) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onChange = onChange
}

// Available reports whether the manager has the dependencies needed to run.
func (m *Manager) Available() bool {
	return m != nil && m.store != nil && m.conversations != nil
}

// StartResult describes whether a prompt started immediately or was queued.
type StartResult struct {
	Status         string
	CompletionID   string
	QueuedPromptID string
	Queue          []Prompt
}

// StartOrQueue starts req now unless the thread is busy or runAfter is set. In
// those cases it appends queued and schedules the queue.
func (m *Manager) StartOrQueue(threadID string, req agent.PromptRequest, queued Prompt) (StartResult, error) {
	if !m.Available() {
		return StartResult{}, errors.New("prompt queue unavailable")
	}

	if activeID := m.conversations.ActiveCompletionID(threadID); activeID != "" || !queued.RunAfter.IsZero() {
		queue, saved, err := m.Enqueue(threadID, queued)
		if err != nil {
			return StartResult{}, err
		}
		return StartResult{Status: "queued", QueuedPromptID: saved.ID, Queue: queue}, nil
	}

	completionID, err := m.startPromptRequest(threadID, req)
	if err != nil {
		return StartResult{}, err
	}
	return StartResult{Status: "started", CompletionID: completionID}, nil
}

// List returns the queued prompts for a thread.
func (m *Manager) List(threadID string) ([]Prompt, error) {
	if !m.Available() {
		return nil, errors.New("prompt queue unavailable")
	}
	return m.store.List(threadID)
}

// Enqueue appends a prompt and schedules it when eligible.
func (m *Manager) Enqueue(threadID string, queued Prompt) ([]Prompt, Prompt, error) {
	if !m.Available() {
		return nil, Prompt{}, errors.New("prompt queue unavailable")
	}

	m.mu.Lock()
	queue, saved, err := m.store.Append(threadID, queued)
	if err == nil {
		m.rescheduleLocked(threadID)
	}
	m.mu.Unlock()
	if err != nil {
		return nil, Prompt{}, err
	}
	m.notifyChange(threadID, queue)
	return queue, saved, nil
}

// Prepend pushes a prompt to the front of the queue and schedules it.
func (m *Manager) Prepend(threadID string, queued Prompt) ([]Prompt, error) {
	if !m.Available() {
		return nil, errors.New("prompt queue unavailable")
	}

	m.mu.Lock()
	queue, err := m.store.Prepend(threadID, queued)
	if err == nil {
		m.rescheduleLocked(threadID)
	}
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	m.notifyChange(threadID, queue)
	return queue, nil
}

// Delete removes a queued prompt and reschedules the queue.
func (m *Manager) Delete(threadID, promptID string) ([]Prompt, bool, error) {
	if !m.Available() {
		return nil, false, errors.New("prompt queue unavailable")
	}

	m.mu.Lock()
	queue, removed, err := m.store.Delete(threadID, promptID)
	if err == nil && removed {
		m.rescheduleLocked(threadID)
	}
	m.mu.Unlock()
	if err != nil || !removed {
		return queue, removed, err
	}
	m.notifyChange(threadID, queue)
	return queue, true, nil
}

// UpdatePrompt updates one queued prompt and reschedules the queue.
func (m *Manager) UpdatePrompt(threadID, promptID string, update Update) ([]Prompt, bool, error) {
	if !m.Available() {
		return nil, false, errors.New("prompt queue unavailable")
	}

	m.mu.Lock()
	queue, updated, err := m.store.UpdatePrompt(threadID, promptID, update)
	if err == nil && updated {
		m.rescheduleLocked(threadID)
	}
	m.mu.Unlock()
	if err != nil || !updated {
		return queue, updated, err
	}
	m.notifyChange(threadID, queue)
	return queue, true, nil
}

// StartNext starts the next eligible queued prompt for a thread, restoring it
// to the front of the queue if the turn cannot be started.
func (m *Manager) StartNext(threadID string) {
	if !m.Available() {
		return
	}

	m.mu.Lock()
	m.stopTimerLocked(threadID)
	if m.conversations.ActiveCompletionID(threadID) != "" {
		m.rescheduleLocked(threadID)
		m.mu.Unlock()
		return
	}

	queue, queuedPrompt, err := m.store.Pop(threadID)
	if err != nil {
		log.Printf("queue: failed to pop queued prompt for %s: %v", threadID, err)
		m.rescheduleLocked(threadID)
		m.mu.Unlock()
		return
	}
	if queuedPrompt == nil {
		m.rescheduleLocked(threadID)
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()
	m.notifyChange(threadID, queue)

	req := agent.PromptRequest{
		UserParts:   append([]message.UIPart{}, queuedPrompt.Message.Parts...),
		Metadata:    queuedPrompt.Message.Metadata,
		Model:       queuedPrompt.Model,
		Reasoning:   queuedPrompt.Reasoning,
		ServiceTier: queuedPrompt.ServiceTier,
	}
	if _, err := m.startPromptRequest(threadID, req); err != nil {
		if errors.Is(err, agent.ErrPendingQuestionRequiresAnswer) {
			log.Printf("queue: paused queued prompt for %s: pending question requires answer", threadID)
			m.restore(threadID, *queuedPrompt, false)
			return
		}
		log.Printf("queue: failed to start queued prompt for %s: %v", threadID, err)
		if queuedPrompt.RunAfter.IsZero() {
			queuedPrompt.RunAfter = time.Now().UTC().Add(restoreRetryDelay)
		}
		m.restore(threadID, *queuedPrompt, true)
		return
	}

	m.mu.Lock()
	m.rescheduleLocked(threadID)
	m.mu.Unlock()
}

// EnableTimers enables queue timers and resumes scheduling for existing threads.
func (m *Manager) EnableTimers() {
	if !m.Available() {
		return
	}
	m.mu.Lock()
	if m.timersReady {
		m.mu.Unlock()
		return
	}
	m.timersReady = true
	m.mu.Unlock()
	go m.resumeTimers()
}

// ClearTimer stops any pending timer for a thread.
func (m *Manager) ClearTimer(threadID string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopTimerLocked(threadID)
}

func (m *Manager) startPromptRequest(threadID string, req agent.PromptRequest) (string, error) {
	if agent.BuiltinSlashCommand(req.UserParts) != "" {
		return m.conversations.Chat(threadID, req)
	}

	pendingQuestion, err := m.conversations.PendingQuestion(threadID)
	if err != nil {
		return "", err
	}
	if pendingQuestion != nil {
		return "", agent.ErrPendingQuestionRequiresAnswer
	}
	interrupted, err := m.conversations.HasInterruptedTurn(threadID)
	if err != nil {
		return "", err
	}
	if interrupted {
		return m.conversations.Resume(threadID, req)
	}
	completionID, err := m.conversations.Chat(threadID, req)
	if errors.Is(err, agent.ErrInterruptedTurnRequiresResume) {
		return m.conversations.Resume(threadID, req)
	}
	return completionID, err
}

func (m *Manager) restore(threadID string, prompt Prompt, reschedule bool) {
	m.mu.Lock()
	queue, err := m.store.Prepend(threadID, prompt)
	if err != nil {
		log.Printf("queue: failed to restore queued prompt for %s: %v", threadID, err)
		m.rescheduleLocked(threadID)
		m.mu.Unlock()
		return
	}
	if reschedule {
		m.rescheduleLocked(threadID)
	}
	m.mu.Unlock()
	m.notifyChange(threadID, queue)
}

func (m *Manager) resumeTimers() {
	threadIDs, err := m.conversations.ListThreads()
	if err != nil {
		log.Printf("queue: failed to list threads for timer resume: %v", err)
		return
	}
	for _, threadID := range threadIDs {
		m.mu.Lock()
		m.rescheduleLocked(threadID)
		m.mu.Unlock()
	}
}

func (m *Manager) stopTimerLocked(threadID string) {
	timer := m.timers[threadID]
	if timer == nil {
		return
	}
	timer.Stop()
	delete(m.timers, threadID)
}

func (m *Manager) rescheduleLocked(threadID string) {
	if !m.timersReady {
		return
	}
	m.stopTimerLocked(threadID)
	if m.conversations.ActiveCompletionID(threadID) != "" {
		return
	}

	nextRunAfter, ready, err := m.store.NextRunAfter(threadID, time.Now().UTC())
	if err != nil {
		log.Printf("queue: failed to load queue for timer reschedule on %s: %v", threadID, err)
		return
	}
	if ready {
		go m.StartNext(threadID)
		return
	}
	if nextRunAfter == nil {
		return
	}

	delay := max(time.Until(*nextRunAfter), 0)
	m.timers[threadID] = time.AfterFunc(delay, func() {
		m.StartNext(threadID)
	})
}

func (m *Manager) notifyChange(threadID string, queue []Prompt) {
	m.mu.Lock()
	onChange := m.onChange
	m.mu.Unlock()
	if onChange != nil {
		onChange(threadID, append([]Prompt{}, queue...))
	}
}
