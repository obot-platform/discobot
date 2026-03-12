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
	"github.com/obot-platform/discobot/agent-go/message"
	"github.com/obot-platform/discobot/agent-go/providers"
)

type streamTestAgent struct {
	promptFn   func(ctx context.Context, threadID string, req agent.PromptRequest) iter.Seq2[message.MessageChunk, error]
	messagesFn func(threadID, leafID string) ([]json.RawMessage, error)
}

func (m *streamTestAgent) Prompt(ctx context.Context, threadID string, req agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
	if m.promptFn != nil {
		return m.promptFn(ctx, threadID, req)
	}
	return func(_ func(message.MessageChunk, error) bool) {}
}

func (m *streamTestAgent) Cancel(_ string) bool { return false }
func (m *streamTestAgent) Messages(threadID, leafID string) ([]json.RawMessage, error) {
	if m.messagesFn != nil {
		return m.messagesFn(threadID, leafID)
	}
	return nil, nil
}
func (m *streamTestAgent) ListModels(_ context.Context) ([]providers.ModelInfo, error) {
	return nil, nil
}
func (m *streamTestAgent) ListThreads() ([]string, error)                           { return nil, nil }
func (m *streamTestAgent) InterruptedThreads() ([]string, error)                    { return nil, nil }
func (m *streamTestAgent) PendingQuestion(_ string) (*agent.PendingQuestion, error) { return nil, nil }
func (m *streamTestAgent) SubmitAnswer(_, _ string, _ map[string]string) error      { return nil }
func (m *streamTestAgent) FinalResponse(_ string) (string, error)                   { return "", nil }
func (m *streamTestAgent) ListCommands() ([]agent.Command, error)                   { return nil, nil }

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

func TestChatStream_FreshRequest_ReplaysHistoryThenCachedDeltas(t *testing.T) {
	historyMsg := json.RawMessage(`{"id":"hist-1","role":"user","parts":[{"type":"text","text":"old"}]}`)
	liveChunk := message.TextDeltaChunk{ID: "delta-1", Delta: "live"}
	liveChunkJSON, err := message.MarshalChunk(liveChunk)
	if err != nil {
		t.Fatal(err)
	}

	ma := &streamTestAgent{
		promptFn: yieldChunksAndBlock(liveChunk),
		messagesFn: func(_, _ string) ([]json.RawMessage, error) {
			return []json.RawMessage{historyMsg}, nil
		},
	}
	cm := agent.NewCompletionManager(ma)
	completionID, err := cm.Chat("thread-1", agent.PromptRequest{
		LeafID:    "leaf-before",
		UserParts: []message.Part{message.TextPart{Text: "hi"}},
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
	if frames[1].Event != "history-message" || frames[1].Data != string(historyMsg) {
		t.Fatalf("unexpected history message frame: %+v", frames[1])
	}
	if frames[2].Event != "chunk" || frames[2].ID != completionID+":0" || frames[2].Data != string(liveChunkJSON) {
		t.Fatalf("unexpected cached delta frame: %+v", frames[2])
	}
	if frames[3].Event != "history-end" {
		t.Fatalf("expected history-end frame, got %+v", frames[3])
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
		UserParts: []message.Part{message.TextPart{Text: "hi"}},
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

func TestChatStream_InvalidLastEventID_TreatedAsFreshRequest(t *testing.T) {
	historyMsg := json.RawMessage(`{"id":"hist-2","role":"user","parts":[{"type":"text","text":"old"}]}`)
	ma := &streamTestAgent{
		promptFn: yieldChunksAndBlock(message.TextDeltaChunk{ID: "delta-2", Delta: "live"}),
		messagesFn: func(_, _ string) ([]json.RawMessage, error) {
			return []json.RawMessage{historyMsg}, nil
		},
	}
	cm := agent.NewCompletionManager(ma)
	if _, err := cm.Chat("thread-3", agent.PromptRequest{UserParts: []message.Part{message.TextPart{Text: "hi"}}}); err != nil {
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

func TestChatStream_FreshRequestWithoutActiveCompletion_ReplaysHistoryAndDone(t *testing.T) {
	historyMsg := json.RawMessage(`{"id":"hist-3","role":"assistant","parts":[{"type":"text","text":"done"}]}`)
	ma := &streamTestAgent{
		messagesFn: func(_, _ string) ([]json.RawMessage, error) {
			return []json.RawMessage{historyMsg}, nil
		},
	}
	cm := agent.NewCompletionManager(ma)
	h := New("", cm, nil, nil, nil)
	ts := newStreamTestServer(t, h)
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/threads/thread-4/chat/stream")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	frames := readFrames(t, resp.Body, 0, true)
	if len(frames) != 4 {
		t.Fatalf("expected 4 frames, got %d", len(frames))
	}
	if frames[0].Event != "history-start" {
		t.Fatalf("expected history-start, got %+v", frames[0])
	}
	if frames[1].Event != "history-message" || frames[1].Data != string(historyMsg) {
		t.Fatalf("unexpected history message frame: %+v", frames[1])
	}
	if frames[2].Event != "history-end" {
		t.Fatalf("expected history-end, got %+v", frames[2])
	}
	if frames[3].Event != "done" || !frames[3].Done {
		t.Fatalf("expected final DONE frame, got %+v", frames[3])
	}
}
