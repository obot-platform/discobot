//go:build integration

package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/obot-platform/discobot/agent-go/message"
	"github.com/obot-platform/discobot/agent-go/providers"
)

// readAPIKey reads the OpenAI API key from OPENAI_API_KEY env var or key.txt.
func readAPIKey(t *testing.T) string {
	t.Helper()
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		return key
	}
	// Try reading from key.txt at repo root.
	for _, path := range []string{"../../../key.txt", "../../../../key.txt"} {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "export OPENAI_API_KEY=") {
				key := strings.TrimPrefix(line, "export OPENAI_API_KEY=")
				key = strings.Trim(key, `"'`)
				if key != "" {
					return key
				}
			}
		}
	}
	t.Skip("OPENAI_API_KEY not set and key.txt not found")
	return ""
}

// readCodexWebSocketConfig returns config for real Codex websocket integration
// tests and skips when required environment is not available.
func readCodexWebSocketConfig(t *testing.T) providers.Config {
	t.Helper()

	apiKey := readAPIKey(t)
	baseURL := strings.TrimSpace(os.Getenv("OPENAI_API_BASE"))
	if baseURL == "" || !strings.Contains(baseURL, "chatgpt.com") {
		t.Skip("OPENAI_API_BASE must point to chatgpt.com Codex backend for this integration test")
	}
	accountID := strings.TrimSpace(os.Getenv("CHATGPT_ACCOUNT_ID"))
	if accountID == "" {
		t.Skip("CHATGPT_ACCOUNT_ID is required for Codex websocket integration tests")
	}

	return providers.Config{
		"api_key":          apiKey,
		"base_url":         baseURL,
		configUseWebSocket: "true",
		"account_id":       accountID,
	}
}

func completeAndCaptureResponseID(ctx context.Context, p providers.Provider, req providers.CompleteRequest) (string, error) {
	var responseID string
	for chunk, err := range p.Complete(ctx, req) {
		if err != nil {
			return "", err
		}
		if meta, ok := chunk.(message.ResponseMetadataChunk); ok && meta.ID != "" {
			responseID = meta.ID
		}
	}
	if responseID == "" {
		return "", fmt.Errorf("missing response id from stream")
	}
	return responseID, nil
}

func completeExpectError(ctx context.Context, p providers.Provider, req providers.CompleteRequest) error {
	for _, err := range p.Complete(ctx, req) {
		if err != nil {
			return err
		}
	}
	return nil
}

// TestCodexWebSocket_ContinuationInstructionsDependOnConnection validates the
// real Codex websocket behavior across a provider restart:
// 1) a reused websocket continuation succeeds without top-level instructions,
// 2) after restart (fresh socket), continuation without instructions fails, and
// 3) resending instructions on that fresh socket succeeds.
//
// Run with:
// OPENAI_API_BASE=<chatgpt codex base> CHATGPT_ACCOUNT_ID=<id> OPENAI_API_KEY=<key> \
// go test -tags integration -run TestCodexWebSocket_ContinuationInstructionsDependOnConnection ./providers/openai/
func TestCodexWebSocket_ContinuationInstructionsDependOnConnection(t *testing.T) {
	cfg := readCodexWebSocketConfig(t)
	ctx := context.Background()

	p, err := New(cfg)
	if err != nil {
		t.Fatalf("create initial provider: %v", err)
	}

	turn1 := providers.CompleteRequest{
		Model: providers.ModelRef{ProviderID: "openai", ModelID: "gpt-5.4"},
		Messages: []message.Message{
			{Role: "system", Parts: []message.Part{message.TextPart{Text: "You are Codex. Respond briefly."}}},
			{Role: "user", Parts: []message.Part{message.TextPart{Text: "Reply with: ONE"}}},
		},
	}
	resp1ID, err := completeAndCaptureResponseID(ctx, p, turn1)
	if err != nil {
		t.Fatalf("turn 1 failed: %v", err)
	}

	// No system message on purpose: reused websocket chain should still work.
	turn2 := providers.CompleteRequest{
		Model: providers.ModelRef{ProviderID: "openai", ModelID: "gpt-5.4"},
		Messages: []message.Message{
			{Role: "user", Parts: []message.Part{message.TextPart{Text: "Reply with: ONE"}}},
			{Role: "assistant", ID: resp1ID, Parts: []message.Part{message.TextPart{Text: "ONE"}}},
			{Role: "user", Parts: []message.Part{message.TextPart{Text: "Reply with: TWO"}}},
		},
	}
	resp2ID, err := completeAndCaptureResponseID(ctx, p, turn2)
	if err != nil {
		t.Fatalf("turn 2 should succeed on reused websocket without instructions: %v", err)
	}

	// Simulate agent-go restart: new provider, empty websocket pool.
	restartedProvider, err := New(cfg)
	if err != nil {
		t.Fatalf("create restarted provider: %v", err)
	}

	// Fresh websocket continuation WITHOUT system instructions should fail.
	restartNoInstructions := providers.CompleteRequest{
		Model: providers.ModelRef{ProviderID: "openai", ModelID: "gpt-5.4"},
		Messages: []message.Message{
			{Role: "user", Parts: []message.Part{message.TextPart{Text: "Reply with: ONE"}}},
			{Role: "assistant", ID: resp2ID, Parts: []message.Part{message.TextPart{Text: "TWO"}}},
			{Role: "user", Parts: []message.Part{message.TextPart{Text: "Reply with: THREE"}}},
		},
	}
	err = completeExpectError(ctx, restartedProvider, restartNoInstructions)
	if err == nil {
		t.Fatal("expected fresh-connection continuation without instructions to fail")
	}
	if !strings.Contains(err.Error(), "Instructions are required") {
		t.Fatalf("expected 'Instructions are required' error, got: %v", err)
	}

	// Re-sending instructions on the fresh connection should succeed.
	restartWithInstructions := providers.CompleteRequest{
		Model: providers.ModelRef{ProviderID: "openai", ModelID: "gpt-5.4"},
		Messages: []message.Message{
			{Role: "system", Parts: []message.Part{message.TextPart{Text: "You are Codex. Respond briefly."}}},
			{Role: "user", Parts: []message.Part{message.TextPart{Text: "Reply with: ONE"}}},
			{Role: "assistant", ID: resp2ID, Parts: []message.Part{message.TextPart{Text: "TWO"}}},
			{Role: "user", Parts: []message.Part{message.TextPart{Text: "Reply with: THREE"}}},
		},
	}
	if _, err := completeAndCaptureResponseID(ctx, restartedProvider, restartWithInstructions); err != nil {
		t.Fatalf("fresh-connection continuation with instructions should succeed: %v", err)
	}
}

func TestCompleteToolCallDeltasHaveCallID(t *testing.T) {
	apiKey := readAPIKey(t)

	p, err := New(providers.Config{"api_key": apiKey}, false, defaultBaseURL)
	if err != nil {
		t.Fatal(err)
	}

	req := providers.CompleteRequest{
		Model: providers.ModelRef{ProviderID: "openai", ModelID: "gpt-4o"},
		Messages: []message.Message{
			{Role: "user", Parts: []message.Part{
				message.TextPart{Text: "Use the echo tool to echo back the text: hello world"},
			}},
		},
		Tools: []providers.ToolDefinition{
			{
				Name:        "echo",
				Description: "Echo back the provided text",
				InputSchema: json.RawMessage(`{"type":"object","properties":{"text":{"type":"string","description":"The text to echo"}},"required":["text"]}`),
			},
		},
	}

	var (
		sawToolInputStart bool
		deltaIDs          []string
		endIDs            []string
	)
	for chunk, err := range p.Complete(context.Background(), req) {
		if err != nil {
			t.Fatalf("unexpected error from Complete: %v", err)
		}
		switch c := chunk.(type) {
		case message.ToolInputStartChunk:
			sawToolInputStart = true
			if c.ToolCallID == "" {
				t.Error("ToolInputStartChunk has empty ToolCallID")
			}
		case message.ToolInputDeltaChunk:
			deltaIDs = append(deltaIDs, c.ToolCallID)
		case message.ToolInputEndChunk:
			endIDs = append(endIDs, c.ToolCallID)
		}
	}

	if !sawToolInputStart {
		t.Fatal("expected at least one tool call, got none (model may have responded without tool use)")
	}

	for i, id := range deltaIDs {
		if id == "" {
			t.Errorf("ToolInputDeltaChunk[%d] has empty ToolCallID (bug: item_id→call_id lookup missing)", i)
		}
	}
	for i, id := range endIDs {
		if id == "" {
			t.Errorf("ToolInputEndChunk[%d] has empty ToolCallID (bug: item_id→call_id lookup missing)", i)
		}
	}

	if len(deltaIDs) == 0 {
		t.Error("expected at least one ToolInputDeltaChunk")
	}
}
