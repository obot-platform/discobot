package handler

import (
	"context"
	"encoding/json"
	"iter"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/obot-platform/discobot/agent-go/agent"
	"github.com/obot-platform/discobot/agent-go/agentimpl"
	"github.com/obot-platform/discobot/agent-go/internal/hooks"
	"github.com/obot-platform/discobot/agent-go/message"
	"github.com/obot-platform/discobot/agent-go/promptqueue"
	"github.com/obot-platform/discobot/agent-go/thread"
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
	cm := agent.NewConversationManager(ma)
	hookManager := hooks.NewManager(t.TempDir(), "session-123")
	hookManager.SetRepromptRunner(cm, nil)

	err := hookManager.StartFailureReprompt("thread-1", hooks.FileHookEvalResult{
		ShouldReprompt: true,
		LLMMessage:     "### Hook failed: Go Check",
		HookFailure: &hooks.HookFailureMessageMetadata{
			Kind:     "hook-failure",
			HookName: "Go Check",
			ExitCode: 1,
		},
	})
	if err != nil {
		t.Fatalf("StartFailureReprompt() failed: %v", err)
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

func TestStartHookFailureReprompt_SuppressedWhenExecutionPaused(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)

	reqCh := make(chan agent.PromptRequest, 1)
	ma := &streamTestAgent{
		promptFn: func(_ context.Context, _ string, req agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
			reqCh <- req
			return func(_ func(message.MessageChunk, error) bool) {}
		},
	}
	cm := agent.NewConversationManager(ma)
	hookManager := hooks.NewManager(t.TempDir(), "paused-session")
	hookManager.SetRepromptRunner(cm, nil)
	if err := hookManager.SetExecutionPaused(true); err != nil {
		t.Fatalf("SetExecutionPaused() failed: %v", err)
	}

	err := hookManager.StartFailureReprompt("thread-1", hooks.FileHookEvalResult{
		ShouldReprompt: true,
		LLMMessage:     "### Hook failed: Go Check",
	})
	if err != nil {
		t.Fatalf("StartFailureReprompt() failed: %v", err)
	}

	select {
	case <-reqCh:
		t.Fatal("expected paused hooks not to send prompt request")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestStartHookFailureReprompt_SuppressedWhenHookExecutionPaused(t *testing.T) {
	homeDir := t.TempDir()
	workspaceRoot := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)

	hooksDir := filepath.Join(workspaceRoot, hooks.HooksDir)
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	hookPath := filepath.Join(hooksDir, "go-check.sh")
	hookSource := `#!/bin/bash
#---
# name: Go Check
# type: file
# pattern: "*.go"
#---
echo "lint failed"
exit 1
`
	if err := os.WriteFile(hookPath, []byte(hookSource), 0o755); err != nil {
		t.Fatal(err)
	}

	reqCh := make(chan agent.PromptRequest, 1)
	ma := &streamTestAgent{
		promptFn: func(_ context.Context, _ string, req agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
			reqCh <- req
			return func(_ func(message.MessageChunk, error) bool) {}
		},
	}
	cm := agent.NewConversationManager(ma)
	hookManager := hooks.NewManager(workspaceRoot, "hook-paused-session")
	if err := hookManager.Init(); err != nil {
		t.Fatal(err)
	}
	hookManager.SetRepromptRunner(cm, nil)
	if err := hookManager.SetHookExecutionPaused("go-check", true); err != nil {
		t.Fatalf("SetHookExecutionPaused() failed: %v", err)
	}

	err := hookManager.StartFailureReprompt("thread-1", hooks.FileHookEvalResult{
		ShouldReprompt: true,
		LLMMessage:     "### Hook failed: Go Check",
		FailedResult: &hooks.HookResult{
			Hook: hooks.Hook{ID: "go-check", Name: "Go Check"},
		},
	})
	if err != nil {
		t.Fatalf("StartFailureReprompt() failed: %v", err)
	}

	select {
	case <-reqCh:
		t.Fatal("expected paused hook not to send prompt request")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestStartHookFailureReprompt_UsesResumeForInterruptedTurn(t *testing.T) {
	type resumeCall struct {
		threadID string
		req      agent.PromptRequest
	}

	threadDir := t.TempDir()
	store := thread.NewStore(threadDir)
	if err := store.CreateThread("thread-1"); err != nil {
		t.Fatal(err)
	}
	resumeCh := make(chan resumeCall, 1)
	ma := &streamTestAgent{
		hasInterruptedTurnFn: func(threadID string) (bool, error) {
			if threadID != "thread-1" {
				t.Fatalf("threadID = %q, want %q", threadID, "thread-1")
			}
			return true, nil
		},
		promptFn: func(_ context.Context, _ string, _ agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
			t.Fatal("StartFailureReprompt should resume interrupted turns")
			return func(_ func(message.MessageChunk, error) bool) {}
		},
		resumeFn: func(_ context.Context, threadID string, req agent.PromptRequest) (agent.ResumeResult, error) {
			resumeCh <- resumeCall{threadID: threadID, req: req}
			return agent.ResumeResult{Stream: func(_ func(message.MessageChunk, error) bool) {}}, nil
		},
	}
	cm := agent.NewConversationManager(ma)
	hookManager := hooks.NewManager(t.TempDir(), "session-123")
	hookManager.SetRepromptRunner(cm, nil)

	err := hookManager.StartFailureReprompt("thread-1", hooks.FileHookEvalResult{
		ShouldReprompt: true,
		LLMMessage:     "### Hook failed: Go Check",
		HookFailure: &hooks.HookFailureMessageMetadata{
			Kind:     "hook-failure",
			HookName: "Go Check",
			ExitCode: 1,
		},
	})
	if err != nil {
		t.Fatalf("StartFailureReprompt() failed: %v", err)
	}

	select {
	case call := <-resumeCh:
		if call.threadID != "thread-1" {
			t.Fatalf("threadID = %q, want %q", call.threadID, "thread-1")
		}
		if len(call.req.UserParts) != 1 {
			t.Fatalf("expected 1 user part, got %d", len(call.req.UserParts))
		}
		part, ok := call.req.UserParts[0].(message.UITextPart)
		if !ok {
			t.Fatalf("expected UITextPart, got %T", call.req.UserParts[0])
		}
		if part.Text != "### Hook failed: Go Check" {
			t.Fatalf("part text = %q, want %q", part.Text, "### Hook failed: Go Check")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected hook failure resume request")
	}

	waitForCompletionDone(t, cm, "thread-1")
}

func TestEnqueueHookFailureReprompt_PrependsQueuedPromptWithMetadata(t *testing.T) {
	threadDir := t.TempDir()
	store := thread.NewStore(threadDir)
	if err := store.CreateThread("thread-1"); err != nil {
		t.Fatal(err)
	}
	queueStore := promptqueue.NewStore(threadDir)
	defaultAgent := agentimpl.NewDefaultAgent(store, nil, nil, t.TempDir(), agentimpl.MCPConfig{})
	h := New("", agent.NewConversationManager(&streamTestAgent{}), nil, nil, defaultAgent, queueStore)

	if _, _, err := queueStore.Append("thread-1", promptqueue.Prompt{
		Message: message.UIMessage{
			ID:    "user-queued",
			Role:  "user",
			Parts: []message.UIPart{message.UITextPart{Text: "normal follow-up"}},
		},
	}); err != nil {
		t.Fatal(err)
	}

	result := hooks.FileHookEvalResult{
		ShouldReprompt: true,
		LLMMessage:     "### Hook failed: Go Check",
		HookFailure: &hooks.HookFailureMessageMetadata{
			Kind:     "hook-failure",
			HookName: "Go Check",
			ExitCode: 1,
		},
	}
	if _, err := h.promptQueue.Prepend("thread-1", hooks.HookFailureQueuedPrompt(result)); err != nil {
		t.Fatalf("Prepend() failed: %v", err)
	}

	queue, err := queueStore.List("thread-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(queue) != 2 {
		t.Fatalf("prompt queue length = %d, want 2", len(queue))
	}

	first := queue[0]
	if first.ID == "" {
		t.Fatal("expected queued hook re-prompt id")
	}
	part, ok := first.Message.Parts[0].(message.UITextPart)
	if !ok {
		t.Fatalf("expected UITextPart, got %T", first.Message.Parts[0])
	}
	if part.Text != "### Hook failed: Go Check" {
		t.Fatalf("part text = %q, want %q", part.Text, "### Hook failed: Go Check")
	}
	if len(first.Message.Metadata) == 0 {
		t.Fatal("expected metadata on queued hook re-prompt")
	}

	var meta struct {
		Discobot hooks.HookFailureMessageMetadata `json:"discobot"`
	}
	if err := json.Unmarshal(first.Message.Metadata, &meta); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}
	if meta.Discobot.HookName != "Go Check" {
		t.Fatalf("hook name = %q, want %q", meta.Discobot.HookName, "Go Check")
	}

	second := queue[1]
	secondPart, ok := second.Message.Parts[0].(message.UITextPart)
	if !ok {
		t.Fatalf("expected second UITextPart, got %T", second.Message.Parts[0])
	}
	if secondPart.Text != "normal follow-up" {
		t.Fatalf("second part text = %q, want %q", secondPart.Text, "normal follow-up")
	}
}

func TestScheduleHookEvaluation_SticksNotificationToFirstThread(t *testing.T) {
	homeDir := t.TempDir()
	workspaceRoot := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)

	cmd := exec.Command("git", "init")
	cmd.Dir = workspaceRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, out)
	}

	hooksDir := filepath.Join(workspaceRoot, hooks.HooksDir)
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	hookPath := filepath.Join(hooksDir, "go-check.sh")
	hookSource := `#!/bin/bash
#---
# name: Go Check
# type: file
# pattern: "*.go"
#---
echo "lint failed"
exit 1
`
	if err := os.WriteFile(hookPath, []byte(hookSource), 0o755); err != nil {
		t.Fatal(err)
	}
	mainPath := filepath.Join(workspaceRoot, "main.go")
	if err := os.WriteFile(mainPath, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	hookManager := hooks.NewManager(workspaceRoot, "session-123")
	if err := hookManager.Init(); err != nil {
		t.Fatal(err)
	}

	reqThreadCh := make(chan string, 4)
	ma := &streamTestAgent{
		promptFn: func(_ context.Context, threadID string, _ agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
			reqThreadCh <- threadID
			return func(yield func(message.MessageChunk, error) bool) {
				yield(message.StartChunk{MessageID: "assistant-1"}, nil)
			}
		},
	}
	cm := agent.NewConversationManager(ma)
	_ = New("", cm, hookManager, nil, nil)

	hookManager.OnTurnComplete("thread-1")
	select {
	case got := <-reqThreadCh:
		if got != "thread-1" {
			t.Fatalf("first hook notification thread = %q, want %q", got, "thread-1")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for first hook notification")
	}
	waitForCompletionDone(t, cm, "thread-1")
	firstStatus := hookManager.GetStatus()
	firstRunCount := firstStatus.Hooks["go-check"].RunCount

	time.Sleep(1100 * time.Millisecond)
	if err := os.WriteFile(mainPath, []byte("package main\n\nvar _ = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	hookManager.OnTurnComplete("thread-2")
	select {
	case got := <-reqThreadCh:
		t.Fatalf("unexpected hook notification for %q; wanted original thread only", got)
	case <-time.After(400 * time.Millisecond):
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		status := hookManager.GetStatus()
		if status.Hooks["go-check"].RunCount > firstRunCount {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("timed out waiting for second hook evaluation to finish")
}

func TestResumeHookExecution_ReevaluatesPendingHooksOnLastThread(t *testing.T) {
	homeDir := t.TempDir()
	workspaceRoot := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)

	cmd := exec.Command("git", "init")
	cmd.Dir = workspaceRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, out)
	}

	hooksDir := filepath.Join(workspaceRoot, hooks.HooksDir)
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	hookPath := filepath.Join(hooksDir, "go-check.sh")
	hookSource := `#!/bin/bash
#---
# name: Go Check
# type: file
# pattern: "*.go"
#---
echo "lint failed"
exit 1
`
	if err := os.WriteFile(hookPath, []byte(hookSource), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspaceRoot, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	hookManager := hooks.NewManager(workspaceRoot, "session-123")
	if err := hookManager.Init(); err != nil {
		t.Fatal(err)
	}
	if err := hookManager.SetExecutionPaused(true); err != nil {
		t.Fatal(err)
	}

	reqThreadCh := make(chan string, 2)
	ma := &streamTestAgent{
		promptFn: func(_ context.Context, threadID string, _ agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
			reqThreadCh <- threadID
			return func(_ func(message.MessageChunk, error) bool) {}
		},
	}
	cm := agent.NewConversationManager(ma)
	_ = New("", cm, hookManager, nil, nil)

	hookManager.OnTurnComplete("thread-1")
	time.Sleep(300 * time.Millisecond)
	status := hookManager.GetStatus()
	if status.Hooks["go-check"].RunCount != 0 {
		t.Fatalf("paused hook run count = %d, want 0", status.Hooks["go-check"].RunCount)
	}
	select {
	case got := <-reqThreadCh:
		t.Fatalf("unexpected hook notification while paused for %q", got)
	case <-time.After(200 * time.Millisecond):
	}

	if err := hookManager.SetExecutionPaused(false); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-reqThreadCh:
		if got != "thread-1" {
			t.Fatalf("resumed hook notification thread = %q, want %q", got, "thread-1")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for hook notification after resume")
	}
}

func TestScheduleHookEvaluation_QueuesHookRepromptWhenThreadBusy(t *testing.T) {
	homeDir := t.TempDir()
	workspaceRoot := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)

	cmd := exec.Command("git", "init")
	cmd.Dir = workspaceRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, out)
	}

	hooksDir := filepath.Join(workspaceRoot, hooks.HooksDir)
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	hookPath := filepath.Join(hooksDir, "go-check.sh")
	hookSource := `#!/bin/bash
#---
# name: Go Check
# type: file
# pattern: "*.go"
#---
echo "lint failed"
exit 1
`
	if err := os.WriteFile(hookPath, []byte(hookSource), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspaceRoot, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	hookManager := hooks.NewManager(workspaceRoot, "session-123")
	if err := hookManager.Init(); err != nil {
		t.Fatal(err)
	}

	threadDir := t.TempDir()
	store := thread.NewStore(threadDir)
	if err := store.CreateThread("thread-1"); err != nil {
		t.Fatal(err)
	}
	defaultAgent := agentimpl.NewDefaultAgent(store, nil, nil, t.TempDir(), agentimpl.MCPConfig{})

	release := make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	reqCh := make(chan agent.PromptRequest, 2)
	ma := &streamTestAgent{
		promptFn: func(ctx context.Context, _ string, req agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
			reqCh <- req
			return func(yield func(message.MessageChunk, error) bool) {
				if !yield(message.StartChunk{MessageID: "assistant-1"}, nil) {
					return
				}
				select {
				case <-release:
				case <-ctx.Done():
				}
			}
		},
	}
	cm := agent.NewConversationManager(ma)
	queueStore := promptqueue.NewStore(threadDir)
	_ = New("", cm, hookManager, nil, defaultAgent, queueStore)

	if _, err := cm.Chat("thread-1", agent.PromptRequest{
		UserParts: []message.UIPart{message.UITextPart{Text: "active turn"}},
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-reqCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for active turn")
	}

	hookManager.OnTurnComplete("thread-1")

	var queue []promptqueue.Prompt
	var err error
	deadline := time.After(5 * time.Second)
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for len(queue) == 0 {
		select {
		case <-deadline:
			t.Fatalf("prompt queue length = %d, want 1", len(queue))
		case <-tick.C:
			queue, err = queueStore.List("thread-1")
			if err != nil {
				if runtime.GOOS == "windows" && strings.Contains(err.Error(), "being used by another process") {
					continue
				}
				t.Fatal(err)
			}
		}
	}
	if len(queue) != 1 {
		t.Fatalf("prompt queue length = %d, want 1", len(queue))
	}
	part, ok := queue[0].Message.Parts[0].(message.UITextPart)
	if !ok {
		t.Fatalf("expected queued UITextPart, got %T", queue[0].Message.Parts[0])
	}
	if !strings.Contains(part.Text, "### Hook failed: Go Check") {
		t.Fatalf("queued prompt text = %q, want hook failure prompt", part.Text)
	}

	close(release)
	released = true
	select {
	case queuedReq := <-reqCh:
		part, ok := queuedReq.UserParts[0].(message.UITextPart)
		if !ok {
			t.Fatalf("expected queued UITextPart, got %T", queuedReq.UserParts[0])
		}
		if !strings.Contains(part.Text, "### Hook failed: Go Check") {
			t.Fatalf("queued prompt text = %q, want hook failure prompt", part.Text)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for queued hook re-prompt to start")
	}
	waitForCompletionDone(t, cm, "thread-1")
}

func TestTurnCompleteSchedulesHookEvaluationWhenStreamPausesForAnswer(t *testing.T) {
	homeDir := t.TempDir()
	workspaceRoot := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)

	cmd := exec.Command("git", "init")
	cmd.Dir = workspaceRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, out)
	}

	hooksDir := filepath.Join(workspaceRoot, hooks.HooksDir)
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	hookPath := filepath.Join(hooksDir, "go-check.sh")
	hookSource := `#!/bin/bash
#---
# name: Go Check
# type: file
# pattern: "*.go"
#---
echo "lint failed"
exit 1
`
	if err := os.WriteFile(hookPath, []byte(hookSource), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspaceRoot, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	hookManager := hooks.NewManager(workspaceRoot, "session-123")
	if err := hookManager.Init(); err != nil {
		t.Fatal(err)
	}

	firstTurnCh := make(chan struct{}, 1)
	repromptCh := make(chan agent.PromptRequest, 1)
	var promptCalls atomic.Int32
	ma := &streamTestAgent{
		promptFn: func(_ context.Context, threadID string, req agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
			if threadID != "thread-1" {
				t.Fatalf("threadID = %q, want %q", threadID, "thread-1")
			}
			if promptCalls.Add(1) == 1 {
				firstTurnCh <- struct{}{}
				return func(yield func(message.MessageChunk, error) bool) {
					// A paused turn returns without a response-finish chunk while
					// leaving the persisted turn state waiting for an answer. The
					// conversation manager should still treat the stream as complete
					// enough to notify post-turn listeners.
					yield(message.StartChunk{MessageID: "assistant-1"}, nil)
				}
			}
			repromptCh <- req
			return func(_ func(message.MessageChunk, error) bool) {}
		},
	}
	cm := agent.NewConversationManager(ma)
	_ = New("", cm, hookManager, nil, nil)

	if _, err := cm.Chat("thread-1", agent.PromptRequest{
		UserParts: []message.UIPart{message.UITextPart{Text: "initial turn"}},
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-firstTurnCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for initial paused turn")
	}

	select {
	case req := <-repromptCh:
		part, ok := req.UserParts[0].(message.UITextPart)
		if !ok {
			t.Fatalf("expected UITextPart, got %T", req.UserParts[0])
		}
		if !strings.Contains(part.Text, "### Hook failed: Go Check") {
			t.Fatalf("re-prompt text = %q, want hook failure prompt", part.Text)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for hook evaluation after paused turn")
	}
}

func TestScheduleHookEvaluation_QueuesHookRepromptWhenAnswerNeeded(t *testing.T) {
	homeDir := t.TempDir()
	workspaceRoot := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)

	cmd := exec.Command("git", "init")
	cmd.Dir = workspaceRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, out)
	}

	hooksDir := filepath.Join(workspaceRoot, hooks.HooksDir)
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	hookPath := filepath.Join(hooksDir, "go-check.sh")
	hookSource := `#!/bin/bash
#---
# name: Go Check
# type: file
# pattern: "*.go"
#---
echo "lint failed"
exit 1
`
	if err := os.WriteFile(hookPath, []byte(hookSource), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspaceRoot, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	hookManager := hooks.NewManager(workspaceRoot, "session-123")
	if err := hookManager.Init(); err != nil {
		t.Fatal(err)
	}

	queueStore := promptqueue.NewStore(t.TempDir())
	ma := &streamTestAgent{
		pendingQuestionFn: func(threadID string) (*agent.PendingQuestion, error) {
			if threadID != "thread-1" {
				t.Fatalf("threadID = %q, want %q", threadID, "thread-1")
			}
			return &agent.PendingQuestion{ApprovalID: "approval-1"}, nil
		},
		promptFn: func(context.Context, string, agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
			t.Fatal("hook re-prompt should queue while an answer is pending")
			return func(_ func(message.MessageChunk, error) bool) {}
		},
	}
	cm := agent.NewConversationManager(ma)
	promptQueue := promptqueue.NewManager(queueStore, cm, nil)
	_ = New("", cm, hookManager, nil, nil, promptQueue)

	hookManager.OnTurnComplete("thread-1")

	var queue []promptqueue.Prompt
	var err error
	deadline := time.After(5 * time.Second)
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for len(queue) == 0 {
		select {
		case <-deadline:
			t.Fatalf("queued prompts = %d, want 1", len(queue))
		case <-tick.C:
			queue, err = queueStore.List("thread-1")
			if err != nil {
				if runtime.GOOS == "windows" && strings.Contains(err.Error(), "being used by another process") {
					continue
				}
				t.Fatal(err)
			}
		}
	}
	if len(queue) != 1 {
		t.Fatalf("queued prompts = %d, want 1", len(queue))
	}
	part, ok := queue[0].Message.Parts[0].(message.UITextPart)
	if !ok {
		t.Fatalf("expected UITextPart, got %T", queue[0].Message.Parts[0])
	}
	if !strings.Contains(part.Text, "### Hook failed: Go Check") {
		t.Fatalf("queued prompt text = %q, want hook failure prompt", part.Text)
	}
}
