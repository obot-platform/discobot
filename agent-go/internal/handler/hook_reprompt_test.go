package handler

import (
	"context"
	"encoding/json"
	"iter"
	"testing"
	"time"

	"github.com/obot-platform/discobot/agent-go/agent"
	"github.com/obot-platform/discobot/agent-go/internal/hooks"
	"github.com/obot-platform/discobot/agent-go/message"
)

func TestStartHookFailureReprompt_SendsPromptRequest(t *testing.T) {
	reqCh := make(chan agent.PromptRequest, 1)
	ma := &streamTestAgent{
		promptFn: func(_ context.Context, threadID string, req agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
			if threadID != "thread-1" {
				t.Fatalf("threadID = %q, want %q", threadID, "thread-1")
			}
			reqCh <- req
			return func(_ func(message.MessageChunk, error) bool) {}
		},
	}
	cm := agent.NewCompletionManager(ma)
	h := New("", cm, nil, nil, nil)

	err := h.startHookFailureReprompt("thread-1", hooks.FileHookEvalResult{
		ShouldReprompt: true,
		LLMMessage:     "### Hook failed: Go Check",
		HookFailure: &hooks.HookFailureMessageMetadata{
			Kind:     "hook-failure",
			HookName: "Go Check",
			ExitCode: 1,
		},
	})
	if err != nil {
		t.Fatalf("startHookFailureReprompt() failed: %v", err)
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
		if part.Text != "### Hook failed: Go Check" {
			t.Fatalf("part text = %q, want %q", part.Text, "### Hook failed: Go Check")
		}

		var meta struct {
			Discobot hooks.HookFailureMessageMetadata `json:"discobot"`
		}
		if err := json.Unmarshal(req.Metadata, &meta); err != nil {
			t.Fatalf("unmarshal metadata: %v", err)
		}
		if meta.Discobot.Kind != "hook-failure" {
			t.Fatalf("kind = %q, want %q", meta.Discobot.Kind, "hook-failure")
		}
		if meta.Discobot.HookName != "Go Check" {
			t.Fatalf("hook name = %q, want %q", meta.Discobot.HookName, "Go Check")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected prompt request")
	}
}
