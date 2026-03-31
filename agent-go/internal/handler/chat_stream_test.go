package handler

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/obot-platform/discobot/agent-go/agent"
	"github.com/obot-platform/discobot/agent-go/agentimpl"
	"github.com/obot-platform/discobot/agent-go/internal/api"
	"github.com/obot-platform/discobot/agent-go/message"
	"github.com/obot-platform/discobot/agent-go/providers"
	"github.com/obot-platform/discobot/agent-go/thread"
)

type streamTestAgent struct {
	promptFn             func(ctx context.Context, threadID string, req agent.PromptRequest) iter.Seq2[message.MessageChunk, error]
	resumeFn             func(ctx context.Context, threadID string) iter.Seq2[message.MessageChunk, error]
	messagesFn           func(threadID, leafID string) ([]message.UIMessage, error)
	pendingQuestionFn    func(threadID string) (*agent.PendingQuestion, error)
	submitAnswerFn       func(threadID, approvalID string, req api.AnswerQuestionRequest) error
	hasInterruptedTurnFn func(threadID string) (bool, error)
	listThreadsFn        func() ([]string, error)
}

func (m *streamTestAgent) Prompt(ctx context.Context, threadID string, req agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
	if m.promptFn != nil {
		return m.promptFn(ctx, threadID, req)
	}
	return func(_ func(message.MessageChunk, error) bool) {}
}

func (m *streamTestAgent) Resume(ctx context.Context, threadID string) iter.Seq2[message.MessageChunk, error] {
	if m.resumeFn != nil {
		return m.resumeFn(ctx, threadID)
	}
	return func(_ func(message.MessageChunk, error) bool) {}
}

func (m *streamTestAgent) Cancel(_ string) bool { return false }
func (m *streamTestAgent) Messages(threadID, leafID string) ([]message.UIMessage, error) {
	if m.messagesFn != nil {
		return m.messagesFn(threadID, leafID)
	}
	return nil, nil
}
func (m *streamTestAgent) ListModels(_ context.Context) ([]providers.ModelInfo, error) {
	return nil, nil
}
func (m *streamTestAgent) ListThreads() ([]string, error) {
	if m.listThreadsFn != nil {
		return m.listThreadsFn()
	}
	return nil, nil
}
func (m *streamTestAgent) HasInterruptedTurn(threadID string) (bool, error) {
	if m.hasInterruptedTurnFn != nil {
		return m.hasInterruptedTurnFn(threadID)
	}
	return false, nil
}
func (m *streamTestAgent) PendingQuestion(threadID string) (*agent.PendingQuestion, error) {
	if m.pendingQuestionFn != nil {
		return m.pendingQuestionFn(threadID)
	}
	return nil, nil
}
func (m *streamTestAgent) SubmitAnswer(threadID, approvalID string, req api.AnswerQuestionRequest) error {
	if m.submitAnswerFn != nil {
		return m.submitAnswerFn(threadID, approvalID, req)
	}
	return nil
}
func (m *streamTestAgent) FinalResponse(_ string) (string, error) { return "", nil }
func (m *streamTestAgent) ListCommands() ([]agent.Command, error) { return nil, nil }

type sseFrame struct {
	ID    string
	Event string
	Data  string
	Done  bool
}

func assertCompletionStatusFrame(
	t *testing.T,
	frame sseFrame,
	threadID, completionID string,
	isRunning bool,
) {
	t.Helper()
	if frame.Event != "chunk" {
		t.Fatalf("expected chunk frame, got %+v", frame)
	}
	chunk, err := message.UnmarshalChunk([]byte(frame.Data))
	if err != nil {
		t.Fatalf("failed to parse chunk frame %+v: %v", frame, err)
	}
	status, ok := chunk.(message.CompletionStatusChunk)
	if !ok {
		t.Fatalf("expected completion status chunk, got %T (%+v)", chunk, frame)
	}
	if status.Data.ThreadID != threadID || status.Data.IsRunning != isRunning {
		t.Fatalf("unexpected completion status payload: %+v", status.Data)
	}
	if completionID != "" && status.Data.CompletionID != completionID {
		t.Fatalf("unexpected completion status payload: %+v", status.Data)
	}
}

func yieldChunksAndBlock(chunks ...message.MessageChunk) func(context.Context, string, agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
	return func(ctx context.Context, _ string, _ agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
		return func(yield func(message.MessageChunk, error) bool) {
			for _, chunk := range chunks {
				if !yield(chunk, nil) {
					return
				}
			}
			<-ctx.Done()
		}
	}
}

func yieldChunksAndFinish(chunks ...message.MessageChunk) func(context.Context, string, agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
	return func(_ context.Context, _ string, _ agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
		return func(yield func(message.MessageChunk, error) bool) {
			for _, chunk := range chunks {
				if !yield(chunk, nil) {
					return
				}
			}
		}
	}
}

func readFrames(t *testing.T, body io.ReadCloser, count int, stopAtDone bool) []sseFrame {
	t.Helper()
	defer body.Close()

	scanner := bufio.NewScanner(body)
	frames := make([]sseFrame, 0, count)
	current := sseFrame{}
	hasCurrent := false

	appendCurrent := func() bool {
		if !hasCurrent {
			return false
		}
		frames = append(frames, current)
		current = sseFrame{}
		hasCurrent = false
		return true
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if appendCurrent() {
				if stopAtDone && frames[len(frames)-1].Done {
					return frames
				}
				if count > 0 && len(frames) >= count {
					return frames
				}
			}
			continue
		}
		hasCurrent = true
		switch {
		case strings.HasPrefix(line, "id: "):
			current.ID = strings.TrimPrefix(line, "id: ")
		case strings.HasPrefix(line, "event: "):
			current.Event = strings.TrimPrefix(line, "event: ")
			if current.Event == "done" {
				current.Done = true
			}
		case strings.HasPrefix(line, "data: "):
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				current.Done = true
			} else if current.Data == "" {
				current.Data = data
			} else {
				current.Data += "\n" + data
			}
		}
	}
	if hasCurrent {
		appendCurrent()
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("failed reading SSE frames: %v", err)
	}
	return frames
}

func newStreamTestServer(t *testing.T, h *Handler) *httptest.Server {
	t.Helper()
	r := chi.NewRouter()
	r.Get("/threads/{id}/chat/stream", h.ChatStream)
	return httptest.NewServer(r)
}

func newChatTestServer(t *testing.T, h *Handler) *httptest.Server {
	t.Helper()
	r := chi.NewRouter()
	r.Post("/threads/{id}/chat", h.PostChat)
	r.Get("/threads/{id}", h.GetThread)
	return httptest.NewServer(r)
}

func newFullHandlerTestServer(t *testing.T, h *Handler) *httptest.Server {
	t.Helper()
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	return httptest.NewServer(r)
}

func newAnswerTestServer(t *testing.T, h *Handler) *httptest.Server {
	t.Helper()
	r := chi.NewRouter()
	r.Post("/threads/{id}/chat/answer/{questionId}", h.PostAnswer)
	return httptest.NewServer(r)
}

func TestPostChat_RejectsEmptyMessages(t *testing.T) {
	ma := &streamTestAgent{}
	cm := agent.NewCompletionManager(ma)
	h := New("", cm, nil, nil, nil)
	ts := newChatTestServer(t, h)
	defer ts.Close()

	body, err := json.Marshal(api.ChatRequest{Messages: []message.UIMessage{}})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := ts.Client().Post(ts.URL+"/threads/thread-1/chat", "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestPostChat_AcceptsSingleUserMessage(t *testing.T) {
	reqCh := make(chan agent.PromptRequest, 1)
	ma := &streamTestAgent{
		promptFn: func(_ context.Context, _ string, req agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
			reqCh <- req
			return func(_ func(message.MessageChunk, error) bool) {}
		},
	}
	cm := agent.NewCompletionManager(ma)
	h := New("", cm, nil, nil, nil)
	ts := newChatTestServer(t, h)
	defer ts.Close()

	body, err := json.Marshal(api.ChatRequest{Messages: []message.UIMessage{{
		ID:    "msg-1",
		Role:  "user",
		Parts: []message.UIPart{message.UITextPart{Text: "hi", State: "done"}},
	}}})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := ts.Client().Post(ts.URL+"/threads/thread-1/chat", "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected status 202, got %d", resp.StatusCode)
	}

	select {
	case req := <-reqCh:
		if len(req.UserParts) != 1 {
			t.Fatalf("expected 1 user part, got %d", len(req.UserParts))
		}
		part, ok := req.UserParts[0].(message.UITextPart)
		if !ok {
			t.Fatalf("expected UITextPart, got %T", req.UserParts[0])
		}
		if part.Text != "hi" {
			t.Fatalf("expected text %q, got %q", "hi", part.Text)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Prompt request")
	}
}

func TestPostChat_StartsCompletion(t *testing.T) {
	reqCh := make(chan agent.PromptRequest, 1)
	ma := &streamTestAgent{
		promptFn: func(_ context.Context, _ string, req agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
			reqCh <- req
			return func(_ func(message.MessageChunk, error) bool) {}
		},
	}
	cm := agent.NewCompletionManager(ma)
	h := New("", cm, nil, nil, nil)
	ts := newChatTestServer(t, h)
	defer ts.Close()

	body, err := json.Marshal(api.ChatRequest{Messages: []message.UIMessage{{
		ID:    "msg-1",
		Role:  "user",
		Parts: []message.UIPart{message.UITextPart{Text: "Investigate thread metadata", State: "done"}},
	}}, Model: "openai/gpt-5.4", Reasoning: "high", Mode: "plan"})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := ts.Client().Post(ts.URL+"/threads/thread-1/chat", "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected status 202, got %d", resp.StatusCode)
	}

	select {
	case req := <-reqCh:
		if req.Model != "openai/gpt-5.4" {
			t.Fatalf("expected prompt model to be forwarded, got %+v", req)
		}
		if req.Reasoning != "high" {
			t.Fatalf("expected prompt reasoning to be forwarded, got %+v", req)
		}
		if req.Mode != "plan" {
			t.Fatalf("expected prompt mode to be forwarded, got %+v", req)
		}
		if len(req.UserParts) != 1 {
			t.Fatalf("expected 1 user part, got %d", len(req.UserParts))
		}
		part, ok := req.UserParts[0].(message.UITextPart)
		if !ok {
			t.Fatalf("expected UITextPart, got %T", req.UserParts[0])
		}
		if part.Text != "Investigate thread metadata" {
			t.Fatalf("expected forwarded text %q, got %q", "Investigate thread metadata", part.Text)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Prompt request")
	}
}

func TestPostChat_QueuesPromptWhileCompletionIsActive(t *testing.T) {
	store := thread.NewStore(t.TempDir())
	if err := store.CreateThread("thread-1"); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveConfig("thread-1", thread.Config{Name: "Thread 1", NameSource: thread.ThreadNameSourceUser}); err != nil {
		t.Fatal(err)
	}

	ma := &streamTestAgent{
		promptFn: yieldChunksAndBlock(message.StartChunk{MessageID: "assistant-1"}),
	}
	cm := agent.NewCompletionManager(ma)
	defaultAgent := agentimpl.NewDefaultAgent(store, nil, nil, t.TempDir(), agentimpl.MCPConfig{})
	h := New("", cm, nil, nil, defaultAgent)
	ts := newFullHandlerTestServer(t, h)
	defer ts.Close()

	if _, err := cm.Chat("thread-1", agent.PromptRequest{}); err != nil {
		t.Fatal(err)
	}
	defer cm.Cancel("thread-1")

	body, err := json.Marshal(api.ChatRequest{Messages: []message.UIMessage{{
		ID:    "msg-1",
		Role:  "user",
		Parts: []message.UIPart{message.UITextPart{Text: "queued follow-up", State: "done"}},
	}}})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := ts.Client().Post(ts.URL+"/threads/thread-1/chat", "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected status 202, got %d", resp.StatusCode)
	}

	var started api.ChatStartedResponse
	if err := json.NewDecoder(resp.Body).Decode(&started); err != nil {
		t.Fatal(err)
	}
	if started.Status != "queued" {
		t.Fatalf("expected queued status, got %#v", started)
	}
	if started.QueuedPromptID == "" {
		t.Fatalf("expected queuedPromptId, got %#v", started)
	}

	threadResp, err := ts.Client().Get(ts.URL + "/threads/thread-1")
	if err != nil {
		t.Fatal(err)
	}
	defer threadResp.Body.Close()
	if threadResp.StatusCode != http.StatusOK {
		t.Fatalf("expected get status 200, got %d", threadResp.StatusCode)
	}

	var got api.Thread
	if err := json.NewDecoder(threadResp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got.PromptQueue) != 1 {
		t.Fatalf("expected 1 queued prompt, got %#v", got.PromptQueue)
	}
	if got.PromptQueue[0].ID != started.QueuedPromptID {
		t.Fatalf("expected queued prompt id %q, got %#v", started.QueuedPromptID, got.PromptQueue[0])
	}
	part, ok := got.PromptQueue[0].Message.Parts[0].(message.UITextPart)
	if !ok || part.Text != "queued follow-up" {
		t.Fatalf("expected queued prompt text %q, got %#v", "queued follow-up", got.PromptQueue[0].Message.Parts)
	}
}

func TestOnTurnComplete_StartsNextQueuedPrompt(t *testing.T) {
	store := thread.NewStore(t.TempDir())
	if err := store.CreateThread("thread-1"); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveConfig("thread-1", thread.Config{Name: "Thread 1", NameSource: thread.ThreadNameSourceUser}); err != nil {
		t.Fatal(err)
	}

	reqCh := make(chan agent.PromptRequest, 2)
	releaseFirst := make(chan struct{})
	var callCount int
	var callCountMu sync.Mutex
	ma := &streamTestAgent{
		promptFn: func(ctx context.Context, _ string, req agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
			callCountMu.Lock()
			callCount++
			currentCall := callCount
			callCountMu.Unlock()
			reqCh <- req
			if currentCall == 1 {
				return func(yield func(message.MessageChunk, error) bool) {
					if !yield(message.StartChunk{MessageID: "assistant-1"}, nil) {
						return
					}
					select {
					case <-releaseFirst:
					case <-ctx.Done():
					}
				}
			}
			return yieldChunksAndFinish(message.StartChunk{MessageID: "assistant-2"})(ctx, "thread-1", req)
		},
	}
	cm := agent.NewCompletionManager(ma)
	defaultAgent := agentimpl.NewDefaultAgent(store, nil, nil, t.TempDir(), agentimpl.MCPConfig{})
	h := New("", cm, nil, nil, defaultAgent)
	ts := newFullHandlerTestServer(t, h)
	defer ts.Close()

	firstBody, err := json.Marshal(api.ChatRequest{Messages: []message.UIMessage{{
		ID:    "msg-1",
		Role:  "user",
		Parts: []message.UIPart{message.UITextPart{Text: "first", State: "done"}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	secondBody, err := json.Marshal(api.ChatRequest{Messages: []message.UIMessage{{
		ID:    "msg-2",
		Role:  "user",
		Parts: []message.UIPart{message.UITextPart{Text: "second", State: "done"}},
	}}})
	if err != nil {
		t.Fatal(err)
	}

	firstResp, err := ts.Client().Post(ts.URL+"/threads/thread-1/chat", "application/json", strings.NewReader(string(firstBody)))
	if err != nil {
		t.Fatal(err)
	}
	firstResp.Body.Close()

	select {
	case req := <-reqCh:
		part, ok := req.UserParts[0].(message.UITextPart)
		if !ok || part.Text != "first" {
			t.Fatalf("expected first prompt, got %#v", req.UserParts)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first prompt")
	}

	secondResp, err := ts.Client().Post(ts.URL+"/threads/thread-1/chat", "application/json", strings.NewReader(string(secondBody)))
	if err != nil {
		t.Fatal(err)
	}
	secondResp.Body.Close()

	close(releaseFirst)

	select {
	case req := <-reqCh:
		part, ok := req.UserParts[0].(message.UITextPart)
		if !ok || part.Text != "second" {
			t.Fatalf("expected second queued prompt, got %#v", req.UserParts)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for queued prompt to start")
	}
}

func TestPostChat_ReturnsPendingQuestionConflict(t *testing.T) {
	ma := &streamTestAgent{
		pendingQuestionFn: func(threadID string) (*agent.PendingQuestion, error) {
			if threadID != "thread-1" {
				t.Fatalf("expected thread-1, got %s", threadID)
			}
			return &agent.PendingQuestion{ApprovalID: "approval-123"}, nil
		},
	}
	cm := agent.NewCompletionManager(ma)
	h := New("", cm, nil, nil, nil)
	ts := newChatTestServer(t, h)
	defer ts.Close()

	body, err := json.Marshal(api.ChatRequest{Messages: []message.UIMessage{{
		ID:    "msg-1",
		Role:  "user",
		Parts: []message.UIPart{message.UITextPart{Text: "hi", State: "done"}},
	}}})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := ts.Client().Post(ts.URL+"/threads/thread-1/chat", "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected status 409, got %d", resp.StatusCode)
	}

	var got api.ChatTurnStateConflictResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Error != "pending_question_requires_answer" {
		t.Fatalf("expected pending question error, got %#v", got)
	}
	if got.QuestionID != "approval-123" {
		t.Fatalf("expected questionId approval-123, got %#v", got)
	}
}

func TestPostChat_ReturnsInterruptedTurnConflict(t *testing.T) {
	resumeCh := make(chan string, 1)
	ma := &streamTestAgent{
		hasInterruptedTurnFn: func(threadID string) (bool, error) {
			if threadID != "thread-1" {
				t.Fatalf("expected thread-1, got %s", threadID)
			}
			return true, nil
		},
		resumeFn: func(_ context.Context, threadID string) iter.Seq2[message.MessageChunk, error] {
			resumeCh <- threadID
			return func(_ func(message.MessageChunk, error) bool) {}
		},
	}
	cm := agent.NewCompletionManager(ma)
	h := New("", cm, nil, nil, nil)
	ts := newChatTestServer(t, h)
	defer ts.Close()

	body, err := json.Marshal(api.ChatRequest{Messages: []message.UIMessage{{
		ID:    "msg-1",
		Role:  "user",
		Parts: []message.UIPart{message.UITextPart{Text: "hi", State: "done"}},
	}}})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := ts.Client().Post(ts.URL+"/threads/thread-1/chat", "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected status 409, got %d", resp.StatusCode)
	}

	var got api.ChatTurnStateConflictResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Error != "interrupted_turn_requires_resume" {
		t.Fatalf("expected interrupted turn error, got %#v", got)
	}
	if got.QuestionID != "" {
		t.Fatalf("expected empty questionId, got %#v", got)
	}
	if got.CompletionID == "" {
		t.Fatalf("expected completionId, got %#v", got)
	}
	select {
	case threadID := <-resumeCh:
		if threadID != "thread-1" {
			t.Fatalf("expected resume for thread-1, got %q", threadID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for resume call")
	}
}

func TestRegisterRoutes_GetThreadMatchesListThreads(t *testing.T) {
	store := thread.NewStore(t.TempDir())
	ma := &streamTestAgent{listThreadsFn: store.ListThreads}
	cm := agent.NewCompletionManager(ma)
	defaultAgent := agentimpl.NewDefaultAgent(store, nil, nil, t.TempDir(), agentimpl.MCPConfig{})
	h := New("", cm, nil, nil, defaultAgent)
	ts := newFullHandlerTestServer(t, h)
	defer ts.Close()

	if err := store.CreateThread("thread-1"); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveConfig("thread-1", thread.Config{Name: "Thread 1", NameSource: thread.ThreadNameSourceUser}); err != nil {
		t.Fatal(err)
	}

	listResp, err := ts.Client().Get(ts.URL + "/threads")
	if err != nil {
		t.Fatal(err)
	}
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("expected list status 200, got %d", listResp.StatusCode)
	}

	var listed api.ListThreadsResponse
	if err := json.NewDecoder(listResp.Body).Decode(&listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Threads) != 1 {
		t.Fatalf("expected 1 listed thread, got %d", len(listed.Threads))
	}
	if listed.Threads[0].Mode != "build" {
		t.Fatalf("expected listed thread mode %q, got %q", "build", listed.Threads[0].Mode)
	}

	threadResp, err := ts.Client().Get(ts.URL + "/threads/thread-1")
	if err != nil {
		t.Fatal(err)
	}
	defer threadResp.Body.Close()
	if threadResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(threadResp.Body)
		t.Fatalf("expected get status 200, got %d: %s", threadResp.StatusCode, string(body))
	}

	var got api.Thread
	if err := json.NewDecoder(threadResp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, listed.Threads[0]) {
		t.Fatalf("expected get thread %+v to match listed thread %+v", got, listed.Threads[0])
	}
}

func TestRegisterRoutes_ThreadModeIncludesPlanAndBuild(t *testing.T) {
	store := thread.NewStore(t.TempDir())
	ma := &streamTestAgent{listThreadsFn: store.ListThreads}
	cm := agent.NewCompletionManager(ma)
	defaultAgent := agentimpl.NewDefaultAgent(store, nil, nil, t.TempDir(), agentimpl.MCPConfig{})
	h := New("", cm, nil, nil, defaultAgent)
	ts := newFullHandlerTestServer(t, h)
	defer ts.Close()

	if err := store.CreateThread("thread-build"); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveConfig("thread-build", thread.Config{Name: "Build Thread", NameSource: thread.ThreadNameSourceUser, Mode: thread.ModeState{Value: "build"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateThread("thread-plan"); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveConfig("thread-plan", thread.Config{Name: "Plan Thread", NameSource: thread.ThreadNameSourceUser, Mode: thread.ModeState{Value: "plan"}}); err != nil {
		t.Fatal(err)
	}

	listResp, err := ts.Client().Get(ts.URL + "/threads")
	if err != nil {
		t.Fatal(err)
	}
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("expected list status 200, got %d", listResp.StatusCode)
	}

	var listed api.ListThreadsResponse
	if err := json.NewDecoder(listResp.Body).Decode(&listed); err != nil {
		t.Fatal(err)
	}

	modes := map[string]string{}
	for _, item := range listed.Threads {
		modes[item.ID] = item.Mode
	}
	if modes["thread-build"] != "build" {
		t.Fatalf("expected build thread mode %q, got %q", "build", modes["thread-build"])
	}
	if modes["thread-plan"] != "plan" {
		t.Fatalf("expected plan thread mode %q, got %q", "plan", modes["thread-plan"])
	}
}

func TestRegisterRoutes_ThreadIncludesLastUserPrompt(t *testing.T) {
	store := thread.NewStore(t.TempDir())
	ma := &streamTestAgent{listThreadsFn: store.ListThreads}
	cm := agent.NewCompletionManager(ma)
	defaultAgent := agentimpl.NewDefaultAgent(store, nil, nil, t.TempDir(), agentimpl.MCPConfig{})
	h := New("", cm, nil, nil, defaultAgent)
	ts := newFullHandlerTestServer(t, h)
	defer ts.Close()

	if err := store.CreateThread("thread-1"); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveConfig("thread-1", thread.Config{
		Name:        "Thread 1",
		NameSource:  thread.ThreadNameSourceUser,
		LastMessage: "latest prompt",
	}); err != nil {
		t.Fatal(err)
	}

	listResp, err := ts.Client().Get(ts.URL + "/threads")
	if err != nil {
		t.Fatal(err)
	}
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("expected list status 200, got %d", listResp.StatusCode)
	}

	var listed api.ListThreadsResponse
	if err := json.NewDecoder(listResp.Body).Decode(&listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Threads) != 1 {
		t.Fatalf("expected 1 listed thread, got %d", len(listed.Threads))
	}
	if listed.Threads[0].LastMessage != "latest prompt" {
		t.Fatalf("expected listed lastMessage %q, got %+v", "latest prompt", listed.Threads[0])
	}

	threadResp, err := ts.Client().Get(ts.URL + "/threads/thread-1")
	if err != nil {
		t.Fatal(err)
	}
	defer threadResp.Body.Close()
	if threadResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(threadResp.Body)
		t.Fatalf("expected get status 200, got %d: %s", threadResp.StatusCode, string(body))
	}

	var got api.Thread
	if err := json.NewDecoder(threadResp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.LastMessage != "latest prompt" {
		t.Fatalf("expected get lastMessage %q, got %+v", "latest prompt", got)
	}
}

func TestRegisterRoutes_ThreadIncludesCancelledState(t *testing.T) {
	store := thread.NewStore(t.TempDir())
	ma := &streamTestAgent{listThreadsFn: store.ListThreads}
	cm := agent.NewCompletionManager(ma)
	defaultAgent := agentimpl.NewDefaultAgent(store, nil, nil, t.TempDir(), agentimpl.MCPConfig{})
	h := New("", cm, nil, nil, defaultAgent)
	ts := newFullHandlerTestServer(t, h)
	defer ts.Close()

	if err := store.CreateThread("thread-1"); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveConfig("thread-1", thread.Config{
		Name:          "Thread 1",
		NameSource:    thread.ThreadNameSourceUser,
		LastTurnState: thread.StateCancelled,
	}); err != nil {
		t.Fatal(err)
	}

	resp, err := ts.Client().Get(ts.URL + "/threads/thread-1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected get status 200, got %d", resp.StatusCode)
	}

	var got api.Thread
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.State != "cancelled" {
		t.Fatalf("expected cancelled state, got %+v", got)
	}
}

func TestRegisterRoutes_ThreadIncludesInterruptedState(t *testing.T) {
	store := thread.NewStore(t.TempDir())
	ma := &streamTestAgent{
		listThreadsFn:        store.ListThreads,
		hasInterruptedTurnFn: func(threadID string) (bool, error) { return threadID == "thread-1", nil },
	}
	cm := agent.NewCompletionManager(ma)
	defaultAgent := agentimpl.NewDefaultAgent(store, nil, nil, t.TempDir(), agentimpl.MCPConfig{})
	h := New("", cm, nil, nil, defaultAgent)
	ts := newFullHandlerTestServer(t, h)
	defer ts.Close()

	if err := store.CreateThread("thread-1"); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveConfig("thread-1", thread.Config{Name: "Thread 1", NameSource: thread.ThreadNameSourceUser}); err != nil {
		t.Fatal(err)
	}

	resp, err := ts.Client().Get(ts.URL + "/threads/thread-1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected get status 200, got %d", resp.StatusCode)
	}

	var got api.Thread
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.State != "interrupted" {
		t.Fatalf("expected interrupted state, got %+v", got)
	}
}

func TestRegisterRoutes_ActiveCompletionDoesNotMarkThreadInterrupted(t *testing.T) {
	store := thread.NewStore(t.TempDir())
	ma := &streamTestAgent{
		listThreadsFn:        store.ListThreads,
		hasInterruptedTurnFn: func(threadID string) (bool, error) { return threadID == "thread-1", nil },
		promptFn:             yieldChunksAndBlock(message.StartChunk{MessageID: "assistant-1"}),
	}
	cm := agent.NewCompletionManager(ma)
	defaultAgent := agentimpl.NewDefaultAgent(store, nil, nil, t.TempDir(), agentimpl.MCPConfig{})
	h := New("", cm, nil, nil, defaultAgent)
	ts := newFullHandlerTestServer(t, h)
	defer ts.Close()

	if err := store.CreateThread("thread-1"); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveConfig("thread-1", thread.Config{Name: "Thread 1", NameSource: thread.ThreadNameSourceUser}); err != nil {
		t.Fatal(err)
	}

	completionID, err := cm.Chat("thread-1", agent.PromptRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if completionID == "" {
		t.Fatal("expected active completion id")
	}
	defer cm.Cancel("thread-1")

	resp, err := ts.Client().Get(ts.URL + "/threads/thread-1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected get status 200, got %d", resp.StatusCode)
	}

	var got api.Thread
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.State != "" {
		t.Fatalf("expected empty state while active completion is running, got %+v", got)
	}
}

func TestPostChat_AcceptsMultipleUserMessages(t *testing.T) {
	reqCh := make(chan agent.PromptRequest, 1)
	ma := &streamTestAgent{
		promptFn: func(_ context.Context, _ string, req agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
			reqCh <- req
			return func(_ func(message.MessageChunk, error) bool) {}
		},
	}
	cm := agent.NewCompletionManager(ma)
	h := New("", cm, nil, nil, nil)
	ts := newChatTestServer(t, h)
	defer ts.Close()

	// Multiple user messages with no assistant: the last user message's parts
	// are used as the prompt and no leaf validation is required.
	body, err := json.Marshal(api.ChatRequest{Messages: []message.UIMessage{
		{ID: "msg-1", Role: "user", Parts: []message.UIPart{message.UITextPart{Text: "one", State: "done"}}},
		{ID: "msg-2", Role: "user", Parts: []message.UIPart{message.UITextPart{Text: "two", State: "done"}}},
	}})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := ts.Client().Post(ts.URL+"/threads/thread-1/chat", "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected status 202, got %d", resp.StatusCode)
	}

	select {
	case req := <-reqCh:
		part, ok := req.UserParts[0].(message.UITextPart)
		if !ok || part.Text != "two" {
			t.Fatalf("expected last user message text %q, got %q", "two", part.Text)
		}
		if req.LeafID != "" {
			t.Fatalf("expected empty LeafID, got %q", req.LeafID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Prompt request")
	}
}

func TestPostChat_RejectsNoUserMessageAfterAssistant(t *testing.T) {
	ma := &streamTestAgent{}
	cm := agent.NewCompletionManager(ma)
	h := New("", cm, nil, nil, nil)
	ts := newChatTestServer(t, h)
	defer ts.Close()

	// A conversation that ends with an assistant message and no follow-up user
	// message is invalid — there is nothing to prompt the agent with.
	body, err := json.Marshal(api.ChatRequest{Messages: []message.UIMessage{{
		ID:    "msg-1",
		Role:  "assistant",
		Parts: []message.UIPart{message.UITextPart{Text: "hi", State: "done"}},
	}}})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := ts.Client().Post(ts.URL+"/threads/thread-1/chat", "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.StatusCode)
	}

	var got api.ErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Error != "no user message found after the last assistant message" {
		t.Fatalf("unexpected error %q", got.Error)
	}
}

func TestPostAnswer_UsesResumeWithoutCachedCompletion(t *testing.T) {
	resumeCh := make(chan string, 1)
	ma := &streamTestAgent{
		submitAnswerFn: func(threadID, toolCallID string, req api.AnswerQuestionRequest) error {
			if threadID != "thread-1" {
				t.Fatalf("expected thread-1, got %q", threadID)
			}
			if toolCallID != "question-1" {
				t.Fatalf("expected question-1, got %q", toolCallID)
			}
			if req.Answers["q1"] != "yes" {
				t.Fatalf("expected answer yes, got %+v", req.Answers)
			}
			return nil
		},
		resumeFn: func(_ context.Context, threadID string) iter.Seq2[message.MessageChunk, error] {
			resumeCh <- threadID
			return func(_ func(message.MessageChunk, error) bool) {}
		},
	}
	cm := agent.NewCompletionManager(ma)
	h := New("", cm, nil, nil, nil)
	ts := newAnswerTestServer(t, h)
	defer ts.Close()

	body, err := json.Marshal(api.AnswerQuestionRequest{Answers: map[string]string{"q1": "yes"}})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := ts.Client().Post(ts.URL+"/threads/thread-1/chat/answer/question-1", "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	select {
	case threadID := <-resumeCh:
		if threadID != "thread-1" {
			t.Fatalf("expected resume for thread-1, got %q", threadID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Resume call")
	}
}

func TestPostAnswer_UsesResumeWhenOnlyDoneCachedCompletionExists(t *testing.T) {
	promptCh := make(chan agent.PromptRequest, 1)
	resumeCh := make(chan string, 2)
	ma := &streamTestAgent{
		submitAnswerFn: func(_ string, _ string, _ api.AnswerQuestionRequest) error {
			return nil
		},
		promptFn: func(_ context.Context, _ string, req agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
			promptCh <- req
			return func(_ func(message.MessageChunk, error) bool) {}
		},
		resumeFn: func(_ context.Context, threadID string) iter.Seq2[message.MessageChunk, error] {
			resumeCh <- threadID
			return func(_ func(message.MessageChunk, error) bool) {}
		},
	}
	cm := agent.NewCompletionManager(ma)
	if _, err := cm.Chat("thread-1", agent.PromptRequest{
		UserParts: []message.UIPart{message.UITextPart{Text: "seed"}},
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-promptCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for seed Prompt request")
	}

	h := New("", cm, nil, nil, nil)
	ts := newAnswerTestServer(t, h)
	defer ts.Close()

	body, err := json.Marshal(api.AnswerQuestionRequest{Answers: map[string]string{"q1": "yes"}})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := ts.Client().Post(ts.URL+"/threads/thread-1/chat/answer/question-1", "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	select {
	case threadID := <-resumeCh:
		if threadID != "thread-1" {
			t.Fatalf("expected resume for thread-1, got %q", threadID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Resume call")
	}
}

func TestChatStream_PendingQuestionConnectionContinuesAfterAnswer(t *testing.T) {
	historyMsg := message.UIMessage{
		ID:   "hist-approval-1",
		Role: "assistant",
		Parts: []message.UIPart{
			message.DynamicToolPart{
				ToolName:   "AskUserQuestion",
				ToolCallID: "tool-approval-1",
				State:      "approval-requested",
				Approval:   &message.ToolApproval{ID: "approval-1"},
			},
			message.UITextPart{Text: "Waiting for your answer.", State: "done"},
		},
	}
	historyMsgJSON, err := json.Marshal(historyMsg)
	if err != nil {
		t.Fatal(err)
	}

	answerSubmitted := make(chan struct{}, 1)
	ma := &streamTestAgent{
		messagesFn: func(threadID, leafID string) ([]message.UIMessage, error) {
			if threadID != "thread-approval" {
				t.Fatalf("expected thread-approval, got %q", threadID)
			}
			if leafID != "" {
				t.Fatalf("expected empty leafID, got %q", leafID)
			}
			return []message.UIMessage{historyMsg}, nil
		},
		submitAnswerFn: func(threadID, approvalID string, req api.AnswerQuestionRequest) error {
			if threadID != "thread-approval" {
				t.Fatalf("expected thread-approval, got %q", threadID)
			}
			if approvalID != "approval-1" {
				t.Fatalf("expected approval-1, got %q", approvalID)
			}
			if req.Answers["Scope"] != "Proceed" {
				t.Fatalf("expected answer Proceed, got %#v", req.Answers)
			}
			answerSubmitted <- struct{}{}
			return nil
		},
		resumeFn: func(_ context.Context, threadID string) iter.Seq2[message.MessageChunk, error] {
			if threadID != "thread-approval" {
				t.Fatalf("expected thread-approval, got %q", threadID)
			}
			return func(yield func(message.MessageChunk, error) bool) {
				select {
				case <-answerSubmitted:
				case <-time.After(2 * time.Second):
					t.Fatal("timed out waiting for submitted answer")
				}
				if !yield(message.ThreadResumeChunk{
					Data: message.ThreadResumeData{
						ThreadID:  "thread-approval",
						MessageID: "assistant-approval",
					},
				}, nil) {
					return
				}
				if !yield(message.ToolApprovalResponseDataChunk{
					Data: message.ToolApprovalResponseData{
						ApprovalID: "approval-1",
						ToolCallID: "tool-approval-1",
						Approved:   true,
					},
				}, nil) {
					return
				}
				yield(message.TextDeltaChunk{ID: "delta-approval", Delta: "resumed"}, nil) //nolint:errcheck
			}
		},
	}
	cm := agent.NewCompletionManager(ma)
	h := New("", cm, nil, nil, nil)
	h.chatPingEvery = time.Second
	ts := newFullHandlerTestServer(t, h)
	defer ts.Close()

	answerErrCh := make(chan error, 1)
	time.AfterFunc(20*time.Millisecond, func() {
		body, err := json.Marshal(api.AnswerQuestionRequest{
			Answers: map[string]string{"Scope": "Proceed"},
		})
		if err != nil {
			answerErrCh <- err
			return
		}
		resp, err := ts.Client().Post(
			ts.URL+"/threads/thread-approval/chat/answer/approval-1",
			"application/json",
			strings.NewReader(string(body)),
		)
		if err != nil {
			answerErrCh <- err
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			answerErrCh <- fmt.Errorf("expected answer status 200, got %d", resp.StatusCode)
			return
		}
		answerErrCh <- nil
	})

	resp, err := ts.Client().Get(ts.URL + "/threads/thread-approval/chat/stream")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	frames := readFrames(t, resp.Body, 6, false)
	if len(frames) != 6 {
		t.Fatalf("expected 6 frames, got %d", len(frames))
	}
	if frames[0].Event != "history-start" {
		t.Fatalf("expected history-start, got %+v", frames[0])
	}
	if frames[1].Event != "history-message" || frames[1].Data != string(historyMsgJSON) {
		t.Fatalf("unexpected history message frame: %+v", frames[1])
	}
	if frames[2].Event != "history-end" {
		t.Fatalf("expected history-end, got %+v", frames[2])
	}
	if frames[3].Event != "chunk" {
		t.Fatalf("expected first resumed chunk, got %+v", frames[3])
	}
	if frames[4].Event != "chunk" {
		t.Fatalf("expected approval response chunk, got %+v", frames[4])
	}
	if frames[5].Event != "chunk" {
		t.Fatalf("expected resumed text chunk, got %+v", frames[5])
	}

	select {
	case err := <-answerErrCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for answer request")
	}
}

func TestChatStream_FreshRequest_ReplaysHistoryThenCachedDeltas(t *testing.T) {
	historyMsg := message.UIMessage{
		ID:    "hist-1",
		Role:  "user",
		Parts: []message.UIPart{message.UITextPart{Text: "old"}},
	}
	historyMsgJSON, err := json.Marshal(historyMsg)
	if err != nil {
		t.Fatal(err)
	}
	liveChunk := message.TextDeltaChunk{ID: "delta-1", Delta: "live"}
	liveChunkJSON, err := message.MarshalChunk(liveChunk)
	if err != nil {
		t.Fatal(err)
	}

	ma := &streamTestAgent{
		promptFn: yieldChunksAndBlock(liveChunk),
		messagesFn: func(_, _ string) ([]message.UIMessage, error) {
			return []message.UIMessage{historyMsg}, nil
		},
	}
	cm := agent.NewCompletionManager(ma)
	completionID, err := cm.Chat("thread-1", agent.PromptRequest{
		LeafID:    "leaf-before",
		UserParts: []message.UIPart{message.UITextPart{Text: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(20 * time.Millisecond)

	h := New("", cm, nil, nil, nil)
	ts := newStreamTestServer(t, h)
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/threads/thread-1/chat/stream")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	frames := readFrames(t, resp.Body, 5, false)
	if len(frames) != 5 {
		t.Fatalf("expected 5 frames, got %d", len(frames))
	}
	if frames[0].Event != "history-start" {
		t.Fatalf("expected first frame history-start, got %+v", frames[0])
	}
	if frames[1].Event != "history-message" || frames[1].Data != string(historyMsgJSON) {
		t.Fatalf("unexpected history message frame: %+v", frames[1])
	}
	if frames[2].Event != "history-end" {
		t.Fatalf("expected history-end frame, got %+v", frames[2])
	}
	assertCompletionStatusFrame(t, frames[3], "thread-1", completionID, true)
	if frames[4].Event != "chunk" || frames[4].ID != completionID+":0" || frames[4].Data != string(liveChunkJSON) {
		t.Fatalf("expected cached delta after history-end, got %+v", frames[4])
	}
}

func TestChatStream_FreshRequest_DoesNotReplayCompletedSnapshot(t *testing.T) {
	historyMsg := message.UIMessage{
		ID:    "hist-done-1",
		Role:  "user",
		Parts: []message.UIPart{message.UITextPart{Text: "prompt"}},
	}
	historyMsgJSON, err := json.Marshal(historyMsg)
	if err != nil {
		t.Fatal(err)
	}
	cachedChunk := message.TextDeltaChunk{ID: "delta-done-1", Delta: "completed"}

	ma := &streamTestAgent{
		promptFn: yieldChunksAndFinish(cachedChunk),
		messagesFn: func(_, _ string) ([]message.UIMessage, error) {
			return []message.UIMessage{historyMsg}, nil
		},
	}
	cm := agent.NewCompletionManager(ma)
	if _, err := cm.Chat("thread-done", agent.PromptRequest{
		UserParts: []message.UIPart{message.UITextPart{Text: "hi"}},
	}); err != nil {
		t.Fatal(err)
	}

	time.Sleep(20 * time.Millisecond)

	h := New("", cm, nil, nil, nil)
	h.chatPingEvery = 10 * time.Millisecond
	ts := newStreamTestServer(t, h)
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/threads/thread-done/chat/stream")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	frames := readFrames(t, resp.Body, 4, false)
	if len(frames) != 4 {
		t.Fatalf("expected 4 frames, got %d", len(frames))
	}
	if frames[0].Event != "history-start" {
		t.Fatalf("expected history-start, got %+v", frames[0])
	}
	if frames[1].Event != "history-message" || frames[1].Data != string(historyMsgJSON) {
		t.Fatalf("unexpected history message frame: %+v", frames[1])
	}
	if frames[2].Event != "history-end" {
		t.Fatalf("expected history-end after history replay, got %+v", frames[2])
	}
	if frames[3].Event != "ping" {
		t.Fatalf("expected ping after replay completes, got %+v", frames[3])
	}
	for _, frame := range frames {
		if frame.Event == "chunk" {
			t.Fatalf("did not expect completed snapshot chunk replay: %+v", frame)
		}
	}
}

func TestChatStream_FreshRequest_SkipsCachedSnapshotForPendingQuestion(t *testing.T) {
	historyMsg := message.UIMessage{
		ID:   "hist-pending-1",
		Role: "assistant",
		Parts: []message.UIPart{
			message.DynamicToolPart{
				ToolName:   "AskUserQuestion",
				ToolCallID: "tool-pending-1",
				State:      "approval-requested",
				Approval:   &message.ToolApproval{ID: "approval-pending-1"},
			},
			message.UITextPart{Text: "Waiting for your answer.", State: "done"},
		},
	}
	historyMsgJSON, err := json.Marshal(historyMsg)
	if err != nil {
		t.Fatal(err)
	}

	ma := &streamTestAgent{
		promptFn: yieldChunksAndFinish(
			message.TextDeltaChunk{ID: "delta-pending-1", Delta: "duplicate"},
		),
		messagesFn: func(_, _ string) ([]message.UIMessage, error) {
			return []message.UIMessage{historyMsg}, nil
		},
		pendingQuestionFn: func(threadID string) (*agent.PendingQuestion, error) {
			if threadID != "thread-pending" {
				t.Fatalf("expected thread-pending, got %q", threadID)
			}
			return &agent.PendingQuestion{
				ApprovalID: "approval-pending-1",
				Questions: []api.AskUserQuestion{{
					Header:   "Scope",
					Question: "Proceed?",
				}},
			}, nil
		},
	}
	cm := agent.NewCompletionManager(ma)
	if _, err := cm.Chat("thread-pending", agent.PromptRequest{
		UserParts: []message.UIPart{message.UITextPart{Text: "hi"}},
	}); err != nil {
		t.Fatal(err)
	}

	time.Sleep(20 * time.Millisecond)

	h := New("", cm, nil, nil, nil)
	h.chatPingEvery = 10 * time.Millisecond
	ts := newStreamTestServer(t, h)
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/threads/thread-pending/chat/stream")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	frames := readFrames(t, resp.Body, 4, false)
	if len(frames) != 4 {
		t.Fatalf("expected 4 frames, got %d", len(frames))
	}
	if frames[0].Event != "history-start" {
		t.Fatalf("expected history-start, got %+v", frames[0])
	}
	if frames[1].Event != "history-message" || frames[1].Data != string(historyMsgJSON) {
		t.Fatalf("unexpected history message frame: %+v", frames[1])
	}
	if frames[2].Event != "history-end" {
		t.Fatalf("expected history-end, got %+v", frames[2])
	}
	if frames[3].Event != "ping" {
		t.Fatalf("expected ping after replay completes, got %+v", frames[3])
	}
	for _, frame := range frames {
		if frame.Event == "chunk" {
			t.Fatalf("did not expect cached chunk replay for pending question: %+v", frame)
		}
	}
}

func TestChatStream_FreshRequest_WithPendingQuestionAndNoSnapshot_ReplaysHistoryOnly(t *testing.T) {
	historyMsg := message.UIMessage{
		ID:   "hist-pending-nosnapshot-1",
		Role: "assistant",
		Parts: []message.UIPart{
			message.DynamicToolPart{
				ToolName:   "AskUserQuestion",
				ToolCallID: "tool-pending-nosnapshot-1",
				State:      "approval-requested",
				Approval:   &message.ToolApproval{ID: "approval-pending-nosnapshot-1"},
			},
			message.UITextPart{Text: "Waiting for your answer.", State: "done"},
		},
	}
	historyMsgJSON, err := json.Marshal(historyMsg)
	if err != nil {
		t.Fatal(err)
	}

	ma := &streamTestAgent{
		messagesFn: func(_, _ string) ([]message.UIMessage, error) {
			return []message.UIMessage{historyMsg}, nil
		},
		pendingQuestionFn: func(threadID string) (*agent.PendingQuestion, error) {
			if threadID != "thread-pending-nosnapshot" {
				t.Fatalf("expected thread-pending-nosnapshot, got %q", threadID)
			}
			return &agent.PendingQuestion{
				ApprovalID: "approval-pending-nosnapshot-1",
				Questions: []api.AskUserQuestion{{
					Header:   "Scope",
					Question: "Proceed?",
				}},
			}, nil
		},
	}
	cm := agent.NewCompletionManager(ma)
	h := New("", cm, nil, nil, nil)
	h.chatPingEvery = 10 * time.Millisecond
	ts := newStreamTestServer(t, h)
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/threads/thread-pending-nosnapshot/chat/stream")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	frames := readFrames(t, resp.Body, 4, false)
	if len(frames) != 4 {
		t.Fatalf("expected 4 frames, got %d", len(frames))
	}
	if frames[0].Event != "history-start" {
		t.Fatalf("expected history-start, got %+v", frames[0])
	}
	if frames[1].Event != "history-message" || frames[1].Data != string(historyMsgJSON) {
		t.Fatalf("unexpected history message frame: %+v", frames[1])
	}
	if frames[2].Event != "history-end" {
		t.Fatalf("expected history-end, got %+v", frames[2])
	}
	if frames[3].Event != "ping" {
		t.Fatalf("expected ping after replay completes, got %+v", frames[3])
	}
	for _, frame := range frames {
		if frame.Event == "chunk" {
			t.Fatalf("did not expect chunk replay without an active snapshot: %+v", frame)
		}
	}
}

func TestChatStream_FreshRequest_StartsInterruptedTurnRecovery(t *testing.T) {
	resumeCh := make(chan string, 1)
	release := make(chan struct{})
	liveChunk := message.TextDeltaChunk{ID: "delta-recover-1", Delta: "resumed"}
	liveChunkJSON, err := message.MarshalChunk(liveChunk)
	if err != nil {
		t.Fatal(err)
	}

	ma := &streamTestAgent{
		resumeFn: func(ctx context.Context, threadID string) iter.Seq2[message.MessageChunk, error] {
			resumeCh <- threadID
			return func(yield func(message.MessageChunk, error) bool) {
				<-release
				if !yield(liveChunk, nil) {
					return
				}
				<-ctx.Done()
			}
		},
		messagesFn: func(_, _ string) ([]message.UIMessage, error) {
			return nil, nil
		},
		hasInterruptedTurnFn: func(threadID string) (bool, error) {
			return threadID == "thread-recover", nil
		},
	}
	cm := agent.NewCompletionManager(ma)
	h := New("", cm, nil, nil, nil)
	h.chatPingEvery = time.Second
	ts := newStreamTestServer(t, h)
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/threads/thread-recover/chat/stream")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	select {
	case threadID := <-resumeCh:
		if threadID != "thread-recover" {
			t.Fatalf("expected resume for thread-recover, got %q", threadID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for resume call")
	}

	close(release)

	frames := readFrames(t, resp.Body, 4, false)
	if len(frames) != 4 {
		t.Fatalf("expected 4 frames, got %d", len(frames))
	}
	if frames[0].Event != "history-start" {
		t.Fatalf("expected history-start frame, got %+v", frames[0])
	}
	if frames[1].Event != "history-end" {
		t.Fatalf("expected history-end frame, got %+v", frames[1])
	}
	assertCompletionStatusFrame(t, frames[2], "thread-recover", "", true)
	if frames[3].Event != "chunk" || frames[3].Data != string(liveChunkJSON) {
		t.Fatalf("unexpected recovery chunk frame: %+v", frames[3])
	}
}

func TestChatStream_ValidLastEventID_ResumesWithoutHistory(t *testing.T) {
	chunk1 := message.TextDeltaChunk{ID: "delta-1", Delta: "one"}
	chunk2 := message.TextDeltaChunk{ID: "delta-1", Delta: "two"}
	chunk2JSON, err := message.MarshalChunk(chunk2)
	if err != nil {
		t.Fatal(err)
	}

	ma := &streamTestAgent{promptFn: yieldChunksAndBlock(chunk1, chunk2)}
	cm := agent.NewCompletionManager(ma)
	completionID, err := cm.Chat("thread-2", agent.PromptRequest{
		UserParts: []message.UIPart{message.UITextPart{Text: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(20 * time.Millisecond)

	h := New("", cm, nil, nil, nil)
	ts := newStreamTestServer(t, h)
	defer ts.Close()

	req, err := http.NewRequest("GET", ts.URL+"/threads/thread-2/chat/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Last-Event-ID", completionID+":0")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	frames := readFrames(t, resp.Body, 1, false)
	if len(frames) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(frames))
	}
	if frames[0].Event == "history-start" || frames[0].Event == "history-message" {
		t.Fatalf("did not expect history replay frame for valid resume: %+v", frames[0])
	}
	if frames[0].Event != "chunk" || frames[0].ID != completionID+":1" || frames[0].Data != string(chunk2JSON) {
		t.Fatalf("unexpected resume frame: %+v", frames[0])
	}
}

func TestChatStream_FreshRequest_CoalescesCachedDeltaBatch(t *testing.T) {
	chunk1 := message.TextDeltaChunk{ID: "delta-coalesce-1", Delta: "one"}
	chunk2 := message.TextDeltaChunk{ID: "delta-coalesce-1", Delta: "two"}
	coalescedJSON, err := message.MarshalChunk(message.TextDeltaChunk{ID: "delta-coalesce-1", Delta: "onetwo"})
	if err != nil {
		t.Fatal(err)
	}

	ma := &streamTestAgent{promptFn: yieldChunksAndBlock(chunk1, chunk2)}
	cm := agent.NewCompletionManager(ma)
	completionID, err := cm.Chat("thread-coalesce", agent.PromptRequest{
		UserParts: []message.UIPart{message.UITextPart{Text: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(20 * time.Millisecond)

	h := New("", cm, nil, nil, nil)
	ts := newStreamTestServer(t, h)
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/threads/thread-coalesce/chat/stream")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	frames := readFrames(t, resp.Body, 4, false)
	if len(frames) != 4 {
		t.Fatalf("expected 4 frames, got %d", len(frames))
	}
	if frames[0].Event != "history-start" {
		t.Fatalf("expected history-start, got %+v", frames[0])
	}
	if frames[1].Event != "history-end" {
		t.Fatalf("expected history-end, got %+v", frames[1])
	}
	assertCompletionStatusFrame(t, frames[2], "thread-coalesce", completionID, true)
	if frames[3].Event != "chunk" || frames[3].ID != completionID+":1" || frames[3].Data != string(coalescedJSON) {
		t.Fatalf("unexpected coalesced chunk frame: %+v", frames[3])
	}
}

func TestChatStream_ForwardsThreadUpdateChunk(t *testing.T) {
	threadUpdateChunk := message.ThreadUpdateChunk{
		Data: message.ThreadUpdateData{Thread: message.ThreadUpdateInfo{
			ID:   "thread-name",
			Name: "Fix thread naming",
			Mode: "build",
		}},
	}
	threadUpdateChunkJSON, err := message.MarshalChunk(threadUpdateChunk)
	if err != nil {
		t.Fatal(err)
	}

	ma := &streamTestAgent{promptFn: yieldChunksAndBlock(threadUpdateChunk)}
	cm := agent.NewCompletionManager(ma)
	if _, err := cm.Chat("thread-name", agent.PromptRequest{
		UserParts: []message.UIPart{message.UITextPart{Text: "hi"}},
	}); err != nil {
		t.Fatal(err)
	}

	time.Sleep(20 * time.Millisecond)

	h := New("", cm, nil, nil, nil)
	h.chatPingEvery = 25 * time.Millisecond
	ts := newStreamTestServer(t, h)
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/threads/thread-name/chat/stream")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	time.AfterFunc(5*time.Millisecond, func() {
		cm.Cancel("thread-name")
	})

	frames := readFrames(t, resp.Body, 5, false)
	if len(frames) != 5 {
		t.Fatalf("expected 5 frames, got %d", len(frames))
	}
	if frames[1].Event != "history-end" {
		t.Fatalf("expected history-end frame, got %+v", frames[1])
	}
	assertCompletionStatusFrame(t, frames[2], "thread-name", "", true)
	if frames[3].Event != "chunk" || frames[3].Data != string(threadUpdateChunkJSON) {
		t.Fatalf("expected thread-update chunk frame after history-end, got %+v", frames[3])
	}
}

func TestChatStream_DoesNotReplayPastEphemeralChunk(t *testing.T) {
	cm := agent.NewCompletionManager(&streamTestAgent{})
	cm.EmitEphemeralChunk("hooks-status", message.DataChunk{
		DataType: "hooks-status",
		Data:     []byte(`{"hooks":{"go-check":{"hookId":"go-check"}}}`),
	})

	h := New("", cm, nil, nil, nil)
	h.chatPingEvery = 10 * time.Millisecond
	ts := newStreamTestServer(t, h)
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/threads/thread-hooks/chat/stream")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	frames := readFrames(t, resp.Body, 3, false)
	if len(frames) != 3 {
		t.Fatalf("expected 3 frames, got %d", len(frames))
	}
	if frames[0].Event != "history-start" {
		t.Fatalf("expected history-start, got %+v", frames[0])
	}
	if frames[1].Event != "history-end" {
		t.Fatalf("expected history-end, got %+v", frames[1])
	}
	if frames[2].Event != "ping" {
		t.Fatalf("expected ping with no replayed ephemeral chunk, got %+v", frames[2])
	}
}

func TestChatStream_ForwardsLiveEphemeralChunk(t *testing.T) {
	expectedChunk, err := message.MarshalChunk(message.DataChunk{
		DataType: "hooks-status",
		Data:     []byte(`{"hooks":{"go-check":{"hookId":"go-check"}}}`),
	})
	if err != nil {
		t.Fatal(err)
	}

	cm := agent.NewCompletionManager(&streamTestAgent{})
	h := New("", cm, nil, nil, nil)
	h.chatPingEvery = 25 * time.Millisecond
	ts := newStreamTestServer(t, h)
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/threads/thread-hooks/chat/stream")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	time.AfterFunc(5*time.Millisecond, func() {
		cm.EmitEphemeralChunk("hooks-status", message.DataChunk{
			DataType: "hooks-status",
			Data:     []byte(`{"hooks":{"go-check":{"hookId":"go-check"}}}`),
		})
	})

	frames := readFrames(t, resp.Body, 3, false)
	if len(frames) != 3 {
		t.Fatalf("expected 3 frames, got %d", len(frames))
	}
	if frames[0].Event != "history-start" {
		t.Fatalf("expected history-start, got %+v", frames[0])
	}
	if frames[1].Event != "history-end" {
		t.Fatalf("expected history-end, got %+v", frames[1])
	}
	if frames[2].Event != "chunk" || frames[2].Data != string(expectedChunk) {
		t.Fatalf("expected live ephemeral chunk after subscribe, got %+v", frames[2])
	}
}

func TestChatStream_InvalidLastEventID_TreatedAsFreshRequest(t *testing.T) {
	historyMsg := message.UIMessage{
		ID:    "hist-2",
		Role:  "user",
		Parts: []message.UIPart{message.UITextPart{Text: "old"}},
	}
	ma := &streamTestAgent{
		promptFn: yieldChunksAndBlock(message.TextDeltaChunk{ID: "delta-2", Delta: "live"}),
		messagesFn: func(_, _ string) ([]message.UIMessage, error) {
			return []message.UIMessage{historyMsg}, nil
		},
	}
	cm := agent.NewCompletionManager(ma)
	if _, err := cm.Chat("thread-3", agent.PromptRequest{UserParts: []message.UIPart{message.UITextPart{Text: "hi"}}}); err != nil {
		t.Fatal(err)
	}

	time.Sleep(20 * time.Millisecond)

	h := New("", cm, nil, nil, nil)
	ts := newStreamTestServer(t, h)
	defer ts.Close()

	req, err := http.NewRequest("GET", ts.URL+"/threads/thread-3/chat/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Last-Event-ID", "invalid-event-id")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	frames := readFrames(t, resp.Body, 1, false)
	if len(frames) != 1 || frames[0].Event != "history-start" {
		t.Fatalf("expected invalid Last-Event-ID to trigger fresh replay, got %+v", frames)
	}
}

func TestChatStream_FreshRequestWithoutActiveCompletion_ReplaysHistoryAndPing(t *testing.T) {
	historyMsg := message.UIMessage{
		ID:    "hist-3",
		Role:  "assistant",
		Parts: []message.UIPart{message.UITextPart{Text: "done"}},
	}
	historyMsgJSON, err := json.Marshal(historyMsg)
	if err != nil {
		t.Fatal(err)
	}
	ma := &streamTestAgent{
		messagesFn: func(_, _ string) ([]message.UIMessage, error) {
			return []message.UIMessage{historyMsg}, nil
		},
	}
	cm := agent.NewCompletionManager(ma)
	h := New("", cm, nil, nil, nil)
	h.chatPingEvery = 10 * time.Millisecond
	ts := newStreamTestServer(t, h)
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/threads/thread-4/chat/stream")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	frames := readFrames(t, resp.Body, 4, false)
	if len(frames) != 4 {
		t.Fatalf("expected 4 frames, got %d", len(frames))
	}
	if frames[0].Event != "history-start" {
		t.Fatalf("expected history-start, got %+v", frames[0])
	}
	if frames[1].Event != "history-message" || frames[1].Data != string(historyMsgJSON) {
		t.Fatalf("unexpected history message frame: %+v", frames[1])
	}
	if frames[2].Event != "history-end" {
		t.Fatalf("expected history-end, got %+v", frames[2])
	}
	if frames[3].Event != "ping" {
		t.Fatalf("expected ping after history replay, got %+v", frames[3])
	}
}

func TestChatStream_CompletionEndDoesNotCloseStream(t *testing.T) {
	liveChunk := message.TextDeltaChunk{ID: "delta-5", Delta: "live"}
	liveChunkJSON, err := message.MarshalChunk(liveChunk)
	if err != nil {
		t.Fatal(err)
	}

	ma := &streamTestAgent{promptFn: yieldChunksAndBlock(liveChunk)}
	cm := agent.NewCompletionManager(ma)
	if _, err := cm.Chat("thread-5", agent.PromptRequest{
		UserParts: []message.UIPart{message.UITextPart{Text: "hi"}},
	}); err != nil {
		t.Fatal(err)
	}

	time.Sleep(20 * time.Millisecond)

	h := New("", cm, nil, nil, nil)
	h.chatPingEvery = 25 * time.Millisecond
	ts := newStreamTestServer(t, h)
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/threads/thread-5/chat/stream")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	time.AfterFunc(5*time.Millisecond, func() {
		cm.Cancel("thread-5")
	})

	frames := readFrames(t, resp.Body, 6, false)
	if len(frames) != 6 {
		t.Fatalf("expected 6 frames, got %d", len(frames))
	}
	if frames[0].Event != "history-start" {
		t.Fatalf("expected history-start, got %+v", frames[0])
	}
	if frames[1].Event != "history-end" {
		t.Fatalf("expected history-end, got %+v", frames[1])
	}
	assertCompletionStatusFrame(t, frames[2], "thread-5", "", true)
	if frames[3].Event != "chunk" || frames[3].Data != string(liveChunkJSON) {
		t.Fatalf("unexpected chunk frame after history-end: %+v", frames[3])
	}
	assertCompletionStatusFrame(t, frames[4], "thread-5", "", false)
	if frames[5].Event != "ping" {
		t.Fatalf("expected ping after completion finished, got %+v", frames[5])
	}
}

func TestChatStream_ContinuesIntoLaterCompletionOnSameConnection(t *testing.T) {
	nextChunk := message.TextDeltaChunk{ID: "delta-6", Delta: "later"}
	nextChunkJSON, err := message.MarshalChunk(nextChunk)
	if err != nil {
		t.Fatal(err)
	}

	ma := &streamTestAgent{promptFn: yieldChunksAndFinish(nextChunk)}
	cm := agent.NewCompletionManager(ma)
	h := New("", cm, nil, nil, nil)
	h.chatPingEvery = 25 * time.Millisecond
	ts := newStreamTestServer(t, h)
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/threads/thread-6/chat/stream")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	startErrCh := make(chan error, 1)
	time.AfterFunc(5*time.Millisecond, func() {
		_, startErr := cm.Chat("thread-6", agent.PromptRequest{
			UserParts: []message.UIPart{message.UITextPart{Text: "hi"}},
		})
		startErrCh <- startErr
	})

	frames := readFrames(t, resp.Body, 4, false)
	if len(frames) != 4 {
		t.Fatalf("expected 4 frames, got %d", len(frames))
	}
	if frames[0].Event != "history-start" {
		t.Fatalf("expected history-start, got %+v", frames[0])
	}
	if frames[1].Event != "history-end" {
		t.Fatalf("expected history-end, got %+v", frames[1])
	}
	if frames[2].Event != "chunk" || frames[2].Data != string(nextChunkJSON) {
		t.Fatalf("expected next completion chunk, got %+v", frames[2])
	}
	if frames[3].Event != "ping" {
		t.Fatalf("expected ping after later completion, got %+v", frames[3])
	}

	if startErr := <-startErrCh; startErr != nil {
		t.Fatalf("expected later completion to start successfully: %v", startErr)
	}
}
