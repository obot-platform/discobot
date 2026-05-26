package agent

import (
	"context"
	"fmt"
	"iter"
	"strings"
	"sync"

	"github.com/obot-platform/discobot/agent-go/internal/api"
	"github.com/obot-platform/discobot/agent-go/message"
)

// CompletionListener observes completion lifecycle events.
type CompletionListener interface {
	OnTurnStart(threadID string)
	OnTurnComplete(threadID string, err error)
}

type externalCompletionIDProvider struct {
	id       int
	provider func(threadID string) string
}

type externalCompletionCancelProvider struct {
	id       int
	provider func(threadID string) (string, bool)
}

var (
	externalCompletionIDMu            sync.Mutex
	externalCompletionIDProviders     []externalCompletionIDProvider
	externalCompletionCancelProviders []externalCompletionCancelProvider
	nextExternalCompletionProviderID  int
	nextExternalCompletionCancelID    int
)

// RegisterExternalCompletionIDProvider registers a package-level provider that
// reports externally managed in-memory runs for a thread. It returns a cleanup
// function that unregisters the provider.
func RegisterExternalCompletionIDProvider(provider func(threadID string) string) func() {
	if provider == nil {
		return func() {}
	}
	externalCompletionIDMu.Lock()
	nextExternalCompletionProviderID++
	id := nextExternalCompletionProviderID
	externalCompletionIDProviders = append(externalCompletionIDProviders, externalCompletionIDProvider{
		id:       id,
		provider: provider,
	})
	externalCompletionIDMu.Unlock()

	return func() {
		externalCompletionIDMu.Lock()
		defer externalCompletionIDMu.Unlock()
		for i, registered := range externalCompletionIDProviders {
			if registered.id == id {
				externalCompletionIDProviders = append(externalCompletionIDProviders[:i], externalCompletionIDProviders[i+1:]...)
				return
			}
		}
	}
}

// RegisterExternalCompletionCancelProvider registers a package-level provider
// that cancels externally managed in-memory runs for a thread. It returns a
// cleanup function that unregisters the provider.
func RegisterExternalCompletionCancelProvider(provider func(threadID string) (string, bool)) func() {
	if provider == nil {
		return func() {}
	}
	externalCompletionIDMu.Lock()
	nextExternalCompletionCancelID++
	id := nextExternalCompletionCancelID
	externalCompletionCancelProviders = append(externalCompletionCancelProviders, externalCompletionCancelProvider{
		id:       id,
		provider: provider,
	})
	externalCompletionIDMu.Unlock()

	return func() {
		externalCompletionIDMu.Lock()
		defer externalCompletionIDMu.Unlock()
		for i, registered := range externalCompletionCancelProviders {
			if registered.id == id {
				externalCompletionCancelProviders = append(externalCompletionCancelProviders[:i], externalCompletionCancelProviders[i+1:]...)
				return
			}
		}
	}
}

func externalCompletionID(threadID string) string {
	externalCompletionIDMu.Lock()
	providers := append([]externalCompletionIDProvider(nil), externalCompletionIDProviders...)
	externalCompletionIDMu.Unlock()
	for _, registered := range providers {
		if id := strings.TrimSpace(registered.provider(threadID)); id != "" {
			return id
		}
	}
	return ""
}

func cancelExternalCompletion(threadID string) (string, bool) {
	externalCompletionIDMu.Lock()
	providers := append([]externalCompletionCancelProvider(nil), externalCompletionCancelProviders...)
	externalCompletionIDMu.Unlock()
	for _, registered := range providers {
		if id, ok := registered.provider(threadID); ok {
			return strings.TrimSpace(id), true
		}
	}
	return "", false
}

// ConversationManager wraps an Agent with goroutine management, chunk caching,
// and SSE polling. It bridges the synchronous streaming Agent interface to
// the async HTTP handler world.
//
// All critical state is persisted to disk by the underlying Agent; the
// in-memory event cache is non-authoritative and can be rebuilt after a crash.
type ConversationManager struct {
	agent Agent

	mu                      sync.Mutex
	cond                    *sync.Cond
	active                  map[string]*activeCompletion // keyed by threadID
	listeners               []CompletionListener
	ephemeralSubscribers    map[int]chan message.MessageChunk
	nextEphemeralSubscriber int
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

// NewConversationManager creates a ConversationManager wrapping the given Agent.
func NewConversationManager(agent Agent) *ConversationManager {
	cm := &ConversationManager{
		agent:                agent,
		active:               make(map[string]*activeCompletion),
		ephemeralSubscribers: make(map[int]chan message.MessageChunk),
	}
	cm.cond = sync.NewCond(&cm.mu)
	return cm
}

// Chat starts a new turn for the given thread. It returns the completion ID
// or an error if a completion is already running for this thread.
// The turn runs in a background goroutine; chunks are cached for SSE replay.
func (cm *ConversationManager) Chat(threadID string, req PromptRequest) (string, error) {
	switch BuiltinSlashCommand(req.UserParts) {
	case "compact":
		return cm.startCompletion(threadID, func(ctx context.Context) (string, iter.Seq2[message.MessageChunk, error], error) {
			return "", cm.agent.Compact(ctx, threadID, req), nil
		})
	case "reset":
		return cm.startCompletion(threadID, func(ctx context.Context) (string, iter.Seq2[message.MessageChunk, error], error) {
			return "", cm.resetStream(ctx, threadID), nil
		})
	}
	return cm.startCompletion(threadID, func(ctx context.Context) (string, iter.Seq2[message.MessageChunk, error], error) {
		return "", cm.agent.Prompt(ctx, threadID, req), nil
	})
}

func (cm *ConversationManager) resetStream(ctx context.Context, threadID string) iter.Seq2[message.MessageChunk, error] {
	return func(yield func(message.MessageChunk, error) bool) {
		info, err := cm.agent.Reset(ctx, threadID)
		if err != nil {
			yield(nil, err)
			return
		}
		if !yield(threadUpdateChunkFromInfo(info), nil) {
			return
		}
		yieldStatusMessage(yield, "Conversation reset.")
	}
}

func yieldStatusMessage(yield func(message.MessageChunk, error) bool, text string) bool {
	messageID := generateID()
	textID := generateID()
	if !yield(message.StartChunk{MessageID: messageID}, nil) {
		return false
	}
	if !yield(message.TextStartChunk{ID: textID}, nil) {
		return false
	}
	if !yield(message.TextDeltaChunk{ID: textID, Delta: text}, nil) {
		return false
	}
	if !yield(message.TextEndChunk{ID: textID}, nil) {
		return false
	}
	return yield(message.ResponseFinishChunk{FinishReason: "stop"}, nil)
}

func (cm *ConversationManager) ListThreadInfos() ([]ThreadInfo, error) {
	infos, err := cm.agent.ListThreadInfos()
	if err != nil {
		return nil, err
	}
	for i := range infos {
		cm.applyActiveThreadState(&infos[i])
	}
	return infos, nil
}

func (cm *ConversationManager) GetThreadInfo(threadID string) (ThreadInfo, error) {
	info, err := cm.agent.GetThreadInfo(threadID)
	if err != nil {
		return ThreadInfo{}, err
	}
	cm.applyActiveThreadState(&info)
	return info, nil
}

func (cm *ConversationManager) GetThreadTokenUsageDetails(threadID string) (ThreadTokenUsageDetails, error) {
	return cm.agent.GetThreadTokenUsageDetails(threadID)
}

func (cm *ConversationManager) CreateThread(ctx context.Context, req CreateThreadRequest) (ThreadInfo, error) {
	info, err := cm.agent.CreateThread(ctx, req)
	if err != nil {
		return ThreadInfo{}, err
	}
	cm.applyActiveThreadState(&info)
	return info, nil
}

func (cm *ConversationManager) UpdateThread(ctx context.Context, threadID string, req UpdateThreadRequest) (ThreadInfo, error) {
	info, err := cm.agent.UpdateThread(ctx, threadID, req)
	if err != nil {
		return ThreadInfo{}, err
	}
	cm.applyActiveThreadState(&info)
	return info, nil
}

func (cm *ConversationManager) DeleteThread(ctx context.Context, threadID string) error {
	cm.Cancel(threadID)
	return cm.agent.DeleteThread(ctx, threadID)
}

func (cm *ConversationManager) applyActiveThreadState(info *ThreadInfo) {
	if info == nil || cm.ActiveCompletionID(info.ID) != "" {
		return
	}
	if interrupted, err := cm.HasInterruptedTurn(info.ID); err == nil && interrupted {
		info.State = ThreadStateInterrupted
	}
}

// Resume starts a background completion that resumes an interrupted turn.
func (cm *ConversationManager) Resume(threadID string, req PromptRequest) (string, error) {
	return cm.startCompletion(threadID, func(ctx context.Context) (string, iter.Seq2[message.MessageChunk, error], error) {
		result, err := cm.agent.Resume(ctx, threadID, req)
		if err != nil {
			return "", nil, err
		}
		return result.ReplayLeafID, result.Stream, nil
	})
}

// BuiltinSlashCommand reports the built-in command represented by parts.
// Built-ins are handled by ConversationManager before dispatching to Prompt or
// Resume so user-defined slash commands cannot shadow them.
func BuiltinSlashCommand(parts []message.UIPart) string {
	if len(parts) != 1 {
		return ""
	}
	textPart, ok := parts[0].(message.UITextPart)
	if !ok {
		return ""
	}
	switch strings.TrimSpace(textPart.Text) {
	case "/compact":
		return "compact"
	case "/reset":
		return "reset"
	default:
		return ""
	}
}

func threadUpdateChunkFromInfo(info ThreadInfo) message.ThreadUpdateChunk {
	return message.ThreadUpdateChunk{
		Data: message.ThreadUpdateData{
			Thread: message.ThreadUpdateInfo{
				ID:            info.ID,
				Name:          info.Name,
				CWD:           info.CWD,
				LastMessage:   info.LastMessage,
				ErrorMessage:  info.ErrorMessage,
				Model:         info.Model,
				Reasoning:     info.Reasoning,
				ServiceTier:   info.ServiceTier,
				State:         string(info.State),
				ActiveCommand: info.ActiveCommand,
				Metadata:      info.Metadata,
				TokenUsage: message.TokenUsageInfo{
					Total:           info.TokenUsage.Total,
					LastStep:        info.TokenUsage.LastStep,
					LastTurn:        info.TokenUsage.LastTurn,
					ModelMaxTokens:  info.TokenUsage.ModelMaxTokens,
					MaxOutputTokens: info.TokenUsage.MaxOutputTokens,
					Prices:          info.TokenUsage.Prices,
				},
			},
		},
	}
}

func (cm *ConversationManager) startCompletion(
	threadID string,
	prepare func(context.Context) (string, iter.Seq2[message.MessageChunk, error], error),
) (string, error) {
	cm.mu.Lock()
	if existingID := cm.activeCompletionIDLocked(threadID); existingID != "" {
		cm.mu.Unlock()
		return "", fmt.Errorf("completion_in_progress:%s", existingID)
	}

	completionID := generateID()
	ctx, cancel := context.WithCancel(context.Background())
	comp := &activeCompletion{id: completionID, threadID: threadID, cancel: cancel}
	comp.cond = sync.NewCond(&comp.mu)
	cm.active[threadID] = comp
	cm.mu.Unlock()

	leafID, seq, err := prepare(ctx)
	if err != nil {
		cancel()
		cm.mu.Lock()
		if current, ok := cm.active[threadID]; ok && current == comp {
			delete(cm.active, threadID)
			cm.cond.Broadcast()
		}
		cm.mu.Unlock()
		return "", err
	}

	comp.mu.Lock()
	comp.leafMsg = leafID
	comp.mu.Unlock()

	cm.mu.Lock()
	cm.cond.Broadcast()
	listeners := append([]CompletionListener(nil), cm.listeners...)
	cm.mu.Unlock()
	for _, listener := range listeners {
		listener.OnTurnStart(threadID)
	}

	go cm.runCompletion(ctx, comp, threadID, seq)
	return completionID, nil
}

// runCompletion drives a completion iterator in a goroutine, caching chunks.
func (cm *ConversationManager) runCompletion(ctx context.Context, comp *activeCompletion, threadID string, seq iter.Seq2[message.MessageChunk, error]) {
	sawStart := false
	for chunk, err := range seq {
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
	if ctxErr := ctx.Err(); ctxErr != nil {
		comp.err = ctxErr
	}
	comp.done = true
	comp.cond.Broadcast()
	comp.mu.Unlock()
	cm.mu.Lock()
	cm.cond.Broadcast()
	cm.mu.Unlock()
	cm.notifyTurnComplete(threadID, ctx.Err())
}

// PollResult holds the chunks returned by PollChunks or WaitChunks.
type PollResult struct {
	CompletionID string
	Chunks       []message.MessageChunk
	ChunkOffsets []int
	NextOffset   int
	Done         bool
	Err          error
}

// EmitEphemeralChunk publishes a global ephemeral chunk to current subscribers only.
func (cm *ConversationManager) EmitEphemeralChunk(chunk message.MessageChunk) {
	cm.mu.Lock()
	subscribers := make([]chan message.MessageChunk, 0, len(cm.ephemeralSubscribers))
	for _, ch := range cm.ephemeralSubscribers {
		subscribers = append(subscribers, ch)
	}
	cm.mu.Unlock()

	for _, ch := range subscribers {
		select {
		case ch <- chunk:
		default:
		}
	}
}

// SubscribeEphemeral subscribes to live ephemeral chunks until the caller unsubscribes.
func (cm *ConversationManager) SubscribeEphemeral() (<-chan message.MessageChunk, func()) {
	ch := make(chan message.MessageChunk, 16)

	cm.mu.Lock()
	id := cm.nextEphemeralSubscriber
	cm.nextEphemeralSubscriber++
	cm.ephemeralSubscribers[id] = ch
	cm.mu.Unlock()

	return ch, func() {
		cm.mu.Lock()
		delete(cm.ephemeralSubscribers, id)
		cm.mu.Unlock()
	}
}

// PollChunks returns cached events from the given offset for a thread's
// active completion. Returns nil if no active completion exists.
// The event cache is non-authoritative; on crash recovery, events are
// replayed from the step JSONL files on disk.
func (cm *ConversationManager) PollChunks(threadID string, offset int) *PollResult {
	cm.mu.Lock()
	comp, ok := cm.active[threadID]
	cm.mu.Unlock()
	if !ok {
		return nil
	}

	comp.mu.Lock()
	defer comp.mu.Unlock()

	if offset >= len(comp.events) {
		return &PollResult{
			CompletionID: comp.id,
			ChunkOffsets: []int{},
			NextOffset:   len(comp.events),
			Done:         comp.done,
			Err:          comp.err,
		}
	}

	chunks, chunkOffsets := coalesceChunkBatch(comp.events[offset:], offset)
	return &PollResult{
		CompletionID: comp.id,
		Chunks:       chunks,
		ChunkOffsets: chunkOffsets,
		NextOffset:   len(comp.events),
		Done:         comp.done,
		Err:          comp.err,
	}
}

// WaitChunks blocks until new chunks are available at or after offset for the
// expected completion ID (or that completion finishes), then returns them.
// Returns nil if there is no active completion for the thread.
//
// If the thread has already rotated to a newer completion, this returns the
// newer completion immediately from offset 0 so callers do not accidentally
// apply a stale offset from the previous completion to the new one.
//
// Unblocks immediately if ctx is cancelled.
func (cm *ConversationManager) WaitChunks(ctx context.Context, threadID, expectedCompletionID string, offset int) *PollResult {
	cm.mu.Lock()
	comp, ok := cm.active[threadID]
	cm.mu.Unlock()
	if !ok {
		return nil
	}

	if expectedCompletionID != "" && comp.id != expectedCompletionID {
		comp.mu.Lock()
		defer comp.mu.Unlock()
		chunks, chunkOffsets := coalesceChunkBatch(comp.events, 0)
		return &PollResult{
			CompletionID: comp.id,
			Chunks:       chunks,
			ChunkOffsets: chunkOffsets,
			NextOffset:   len(comp.events),
			Done:         comp.done,
			Err:          comp.err,
		}
	}

	// Wake the cond when ctx is cancelled so the Wait loop can exit.
	stop := context.AfterFunc(ctx, func() { comp.cond.Broadcast() })
	defer stop()

	comp.mu.Lock()
	defer comp.mu.Unlock()

	for len(comp.events) <= offset && !comp.done && ctx.Err() == nil {
		comp.cond.Wait()
	}

	chunks, chunkOffsets := coalesceChunkBatch(comp.events[offset:], offset)
	return &PollResult{
		CompletionID: comp.id,
		Chunks:       chunks,
		ChunkOffsets: chunkOffsets,
		NextOffset:   len(comp.events),
		Done:         comp.done,
		Err:          comp.err,
	}
}

// WaitNextCompletion blocks until threadID has a completion whose ID differs
// from afterCompletionID, then returns its current cached chunks and done state.
// This lets SSE consumers observe both new active conversations and conversations
// that started and finished between polls.
// Returns nil if ctx is cancelled first.
func (cm *ConversationManager) WaitNextCompletion(ctx context.Context, threadID, afterCompletionID string) *PollResult {
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
				chunks, chunkOffsets := coalesceChunkBatch(comp.events, 0)
				result := &PollResult{
					CompletionID: comp.id,
					Chunks:       chunks,
					ChunkOffsets: chunkOffsets,
					NextOffset:   len(comp.events),
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
func (cm *ConversationManager) Cancel(threadID string) (string, bool) {
	cm.mu.Lock()
	comp, ok := cm.active[threadID]
	cm.mu.Unlock()
	if !ok {
		if id, ok := cancelExternalCompletion(threadID); ok {
			return id, true
		}
		return "", cm.agent.Cancel(threadID)
	}

	comp.mu.Lock()
	done := comp.done
	comp.mu.Unlock()

	if done {
		if id, ok := cancelExternalCompletion(threadID); ok {
			return id, true
		}
		return comp.id, cm.agent.Cancel(threadID)
	}

	comp.cancel()
	return comp.id, true
}

// EmitChunkIfActive appends a non-provider chunk to the current active
// completion for threadID so connected SSE clients observe thread-scoped updates
// such as prompt queue changes immediately.
func (cm *ConversationManager) EmitChunkIfActive(threadID string, chunk message.MessageChunk) bool {
	cm.mu.Lock()
	comp, ok := cm.active[threadID]
	cm.mu.Unlock()
	if !ok {
		return false
	}

	comp.mu.Lock()
	defer comp.mu.Unlock()
	if comp.done {
		return false
	}
	comp.events = append(comp.events, chunk)
	comp.cond.Broadcast()
	return true
}

// ActiveCompletionID returns the active completion ID for a thread,
// or empty string if none is active.
func (cm *ConversationManager) ActiveCompletionID(threadID string) string {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return cm.activeCompletionIDLocked(threadID)
}

func (cm *ConversationManager) activeCompletionIDLocked(threadID string) string {
	comp, ok := cm.active[threadID]
	if !ok {
		return externalCompletionID(threadID)
	}
	comp.mu.Lock()
	defer comp.mu.Unlock()
	if comp.done {
		return externalCompletionID(threadID)
	}
	return comp.id
}

// Messages returns the conversation history for a thread as UI-projected JSON.
// If a completion is currently running and no leafID was specified, the result
// is clamped to the completion's starting leaf so that in-progress messages
// are not returned (they arrive via the SSE stream instead).
func (cm *ConversationManager) Messages(threadID, leafID string) ([]message.UIMessage, error) {
	if leafID == "" {
		if startLeaf := cm.activeCompletionLeafID(threadID); startLeaf != "" {
			leafID = startLeaf
		}
	}
	return cm.agent.Messages(threadID, leafID)
}

// activeCompletionLeafID returns the pre-completion leaf ID for the active
// (not yet done) completion on threadID, or "" if none is running.
func (cm *ConversationManager) activeCompletionLeafID(threadID string) string {
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

// ListThreads returns all thread IDs.
func (cm *ConversationManager) ListThreads() ([]string, error) {
	return cm.agent.ListThreads()
}

// ListCommands returns all available slash commands.
func (cm *ConversationManager) ListCommands() ([]Command, error) {
	return cm.agent.ListCommands()
}

// AddCompletionListener registers a completion lifecycle listener.
func (cm *ConversationManager) AddCompletionListener(listener CompletionListener) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.listeners = append(cm.listeners, listener)
}

// ResumeInterruptedTurns starts resume conversations for any interrupted threads
// that are not already running.
func (cm *ConversationManager) ResumeInterruptedTurns() error {
	threads, err := cm.ListThreads()
	if err != nil {
		return err
	}
	for _, threadID := range threads {
		if cm.ActiveCompletionID(threadID) != "" {
			continue
		}
		interrupted, err := cm.agent.HasInterruptedTurn(threadID)
		if err != nil {
			return err
		}
		if !interrupted {
			continue
		}
		if _, err := cm.Resume(threadID, PromptRequest{}); err != nil && !strings.Contains(err.Error(), "completion_in_progress") {
			return err
		}
	}
	return nil
}

// HasInterruptedTurn reports whether threadID has an unfinished turn.
func (cm *ConversationManager) HasInterruptedTurn(threadID string) (bool, error) {
	return cm.agent.HasInterruptedTurn(threadID)
}

// PendingQuestion returns the pending AskUserQuestion for a thread, or nil.
func (cm *ConversationManager) PendingQuestion(threadID string) (*PendingQuestion, error) {
	return cm.agent.PendingQuestion(threadID)
}

// SubmitAnswer persists the user's response for a pending approval.
func (cm *ConversationManager) SubmitAnswer(threadID, approvalID string, req api.AnswerQuestionRequest) error {
	return cm.agent.SubmitAnswer(threadID, approvalID, req)
}

func (cm *ConversationManager) notifyTurnComplete(threadID string, err error) {
	cm.mu.Lock()
	listeners := append([]CompletionListener(nil), cm.listeners...)
	cm.mu.Unlock()
	for _, listener := range listeners {
		listener.OnTurnComplete(threadID, err)
	}
}

func coalesceChunkBatch(chunks []message.MessageChunk, baseOffset int) ([]message.MessageChunk, []int) {
	coalesced := make([]message.MessageChunk, 0, len(chunks))
	offsets := make([]int, 0, len(chunks))

	for i, chunk := range chunks {
		rawOffset := baseOffset + i
		if len(coalesced) > 0 {
			switch current := chunk.(type) {
			case message.TextDeltaChunk:
				if previous, ok := coalesced[len(coalesced)-1].(message.TextDeltaChunk); ok && previous.ID == current.ID {
					previous.Delta += current.Delta
					if len(current.ProviderMetadata) > 0 {
						previous.ProviderMetadata = current.ProviderMetadata
					}
					coalesced[len(coalesced)-1] = previous
					offsets[len(offsets)-1] = rawOffset
					continue
				}
			case message.ReasoningDeltaChunk:
				if previous, ok := coalesced[len(coalesced)-1].(message.ReasoningDeltaChunk); ok && previous.ID == current.ID {
					previous.Delta += current.Delta
					if len(current.ProviderMetadata) > 0 {
						previous.ProviderMetadata = current.ProviderMetadata
					}
					coalesced[len(coalesced)-1] = previous
					offsets[len(offsets)-1] = rawOffset
					continue
				}
			}
		}

		coalesced = append(coalesced, chunk)
		offsets = append(offsets, rawOffset)
	}

	return coalesced, offsets
}
