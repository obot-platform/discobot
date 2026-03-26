package agentimpl

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/obot-platform/discobot/agent-go/message"
	"github.com/obot-platform/discobot/agent-go/thread"
)

func TestFormatRuntimeEnvironmentReminder_IncludesSnapshotDetails(t *testing.T) {
	cwd := t.TempDir()
	got := formatRuntimeEnvironmentReminder(cwd, "")

	if !strings.Contains(got, "<system-reminder>") {
		t.Error("missing <system-reminder> start tag")
	}
	if !strings.Contains(got, "</system-reminder>") {
		t.Error("missing </system-reminder> end tag")
	}
	if !strings.Contains(got, "Current working directory:") {
		t.Error("missing current working directory line")
	}
	if !strings.Contains(got, "OS/platform:") {
		t.Error("missing OS/platform line")
	}
	if !strings.Contains(got, "Current date/time:") {
		t.Error("missing current date/time line")
	}
	if !strings.Contains(got, "Git state (captured at the current time of this reminder; this may change throughout the conversation):") {
		t.Error("missing git state snapshot disclaimer")
	}
	if strings.Contains(got, "Current model:") {
		t.Error("expected no model line when modelName is empty")
	}
}

func TestFormatRuntimeEnvironmentReminder_IncludesModelName(t *testing.T) {
	cwd := t.TempDir()
	got := formatRuntimeEnvironmentReminder(cwd, "Claude Sonnet 4")

	if !strings.Contains(got, "- Current model: Claude Sonnet 4") {
		t.Errorf("expected model line, got %q", got)
	}
}

func TestFormatModeChangeReminder_IncludesTargetMode(t *testing.T) {
	plan := formatModeChangeReminder(true)
	if !strings.Contains(plan, "<system-reminder>") || !strings.Contains(plan, "</system-reminder>") {
		t.Error("plan reminder should be wrapped in <system-reminder> tags")
	}
	if !strings.Contains(plan, "mode is now plan") {
		t.Errorf("expected plan reminder to mention plan mode, got %q", plan)
	}

	build := formatModeChangeReminder(false)
	if !strings.Contains(build, "mode is now build") {
		t.Errorf("expected build reminder to mention build mode, got %q", build)
	}
}

func TestResolvePlanMode_OnlyChangesOnExplicitRequest(t *testing.T) {
	cfgPlan := thread.Config{PlanMode: true}
	cfgBuild := thread.Config{PlanMode: false}

	planMode, changed := resolvePlanMode("", cfgPlan, true)
	if !planMode || changed {
		t.Fatalf("empty mode should keep current mode=true with changed=false, got planMode=%v changed=%v", planMode, changed)
	}

	planMode, changed = resolvePlanMode("plan", cfgBuild, true)
	if !planMode || !changed {
		t.Fatalf("plan request from build should change to plan, got planMode=%v changed=%v", planMode, changed)
	}

	planMode, changed = resolvePlanMode("plan", cfgPlan, true)
	if !planMode || changed {
		t.Fatalf("plan request from plan should not change, got planMode=%v changed=%v", planMode, changed)
	}

	planMode, changed = resolvePlanMode("", cfgBuild, false)
	if planMode || changed {
		t.Fatalf("missing config and empty mode should default to build with changed=false, got planMode=%v changed=%v", planMode, changed)
	}

	planMode, changed = resolvePlanMode("plan", cfgBuild, false)
	if !planMode || changed {
		t.Fatalf("explicit plan without prior config should set plan but not mark changed, got planMode=%v changed=%v", planMode, changed)
	}

	planMode, changed = resolvePlanMode("build", cfgPlan, true)
	if planMode || !changed {
		t.Fatalf("explicit build from plan should change to build, got planMode=%v changed=%v", planMode, changed)
	}
}

func TestGeneratedThreadName_UsesFirstMeaningfulText(t *testing.T) {
	got := generatedThreadName([]message.UIPart{
		message.UIFilePart{Type: "file", URL: "file:///tmp/input.txt", MediaType: "text/plain"},
		message.UITextPart{Text: "  Fix the failing CI build for agent-go  ", State: "done"},
	})
	if got != "Fix the failing CI build for agent-go" {
		t.Fatalf("generatedThreadName() = %q", got)
	}
}

func TestGeneratedThreadName_StripsLeadingSlashCommand(t *testing.T) {
	got := generatedThreadName([]message.UIPart{
		message.UITextPart{Text: "/commit fix thread naming in agent-go", State: "done"},
	})
	if got != "fix thread naming in agent-go" {
		t.Fatalf("generatedThreadName() = %q", got)
	}
}

func TestGeneratedThreadName_TruncatesLongText(t *testing.T) {
	got := generatedThreadName([]message.UIPart{
		message.UITextPart{Text: strings.Repeat("a", generatedThreadNameMaxRunes+10), State: "done"},
	})
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("expected ellipsis suffix, got %q", got)
	}
	if len([]rune(got)) != generatedThreadNameMaxRunes {
		t.Fatalf("expected %d runes, got %d", generatedThreadNameMaxRunes, len([]rune(got)))
	}
}

func TestShouldGenerateThreadName_OnlyUsesUnsetNames(t *testing.T) {
	if !shouldGenerateThreadName(thread.Config{}) {
		t.Fatal("expected empty config to be eligible")
	}
	if shouldGenerateThreadName(thread.Config{Name: "Generated", NameSource: thread.ThreadNameSourceGenerated}) {
		t.Fatal("did not expect existing generated name to remain eligible")
	}
	if shouldGenerateThreadName(thread.Config{Name: "Custom", NameSource: thread.ThreadNameSourceUser}) {
		t.Fatal("did not expect non-empty name to be eligible")
	}
}

func TestGitStateSnapshot_NotGitRepo(t *testing.T) {
	got := gitStateSnapshot(t.TempDir())
	if got != "not a git repository" {
		t.Errorf("gitStateSnapshot() = %q, want %q", got, "not a git repository")
	}
}

func TestGitStateSnapshot_CleanAndDirty(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}

	repo := t.TempDir()
	runGit(t, repo, "init", "-q")

	clean := gitStateSnapshot(repo)
	if !strings.Contains(clean, "branch=") {
		t.Errorf("expected branch in clean snapshot, got %q", clean)
	}
	if !strings.Contains(clean, "working_tree=clean") {
		t.Errorf("expected clean working tree, got %q", clean)
	}

	if err := os.WriteFile(filepath.Join(repo, "dirty.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write dirty file: %v", err)
	}

	dirty := gitStateSnapshot(repo)
	if !strings.Contains(dirty, "working_tree=dirty") {
		t.Errorf("expected dirty working tree, got %q", dirty)
	}
}

func runGit(t *testing.T, cwd string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", cwd}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
}
