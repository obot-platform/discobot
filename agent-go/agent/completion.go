package agent

import (
	"context"
	"fmt"
	"iter"
	"strings"
	"sync"

	"github.com/obot-platform/discobot/agent-go/internal/api"
	"github.com/obot-platform/discobot/agent-go/message"
	"github.com/obot-platform/discobot/agent-go/providers"
)

// CompletionListener observes completion lifecycle events.
type CompletionListener interface {
	OnTurnStart(threadID string)
	OnTurnComplete(threadID string, err error)
}

// CompletionManager wraps an Agent with goroutine management, chunk caching,
// and SSE polling. It bridges the synchronous streaming Agent interface to
// the async HTTP handler world.
//
// All critical state is persisted to disk by the underlying Agent; the
// in-memory event cache is non-authoritative and can be rebuilt after a crash.
type CompletionManager struct {
	agent Agent

	mu        sync.Mutex
	cond      *sync.Cond
	active    map[string]*activeCompletion // keyed by threadID
	listeners []CompletionListener
}

type activeCompletion struct {
	id       string
	threadID string
	cancel   context.CancelFunc

	mu      sync.Mutex
	cond    *sync.Cond
	events  []message.MessageChunk // cache for SSE replay (non-authoritative)
	done    bool
	err     error
	leafMsg string // tracks the leaf message ID as messages are saved
}

// NewCompletionManager creates a CompletionManager wrapping the given Agent.
func NewCompletionManager(agent Agent) *CompletionManager {
	cm := &CompletionManager{
		agent:  agent,
		active: make(map[string]*activeCompletion),
	}
	cm.cond = sync.NewCond(&cm.mu)
	return cm
}

// Chat starts a new turn for the given thread. It returns the completion ID
// or an error if a completion is already running for this thread.
// The turn runs in a background goroutine; chunks are cached for SSE replay.
func (cm *CompletionManager) Chat(threadID string, req PromptRequest) (string, error) {
	return cm.startCompletion(threadID, req.LeafID, func(ctx context.Context) iter.Seq2[message.MessageChunk, error] {
		return cm.agent.Prompt(ctx, threadID, req)
	})
}

// Resume starts a background completion that resumes an interrupted turn.
func (cm *CompletionManager) Resume(threadID string) (string, error) {
	return cm.startCompletion(threadID, "", func(ctx context.Context) iter.Seq2[message.MessageChunk, error] {
		return cm.agent.Resume(ctx, threadID)
	})
}

func (cm *CompletionManager) startCompletion(threadID, leafID string, seqFn func(context.Context) iter.Seq2[message.MessageChunk, error]) (string, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if existing, ok := cm.active[threadID]; ok {
		existing.mu.Lock()
		done := existing.done
		existing.mu.Unlock()
		if !done {
			return "", fmt.Errorf("completion_in_progress:%s", existing.id)
		}
	}

	completionID := generateID()
	ctx, cancel := context.WithCancel(context.Background())
	comp := &activeCompletion{id: completionID, threadID: threadID, cancel: cancel, leafMsg: leafID}
	comp.cond = sync.NewCond(&comp.mu)
	cm.active[threadID] = comp
	cm.cond.Broadcast()
	listeners := append([]CompletionListener(nil), cm.listeners...)
	for _, listener := range listeners {
		listener.OnTurnStart(threadID)
	}
	go cm.runCompletion(ctx, comp, threadID, seqFn)
	return completionID, nil
}

// runCompletion drives a completion iterator in a goroutine, caching chunks.
func (cm *CompletionManager) runCompletion(ctx context.Context, comp *activeCompletion, threadID string, seqFn func(context.Context) iter.Seq2[message.MessageChunk, error]) {
	sawStart := false
	for chunk, err := range seqFn(ctx) {
		comp.mu.Lock()
		if err != nil {
			comp.err = err
			if !sawStart {
				comp.events = append(comp.events, message.StartChunk{MessageID: generateID()})
			}
			comp.events = append(comp.events, message.ErrorChunk{ErrorText: err.Error()})
			comp.done = true
			comp.cond.Broadcast()
			comp.mu.Unlock()
			cm.mu.Lock()
			cm.cond.Broadcast()
			cm.mu.Unlock()
			cm.notifyTurnComplete(threadID, err)
			return
		}
		if chunk != nil {
			if _, ok := chunk.(message.StartChunk); ok {
				sawStart = true
			}
			comp.events = append(comp.events, chunk)
			comp.cond.Broadcast()
		}
		comp.mu.Unlock()
	}
	comp.mu.Lock()
	comp.done = true
	comp.cond.Broadcast()
	comp.mu.Unlock()
	cm.mu.Lock()
	cm.cond.Broadcast()
	cm.mu.Unlock()
	cm.notifyTurnComplete(threadID, nil)
}

// PollResult holds the chunks returned by PollChunks or WaitChunks.
type PollResult struct {
	CompletionID string
	Chunks       []message.MessageChunk
	Done         bool
	Err          error
}

// PollChunks returns cached events from the given offset for a thread's
// active completion. Returns nil if no active completion exists.
// The event cache is non-authoritative; on crash recovery, events are
// replayed from the step JSONL files on disk.
func (cm *CompletionManager) PollChunks(threadID string, offset int) *PollResult {
	cm.mu.Lock()
	comp, ok := cm.active[threadID]
	cm.mu.Unlock()
	if !ok {
		return nil
	}

	comp.mu.Lock()
	defer comp.mu.Unlock()

	if offset >= len(comp.events) {
		return &PollResult{CompletionID: comp.id, Done: comp.done, Err: comp.err}
	}

	chunks := make([]message.MessageChunk, len(comp.events)-offset)
	copy(chunks, comp.events[offset:])
	return &PollResult{CompletionID: comp.id, Chunks: chunks, Done: comp.done, Err: comp.err}
}

// WaitChunks blocks until new chunks are available at or after offset (or the
// completion finishes), then returns them. Returns nil if there is no active
// completion for the thread. Unblocks immediately if ctx is cancelled.
func (cm *CompletionManager) WaitChunks(ctx context.Context, threadID string, offset int) *PollResult {
	cm.mu.Lock()
	comp, ok := cm.active[threadID]
	cm.mu.Unlock()
	if !ok {
		return nil
	}

	// Wake the cond when ctx is cancelled so the Wait loop can exit.
	stop := context.AfterFunc(ctx, func() { comp.cond.Broadcast() })
	defer stop()

	comp.mu.Lock()
	defer comp.mu.Unlock()

	for len(comp.events) <= offset && !comp.done && ctx.Err() == nil {
		comp.cond.Wait()
	}

	chunks := make([]message.MessageChunk, len(comp.events)-offset)
	copy(chunks, comp.events[offset:])
	return &PollResult{CompletionID: comp.id, Chunks: chunks, Done: comp.done, Err: comp.err}
}

// WaitNextCompletion blocks until threadID has a completion whose ID differs
// from afterCompletionID, then returns its current cached chunks and done state.
// This lets SSE consumers observe both new active completions and completions
// that started and finished between polls.
// Returns nil if ctx is cancelled first.
func (cm *CompletionManager) WaitNextCompletion(ctx context.Context, threadID, afterCompletionID string) *PollResult {
	stop := context.AfterFunc(ctx, func() {
		cm.mu.Lock()
		cm.cond.Broadcast()
		cm.mu.Unlock()
	})
	defer stop()

	cm.mu.Lock()
	defer cm.mu.Unlock()

	for ctx.Err() == nil {
		comp, ok := cm.active[threadID]
		if ok {
			comp.mu.Lock()
			if comp.id != afterCompletionID {
				chunks := make([]message.MessageChunk, len(comp.events))
				copy(chunks, comp.events)
				result := &PollResult{
					CompletionID: comp.id,
					Chunks:       chunks,
					Done:         comp.done,
					Err:          comp.err,
				}
				comp.mu.Unlock()
				return result
			}
			comp.mu.Unlock()
		}
		cm.cond.Wait()
	}

	return nil
}

// Cancel cancels the active completion for a thread.
// Returns the completion ID and true if there was an active completion,
// or empty string and false if no active completion exists.
func (cm *CompletionManager) Cancel(threadID string) (string, bool) {
	cm.mu.Lock()
	comp, ok := cm.active[threadID]
	cm.mu.Unlock()
	if !ok {
		return "", false
	}

	comp.mu.Lock()
	done := comp.done
	comp.mu.Unlock()

	if done {
		return "", false
	}

	comp.cancel()
	return comp.id, true
}

// ActiveCompletionID returns the active completion ID for a thread,
// or empty string if none is active.
func (cm *CompletionManager) ActiveCompletionID(threadID string) string {
	cm.mu.Lock()
	comp, ok := cm.active[threadID]
	cm.mu.Unlock()
	if !ok {
		return ""
	}
	comp.mu.Lock()
	defer comp.mu.Unlock()
	if comp.done {
		return ""
	}
	return comp.id
}

// Messages returns the conversation history for a thread as UI-projected JSON.
// If a completion is currently running and no leafID was specified, the result
// is clamped to the completion's starting leaf so that in-progress messages
// are not returned (they arrive via the SSE stream instead).
func (cm *CompletionManager) Messages(threadID, leafID string) ([]message.UIMessage, error) {
	if leafID == "" {
		if startLeaf := cm.activeCompletionLeafID(threadID); startLeaf != "" {
			leafID = startLeaf
		}
	}
	return cm.agent.Messages(threadID, leafID)
}

// activeCompletionLeafID returns the pre-completion leaf ID for the active
// (not yet done) completion on threadID, or "" if none is running.
func (cm *CompletionManager) activeCompletionLeafID(threadID string) string {
	cm.mu.Lock()
	comp, ok := cm.active[threadID]
	cm.mu.Unlock()
	if !ok {
		return ""
	}
	comp.mu.Lock()
	defer comp.mu.Unlock()
	if comp.done {
		return ""
	}
	return comp.leafMsg
}

// ListModels returns available models from the provider.
func (cm *CompletionManager) ListModels(ctx context.Context) ([]providers.ModelInfo, error) {
	return cm.agent.ListModels(ctx)
}

// ListThreads returns all thread IDs.
func (cm *CompletionManager) ListThreads() ([]string, error) {
	return cm.agent.ListThreads()
}

// AddCompletionListener registers a completion lifecycle listener.
func (cm *CompletionManager) AddCompletionListener(listener CompletionListener) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.listeners = append(cm.listeners, listener)
}

// ResumeInterruptedTurns starts resume completions for any interrupted threads
// that are not already running.
func (cm *CompletionManager) ResumeInterruptedTurns() error {
	threads, err := cm.ListThreads()
	if err != nil {
		return err
	}
	for _, threadID := range threads {
		interrupted, err := cm.agent.HasInterruptedTurn(threadID)
		if err != nil {
			return err
		}
		if !interrupted {
			continue
		}
		if _, err := cm.Resume(threadID); err != nil && !strings.Contains(err.Error(), "completion_in_progress") {
			return err
		}
	}
	return nil
}

// HasInterruptedTurn reports whether threadID has an unfinished turn.
func (cm *CompletionManager) HasInterruptedTurn(threadID string) (bool, error) {
	return cm.agent.HasInterruptedTurn(threadID)
}

// PendingQuestion returns the pending AskUserQuestion for a thread, or nil.
func (cm *CompletionManager) PendingQuestion(threadID string) (*PendingQuestion, error) {
	return cm.agent.PendingQuestion(threadID)
}

// SubmitAnswer persists the user's response for a pending approval.
func (cm *CompletionManager) SubmitAnswer(threadID, approvalID string, req api.AnswerQuestionRequest) error {
	return cm.agent.SubmitAnswer(threadID, approvalID, req)
}

func (cm *CompletionManager) notifyTurnComplete(threadID string, err error) {
	cm.mu.Lock()
	listeners := append([]CompletionListener(nil), cm.listeners...)
	cm.mu.Unlock()
	for _, listener := range listeners {
		listener.OnTurnComplete(threadID, err)
	}
}
