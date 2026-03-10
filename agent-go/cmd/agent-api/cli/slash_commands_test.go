package cli

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/obot-platform/discobot/agent-go/internal/config"
	"github.com/obot-platform/discobot/agent-go/thread"
)

func TestSelectInitialThreadID_ForceNewThread(t *testing.T) {
	store := thread.NewStore(t.TempDir())
	cfg := &config.Config{SessionID: "explicit-session"}

	threadID := selectInitialThreadID(store, cfg, true)
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

	threadID := selectInitialThreadID(store, cfg, false)
	if threadID != cfg.SessionID {
		t.Fatalf("expected explicit session id %q, got %q", cfg.SessionID, threadID)
	}
}

func TestAgentSlashCommands_LoadsSkillsAndCommands(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))

	skillDir := filepath.Join(root, ".claude", "skills", "deploy")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: deploy\ndescription: Deploy app\n---\nDeploy now."), 0o644); err != nil {
		t.Fatal(err)
	}

	commandsDir := filepath.Join(root, ".claude", "commands")
	if err := os.MkdirAll(commandsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(commandsDir, "release.md"), []byte("Release command body."), 0o644); err != nil {
		t.Fatal(err)
	}

	commands, err := agentSlashCommands(root)
	if err != nil {
		t.Fatalf("agentSlashCommands() error = %v", err)
	}
	if _, ok := commands["/deploy"]; !ok {
		t.Fatalf("expected /deploy in discovered commands")
	}
	if _, ok := commands["/release"]; !ok {
		t.Fatalf("expected /release in discovered commands")
	}
}

func TestHandleSlashCommand_ForwardsKnownAgentCommand(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))

	skillDir := filepath.Join(root, ".claude", "skills", "deploy")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("Deploy skill body."), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{AgentCwd: root}
	threadID, handled := handleSlashCommand(context.Background(), "/deploy", nil, nil, cfg, "thread-1", nil, nil, nil)
	if handled {
		t.Fatalf("expected /deploy to be forwarded to agent, handled=%v", handled)
	}
	if threadID != "thread-1" {
		t.Fatalf("threadID changed unexpectedly: %q", threadID)
	}
}

func TestHandleSlashCommand_UnknownStillHandledLocally(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))

	cfg := &config.Config{AgentCwd: root}
	threadID, handled := handleSlashCommand(context.Background(), "/does-not-exist", nil, nil, cfg, "thread-1", nil, nil, nil)
	if !handled {
		t.Fatalf("expected unknown slash command to be handled locally")
	}
	if threadID != "thread-1" {
		t.Fatalf("threadID changed unexpectedly: %q", threadID)
	}
}

func TestHandleSlashCommand_LocalCommandsTakePriority(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))

	skillDir := filepath.Join(root, ".claude", "skills", "clear")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("Clear skill body."), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{AgentCwd: root}
	newThreadID, handled := handleSlashCommand(context.Background(), "/clear", nil, nil, cfg, "thread-1", nil, nil, nil)
	if !handled {
		t.Fatalf("expected /clear to be handled locally")
	}
	if newThreadID == "thread-1" {
		t.Fatalf("expected /clear to start a new thread")
	}
}

func TestImagePartFromPathInput_DetectsImageFile(t *testing.T) {
	root := t.TempDir()
	pngBytes := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0}
	if err := os.WriteFile(filepath.Join(root, "img.png"), pngBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	part, ok, err := imagePartFromPathInput([]byte("img.png"), root)
	if err != nil {
		t.Fatalf("imagePartFromPathInput() error = %v", err)
	}
	if !ok {
		t.Fatal("expected image path to be detected")
	}
	if !strings.HasPrefix(part.MediaType, "image/") {
		t.Fatalf("expected image media type, got %q", part.MediaType)
	}
	decoded, err := base64.StdEncoding.DecodeString(part.Image)
	if err != nil {
		t.Fatalf("decode base64 image: %v", err)
	}
	if string(decoded) != string(pngBytes) {
		t.Fatalf("decoded image bytes mismatch")
	}
}

func TestImagePartFromRawBytes_DetectsImageData(t *testing.T) {
	input := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0, '\n'}
	part, ok := imagePartFromRawBytes(input)
	if !ok {
		t.Fatal("expected raw image bytes to be detected")
	}
	if !strings.HasPrefix(part.MediaType, "image/") {
		t.Fatalf("expected image media type, got %q", part.MediaType)
	}
	decoded, err := base64.StdEncoding.DecodeString(part.Image)
	if err != nil {
		t.Fatalf("decode base64 image: %v", err)
	}
	trimmed := input[:len(input)-1]
	if string(decoded) != string(trimmed) {
		t.Fatalf("decoded image bytes mismatch")
	}
}

func TestPastedSummary_FormatsLinesAndBytes(t *testing.T) {
	summary := pastedSummary([]byte("hello\nworld\n"))
	if summary != "[pasted 2 lines/12 bytes]" {
		t.Fatalf("unexpected summary: %q", summary)
	}
	if pastedLineCount([]byte("single")) != 1 {
		t.Fatalf("expected single line count")
	}
	if pastedLineCount(nil) != 0 {
		t.Fatalf("expected empty paste line count")
	}
}

func TestNormalizePastedChunks_TrimsInvalidTail(t *testing.T) {
	chunks := []pastedChunk{
		{end: 5, rawLen: 3, dispLen: 8},
		{end: 10, rawLen: 4, dispLen: 9},
	}

	normalized := normalizePastedChunks(chunks, 6)
	if len(normalized) != 1 {
		t.Fatalf("expected one valid chunk, got %d", len(normalized))
	}
	if normalized[0].end != 5 {
		t.Fatalf("unexpected remaining chunk end: %d", normalized[0].end)
	}
}

func TestNormalizePastedChunks_DropsAllInvalidChunks(t *testing.T) {
	chunks := []pastedChunk{{end: 12, rawLen: 6, dispLen: 20}}
	normalized := normalizePastedChunks(chunks, 4)
	if len(normalized) != 0 {
		t.Fatalf("expected no valid chunks, got %d", len(normalized))
	}
}

func TestHistoryView_UsesMostRecentFirstOrder(t *testing.T) {
	h := &cmdHistory{entries: []string{"first", "second", "third"}}
	v := historyView{h: h}

	if v.Len() != 3 {
		t.Fatalf("expected len=3, got %d", v.Len())
	}
	if got := v.At(0); got != "third" {
		t.Fatalf("expected most recent entry, got %q", got)
	}
	if got := v.At(2); got != "first" {
		t.Fatalf("expected oldest entry at tail index, got %q", got)
	}
}

func TestHistoryView_AddIsNoop(t *testing.T) {
	h := &cmdHistory{entries: []string{"one"}}
	v := historyView{h: h}
	v.Add("two")
	if len(h.entries) != 1 {
		t.Fatalf("expected Add to be no-op, got %d entries", len(h.entries))
	}
}
