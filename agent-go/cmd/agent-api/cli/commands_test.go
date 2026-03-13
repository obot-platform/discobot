package cli

import (
	"context"
	"encoding/base64"
	"flag"
	"io"
	"iter"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/obot-platform/discobot/agent-go/internal/config"
	"github.com/obot-platform/discobot/agent-go/message"
	"github.com/obot-platform/discobot/agent-go/providers"
	"github.com/obot-platform/discobot/agent-go/thread"
)

type testModelListProvider struct {
	id     string
	models []providers.ModelInfo
}

func (p *testModelListProvider) ID() string { return p.id }

func (p *testModelListProvider) Complete(_ context.Context, _ providers.CompleteRequest) iter.Seq2[message.ProviderMessageChunk, error] {
	return func(func(message.ProviderMessageChunk, error) bool) {}
}

func (p *testModelListProvider) ListModels(_ context.Context) ([]providers.ModelInfo, error) {
	return p.models, nil
}

func (p *testModelListProvider) DefaultModels() map[string]providers.ModelRef { return nil }

func TestAddFlags_ShortAliases(t *testing.T) {
	oldCommandLine := flag.CommandLine
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	flag.CommandLine = fs
	defer func() {
		flag.CommandLine = oldCommandLine
	}()

	flags := AddFlags()
	if err := flag.CommandLine.Parse([]string{"-m", "anthropic/claude-sonnet-4", "-p", "-r", "thread-123"}); err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got := *flags.model; got != "anthropic/claude-sonnet-4" {
		t.Fatalf("model = %q, want %q", got, "anthropic/claude-sonnet-4")
	}
	if !*flags.plan {
		t.Fatal("expected -p alias to enable plan mode")
	}
	if got := *flags.resume; got != "thread-123" {
		t.Fatalf("resume = %q, want %q", got, "thread-123")
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

func TestHandleSlashCommand_PlanDoesNotCreateThreadBeforePrompt(t *testing.T) {
	threadsDir := t.TempDir()
	store := thread.NewStore(threadsDir)
	cfg := &config.Config{AgentCwd: threadsDir, ThreadsDir: threadsDir}
	planMode := false
	threadID := "thread-lazy"

	newThreadID, handled := handleSlashCommand(context.Background(), "/plan", nil, store, cfg, threadID, nil, nil, &planMode)
	if !handled {
		t.Fatalf("expected /plan to be handled locally")
	}
	if newThreadID != threadID {
		t.Fatalf("expected thread id to remain %q, got %q", threadID, newThreadID)
	}
	if !planMode {
		t.Fatalf("expected /plan to toggle plan mode on")
	}
	if _, err := os.Stat(filepath.Join(threadsDir, threadID)); !os.IsNotExist(err) {
		t.Fatalf("expected no thread directory before first prompt, stat err=%v", err)
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

func TestHandleModelsCommand_SortsModelList(t *testing.T) {
	reg := providers.NewProviderRegistry(nil)
	reg.Add(&testModelListProvider{
		id: "openai",
		models: []providers.ModelInfo{
			{ID: "gpt-4o", DisplayName: "GPT-4o"},
			{ID: "gpt-4.1", DisplayName: "GPT-4.1"},
		},
	})
	reg.Add(&testModelListProvider{
		id: "anthropic",
		models: []providers.ModelInfo{
			{ID: "claude-sonnet-4", DisplayName: "Claude Sonnet 4"},
			{ID: "claude-opus-4", DisplayName: "Claude Opus 4"},
		},
	})

	stdinReader, stdinWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdinReader.Close()
	defer stderrReader.Close()

	if _, err := stdinWriter.WriteString("\n"); err != nil {
		t.Fatal(err)
	}
	if err := stdinWriter.Close(); err != nil {
		t.Fatal(err)
	}

	oldStdin := os.Stdin
	oldStderr := os.Stderr
	oldNoColor := noColor
	os.Stdin = stdinReader
	os.Stderr = stderrWriter
	noColor = true
	defer func() {
		os.Stdin = oldStdin
		os.Stderr = oldStderr
		noColor = oldNoColor
	}()

	currentModel := ""
	handleModelsCommand(context.Background(), reg, &currentModel)

	if err := stderrWriter.Close(); err != nil {
		t.Fatal(err)
	}
	output, err := io.ReadAll(stderrReader)
	if err != nil {
		t.Fatal(err)
	}

	lines := []string{
		"1. anthropic/claude-opus-4 — Claude Opus 4",
		"2. anthropic/claude-sonnet-4 — Claude Sonnet 4",
		"3. openai/gpt-4.1 — GPT-4.1",
		"4. openai/gpt-4o — GPT-4o",
	}
	lastIndex := -1
	for _, line := range lines {
		idx := strings.Index(string(output), line)
		if idx == -1 {
			t.Fatalf("expected output to contain %q, got:\n%s", line, string(output))
		}
		if idx <= lastIndex {
			t.Fatalf("expected output line %q to appear after previous model, got:\n%s", line, string(output))
		}
		lastIndex = idx
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
