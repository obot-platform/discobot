package server

import (
	"context"
	"iter"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/obot-platform/discobot/agent-go/agent"
	"github.com/obot-platform/discobot/agent-go/internal/api"
	"github.com/obot-platform/discobot/agent-go/message"
)

func TestVisibleEnvSnapshotWithoutWorkspaceEnvFile(t *testing.T) {
	workspaceRoot := t.TempDir()

	env := visibleEnvSnapshot(workspaceRoot, func() map[string]string {
		return map[string]string{"API_KEY": "secret"}
	})

	if env["API_KEY"] != "secret" {
		t.Fatalf("API_KEY = %q, want secret", env["API_KEY"])
	}
}

func TestVisibleEnvSnapshotMergesWorkspaceAndCredentialEnv(t *testing.T) {
	workspaceRoot := t.TempDir()
	envDir := filepath.Join(workspaceRoot, ".discobot")
	if err := os.MkdirAll(envDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(envDir, "env"), []byte("WORKSPACE=from-file\nSHARED=from-file\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	env := visibleEnvSnapshot(workspaceRoot, func() map[string]string {
		return map[string]string{"CREDENTIAL": "from-cred", "SHARED": "from-cred"}
	})

	if env["WORKSPACE"] != "from-file" {
		t.Fatalf("WORKSPACE = %q, want from-file", env["WORKSPACE"])
	}
	if env["CREDENTIAL"] != "from-cred" {
		t.Fatalf("CREDENTIAL = %q, want from-cred", env["CREDENTIAL"])
	}
	if env["SHARED"] != "from-cred" {
		t.Fatalf("SHARED = %q, want from-cred", env["SHARED"])
	}
}

func TestEmitThreadUpdateFallsBackToEphemeralWithoutActiveCompletion(t *testing.T) {
	conversations := agent.NewConversationManager(testAgent{})
	ephemeral, unsubscribe := conversations.SubscribeEphemeral()
	defer unsubscribe()

	want := message.ThreadUpdateChunk{
		Data: message.ThreadUpdateData{
			Thread: message.ThreadUpdateInfo{
				ID: "thread-1",
				PromptQueue: []message.ThreadQueuedPromptInfo{
					{ID: "queue-1"},
				},
			},
		},
	}

	emitThreadUpdate(conversations, "thread-1", want)

	select {
	case got := <-ephemeral:
		update, ok := got.(message.ThreadUpdateChunk)
		if !ok {
			t.Fatalf("got chunk type %T, want message.ThreadUpdateChunk", got)
		}
		if update.Data.Thread.ID != want.Data.Thread.ID {
			t.Fatalf("thread ID = %q, want %q", update.Data.Thread.ID, want.Data.Thread.ID)
		}
		if len(update.Data.Thread.PromptQueue) != 1 || update.Data.Thread.PromptQueue[0].ID != "queue-1" {
			t.Fatalf("prompt queue = %#v, want queue-1", update.Data.Thread.PromptQueue)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for ephemeral thread update")
	}
}

type testAgent struct{}

func (testAgent) Prompt(context.Context, string, agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
	return func(func(message.MessageChunk, error) bool) {}
}

func (testAgent) Compact(context.Context, string, agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
	return func(func(message.MessageChunk, error) bool) {}
}

func (testAgent) Reset(_ context.Context, threadID string) (agent.ThreadInfo, error) {
	return agent.ThreadInfo{ID: threadID}, nil
}

func (testAgent) Resume(context.Context, string, agent.PromptRequest) (agent.ResumeResult, error) {
	return agent.ResumeResult{}, nil
}

func (testAgent) Cancel(string) bool { return false }

func (testAgent) Messages(string, string) ([]message.UIMessage, error) { return nil, nil }

func (testAgent) ListThreads() ([]string, error) { return nil, nil }

func (testAgent) ListThreadInfos() ([]agent.ThreadInfo, error) { return nil, nil }

func (testAgent) GetThreadInfo(threadID string) (agent.ThreadInfo, error) {
	return agent.ThreadInfo{ID: threadID}, nil
}

func (testAgent) GetThreadTokenUsageDetails(threadID string) (agent.ThreadTokenUsageDetails, error) {
	return agent.ThreadTokenUsageDetails{ThreadID: threadID}, nil
}

func (testAgent) CreateThread(context.Context, agent.CreateThreadRequest) (agent.ThreadInfo, error) {
	return agent.ThreadInfo{}, nil
}

func (testAgent) UpdateThread(context.Context, string, agent.UpdateThreadRequest) (agent.ThreadInfo, error) {
	return agent.ThreadInfo{}, nil
}

func (testAgent) DeleteThread(context.Context, string) error { return nil }

func (testAgent) HasInterruptedTurn(string) (bool, error) { return false, nil }

func (testAgent) PendingQuestion(string) (*agent.PendingQuestion, error) { return nil, nil }

func (testAgent) SubmitAnswer(string, string, api.AnswerQuestionRequest) error { return nil }

func (testAgent) FinalResponse(string) (string, error) { return "", nil }

func (testAgent) ListCommands() ([]agent.Command, error) { return nil, nil }
