package agent

import (
	"context"
	"iter"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/obot-platform/discobot/agent-go/internal/api"
	"github.com/obot-platform/discobot/agent-go/message"
	"github.com/obot-platform/discobot/agent-go/providers"
)

// --- Mock agent for completion tests ---

type mockAgent struct {
	promptFn   func(ctx context.Context, threadID string, req PromptRequest) iter.Seq2[message.MessageChunk, error]
	resumeFn   func(ctx context.Context, threadID string) iter.Seq2[message.MessageChunk, error]
	messagesFn func(threadID, leafID string) ([]message.UIMessage, error)

	interruptedThreads []string
	models             []providers.ModelInfo
	threads            []string
}

func (m *mockAgent) Prompt(ctx context.Context, threadID string, req PromptRequest) iter.Seq2[message.MessageChunk, error] {
	if m.promptFn != nil {
		return m.promptFn(ctx, threadID, req)
	}
	return func(_ func(message.MessageChunk, error) bool) {}
}

func (m *mockAgent) Resume(ctx context.Context, threadID string) iter.Seq2[message.MessageChunk, error] {
	if m.resumeFn != nil {
		return m.resumeFn(ctx, threadID)
	}
	return func(_ func(message.MessageChunk, error) bool) {}
}

func (m *mockAgent) Cancel(_ string) bool {
	return false
}

func (m *mockAgent) Messages(threadID, leafID string) ([]message.UIMessage, error) {
	if m.messagesFn != nil {
		return m.messagesFn(threadID, leafID)
	}
	return nil, nil
}

func (m *mockAgent) ListModels(_ context.Context) ([]providers.ModelInfo, error) {
	return m.models, nil
}

func (m *mockAgent) ListThreads() ([]string, error) {
	return m.threads, nil
}

func (m *mockAgent) HasInterruptedTurn(threadID string) (bool, error) {
	for _, interrupted := range m.interruptedThreads {
		if interrupted == threadID {
			return true, nil
		}
	}
	return false, nil
}

func (m *mockAgent) PendingQuestion(_ string) (*PendingQuestion, error) {
	return nil, nil
}

func (m *mockAgent) SubmitAnswer(_, _ string, _ api.AnswerQuestionRequest) error {
	return nil
}

func (m *mockAgent) FinalResponse(_ string) (string, error) {
	return "", nil
}

func (m *mockAgent) ListCommands() ([]Command, error) {
	return nil, nil
}

type mockCompletionListener struct {
	startCh    chan string
	completeCh chan string
}

func (m *mockCompletionListener) OnTurnStart(threadID string) {
	if m.startCh != nil {
		m.startCh <- threadID
	}
}

func (m *mockCompletionListener) OnTurnComplete(threadID string, _ error) {
	if m.completeCh != nil {
		m.completeCh <- threadID
	}
}

// --- Helpers ---

func simplePromptFn(chunks []message.MessageChunk) func(context.Context, string, PromptRequest) iter.Seq2[message.MessageChunk, error] {
	return func(_ context.Context, _ string, _ PromptRequest) iter.Seq2[message.MessageChunk, error] {
		return func(yield func(message.MessageChunk, error) bool) {
			for _, c := range chunks {
				if !yield(c, nil) {
					return
				}
			}
		}
	}
}

func blockingPromptFn() func(context.Context, string, PromptRequest) iter.Seq2[message.MessageChunk, error] {
	return func(ctx context.Context, _ string, _ PromptRequest) iter.Seq2[message.MessageChunk, error] {
		return func(_ func(message.MessageChunk, error) bool) {
			<-ctx.Done()
		}
	}
}

func waitForDone(t *testing.T, cm *CompletionManager, threadID string) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for completion")
		default:
			result := cm.PollChunks(threadID, 0)
			if result != nil && result.Done {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}

// --- Tests ---

func TestCompletionManager_Chat_SimpleCompletion(t *testing.T) {
	chunks := []message.MessageChunk{
		message.StreamStartChunk{},
		message.TextStartChunk{ID: "t1"},
		message.TextDeltaChunk{ID: "t1", Delta: "Hello!"},
		message.TextEndChunk{ID: "t1"},
		message.FinishChunk{FinishReason: message.FinishReason{Unified: "stop"}},
	}

	agent := &mockAgent{promptFn: simplePromptFn(chunks)}
	cm := NewCompletionManager(agent)

	completionID, err := cm.Chat("thread1", PromptRequest{
		UserParts: []message.UIPart{message.UITextPart{Text: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if completionID == "" {
		t.Error("expected non-empty completion ID")
	}

	waitForDone(t, cm, "thread1")

	result := cm.PollChunks("thread1", 0)
	if result == nil {
		t.Fatal("expected poll result")
	}
	if !result.Done {
		t.Error("expected completion to be done")
	}
	if len(result.Chunks) == 0 {
		t.Error("expected at least some chunks")
	}

	hasText := false
	for _, c := range result.Chunks {
		if _, ok := c.(message.TextDeltaChunk); ok {
			hasText = true
		}
	}
	if !hasText {
		t.Error("missing TextDeltaChunk")
	}
}

func TestCompletionManager_Chat_PrependsStartBeforeEarlyError(t *testing.T) {
	agent := &mockAgent{
		promptFn: func(_ context.Context, _ string, _ PromptRequest) iter.Seq2[message.MessageChunk, error] {
			return func(yield func(message.MessageChunk, error) bool) {
				yield(nil, context.Canceled)
			}
		},
	}
	cm := NewCompletionManager(agent)

	_, err := cm.Chat("thread1", PromptRequest{
		UserParts: []message.UIPart{message.UITextPart{Text: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	waitForDone(t, cm, "thread1")

	result := cm.PollChunks("thread1", 0)
	if result == nil {
		t.Fatal("expected poll result")
	}
	if len(result.Chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(result.Chunks))
	}
	start, ok := result.Chunks[0].(message.StartChunk)
	if !ok {
		t.Fatalf("expected first chunk to be StartChunk, got %T", result.Chunks[0])
	}
	if start.MessageID == "" {
		t.Fatal("expected synthetic start chunk to include a message ID")
	}
	errChunk, ok := result.Chunks[1].(message.ErrorChunk)
	if !ok {
		t.Fatalf("expected second chunk to be ErrorChunk, got %T", result.Chunks[1])
	}
	if errChunk.ErrorText != context.Canceled.Error() {
		t.Fatalf("expected error text %q, got %q", context.Canceled.Error(), errChunk.ErrorText)
	}
}

func TestCompletionManager_Chat_ConflictWhenActive(t *testing.T) {
	agent := &mockAgent{promptFn: blockingPromptFn()}
	cm := NewCompletionManager(agent)

	_, err := cm.Chat("thread1", PromptRequest{
		UserParts: []message.UIPart{message.UITextPart{Text: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Second chat should fail.
	_, err = cm.Chat("thread1", PromptRequest{
		UserParts: []message.UIPart{message.UITextPart{Text: "hi again"}},
	})
	if err == nil {
		t.Error("expected error for concurrent completion")
	}
	if !strings.Contains(err.Error(), "completion_in_progress") {
		t.Errorf("expected completion_in_progress error, got: %v", err)
	}

	// Cancel to clean up.
	cm.Cancel("thread1")
	waitForDone(t, cm, "thread1")
}

func TestCompletionManager_Chat_DifferentThreadsIndependent(t *testing.T) {
	callCount := 0
	agent := &mockAgent{
		promptFn: func(_ context.Context, _ string, _ PromptRequest) iter.Seq2[message.MessageChunk, error] {
			return func(yield func(message.MessageChunk, error) bool) {
				callCount++
				yield(message.TextDeltaChunk{ID: "t1", Delta: "ok"}, nil)
			}
		},
	}
	cm := NewCompletionManager(agent)

	_, err1 := cm.Chat("thread1", PromptRequest{
		UserParts: []message.UIPart{message.UITextPart{Text: "a"}},
	})
	_, err2 := cm.Chat("thread2", PromptRequest{
		UserParts: []message.UIPart{message.UITextPart{Text: "b"}},
	})

	if err1 != nil {
		t.Fatal(err1)
	}
	if err2 != nil {
		t.Fatal(err2)
	}

	waitForDone(t, cm, "thread1")
	waitForDone(t, cm, "thread2")
}

func TestCompletionManager_Cancel(t *testing.T) {
	agent := &mockAgent{promptFn: blockingPromptFn()}
	cm := NewCompletionManager(agent)

	completionID, err := cm.Chat("thread1", PromptRequest{
		UserParts: []message.UIPart{message.UITextPart{Text: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(10 * time.Millisecond)

	cancelledID, ok := cm.Cancel("thread1")
	if !ok {
		t.Error("expected successful cancellation")
	}
	if cancelledID != completionID {
		t.Errorf("expected cancelled ID=%s, got %s", completionID, cancelledID)
	}

	waitForDone(t, cm, "thread1")
}

func TestCompletionManager_Cancel_NoActive(t *testing.T) {
	agent := &mockAgent{}
	cm := NewCompletionManager(agent)

	_, ok := cm.Cancel("thread1")
	if ok {
		t.Error("expected no active completion to cancel")
	}
}

func TestCompletionManager_PollChunks_NoActiveCompletion(t *testing.T) {
	agent := &mockAgent{}
	cm := NewCompletionManager(agent)

	result := cm.PollChunks("thread1", 0)
	if result != nil {
		t.Error("expected nil for non-existent thread")
	}
}

func TestCompletionManager_PollChunks_WithOffset(t *testing.T) {
	chunks := []message.MessageChunk{
		message.StreamStartChunk{},
		message.TextStartChunk{ID: "t1"},
		message.TextDeltaChunk{ID: "t1", Delta: "hello"},
		message.TextEndChunk{ID: "t1"},
		message.FinishChunk{FinishReason: message.FinishReason{Unified: "stop"}},
	}

	agent := &mockAgent{promptFn: simplePromptFn(chunks)}
	cm := NewCompletionManager(agent)

	_, err := cm.Chat("thread1", PromptRequest{
		UserParts: []message.UIPart{message.UITextPart{Text: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	waitForDone(t, cm, "thread1")

	allResult := cm.PollChunks("thread1", 0)
	total := len(allResult.Chunks)
	if total == 0 {
		t.Fatal("expected chunks")
	}

	// Poll with offset should return fewer chunks.
	partial := cm.PollChunks("thread1", total-1)
	if len(partial.Chunks) != 1 {
		t.Errorf("expected 1 chunk from offset, got %d", len(partial.Chunks))
	}

	// Poll past all chunks should return empty.
	empty := cm.PollChunks("thread1", total)
	if len(empty.Chunks) != 0 {
		t.Errorf("expected 0 chunks past end, got %d", len(empty.Chunks))
	}
}

func TestCompletionManager_PollChunks_CoalescesConsecutiveDeltaChunks(t *testing.T) {
	chunks := []message.MessageChunk{
		message.TextDeltaChunk{ID: "t1", Delta: "hel"},
		message.TextDeltaChunk{ID: "t1", Delta: "lo"},
		message.ThreadUpdateChunk{},
		message.TextDeltaChunk{ID: "t1", Delta: " world"},
		message.ReasoningDeltaChunk{ID: "r1", Delta: "why"},
		message.ReasoningDeltaChunk{ID: "r1", Delta: " now"},
	}

	agent := &mockAgent{promptFn: simplePromptFn(chunks)}
	cm := NewCompletionManager(agent)

	if _, err := cm.Chat("thread1", PromptRequest{
		UserParts: []message.UIPart{message.UITextPart{Text: "hi"}},
	}); err != nil {
		t.Fatal(err)
	}

	waitForDone(t, cm, "thread1")

	result := cm.PollChunks("thread1", 0)
	if result == nil {
		t.Fatal("expected poll result")
	}
	if result.NextOffset != len(chunks) {
		t.Fatalf("expected next offset %d, got %d", len(chunks), result.NextOffset)
	}
	if len(result.Chunks) != 4 {
		t.Fatalf("expected 4 coalesced chunks, got %d", len(result.Chunks))
	}
	if len(result.ChunkOffsets) != 4 {
		t.Fatalf("expected 4 chunk offsets, got %d", len(result.ChunkOffsets))
	}

	text1, ok := result.Chunks[0].(message.TextDeltaChunk)
	if !ok || text1.Delta != "hello" {
		t.Fatalf("unexpected first chunk: %#v", result.Chunks[0])
	}
	if result.ChunkOffsets[0] != 1 {
		t.Fatalf("expected first chunk offset 1, got %d", result.ChunkOffsets[0])
	}

	if _, ok := result.Chunks[1].(message.ThreadUpdateChunk); !ok {
		t.Fatalf("expected thread update barrier, got %#v", result.Chunks[1])
	}
	if result.ChunkOffsets[1] != 2 {
		t.Fatalf("expected second chunk offset 2, got %d", result.ChunkOffsets[1])
	}

	text2, ok := result.Chunks[2].(message.TextDeltaChunk)
	if !ok || text2.Delta != " world" {
		t.Fatalf("unexpected third chunk: %#v", result.Chunks[2])
	}
	if result.ChunkOffsets[2] != 3 {
		t.Fatalf("expected third chunk offset 3, got %d", result.ChunkOffsets[2])
	}

	reasoning, ok := result.Chunks[3].(message.ReasoningDeltaChunk)
	if !ok || reasoning.Delta != "why now" {
		t.Fatalf("unexpected fourth chunk: %#v", result.Chunks[3])
	}
	if result.ChunkOffsets[3] != 5 {
		t.Fatalf("expected fourth chunk offset 5, got %d", result.ChunkOffsets[3])
	}
}

func TestCompletionManager_PollChunks_CoalescesOnlyUnreadBatch(t *testing.T) {
	chunks := []message.MessageChunk{
		message.TextDeltaChunk{ID: "t1", Delta: "a"},
		message.TextDeltaChunk{ID: "t1", Delta: "b"},
		message.TextDeltaChunk{ID: "t1", Delta: "c"},
	}

	agent := &mockAgent{promptFn: simplePromptFn(chunks)}
	cm := NewCompletionManager(agent)

	if _, err := cm.Chat("thread1", PromptRequest{
		UserParts: []message.UIPart{message.UITextPart{Text: "hi"}},
	}); err != nil {
		t.Fatal(err)
	}

	waitForDone(t, cm, "thread1")

	result := cm.PollChunks("thread1", 1)
	if result == nil {
		t.Fatal("expected poll result")
	}
	if len(result.Chunks) != 1 {
		t.Fatalf("expected 1 chunk from unread suffix, got %d", len(result.Chunks))
	}
	text, ok := result.Chunks[0].(message.TextDeltaChunk)
	if !ok || text.Delta != "bc" {
		t.Fatalf("unexpected coalesced unread chunk: %#v", result.Chunks[0])
	}
	if len(result.ChunkOffsets) != 1 || result.ChunkOffsets[0] != 2 {
		t.Fatalf("unexpected unread chunk offsets: %#v", result.ChunkOffsets)
	}
	if result.NextOffset != len(chunks) {
		t.Fatalf("expected next offset %d, got %d", len(chunks), result.NextOffset)
	}
}

func TestCompletionManager_ActiveCompletionID(t *testing.T) {
	agent := &mockAgent{promptFn: blockingPromptFn()}
	cm := NewCompletionManager(agent)

	// No active completion.
	if id := cm.ActiveCompletionID("thread1"); id != "" {
		t.Errorf("expected empty, got %s", id)
	}

	completionID, err := cm.Chat("thread1", PromptRequest{
		UserParts: []message.UIPart{message.UITextPart{Text: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(10 * time.Millisecond)

	if id := cm.ActiveCompletionID("thread1"); id != completionID {
		t.Errorf("expected %s, got %s", completionID, id)
	}

	cm.Cancel("thread1")
	waitForDone(t, cm, "thread1")
}

func TestCompletionManager_ChatAfterDone(t *testing.T) {
	callCount := 0
	agent := &mockAgent{
		promptFn: func(_ context.Context, _ string, _ PromptRequest) iter.Seq2[message.MessageChunk, error] {
			callCount++
			return func(yield func(message.MessageChunk, error) bool) {
				yield(message.TextDeltaChunk{ID: "t1", Delta: "ok"}, nil)
			}
		},
	}
	cm := NewCompletionManager(agent)

	_, err := cm.Chat("thread1", PromptRequest{
		UserParts: []message.UIPart{message.UITextPart{Text: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	waitForDone(t, cm, "thread1")

	// Starting a new chat on same thread should succeed after previous is done.
	_, err = cm.Chat("thread1", PromptRequest{
		UserParts: []message.UIPart{message.UITextPart{Text: "hi again"}},
	})
	if err != nil {
		t.Fatalf("expected chat to succeed after previous done: %v", err)
	}

	waitForDone(t, cm, "thread1")
}

func TestCompletionManager_AddListener_ReceivesLifecycleEvents(t *testing.T) {
	chunks := []message.MessageChunk{
		message.TextDeltaChunk{ID: "t1", Delta: "done"},
	}

	agent := &mockAgent{promptFn: simplePromptFn(chunks)}
	cm := NewCompletionManager(agent)
	listener := &mockCompletionListener{
		startCh:    make(chan string, 1),
		completeCh: make(chan string, 1),
	}
	cm.AddCompletionListener(listener)

	_, err := cm.Chat("thread1", PromptRequest{
		UserParts: []message.UIPart{message.UITextPart{Text: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	select {
	case tid := <-listener.startCh:
		if tid != "thread1" {
			t.Fatalf("expected thread1 start, got %s", tid)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for start event")
	}

	select {
	case tid := <-listener.completeCh:
		if tid != "thread1" {
			t.Fatalf("expected thread1 complete, got %s", tid)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for complete event")
	}
}

func TestCompletionManager_WaitNextCompletion_ReturnsFinishedNewCompletion(t *testing.T) {
	agent := &mockAgent{
		promptFn: simplePromptFn([]message.MessageChunk{
			message.TextDeltaChunk{ID: "t1", Delta: "done"},
		}),
	}
	cm := NewCompletionManager(agent)

	firstCompletionID, err := cm.Chat("thread1", PromptRequest{
		UserParts: []message.UIPart{message.UITextPart{Text: "first"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForDone(t, cm, "thread1")

	_, err = cm.Chat("thread1", PromptRequest{
		UserParts: []message.UIPart{message.UITextPart{Text: "second"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForDone(t, cm, "thread1")

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	result := cm.WaitNextCompletion(ctx, "thread1", firstCompletionID)
	if result == nil {
		t.Fatal("expected next completion result")
	}
	if !result.Done {
		t.Fatal("expected finished completion to be returned")
	}
	if result.CompletionID == firstCompletionID {
		t.Fatalf("expected a newer completion than %s", firstCompletionID)
	}
	if len(result.Chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(result.Chunks))
	}
	if delta, ok := result.Chunks[0].(message.TextDeltaChunk); !ok || delta.Delta != "done" {
		t.Fatalf("unexpected chunk: %#v", result.Chunks[0])
	}
}

func TestCompletionManager_WaitChunks_SwitchesToNewCompletionWithoutReusingOffset(t *testing.T) {
	cm := NewCompletionManager(&mockAgent{})

	newCompletion := &activeCompletion{
		id:       "completion-new",
		threadID: "thread1",
		events: []message.MessageChunk{
			message.ThreadResumeChunk{
				Data: message.ThreadResumeData{
					ThreadID:  "thread1",
					MessageID: "assistant-1",
				},
			},
		},
	}
	newCompletion.cond = sync.NewCond(&newCompletion.mu)

	cm.mu.Lock()
	cm.active["thread1"] = newCompletion
	cm.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	result := cm.WaitChunks(ctx, "thread1", "completion-old", 5)
	if result == nil {
		t.Fatal("expected result")
	}
	if result.CompletionID != "completion-new" {
		t.Fatalf("expected completion-new, got %q", result.CompletionID)
	}
	if len(result.Chunks) != 1 {
		t.Fatalf("expected 1 chunk from new completion, got %d", len(result.Chunks))
	}
	if _, ok := result.Chunks[0].(message.ThreadResumeChunk); !ok {
		t.Fatalf("expected thread resume chunk, got %#v", result.Chunks[0])
	}
	if len(result.ChunkOffsets) != 1 || result.ChunkOffsets[0] != 0 {
		t.Fatalf("expected chunk offset 0, got %#v", result.ChunkOffsets)
	}
	if result.NextOffset != 1 {
		t.Fatalf("expected next offset 1, got %d", result.NextOffset)
	}
}

func TestCompletionManager_ResumeInterruptedTurns(t *testing.T) {
	resumeCh := make(chan string, 2)
	agent := &mockAgent{
		threads:            []string{"thread-a", "thread-b"},
		interruptedThreads: []string{"thread-b"},
		resumeFn: func(_ context.Context, threadID string) iter.Seq2[message.MessageChunk, error] {
			if threadID == "thread-b" {
				resumeCh <- threadID
			}
			return func(_ func(message.MessageChunk, error) bool) {}
		},
	}
	cm := NewCompletionManager(agent)
	if err := cm.ResumeInterruptedTurns(); err != nil {
		t.Fatal(err)
	}
	select {
	case threadID := <-resumeCh:
		if threadID != "thread-b" {
			t.Fatalf("expected thread-b resume, got %s", threadID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for resume call")
	}
}

// TestCompletionManager_Messages_ClampsToStartLeaf verifies that while a
// completion is in progress, Messages() passes the completion's starting
// leafID to the underlying agent so that in-progress messages are not
// returned (they arrive via SSE instead).
func TestCompletionManager_Messages_ClampsToStartLeaf(t *testing.T) {
	var capturedLeafID string

	ma := &mockAgent{
		promptFn: blockingPromptFn(),
		messagesFn: func(_, leafID string) ([]message.UIMessage, error) {
			capturedLeafID = leafID
			return nil, nil
		},
	}
	cm := NewCompletionManager(ma)

	startingLeaf := "leaf-before-completion"

	_, err := cm.Chat("thread1", PromptRequest{
		LeafID:    startingLeaf,
		UserParts: []message.UIPart{message.UITextPart{Text: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Give the goroutine time to block inside Prompt.
	time.Sleep(20 * time.Millisecond)

	// ListMessages with no explicit leafID — should be clamped to the
	// completion's starting leaf so SSE and list-messages don't overlap.
	_, _ = cm.Messages("thread1", "")
	if capturedLeafID != startingLeaf {
		t.Errorf("expected leafID %q, got %q", startingLeaf, capturedLeafID)
	}

	// An explicit caller-supplied leafID should pass through unchanged.
	callerLeaf := "explicit-leaf"
	_, _ = cm.Messages("thread1", callerLeaf)
	if capturedLeafID != callerLeaf {
		t.Errorf("explicit leafID: expected %q, got %q", callerLeaf, capturedLeafID)
	}

	// After the completion finishes, no clamping should happen.
	cm.Cancel("thread1")
	waitForDone(t, cm, "thread1")

	_, _ = cm.Messages("thread1", "")
	if capturedLeafID != "" {
		t.Errorf("after completion done: expected empty leafID, got %q", capturedLeafID)
	}
}
