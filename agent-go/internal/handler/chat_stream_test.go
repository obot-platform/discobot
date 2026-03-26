package handler

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"iter"
	"net/http"
	"net/http/httptest"
	"strings"
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
	messagesFn           func(threadID, leafID string) ([]message.UIMessage, error)
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
func (m *streamTestAgent) PendingQuestion(_ string) (*agent.PendingQuestion, error) { return nil, nil }
func (m *streamTestAgent) SubmitAnswer(threadID, approvalID string, req api.AnswerQuestionRequest) error {
	if m.submitAnswerFn != nil {
		return m.submitAnswerFn(threadID, approvalID, req)
	}
	return nil
}
func (m *streamTestAgent) FinalResponse(_ string) (string, error) { return "", nil }
func (m *streamTestAgent) ListCommands() ([]agent.Command, error) { return nil, nil }
func (m *streamTestAgent) IsLeaf(_, _ string) (bool, error)       { return true, nil }

type sseFrame struct {
	ID    string
	Event string
	Data  string
	Done  bool
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

func TestPostChat_SeedsThreadMetadataBeforePromptStarts(t *testing.T) {
	reqCh := make(chan agent.PromptRequest, 1)
	ma := &streamTestAgent{
		promptFn: func(_ context.Context, _ string, req agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
			reqCh <- req
			return func(_ func(message.MessageChunk, error) bool) {}
		},
	}
	cm := agent.NewCompletionManager(ma)
	store := thread.NewStore(t.TempDir())
	defaultAgent := agentimpl.NewDefaultAgent(store, nil, nil, t.TempDir(), agentimpl.MCPConfig{})
	h := New("", cm, nil, nil, defaultAgent)
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

	threadResp, err := ts.Client().Get(ts.URL + "/threads/thread-1")
	if err != nil {
		t.Fatal(err)
	}
	defer threadResp.Body.Close()

	if threadResp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", threadResp.StatusCode)
	}

	var got api.Thread
	if err := json.NewDecoder(threadResp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "" {
		t.Fatalf("expected empty thread name before AI rename, got %+v", got)
	}
	if got.Model != "openai/gpt-5.4" {
		t.Fatalf("expected seeded model, got %+v", got)
	}
	if got.Reasoning != "high" {
		t.Fatalf("expected seeded reasoning, got %+v", got)
	}
	if got.Mode != "plan" {
		t.Fatalf("expected seeded mode, got %+v", got)
	}

	select {
	case <-reqCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Prompt request")
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
	if got != listed.Threads[0] {
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
	if err := store.SaveConfig("thread-build", thread.Config{Name: "Build Thread", NameSource: thread.ThreadNameSourceUser, PlanMode: false}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateThread("thread-plan"); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveConfig("thread-plan", thread.Config{Name: "Plan Thread", NameSource: thread.ThreadNameSourceUser, PlanMode: true}); err != nil {
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

func TestPostAnswer_UsesReplayTurnWithoutCachedCompletion(t *testing.T) {
	reqCh := make(chan agent.PromptRequest, 1)
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
		promptFn: func(_ context.Context, _ string, req agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
			reqCh <- req
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
	case req := <-reqCh:
		if !req.ReplayTurn {
			t.Fatal("expected ReplayTurn to be true when no cached completion exists")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Prompt request")
	}
}

func TestPostAnswer_UsesReplayTurnWhenOnlyDoneCachedCompletionExists(t *testing.T) {
	reqCh := make(chan agent.PromptRequest, 2)
	ma := &streamTestAgent{
		submitAnswerFn: func(_ string, _ string, _ api.AnswerQuestionRequest) error {
			return nil
		},
		promptFn: func(_ context.Context, _ string, req agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
			reqCh <- req
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
	case <-reqCh:
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
	case req := <-reqCh:
		if !req.ReplayTurn {
			t.Fatal("expected ReplayTurn to be true when the cached completion is already done")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for resumed Prompt request")
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

	frames := readFrames(t, resp.Body, 4, false)
	if len(frames) != 4 {
		t.Fatalf("expected 4 frames, got %d", len(frames))
	}
	if frames[0].Event != "history-start" {
		t.Fatalf("expected first frame history-start, got %+v", frames[0])
	}
	if frames[1].Event != "history-message" || frames[1].Data != string(historyMsgJSON) {
		t.Fatalf("unexpected history message frame: %+v", frames[1])
	}
	if frames[2].Event != "chunk" || frames[2].ID != completionID+":0" || frames[2].Data != string(liveChunkJSON) {
		t.Fatalf("unexpected cached delta frame: %+v", frames[2])
	}
	if frames[3].Event != "history-end" {
		t.Fatalf("expected history-end frame, got %+v", frames[3])
	}
}

func TestChatStream_FreshRequest_ReplaysCompletedSnapshotBeforeHistoryEnd(t *testing.T) {
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
	cachedChunkJSON, err := message.MarshalChunk(cachedChunk)
	if err != nil {
		t.Fatal(err)
	}

	ma := &streamTestAgent{
		promptFn: yieldChunksAndFinish(cachedChunk),
		messagesFn: func(_, _ string) ([]message.UIMessage, error) {
			return []message.UIMessage{historyMsg}, nil
		},
	}
	cm := agent.NewCompletionManager(ma)
	completionID, err := cm.Chat("thread-done", agent.PromptRequest{
		UserParts: []message.UIPart{message.UITextPart{Text: "hi"}},
	})
	if err != nil {
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

	frames := readFrames(t, resp.Body, 5, false)
	if len(frames) != 5 {
		t.Fatalf("expected 5 frames, got %d", len(frames))
	}
	if frames[0].Event != "history-start" {
		t.Fatalf("expected history-start, got %+v", frames[0])
	}
	if frames[1].Event != "history-message" || frames[1].Data != string(historyMsgJSON) {
		t.Fatalf("unexpected history message frame: %+v", frames[1])
	}
	if frames[2].Event != "chunk" || frames[2].ID != completionID+":0" || frames[2].Data != string(cachedChunkJSON) {
		t.Fatalf("expected completed snapshot chunk before history-end, got %+v", frames[2])
	}
	if frames[3].Event != "history-end" {
		t.Fatalf("expected history-end after completed snapshot chunk, got %+v", frames[3])
	}
	if frames[4].Event != "ping" {
		t.Fatalf("expected ping after replay completes, got %+v", frames[4])
	}
}

func TestChatStream_FreshRequest_StartsInterruptedTurnRecovery(t *testing.T) {
	reqCh := make(chan agent.PromptRequest, 1)
	release := make(chan struct{})
	liveChunk := message.TextDeltaChunk{ID: "delta-recover-1", Delta: "resumed"}
	liveChunkJSON, err := message.MarshalChunk(liveChunk)
	if err != nil {
		t.Fatal(err)
	}

	ma := &streamTestAgent{
		promptFn: func(ctx context.Context, _ string, req agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
			reqCh <- req
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
	case req := <-reqCh:
		if len(req.UserParts) != 0 {
			t.Fatalf("expected recovery prompt to have no user parts, got %d", len(req.UserParts))
		}
		if req.LeafID != "" {
			t.Fatalf("expected empty leaf ID for recovery prompt, got %q", req.LeafID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for recovery prompt")
	}

	close(release)

	frames := readFrames(t, resp.Body, 3, false)
	if len(frames) != 3 {
		t.Fatalf("expected 3 frames, got %d", len(frames))
	}
	if frames[0].Event != "history-start" {
		t.Fatalf("expected history-start frame, got %+v", frames[0])
	}
	if frames[1].Event != "history-end" {
		t.Fatalf("expected history-end frame, got %+v", frames[1])
	}
	if frames[2].Event != "chunk" || frames[2].Data != string(liveChunkJSON) {
		t.Fatalf("unexpected recovery chunk frame: %+v", frames[2])
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

func TestChatStream_ForwardsThreadNameChunk(t *testing.T) {
	nameChunk := message.ThreadNameChunk{
		Data: message.ThreadNameData{Name: "Fix thread naming"},
	}
	nameChunkJSON, err := message.MarshalChunk(nameChunk)
	if err != nil {
		t.Fatal(err)
	}

	ma := &streamTestAgent{promptFn: yieldChunksAndBlock(nameChunk)}
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

	frames := readFrames(t, resp.Body, 4, false)
	if len(frames) != 4 {
		t.Fatalf("expected 4 frames, got %d", len(frames))
	}
	if frames[1].Event != "chunk" || frames[1].Data != string(nameChunkJSON) {
		t.Fatalf("expected thread-name chunk frame, got %+v", frames[1])
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

	frames := readFrames(t, resp.Body, 4, false)
	if len(frames) != 4 {
		t.Fatalf("expected 4 frames, got %d", len(frames))
	}
	if frames[0].Event != "history-start" {
		t.Fatalf("expected history-start, got %+v", frames[0])
	}
	if frames[1].Event != "chunk" || frames[1].Data != string(liveChunkJSON) {
		t.Fatalf("unexpected chunk frame: %+v", frames[1])
	}
	if frames[2].Event != "history-end" {
		t.Fatalf("expected history-end, got %+v", frames[2])
	}
	if frames[3].Event != "ping" {
		t.Fatalf("expected ping after completion finished, got %+v", frames[3])
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
