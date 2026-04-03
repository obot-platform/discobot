package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/obot-platform/discobot/agent-go/message"
	"github.com/obot-platform/discobot/agent-go/providers"
	"github.com/obot-platform/discobot/agent-go/providers/transport"
)

func TestNewWSPool_URLDerivation(t *testing.T) {
	cases := []struct {
		httpBase string
		wantWS   string
	}{
		{"https://api.openai.com/v1", "wss://api.openai.com/v1/responses"},
		{"http://localhost:8080/v1", "ws://localhost:8080/v1/responses"},
	}
	for _, tc := range cases {
		pool := newWSPool("key", tc.httpBase)
		if pool.wsURL != tc.wantWS {
			t.Errorf("newWSPool(%q).wsURL = %q, want %q", tc.httpBase, pool.wsURL, tc.wantWS)
		}
	}
}

func TestNew_WebSocketMode(t *testing.T) {
	t.Run("disabled by default", func(t *testing.T) {
		p, err := New(providers.Config{"api_key": "key"}, false, defaultBaseURL)
		if err != nil {
			t.Fatal(err)
		}
		op := p.(*Provider)
		if op.ws != nil {
			t.Fatal("expected ws=nil for default config")
		}
		if op.baseURL != "https://api.openai.com/v1" {
			t.Errorf("expected baseURL %q, got %q", "https://api.openai.com/v1", op.baseURL)
		}
	})

	t.Run("enabled via use_websocket=true", func(t *testing.T) {
		p, err := New(providers.Config{"api_key": "key", configUseWebSocket: "true", "base_url": "https://api.openai.com/v1"}, false, defaultBaseURL)
		if err != nil {
			t.Fatal(err)
		}
		op := p.(*Provider)
		if op.ws == nil {
			t.Fatal("expected ws!=nil with use_websocket=true")
		}
		if op.baseURL != "https://api.openai.com/v1" {
			t.Errorf("expected baseURL %q, got %q", "https://api.openai.com/v1", op.baseURL)
		}
		if op.ws.wsURL != "wss://api.openai.com/v1/responses" {
			t.Errorf("unexpected wsURL: %q", op.ws.wsURL)
		}
	})

	t.Run("disabled via explicit https base_url", func(t *testing.T) {
		p, err := New(providers.Config{"api_key": "key", "base_url": "https://api.openai.com/v1"}, false, defaultBaseURL)
		if err != nil {
			t.Fatal(err)
		}
		if p.(*Provider).ws != nil {
			t.Error("expected ws=nil for explicit https base_url")
		}
	})

	t.Run("enabled and URL normalised via wss:// base_url", func(t *testing.T) {
		p, err := New(providers.Config{"api_key": "key", "base_url": "wss://custom.api.com/v1"}, false, defaultBaseURL)
		if err != nil {
			t.Fatal(err)
		}
		op := p.(*Provider)
		if op.ws == nil {
			t.Fatal("expected ws!=nil with wss:// base_url")
		}
		if op.baseURL != "https://custom.api.com/v1" {
			t.Errorf("expected https-normalised baseURL, got %q", op.baseURL)
		}
		if op.ws.wsURL != "wss://custom.api.com/v1/responses" {
			t.Errorf("unexpected wsURL: %q", op.ws.wsURL)
		}
	})

	t.Run("HTTP base URL is preserved for REST endpoints", func(t *testing.T) {
		p, err := New(providers.Config{"api_key": "key", configUseWebSocket: "true", "base_url": "https://api.openai.com/v1"}, false, defaultBaseURL)
		if err != nil {
			t.Fatal(err)
		}
		op := p.(*Provider)
		if op.baseURL != "https://api.openai.com/v1" {
			t.Errorf("expected https baseURL unchanged, got %q", op.baseURL)
		}
	})
}

// wsTestServer starts an httptest.Server whose sole handler upgrades the
// connection at /responses to WebSocket and calls handler.
func wsTestServer(t *testing.T, handler func(conn *websocket.Conn, r *http.Request)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Errorf("unexpected path %q", r.URL.Path)
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true,
		})
		if err != nil {
			t.Errorf("websocket accept: %v", err)
			return
		}
		defer conn.CloseNow()
		handler(conn, r)
	}))
}

// sendWSEvents writes a sequence of events as JSON text frames.
func sendWSEvents(ctx context.Context, t *testing.T, conn *websocket.Conn, events []map[string]any) {
	t.Helper()
	for _, ev := range events {
		data, _ := json.Marshal(ev)
		if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
			t.Errorf("write event: %v", err)
			return
		}
	}
}

func minimalWSCompletion(id string) []map[string]any {
	return []map[string]any{
		{"type": "response.created", "response": map[string]any{"id": id, "model": "gpt-4o"}},
		{"type": "response.completed", "response": map[string]any{
			"status": "completed",
			"output": []any{},
			"usage": map[string]any{
				"input_tokens":          1,
				"input_tokens_details":  map[string]any{"cached_tokens": 0},
				"output_tokens":         1,
				"output_tokens_details": map[string]any{"reasoning_tokens": 0},
			},
		}},
	}
}

func TestParseWebSocketStream(t *testing.T) {
	t.Run("text stream", func(t *testing.T) {
		events := []map[string]any{
			{"type": "response.created", "response": map[string]any{"id": "resp_1", "model": "gpt-4o"}},
			{"type": "response.output_item.added", "item": map[string]any{"id": "msg_1", "type": "message"}},
			{"type": "response.content_part.added", "part": map[string]any{"type": "output_text"}, "item_id": "msg_1"},
			{"type": "response.output_text.delta", "item_id": "msg_1", "delta": "Hi!"},
			{"type": "response.output_text.done", "item_id": "msg_1"},
			{"type": "response.output_item.done", "item": map[string]any{"id": "msg_1", "type": "message"}},
			{"type": "response.completed", "response": map[string]any{
				"status": "completed",
				"output": []map[string]any{{"type": "message"}},
				"usage": map[string]any{
					"input_tokens":          5,
					"input_tokens_details":  map[string]any{"cached_tokens": 0},
					"output_tokens":         2,
					"output_tokens_details": map[string]any{"reasoning_tokens": 0},
				},
			}},
		}

		ts := wsTestServer(t, func(conn *websocket.Conn, r *http.Request) {
			sendWSEvents(r.Context(), t, conn, events)
		})
		defer ts.Close()

		wsURL := strings.Replace(ts.URL, "http://", "ws://", 1) + "/responses"
		conn, _, err := websocket.Dial(context.Background(), wsURL, nil)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer conn.CloseNow()

		var chunks []message.ProviderMessageChunk
		respID, clean := parseWebSocketStream(context.Background(), conn, func(chunk message.ProviderMessageChunk, err error) bool {
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return false
			}
			if chunk != nil {
				chunks = append(chunks, chunk)
			}
			return true
		})

		if !clean {
			t.Error("expected clean=true for response.completed")
		}
		if respID != "resp_1" {
			t.Errorf("expected respID %q, got %q", "resp_1", respID)
		}
		assertChunkTypes(t, chunks,
			"stream-start", "response-metadata",
			"text-start", "text-delta", "text-end",
			"finish",
		)
		finish := chunks[len(chunks)-1].(message.FinishChunk)
		if finish.FinishReason.Unified != "stop" {
			t.Errorf("expected finish reason %q, got %q", "stop", finish.FinishReason.Unified)
		}
	})

	t.Run("response.failed returns clean=false", func(t *testing.T) {
		events := []map[string]any{
			{"type": "response.created", "response": map[string]any{"id": "resp_err", "model": "gpt-4o"}},
			{"type": "response.failed", "response": map[string]any{
				"error": map[string]any{"message": "internal error"},
			}},
		}

		ts := wsTestServer(t, func(conn *websocket.Conn, r *http.Request) {
			sendWSEvents(r.Context(), t, conn, events)
		})
		defer ts.Close()

		wsURL := strings.Replace(ts.URL, "http://", "ws://", 1) + "/responses"
		conn, _, err := websocket.Dial(context.Background(), wsURL, nil)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer conn.CloseNow()

		var gotErr error
		_, clean := parseWebSocketStream(context.Background(), conn, func(_ message.ProviderMessageChunk, err error) bool {
			if err != nil {
				gotErr = err
			}
			return true // keep consuming even on error
		})

		if clean {
			t.Error("expected clean=false for response.failed")
		}
		if gotErr == nil {
			t.Error("expected error from response.failed event")
		}
	})

	t.Run("tool call stream", func(t *testing.T) {
		events := []map[string]any{
			{"type": "response.created", "response": map[string]any{"id": "resp_tc", "model": "gpt-4o"}},
			{"type": "response.output_item.added", "item": map[string]any{
				"id": "fc_1", "type": "function_call", "name": "get_weather", "call_id": "call_abc",
			}},
			{"type": "response.function_call_arguments.delta", "item_id": "fc_1", "call_id": "call_abc", "delta": `{"loc`},
			{"type": "response.function_call_arguments.delta", "item_id": "fc_1", "call_id": "call_abc", "delta": `ation":"Paris"}`},
			{"type": "response.function_call_arguments.done", "item_id": "fc_1", "call_id": "call_abc"},
			{"type": "response.completed", "response": map[string]any{
				"status": "completed",
				"output": []map[string]any{{"type": "function_call"}},
				"usage": map[string]any{
					"input_tokens":          3,
					"input_tokens_details":  map[string]any{"cached_tokens": 0},
					"output_tokens":         5,
					"output_tokens_details": map[string]any{"reasoning_tokens": 0},
				},
			}},
		}

		ts := wsTestServer(t, func(conn *websocket.Conn, r *http.Request) {
			sendWSEvents(r.Context(), t, conn, events)
		})
		defer ts.Close()

		wsURL := strings.Replace(ts.URL, "http://", "ws://", 1) + "/responses"
		conn, _, err := websocket.Dial(context.Background(), wsURL, nil)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer conn.CloseNow()

		var chunks []message.ProviderMessageChunk
		_, clean := parseWebSocketStream(context.Background(), conn, func(chunk message.ProviderMessageChunk, err error) bool {
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return false
			}
			if chunk != nil {
				chunks = append(chunks, chunk)
			}
			return true
		})

		if !clean {
			t.Error("expected clean=true")
		}
		assertChunkTypes(t, chunks,
			"stream-start", "response-metadata",
			"tool-input-start", "tool-input-delta", "tool-input-delta", "tool-input-end",
			"finish",
		)
		finish := chunks[len(chunks)-1].(message.FinishChunk)
		if finish.FinishReason.Unified != "tool-calls" {
			t.Errorf("expected finish reason %q, got %q", "tool-calls", finish.FinishReason.Unified)
		}
	})
}

func TestCompleteViaWebSocket_StreamsText(t *testing.T) {
	var receivedReqType, receivedAuth string
	ts := wsTestServer(t, func(conn *websocket.Conn, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		_, data, err := conn.Read(r.Context())
		if err != nil {
			t.Errorf("read request: %v", err)
			return
		}
		var req map[string]any
		json.Unmarshal(data, &req)
		receivedReqType, _ = req["type"].(string)

		events := []map[string]any{
			{"type": "response.created", "response": map[string]any{"id": "resp_1", "model": "gpt-4o"}},
			{"type": "response.content_part.added", "part": map[string]any{"type": "output_text"}, "item_id": "msg_1"},
			{"type": "response.output_text.delta", "item_id": "msg_1", "delta": "Hello"},
			{"type": "response.output_text.done", "item_id": "msg_1"},
			{"type": "response.completed", "response": map[string]any{
				"status": "completed",
				"output": []map[string]any{{"type": "message"}},
				"usage": map[string]any{
					"input_tokens":          3,
					"input_tokens_details":  map[string]any{"cached_tokens": 0},
					"output_tokens":         1,
					"output_tokens_details": map[string]any{"reasoning_tokens": 0},
				},
			}},
		}
		sendWSEvents(r.Context(), t, conn, events)
	})
	defer ts.Close()

	p := &Provider{
		apiKey:  "test-key",
		baseURL: ts.URL,
		client:  ts.Client(),
		ws:      newWSPool("test-key", ts.URL),
	}

	req := providers.CompleteRequest{
		Model:    providers.ModelRef{ProviderID: "openai", ModelID: "gpt-4o"},
		Messages: []message.Message{{Role: "user", Parts: []message.Part{message.TextPart{Text: "Hi"}}}},
	}
	var chunks []message.ProviderMessageChunk
	for chunk, err := range p.Complete(context.Background(), req) {
		if err != nil {
			t.Fatalf("Complete error: %v", err)
		}
		if chunk != nil {
			chunks = append(chunks, chunk)
		}
	}

	if receivedAuth != "Bearer test-key" {
		t.Errorf("expected Authorization %q, got %q", "Bearer test-key", receivedAuth)
	}
	if receivedReqType != "response.create" {
		t.Errorf("expected request type %q, got %q", "response.create", receivedReqType)
	}
	assertChunkTypes(t, chunks,
		"stream-start", "response-metadata",
		"text-start", "text-delta", "text-end",
		"finish",
	)

	// Confirm the response ID is in the pool, ready for the next request on this chain.
	p.ws.mu.Lock()
	pc := p.ws.byPrev["resp_1"]
	p.ws.mu.Unlock()
	if pc == nil {
		t.Error("expected connection pooled under resp_1 after successful completion")
	}
}

func TestCompleteViaWebSocket_SendsCodexAccountHeader(t *testing.T) {
	var receivedAccountID string
	ts := wsTestServer(t, func(conn *websocket.Conn, r *http.Request) {
		receivedAccountID = r.Header.Get("ChatGPT-Account-Id")
		if _, _, err := conn.Read(r.Context()); err != nil {
			t.Errorf("read request: %v", err)
			return
		}
		sendWSEvents(r.Context(), t, conn, minimalWSCompletion("resp_codex"))
	})
	defer ts.Close()

	p := &Provider{
		apiKey:    "test-key",
		baseURL:   ts.URL,
		client:    ts.Client(),
		accountID: "acct_123",
		isCodex:   true,
		ws:        newWSPool("test-key", ts.URL),
	}
	p.ws.accountID = p.accountID

	req := providers.CompleteRequest{
		Model:    providers.ModelRef{ProviderID: "openai", ModelID: "gpt-5.4"},
		Messages: []message.Message{{Role: "user", Parts: []message.Part{message.TextPart{Text: "Hi"}}}},
	}
	for _, err := range p.Complete(context.Background(), req) {
		if err != nil {
			t.Fatalf("completion error: %v", err)
		}
	}

	if receivedAccountID != "acct_123" {
		t.Fatalf("expected ChatGPT-Account-Id %q, got %q", "acct_123", receivedAccountID)
	}
}

func TestCompleteViaWebSocket_CodexInstructionsFreshVsReusedConnection(t *testing.T) {
	var (
		mu                         sync.Mutex
		connCount                  int
		firstTurnInstructions      string
		secondTurnHasInstructions  bool
		restartTurnPrevID          string
		restartTurnInstructions    string
		restartTurnHasInstructions bool
	)

	ts := wsTestServer(t, func(conn *websocket.Conn, r *http.Request) {
		mu.Lock()
		connCount++
		currentConn := connCount
		mu.Unlock()

		switch currentConn {
		case 1:
			for turn := 1; turn <= 2; turn++ {
				_, data, err := conn.Read(r.Context())
				if err != nil {
					t.Errorf("conn 1 turn %d read: %v", turn, err)
					return
				}
				var req map[string]any
				if err := json.Unmarshal(data, &req); err != nil {
					t.Errorf("conn 1 turn %d decode: %v", turn, err)
					return
				}

				switch turn {
				case 1:
					firstTurnInstructions, _ = req["instructions"].(string)
				case 2:
					_, secondTurnHasInstructions = req["instructions"]
				}

				sendWSEvents(r.Context(), t, conn, minimalWSCompletion(fmt.Sprintf("resp_%d", turn)))
			}
		case 2:
			_, data, err := conn.Read(r.Context())
			if err != nil {
				t.Errorf("conn 2 read: %v", err)
				return
			}
			var req map[string]any
			if err := json.Unmarshal(data, &req); err != nil {
				t.Errorf("conn 2 decode: %v", err)
				return
			}
			restartTurnPrevID, _ = req["previous_response_id"].(string)
			restartTurnInstructions, _ = req["instructions"].(string)
			_, restartTurnHasInstructions = req["instructions"]
			sendWSEvents(r.Context(), t, conn, minimalWSCompletion("resp_3"))
		default:
			t.Errorf("unexpected connection %d", currentConn)
		}
	})
	defer ts.Close()

	p := &Provider{
		apiKey:  "test-key",
		baseURL: ts.URL,
		client:  ts.Client(),
		isCodex: true,
		ws:      newWSPool("test-key", ts.URL),
	}

	req1 := providers.CompleteRequest{
		Model: providers.ModelRef{ProviderID: "openai", ModelID: "gpt-5.4"},
		Messages: []message.Message{
			{Role: "system", Parts: []message.Part{message.TextPart{Text: "You are Codex."}}},
			{Role: "user", Parts: []message.Part{message.TextPart{Text: "Hi"}}},
		},
	}
	for _, err := range p.Complete(context.Background(), req1) {
		if err != nil {
			t.Fatalf("turn 1 error: %v", err)
		}
	}

	req2 := providers.CompleteRequest{
		Model: providers.ModelRef{ProviderID: "openai", ModelID: "gpt-5.4"},
		Messages: []message.Message{
			{Role: "system", Parts: []message.Part{message.TextPart{Text: "You are Codex."}}},
			{Role: "user", Parts: []message.Part{message.TextPart{Text: "Hi"}}},
			{Role: "assistant", ID: "resp_1", Parts: []message.Part{message.TextPart{Text: "Hello"}}},
			{Role: "user", Parts: []message.Part{message.TextPart{Text: "Again"}}},
		},
	}
	for _, err := range p.Complete(context.Background(), req2) {
		if err != nil {
			t.Fatalf("turn 2 error: %v", err)
		}
	}

	// Simulate agent-go restart: new provider, empty websocket pool, continuation history.
	restartedProvider := &Provider{
		apiKey:  "test-key",
		baseURL: ts.URL,
		client:  ts.Client(),
		isCodex: true,
		ws:      newWSPool("test-key", ts.URL),
	}

	restartReq := providers.CompleteRequest{
		Model: providers.ModelRef{ProviderID: "openai", ModelID: "gpt-5.4"},
		Messages: []message.Message{
			{Role: "system", Parts: []message.Part{message.TextPart{Text: "You are Codex."}}},
			{Role: "user", Parts: []message.Part{message.TextPart{Text: "Hi"}}},
			{Role: "assistant", ID: "resp_2", Parts: []message.Part{message.TextPart{Text: "Hello again"}}},
			{Role: "user", Parts: []message.Part{message.TextPart{Text: "Resume"}}},
		},
	}
	for _, err := range restartedProvider.Complete(context.Background(), restartReq) {
		if err != nil {
			t.Fatalf("restart turn error: %v", err)
		}
	}

	if firstTurnInstructions != "You are Codex." {
		t.Fatalf("first turn should include instructions, got %q", firstTurnInstructions)
	}
	if secondTurnHasInstructions {
		t.Fatal("reused-connection continuation should omit instructions")
	}
	if restartTurnPrevID != "" {
		t.Fatalf("fresh-connection continuation should drop previous_response_id, got %q", restartTurnPrevID)
	}
	if !restartTurnHasInstructions {
		t.Fatal("fresh-connection continuation should include instructions")
	}
	if restartTurnInstructions != "You are Codex." {
		t.Fatalf("fresh-connection continuation should include full instructions, got %q", restartTurnInstructions)
	}
}

func TestCompleteViaWebSocket_SendsStreamFalse(t *testing.T) {
	// WebSocket requests must NOT include "stream":true — streaming is implicit.
	ts := wsTestServer(t, func(conn *websocket.Conn, r *http.Request) {
		_, data, err := conn.Read(r.Context())
		if err != nil {
			return
		}
		var req map[string]any
		json.Unmarshal(data, &req)
		if _, hasStream := req["stream"]; hasStream {
			t.Error("WebSocket request must not include stream field")
		}
		sendWSEvents(r.Context(), t, conn, minimalWSCompletion("resp_1"))
	})
	defer ts.Close()

	p := &Provider{
		apiKey:  "test-key",
		baseURL: ts.URL,
		client:  ts.Client(),
		ws:      newWSPool("test-key", ts.URL),
	}
	req := providers.CompleteRequest{
		Model:    providers.ModelRef{ProviderID: "openai", ModelID: "gpt-4o"},
		Messages: []message.Message{{Role: "user", Parts: []message.Part{message.TextPart{Text: "Hi"}}}},
	}
	for _, err := range p.Complete(context.Background(), req) {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestCompleteViaWebSocket_AllowsLargeEvents(t *testing.T) {
	largeDelta := strings.Repeat("a", 33_000)

	ts := wsTestServer(t, func(conn *websocket.Conn, r *http.Request) {
		if _, _, err := conn.Read(r.Context()); err != nil {
			t.Errorf("read request: %v", err)
			return
		}
		sendWSEvents(r.Context(), t, conn, []map[string]any{
			{"type": "response.created", "response": map[string]any{"id": "resp_large", "model": "gpt-4o"}},
			{"type": "response.content_part.added", "part": map[string]any{"type": "output_text"}, "item_id": "msg_1"},
			{"type": "response.output_text.delta", "item_id": "msg_1", "delta": largeDelta},
			{"type": "response.output_text.done", "item_id": "msg_1"},
			{"type": "response.completed", "response": map[string]any{
				"status": "completed",
				"output": []map[string]any{{"type": "message"}},
				"usage": map[string]any{
					"input_tokens":          3,
					"input_tokens_details":  map[string]any{"cached_tokens": 0},
					"output_tokens":         1,
					"output_tokens_details": map[string]any{"reasoning_tokens": 0},
				},
			}},
		})
	})
	defer ts.Close()

	p := &Provider{
		apiKey:  "test-key",
		baseURL: ts.URL,
		client:  ts.Client(),
		ws:      newWSPool("test-key", ts.URL),
	}
	req := providers.CompleteRequest{
		Model:    providers.ModelRef{ProviderID: "openai", ModelID: "gpt-4o"},
		Messages: []message.Message{{Role: "user", Parts: []message.Part{message.TextPart{Text: "Hi"}}}},
	}

	var got strings.Builder
	for chunk, err := range p.Complete(context.Background(), req) {
		if err != nil {
			t.Fatalf("Complete error: %v", err)
		}
		if delta, ok := chunk.(message.TextDeltaChunk); ok {
			got.WriteString(delta.Delta)
		}
	}

	if got.String() != largeDelta {
		t.Fatalf("expected %d bytes of streamed text, got %d", len(largeDelta), got.Len())
	}
}

func TestCompleteViaWebSocket_TracksAndInjectsPreviousResponseID(t *testing.T) {
	// The server serves both turns on the same WebSocket connection.
	// Turn 1: client dials fresh (no assistant message in history → prevRespID="").
	// Turn 2: client reuses the pooled connection (assistant message with ID="resp_1"
	//         → prevRespID="resp_1" → pool hit → same conn).
	var (
		firstReqPrevID     string
		secondReqPrevID    string
		firstReqHasTools   bool
		secondReqHasTools  bool
		firstReqInputLen   int
		secondReqInputLen  int
		secondReqUserInput string
	)
	wsTools := []providers.ToolDefinition{{
		Name:        "get_weather",
		Description: "Get current weather",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`),
	}}

	ts := wsTestServer(t, func(conn *websocket.Conn, r *http.Request) {
		for turn := 1; turn <= 2; turn++ {
			_, data, err := conn.Read(r.Context())
			if err != nil {
				return
			}
			var req map[string]any
			json.Unmarshal(data, &req)

			inputItems, _ := req["input"].([]any)
			_, hasTools := req["tools"]
			switch turn {
			case 1:
				firstReqPrevID, _ = req["previous_response_id"].(string)
				firstReqHasTools = hasTools
				firstReqInputLen = len(inputItems)
			case 2:
				secondReqPrevID, _ = req["previous_response_id"].(string)
				secondReqHasTools = hasTools
				secondReqInputLen = len(inputItems)
				if len(inputItems) > 0 {
					if item, ok := inputItems[0].(map[string]any); ok {
						secondReqUserInput, _ = item["content"].(string)
					}
				}
			}
			sendWSEvents(r.Context(), t, conn, minimalWSCompletion(fmt.Sprintf("resp_%d", turn)))
		}
	})
	defer ts.Close()

	p := &Provider{
		apiKey:  "test-key",
		baseURL: ts.URL,
		client:  ts.Client(),
		ws:      newWSPool("test-key", ts.URL),
	}

	// First turn: no assistant messages in history → prevRespID = "".
	req1 := providers.CompleteRequest{
		Model:    providers.ModelRef{ProviderID: "openai", ModelID: "gpt-4o"},
		Tools:    wsTools,
		Messages: []message.Message{{Role: "user", Parts: []message.Part{message.TextPart{Text: "Hi"}}}},
	}
	for chunk, err := range p.Complete(context.Background(), req1) {
		if err != nil {
			t.Fatalf("turn 1: %v", err)
		}
		_ = chunk
	}

	// Second turn: history includes the assistant response from turn 1.
	// lastAssistantID() returns "resp_1", which triggers a pool hit and reuses the connection.
	req2 := providers.CompleteRequest{
		Model: providers.ModelRef{ProviderID: "openai", ModelID: "gpt-4o"},
		Tools: wsTools,
		Messages: []message.Message{
			{Role: "user", Parts: []message.Part{message.TextPart{Text: "Hi"}}},
			{Role: "assistant", ID: "resp_1", Parts: []message.Part{message.TextPart{Text: "Hello"}}},
			{Role: "user", Parts: []message.Part{message.TextPart{Text: "Again"}}},
		},
	}
	for chunk, err := range p.Complete(context.Background(), req2) {
		if err != nil {
			t.Fatalf("turn 2: %v", err)
		}
		_ = chunk
	}

	if firstReqPrevID != "" {
		t.Errorf("first request should have no previous_response_id, got %q", firstReqPrevID)
	}
	if !firstReqHasTools {
		t.Error("first request should include tools")
	}
	if firstReqInputLen != 1 {
		t.Errorf("first request should include full input (1 item), got %d", firstReqInputLen)
	}
	if secondReqPrevID != "resp_1" {
		t.Errorf("second request should carry previous_response_id=%q, got %q", "resp_1", secondReqPrevID)
	}
	if secondReqHasTools {
		t.Error("continuation request should omit tools")
	}
	if secondReqInputLen != 1 {
		t.Errorf("continuation request should include only incremental input (1 item), got %d", secondReqInputLen)
	}
	if secondReqUserInput != "Again" {
		t.Errorf("continuation request should send only the new user message, got %q", secondReqUserInput)
	}
}

func TestMessagesAfterAssistantID(t *testing.T) {
	base := []message.Message{
		{Role: "user"},
		{Role: "assistant", ID: "client-msg", ProviderResponseID: "resp_1"},
		{Role: "user"},
	}

	t.Run("returns trailing messages after matching assistant", func(t *testing.T) {
		got := messagesAfterAssistantID(base, "resp_1")
		if len(got) != 1 {
			t.Fatalf("expected 1 trailing message, got %d", len(got))
		}
		if got[0].Role != "user" {
			t.Fatalf("expected trailing message role=user, got %q", got[0].Role)
		}
	})

	t.Run("returns full history when assistant response is missing", func(t *testing.T) {
		got := messagesAfterAssistantID(base, "resp_missing")
		if len(got) != len(base) {
			t.Fatalf("expected full history length %d, got %d", len(base), len(got))
		}
	})

	t.Run("returns empty slice when matched assistant is last", func(t *testing.T) {
		msgs := []message.Message{
			{Role: "user"},
			{Role: "assistant", ID: "resp_last"},
		}
		got := messagesAfterAssistantID(msgs, "resp_last")
		if len(got) != 0 {
			t.Fatalf("expected no trailing messages, got %d", len(got))
		}
	})
}

func TestCompleteViaWebSocket_OmitsPreviousResponseIDAfterCompaction(t *testing.T) {
	var gotPrevID string
	ts := wsTestServer(t, func(conn *websocket.Conn, r *http.Request) {
		_, data, err := conn.Read(r.Context())
		if err != nil {
			t.Errorf("read request: %v", err)
			return
		}
		var req map[string]any
		if err := json.Unmarshal(data, &req); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		gotPrevID, _ = req["previous_response_id"].(string)
		sendWSEvents(r.Context(), t, conn, minimalWSCompletion("resp_after_compaction"))
	})
	defer ts.Close()

	p := &Provider{
		apiKey:  "test-key",
		baseURL: ts.URL,
		client:  ts.Client(),
		ws:      newWSPool("test-key", ts.URL),
	}

	req := providers.CompleteRequest{
		Model: providers.ModelRef{ProviderID: "openai", ModelID: "gpt-4o"},
		Messages: []message.Message{
			{Role: "system", Parts: []message.Part{message.TextPart{Text: "You are helpful."}}},
			{Role: "user", Parts: []message.Part{message.TextPart{Text: "<conversation_summary>\nEarlier turns were compacted.\n</conversation_summary>\n\nThe above is a summary of our earlier conversation. Continue from where we left off."}}},
			{Role: "user", Parts: []message.Part{message.TextPart{Text: "Continue from the summary."}}},
		},
	}
	for _, err := range p.Complete(context.Background(), req) {
		if err != nil {
			t.Fatalf("completion error: %v", err)
		}
	}

	if gotPrevID != "" {
		t.Fatalf("compacted history should not carry previous_response_id, got %q", gotPrevID)
	}
}

func TestCompleteViaWebSocket_DropsPreviousResponseIDAfterCompactionSummaryOnFreshConn(t *testing.T) {
	var gotPrevID string
	ts := wsTestServer(t, func(conn *websocket.Conn, r *http.Request) {
		_, data, err := conn.Read(r.Context())
		if err != nil {
			t.Errorf("read request: %v", err)
			return
		}
		var req map[string]any
		if err := json.Unmarshal(data, &req); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		gotPrevID, _ = req["previous_response_id"].(string)
		sendWSEvents(r.Context(), t, conn, minimalWSCompletion("resp_next"))
	})
	defer ts.Close()

	p := &Provider{
		apiKey:  "test-key",
		baseURL: ts.URL,
		client:  ts.Client(),
		ws:      newWSPool("test-key", ts.URL),
	}

	req := providers.CompleteRequest{
		Model: providers.ModelRef{ProviderID: "openai", ModelID: "gpt-4o"},
		Messages: []message.Message{
			{Role: "system", Parts: []message.Part{message.TextPart{Text: "You are helpful."}}},
			{Role: "user", Parts: []message.Part{message.TextPart{Text: "<conversation_summary>\nEarlier turns were compacted.\n</conversation_summary>\n\nThe above is a summary of our earlier conversation. Continue from where we left off."}}},
			{Role: "assistant", ID: "client-msg-id", ProviderResponseID: "resp_after_compaction", Parts: []message.Part{message.TextPart{Text: "Latest post-compaction reply."}}},
			{Role: "user", Parts: []message.Part{message.TextPart{Text: "Continue from the latest reply."}}},
		},
	}
	for _, err := range p.Complete(context.Background(), req) {
		if err != nil {
			t.Fatalf("completion error: %v", err)
		}
	}

	if gotPrevID != "" {
		t.Fatalf("fresh connection after compaction summary should not carry previous_response_id, got %q", gotPrevID)
	}
}

func TestCompleteViaWebSocket_FallsBackToFullHistoryAfterStalePooledConn(t *testing.T) {
	var (
		mu               sync.Mutex
		connCount        int
		freshReqPrev     string
		freshReqHasTools bool
		freshReqInputLen int
		secondTurnErr    error
		retryEvents      []transport.RetryEvent
	)
	wsTools := []providers.ToolDefinition{{
		Name:        "get_weather",
		Description: "Get current weather",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`),
	}}

	ts := wsTestServer(t, func(conn *websocket.Conn, r *http.Request) {
		mu.Lock()
		connCount++
		currentConn := connCount
		mu.Unlock()

		switch currentConn {
		case 1:
			if _, _, err := conn.Read(r.Context()); err != nil {
				t.Errorf("conn 1 turn 1 read: %v", err)
				return
			}
			sendWSEvents(r.Context(), t, conn, minimalWSCompletion("resp_1"))

			if _, _, err := conn.Read(r.Context()); err != nil {
				t.Errorf("conn 1 turn 2 read: %v", err)
				return
			}
			if err := conn.Close(websocket.StatusInternalError, "keepalive ping timeout"); err != nil {
				t.Errorf("conn 1 close: %v", err)
			}
		case 2:
			_, data, err := conn.Read(r.Context())
			if err != nil {
				t.Errorf("conn 2 read: %v", err)
				return
			}
			var req map[string]any
			if err := json.Unmarshal(data, &req); err != nil {
				t.Errorf("conn 2 decode: %v", err)
				return
			}
			freshReqPrev, _ = req["previous_response_id"].(string)
			_, freshReqHasTools = req["tools"]
			inputItems, _ := req["input"].([]any)
			freshReqInputLen = len(inputItems)
			sendWSEvents(r.Context(), t, conn, minimalWSCompletion("resp_2"))
		default:
			t.Errorf("unexpected connection %d", currentConn)
		}
	})
	defer ts.Close()

	p := &Provider{
		apiKey:  "test-key",
		baseURL: ts.URL,
		client:  ts.Client(),
		ws:      newWSPool("test-key", ts.URL),
	}

	retryCtx := transport.WithRetryObserver(context.Background(), func(event transport.RetryEvent) {
		mu.Lock()
		defer mu.Unlock()
		retryEvents = append(retryEvents, event)
	})

	req1 := providers.CompleteRequest{
		Model:    providers.ModelRef{ProviderID: "openai", ModelID: "gpt-4o"},
		Messages: []message.Message{{Role: "user", Parts: []message.Part{message.TextPart{Text: "Hi"}}}},
	}
	for _, err := range p.Complete(retryCtx, req1) {
		if err != nil {
			t.Fatalf("turn 1: %v", err)
		}
	}

	req2 := providers.CompleteRequest{
		Model: providers.ModelRef{ProviderID: "openai", ModelID: "gpt-4o"},
		Tools: wsTools,
		Messages: []message.Message{
			{Role: "system", Parts: []message.Part{message.TextPart{Text: "You are helpful."}}},
			{Role: "user", Parts: []message.Part{message.TextPart{Text: "Hi"}}},
			{Role: "assistant", ID: "resp_1", Parts: []message.Part{message.TextPart{Text: "Hello"}}},
			{Role: "user", Parts: []message.Part{message.TextPart{Text: "Again"}}},
		},
	}
	for _, err := range p.Complete(retryCtx, req2) {
		if err != nil {
			secondTurnErr = err
		}
	}

	if secondTurnErr != nil {
		t.Fatalf("turn 2 should recover via full-history fallback, got %v", secondTurnErr)
	}
	if connCount != 2 {
		t.Fatalf("continuation failure should fallback on a fresh websocket, got %d connections", connCount)
	}
	if len(retryEvents) != 0 {
		t.Fatalf("continuation fallback should not emit retry events for the first stale pooled failure, got %d", len(retryEvents))
	}
	if freshReqPrev != "" {
		t.Errorf("fallback request should drop previous_response_id, got %q", freshReqPrev)
	}
	if !freshReqHasTools {
		t.Error("fallback request should restore full request tools")
	}
	if freshReqInputLen != 3 {
		t.Errorf("fallback request should restore full non-system input history (3 items), got %d", freshReqInputLen)
	}
}

func TestCompleteViaWebSocket_RetriesAfterImmediateContinuationFallbackFails(t *testing.T) {
	var (
		mu               sync.Mutex
		connCount        int
		requestCount     int
		finalReqPrev     string
		finalReqHasTools bool
		finalReqInputLen int
		secondTurnErr    error
		retryEvents      []transport.RetryEvent
	)
	wsTools := []providers.ToolDefinition{{
		Name:        "get_weather",
		Description: "Get current weather",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`),
	}}

	ts := wsTestServer(t, func(conn *websocket.Conn, r *http.Request) {
		mu.Lock()
		connCount++
		currentConn := connCount
		mu.Unlock()

		switch currentConn {
		case 1:
			if _, _, err := conn.Read(r.Context()); err != nil {
				t.Errorf("conn 1 turn 1 read: %v", err)
				return
			}
			requestCount++
			sendWSEvents(r.Context(), t, conn, minimalWSCompletion("resp_1"))

			if _, _, err := conn.Read(r.Context()); err != nil {
				t.Errorf("conn 1 turn 2 read: %v", err)
				return
			}
			requestCount++
			if err := conn.Close(websocket.StatusInternalError, "keepalive ping timeout"); err != nil {
				t.Errorf("conn 1 close: %v", err)
			}
		case 2:
			if _, _, err := conn.Read(r.Context()); err != nil {
				t.Errorf("conn 2 read: %v", err)
				return
			}
			requestCount++
			if err := conn.Close(websocket.StatusInternalError, "transient network issue"); err != nil {
				t.Errorf("conn 2 close: %v", err)
			}
		case 3:
			_, data, err := conn.Read(r.Context())
			if err != nil {
				t.Errorf("conn 3 read: %v", err)
				return
			}
			requestCount++
			var req map[string]any
			if err := json.Unmarshal(data, &req); err != nil {
				t.Errorf("conn 3 decode: %v", err)
				return
			}
			finalReqPrev, _ = req["previous_response_id"].(string)
			_, finalReqHasTools = req["tools"]
			inputItems, _ := req["input"].([]any)
			finalReqInputLen = len(inputItems)
			sendWSEvents(r.Context(), t, conn, minimalWSCompletion("resp_2"))
		default:
			t.Errorf("unexpected connection %d", currentConn)
		}
	})
	defer ts.Close()

	p := &Provider{
		apiKey:  "test-key",
		baseURL: ts.URL,
		client:  ts.Client(),
		ws:      newWSPool("test-key", ts.URL),
	}

	retryCtx := transport.WithRetryObserver(context.Background(), func(event transport.RetryEvent) {
		mu.Lock()
		defer mu.Unlock()
		retryEvents = append(retryEvents, event)
	})

	req1 := providers.CompleteRequest{
		Model:    providers.ModelRef{ProviderID: "openai", ModelID: "gpt-4o"},
		Messages: []message.Message{{Role: "user", Parts: []message.Part{message.TextPart{Text: "Hi"}}}},
	}
	for _, err := range p.Complete(retryCtx, req1) {
		if err != nil {
			t.Fatalf("turn 1: %v", err)
		}
	}

	req2 := providers.CompleteRequest{
		Model: providers.ModelRef{ProviderID: "openai", ModelID: "gpt-4o"},
		Tools: wsTools,
		Messages: []message.Message{
			{Role: "system", Parts: []message.Part{message.TextPart{Text: "You are helpful."}}},
			{Role: "user", Parts: []message.Part{message.TextPart{Text: "Hi"}}},
			{Role: "assistant", ID: "resp_1", Parts: []message.Part{message.TextPart{Text: "Hello"}}},
			{Role: "user", Parts: []message.Part{message.TextPart{Text: "Again"}}},
		},
	}
	for _, err := range p.Complete(retryCtx, req2) {
		if err != nil {
			secondTurnErr = err
		}
	}

	if secondTurnErr != nil {
		t.Fatalf("turn 2 should recover after retrying the failed full-body fallback, got %v", secondTurnErr)
	}
	if connCount != 3 {
		t.Fatalf("expected 3 websocket connections (saved conn + immediate fallback + delayed retry), got %d", connCount)
	}
	if requestCount != 4 {
		t.Fatalf("expected 4 websocket requests across both turns, got %d", requestCount)
	}
	if len(retryEvents) != 1 {
		t.Fatalf("expected 1 retry event after the immediate fallback failed, got %d", len(retryEvents))
	}
	if retryEvents[0].Attempt != 1 || retryEvents[0].MaxRetries != wsRetryMaxRetries {
		t.Fatalf("unexpected retry event metadata: %+v", retryEvents[0])
	}
	if retryEvents[0].Err == nil {
		t.Fatal("expected retry event to include websocket failure")
	}
	if finalReqPrev != "" {
		t.Errorf("delayed retry should remain a full request without previous_response_id, got %q", finalReqPrev)
	}
	if !finalReqHasTools {
		t.Error("delayed retry should preserve full request tools")
	}
	if finalReqInputLen != 3 {
		t.Errorf("delayed retry should preserve full non-system input history (3 items), got %d", finalReqInputLen)
	}
}

func TestCompleteViaWebSocket_RetriesFreshInitialRequestFailure(t *testing.T) {
	var (
		mu                sync.Mutex
		connCount         int
		firstReqHasTools  bool
		secondReqHasTools bool
		firstReqPrevID    string
		secondReqPrevID   string
		firstReqInputLen  int
		secondReqInputLen int
	)
	wsTools := []providers.ToolDefinition{{
		Name:        "echo",
		Description: "Echo text",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}`),
	}}

	ts := wsTestServer(t, func(conn *websocket.Conn, r *http.Request) {
		mu.Lock()
		connCount++
		currentConn := connCount
		mu.Unlock()

		_, data, err := conn.Read(r.Context())
		if err != nil {
			t.Errorf("conn %d read: %v", currentConn, err)
			return
		}
		var req map[string]any
		if err := json.Unmarshal(data, &req); err != nil {
			t.Errorf("conn %d decode: %v", currentConn, err)
			return
		}
		inputItems, _ := req["input"].([]any)
		_, hasTools := req["tools"]
		prevID, _ := req["previous_response_id"].(string)

		switch currentConn {
		case 1:
			firstReqHasTools = hasTools
			firstReqPrevID = prevID
			firstReqInputLen = len(inputItems)
			if err := conn.Close(websocket.StatusInternalError, "transient network issue"); err != nil {
				t.Errorf("conn 1 close: %v", err)
			}
		case 2:
			secondReqHasTools = hasTools
			secondReqPrevID = prevID
			secondReqInputLen = len(inputItems)
			sendWSEvents(r.Context(), t, conn, minimalWSCompletion("resp_retry_ok"))
		default:
			t.Errorf("unexpected connection %d", currentConn)
		}
	})
	defer ts.Close()

	p := &Provider{
		apiKey:  "test-key",
		baseURL: ts.URL,
		client:  ts.Client(),
		ws:      newWSPool("test-key", ts.URL),
	}

	req := providers.CompleteRequest{
		Model:    providers.ModelRef{ProviderID: "openai", ModelID: "gpt-4o"},
		Tools:    wsTools,
		Messages: []message.Message{{Role: "user", Parts: []message.Part{message.TextPart{Text: "Hello"}}}},
	}
	var gotErr error
	for _, err := range p.Complete(context.Background(), req) {
		if err != nil {
			gotErr = err
		}
	}

	if gotErr != nil {
		t.Fatalf("expected retry to recover from initial websocket failure, got %v", gotErr)
	}
	if connCount != 2 {
		t.Fatalf("expected 2 websocket connections (initial + retry), got %d", connCount)
	}
	if firstReqPrevID != "" || secondReqPrevID != "" {
		t.Fatalf("initial-request retries must not carry previous_response_id, got first=%q second=%q", firstReqPrevID, secondReqPrevID)
	}
	if !firstReqHasTools || !secondReqHasTools {
		t.Fatal("initial-request retries should keep full request tools")
	}
	if firstReqInputLen != 1 || secondReqInputLen != 1 {
		t.Fatalf("initial-request retries should keep full input (1 item), got first=%d second=%d", firstReqInputLen, secondReqInputLen)
	}
}

func TestCompleteViaWebSocket_RetriesInitialDialFailure(t *testing.T) {
	var (
		mu                sync.Mutex
		attemptCount      int
		requestCount      int
		firstReqHasTools  bool
		secondReqHasTools bool
		firstReqPrevID    string
		secondReqPrevID   string
		firstReqInputLen  int
		secondReqInputLen int
		retryEvents       []transport.RetryEvent
	)
	wsTools := []providers.ToolDefinition{{
		Name:        "echo",
		Description: "Echo text",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}`),
	}}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Errorf("unexpected path %q", r.URL.Path)
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		mu.Lock()
		attemptCount++
		currentAttempt := attemptCount
		mu.Unlock()

		if currentAttempt == 1 {
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Fatal("response writer does not support hijacking")
			}
			conn, _, err := hj.Hijack()
			if err != nil {
				t.Fatalf("hijack: %v", err)
			}
			_ = conn.Close()
			return
		}

		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Errorf("websocket accept: %v", err)
			return
		}
		defer conn.CloseNow()

		_, data, err := conn.Read(r.Context())
		if err != nil {
			t.Errorf("conn read: %v", err)
			return
		}
		var req map[string]any
		if err := json.Unmarshal(data, &req); err != nil {
			t.Errorf("decode: %v", err)
			return
		}
		inputItems, _ := req["input"].([]any)
		_, hasTools := req["tools"]
		prevID, _ := req["previous_response_id"].(string)

		mu.Lock()
		requestCount++
		if requestCount == 1 {
			firstReqHasTools = hasTools
			firstReqPrevID = prevID
			firstReqInputLen = len(inputItems)
		} else {
			secondReqHasTools = hasTools
			secondReqPrevID = prevID
			secondReqInputLen = len(inputItems)
		}
		mu.Unlock()

		sendWSEvents(r.Context(), t, conn, minimalWSCompletion("resp_retry_ok"))
	}))
	defer ts.Close()

	p := &Provider{
		apiKey:  "test-key",
		baseURL: ts.URL,
		client:  ts.Client(),
		ws:      newWSPool("test-key", ts.URL),
	}

	ctx := transport.WithRetryObserver(context.Background(), func(event transport.RetryEvent) {
		mu.Lock()
		defer mu.Unlock()
		retryEvents = append(retryEvents, event)
	})

	req := providers.CompleteRequest{
		Model:    providers.ModelRef{ProviderID: "openai", ModelID: "gpt-4o"},
		Tools:    wsTools,
		Messages: []message.Message{{Role: "user", Parts: []message.Part{message.TextPart{Text: "Hello"}}}},
	}
	var gotErr error
	for _, err := range p.Complete(ctx, req) {
		if err != nil {
			gotErr = err
		}
	}

	if gotErr != nil {
		t.Fatalf("expected retry to recover from initial websocket dial failure, got %v", gotErr)
	}
	if attemptCount != 2 {
		t.Fatalf("expected 2 connection attempts (initial dial + retry), got %d", attemptCount)
	}
	if requestCount != 1 {
		t.Fatalf("expected only retried connection to send a websocket request, got %d requests", requestCount)
	}
	if len(retryEvents) != 1 {
		t.Fatalf("expected 1 retry event, got %d", len(retryEvents))
	}
	if retryEvents[0].Attempt != 1 || retryEvents[0].MaxRetries != wsRetryMaxRetries {
		t.Fatalf("unexpected retry event metadata: %+v", retryEvents[0])
	}
	if retryEvents[0].Err == nil {
		t.Fatal("expected retry event to include dial error")
	}
	if firstReqPrevID != "" || secondReqPrevID != "" {
		t.Fatalf("initial-request retries must not carry previous_response_id, got first=%q second=%q", firstReqPrevID, secondReqPrevID)
	}
	if !firstReqHasTools {
		t.Fatal("retried initial request should keep full request tools")
	}
	if firstReqInputLen != 1 {
		t.Fatalf("retried initial request should keep full input (1 item), got %d", firstReqInputLen)
	}
	if secondReqHasTools || secondReqInputLen != 0 {
		t.Fatalf("expected only one websocket request capture, got secondReqHasTools=%v secondReqInputLen=%d", secondReqHasTools, secondReqInputLen)
	}
}

func TestCompleteViaWebSocket_FallsBackToFullHistoryWhenCacheIsGone(t *testing.T) {
	var (
		mu                      sync.Mutex
		connCount               int
		continuationReqPrevID   string
		continuationHasTools    bool
		continuationInputLen    int
		fallbackReqPrev         string
		fallbackReqHasTools     bool
		fallbackReqInputLen     int
		fallbackReqFirstPrompt  string
		fallbackInstructions    string
		fallbackHasInstructions bool
		secondTurnErr           error
		retryEvents             []transport.RetryEvent
	)
	wsTools := []providers.ToolDefinition{{
		Name:        "get_weather",
		Description: "Get current weather",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`),
	}}

	ts := wsTestServer(t, func(conn *websocket.Conn, r *http.Request) {
		mu.Lock()
		connCount++
		currentConn := connCount
		mu.Unlock()

		switch currentConn {
		case 1:
			if _, _, err := conn.Read(r.Context()); err != nil {
				t.Errorf("conn 1 turn 1 read: %v", err)
				return
			}
			sendWSEvents(r.Context(), t, conn, minimalWSCompletion("resp_1"))

			_, data, err := conn.Read(r.Context())
			if err != nil {
				t.Errorf("conn 1 turn 2 read: %v", err)
				return
			}
			var req map[string]any
			if err := json.Unmarshal(data, &req); err != nil {
				t.Errorf("conn 1 turn 2 decode: %v", err)
				return
			}
			continuationReqPrevID, _ = req["previous_response_id"].(string)
			_, continuationHasTools = req["tools"]
			continuationInput, _ := req["input"].([]any)
			continuationInputLen = len(continuationInput)

			if err := conn.Write(r.Context(), websocket.MessageText, []byte(`{"type":"error","error":{"message":"Previous response with id 'resp_1' not found.","code":"previous_response_not_found"}}`)); err != nil {
				t.Errorf("conn 1 turn 2 write error event: %v", err)
			}
		case 2:
			_, data, err := conn.Read(r.Context())
			if err != nil {
				t.Errorf("conn 2 read: %v", err)
				return
			}
			var req map[string]any
			if err := json.Unmarshal(data, &req); err != nil {
				t.Errorf("conn 2 decode: %v", err)
				return
			}
			fallbackReqPrev, _ = req["previous_response_id"].(string)
			_, fallbackReqHasTools = req["tools"]
			fallbackInput, _ := req["input"].([]any)
			fallbackReqInputLen = len(fallbackInput)
			fallbackInstructions, _ = req["instructions"].(string)
			_, fallbackHasInstructions = req["instructions"]
			if len(fallbackInput) > 0 {
				if firstInput, ok := fallbackInput[0].(map[string]any); ok {
					fallbackReqFirstPrompt, _ = firstInput["content"].(string)
				}
			}
			sendWSEvents(r.Context(), t, conn, minimalWSCompletion("resp_2"))
		default:
			t.Errorf("unexpected connection %d", currentConn)
		}
	})
	defer ts.Close()

	p := &Provider{
		apiKey:  "test-key",
		baseURL: ts.URL,
		client:  ts.Client(),
		ws:      newWSPool("test-key", ts.URL),
	}

	retryCtx := transport.WithRetryObserver(context.Background(), func(event transport.RetryEvent) {
		mu.Lock()
		defer mu.Unlock()
		retryEvents = append(retryEvents, event)
	})

	req1 := providers.CompleteRequest{
		Model:    providers.ModelRef{ProviderID: "openai", ModelID: "gpt-4o"},
		Messages: []message.Message{{Role: "user", Parts: []message.Part{message.TextPart{Text: "Hi"}}}},
	}
	for _, err := range p.Complete(retryCtx, req1) {
		if err != nil {
			t.Fatalf("turn 1: %v", err)
		}
	}

	req2 := providers.CompleteRequest{
		Model: providers.ModelRef{ProviderID: "openai", ModelID: "gpt-4o"},
		Tools: wsTools,
		Messages: []message.Message{
			{Role: "system", Parts: []message.Part{message.TextPart{Text: "You are helpful."}}},
			{Role: "user", Parts: []message.Part{message.TextPart{Text: "Hi"}}},
			{Role: "assistant", ID: "resp_1", Parts: []message.Part{message.TextPart{Text: "Hello"}}},
			{Role: "user", Parts: []message.Part{message.TextPart{Text: "Again"}}},
		},
	}
	for _, err := range p.Complete(retryCtx, req2) {
		if err != nil {
			secondTurnErr = err
		}
	}

	if secondTurnErr != nil {
		t.Fatalf("turn 2 should recover via full-history fallback, got %v", secondTurnErr)
	}
	if continuationReqPrevID != "resp_1" {
		t.Errorf("continuation request should include previous_response_id=%q, got %q", "resp_1", continuationReqPrevID)
	}
	if continuationHasTools {
		t.Error("continuation request should omit tools")
	}
	if continuationInputLen != 1 {
		t.Errorf("continuation request should include only incremental input (1 item), got %d", continuationInputLen)
	}
	if fallbackReqPrev != "" {
		t.Errorf("fallback request should drop previous_response_id, got %q", fallbackReqPrev)
	}
	if !fallbackReqHasTools {
		t.Error("fallback request should restore full request tools")
	}
	if fallbackReqInputLen != 3 {
		t.Errorf("fallback request should restore full non-system input history (3 items), got %d", fallbackReqInputLen)
	}
	if fallbackReqFirstPrompt != "Hi" {
		t.Errorf("fallback request should begin with original user message, got %q", fallbackReqFirstPrompt)
	}
	if !fallbackHasInstructions || fallbackInstructions != "You are helpful." {
		t.Fatalf("fallback request should restore full instructions, got present=%v value=%q", fallbackHasInstructions, fallbackInstructions)
	}
	if connCount != 2 {
		t.Fatalf("previous_response_not_found should trigger one fresh websocket fallback, got %d connections", connCount)
	}
	if len(retryEvents) != 0 {
		t.Fatalf("previous_response_not_found fallback should not emit retry events for the first continuation fallback, got %d", len(retryEvents))
	}
}

func TestCompleteViaWebSocket_InvalidatesOnResponseFailed(t *testing.T) {
	ts := wsTestServer(t, func(conn *websocket.Conn, r *http.Request) {
		if _, _, err := conn.Read(r.Context()); err != nil {
			return
		}
		conn.Write(r.Context(), websocket.MessageText, //nolint:errcheck
			[]byte(`{"type":"response.failed","response":{"error":{"message":"server error"}}}`))
	})
	defer ts.Close()

	p := &Provider{
		apiKey:  "test-key",
		baseURL: ts.URL,
		client:  ts.Client(),
		ws:      newWSPool("test-key", ts.URL),
	}
	req := providers.CompleteRequest{
		Model:    providers.ModelRef{ProviderID: "openai", ModelID: "gpt-4o"},
		Messages: []message.Message{{Role: "user", Parts: []message.Part{message.TextPart{Text: "Hi"}}}},
	}

	var gotErr error
	for chunk, err := range p.Complete(context.Background(), req) {
		if err != nil {
			gotErr = err
		}
		_ = chunk
	}

	if gotErr == nil {
		t.Error("expected error from response.failed event")
	}
	// After a failed response the connection must not be in the pool.
	p.ws.mu.Lock()
	poolSize := len(p.ws.byPrev)
	p.ws.mu.Unlock()
	if poolSize != 0 {
		t.Errorf("expected empty pool after response.failed, got %d entries", poolSize)
	}
}

func TestWSPool_CheckoutCheckin(t *testing.T) {
	t.Run("empty prevRespID always returns nil", func(t *testing.T) {
		pool := newWSPool("key", "https://api.openai.com/v1")
		if got := pool.checkout(""); got != nil {
			t.Error("checkout('') should return nil")
		}
	})

	t.Run("miss returns nil", func(t *testing.T) {
		pool := newWSPool("key", "https://api.openai.com/v1")
		if got := pool.checkout("resp_xyz"); got != nil {
			t.Error("checkout on empty pool should return nil")
		}
	})

	t.Run("checkin then checkout", func(t *testing.T) {
		pool := newWSPool("key", "https://api.openai.com/v1")
		pc := &pooledConn{conn: nil}
		pool.checkin("resp_A", pc)

		got := pool.checkout("resp_A")
		if got != pc {
			t.Error("checkout after checkin should return the same pooledConn")
		}
		if pool.checkout("resp_A") != nil {
			t.Error("second checkout should return nil (already removed)")
		}
	})

	t.Run("checkin with empty newRespID does not store the connection", func(t *testing.T) {
		pool := newWSPool("key", "https://api.openai.com/v1")
		pool.checkin("", &pooledConn{conn: nil})
		if len(pool.byPrev) != 0 {
			t.Errorf("expected empty pool, got %d entries", len(pool.byPrev))
		}
	})

	t.Run("evicts idle pooled connections", func(t *testing.T) {
		pool := newWSPool("key", "https://api.openai.com/v1")
		now := time.Now()
		pool.byPrev["resp_old"] = &pooledConn{lastUsedAt: now.Add(-wsIdleTTL - time.Second)}
		pool.byPrev["resp_recent"] = &pooledConn{lastUsedAt: now.Add(-wsIdleTTL + time.Second)}

		pool.evictIdle(now)

		if _, ok := pool.byPrev["resp_old"]; ok {
			t.Error("expected idle connection to be evicted")
		}
		if _, ok := pool.byPrev["resp_recent"]; !ok {
			t.Error("expected non-idle connection to be retained")
		}
	})

	t.Run("checkout evicts stale target before lookup", func(t *testing.T) {
		pool := newWSPool("key", "https://api.openai.com/v1")
		pool.byPrev["resp_old"] = &pooledConn{lastUsedAt: time.Now().Add(-wsIdleTTL - time.Second)}

		if got := pool.checkout("resp_old"); got != nil {
			t.Error("expected idle connection to be evicted before checkout")
		}
		if len(pool.byPrev) != 0 {
			t.Errorf("expected empty pool after eviction, got %d entries", len(pool.byPrev))
		}
	})
}

func TestLastAssistantID(t *testing.T) {
	cases := []struct {
		name string
		msgs []message.Message
		want string
	}{
		{
			name: "empty history",
			msgs: nil,
			want: "",
		},
		{
			name: "no assistant message",
			msgs: []message.Message{
				{Role: "user", ID: "u1"},
			},
			want: "",
		},
		{
			name: "one assistant message",
			msgs: []message.Message{
				{Role: "user", ID: "u1"},
				{Role: "assistant", ID: "resp_A"},
			},
			want: "resp_A",
		},
		{
			name: "prefers preserved provider response ID",
			msgs: []message.Message{
				{Role: "user", ID: "u1"},
				{Role: "assistant", ID: "17c3c609fb1c28ca", ProviderResponseID: "resp_A"},
			},
			want: "resp_A",
		},
		{
			name: "ignores client generated assistant ID without provider response ID",
			msgs: []message.Message{
				{Role: "assistant", ID: "17c3c609fb1c28ca"},
			},
			want: "",
		},
		{
			name: "returns last assistant when multiple",
			msgs: []message.Message{
				{Role: "user", ID: "u1"},
				{Role: "assistant", ID: "resp_A"},
				{Role: "tool", ID: "t1"},
				{Role: "assistant", ID: "resp_B"},
				{Role: "user", ID: "u2"},
			},
			want: "resp_B",
		},
		{
			name: "assistant after tool results",
			msgs: []message.Message{
				{Role: "user", ID: "u1"},
				{Role: "assistant", ID: "resp_A"},
				{Role: "tool", ID: "t1"},
				{Role: "user", ID: "u2"},
			},
			want: "resp_A",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := lastAssistantID(tc.msgs)
			if got != tc.want {
				t.Errorf("lastAssistantID = %q, want %q", got, tc.want)
			}
		})
	}
}
