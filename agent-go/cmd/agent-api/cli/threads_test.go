package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/obot-platform/discobot/agent-go/internal/config"
	"github.com/obot-platform/discobot/agent-go/message"
	"github.com/obot-platform/discobot/agent-go/thread"
)

func TestSelectInitialThreadID_ForceNewThread(t *testing.T) {
	store := thread.NewStore(t.TempDir())
	cfg := &config.Config{SessionID: "explicit-session"}

	threadID := selectInitialThreadID(store, cfg, true, "")
	if threadID == cfg.SessionID {
		t.Fatalf("expected force-new to ignore explicit session id, got %q", threadID)
	}
	if !strings.HasPrefix(threadID, "thread-") {
		t.Fatalf("expected generated thread ID with prefix thread-, got %q", threadID)
	}
}

func TestSelectInitialThreadID_UsesExplicitSessionIDByDefault(t *testing.T) {
	store := thread.NewStore(t.TempDir())
	cfg := &config.Config{SessionID: "explicit-session"}

	threadID := selectInitialThreadID(store, cfg, false, "")
	if threadID != cfg.SessionID {
		t.Fatalf("expected explicit session id %q, got %q", cfg.SessionID, threadID)
	}
}

func TestSelectInitialThreadID_UsesResumeFlagBeforeSessionID(t *testing.T) {
	store := thread.NewStore(t.TempDir())
	cfg := &config.Config{SessionID: "explicit-session"}

	threadID := selectInitialThreadID(store, cfg, false, "resumed-thread")
	if threadID != "resumed-thread" {
		t.Fatalf("expected resume flag thread id %q, got %q", "resumed-thread", threadID)
	}
}

func TestSelectInitialThreadID_DefaultsToFreshThread(t *testing.T) {
	baseDir := t.TempDir()
	store := thread.NewStore(baseDir)
	cfg := &config.Config{SessionID: "default"}

	if err := os.MkdirAll(filepath.Join(baseDir, "thread-existing"), 0o755); err != nil {
		t.Fatal(err)
	}

	threadID := selectInitialThreadID(store, cfg, false, "")
	if threadID == "thread-existing" {
		t.Fatalf("expected a fresh thread id, got existing thread %q", threadID)
	}
	if !strings.HasPrefix(threadID, "thread-") {
		t.Fatalf("expected generated thread ID with prefix thread-, got %q", threadID)
	}
}

func TestPrintThreadHistory_UsesPromptStyleForUserAndInlineAssistant(t *testing.T) {
	store := thread.NewStore(t.TempDir())
	threadID := "thread-1"

	if err := store.SaveMessage(threadID, thread.StoredMessage{
		ID: "msg-1",
		Message: message.Message{
			Role:  "user",
			Parts: []message.Part{message.TextPart{Text: "hello"}},
		},
	}); err != nil {
		t.Fatal(err)
	}

	if err := store.SaveMessage(threadID, thread.StoredMessage{
		ID:       "msg-2",
		ParentID: "msg-1",
		Message: message.Message{
			Role:  "assistant",
			Parts: []message.Part{message.TextPart{Text: "world"}},
		},
	}); err != nil {
		t.Fatal(err)
	}

	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdoutReader.Close()

	oldStdout := os.Stdout
	oldNoColor := noColor
	os.Stdout = stdoutWriter
	noColor = true
	defer func() {
		os.Stdout = oldStdout
		noColor = oldNoColor
	}()

	if !printThreadHistory(store, threadID) {
		t.Fatal("expected printable history")
	}

	if err := stdoutWriter.Close(); err != nil {
		t.Fatal(err)
	}
	output, err := io.ReadAll(stdoutReader)
	if err != nil {
		t.Fatal(err)
	}

	got := string(output)
	if strings.Contains(got, "[User]") || strings.Contains(got, "[Assistant]") {
		t.Fatalf("expected prompt-style history without role labels, got %q", got)
	}
	for _, want := range []string{"> hello", "world"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected history output to contain %q, got %q", want, got)
		}
	}
}
