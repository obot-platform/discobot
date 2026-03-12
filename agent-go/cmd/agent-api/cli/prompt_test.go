package cli

import "testing"

func TestShellQuote(t *testing.T) {
	if got := shellQuote("thread-123"); got != "'thread-123'" {
		t.Fatalf("shellQuote() = %q", got)
	}
	if got := shellQuote("a'b"); got != "'a'\"'\"'b'" {
		t.Fatalf("shellQuote() escaped quote = %q", got)
	}
}

func TestResumeThreadCommand(t *testing.T) {
	got := resumeThreadCommand("thread-123", "/usr/local/bin/agent api")
	want := "'/usr/local/bin/agent api' --resume 'thread-123'"
	if got != want {
		t.Fatalf("resumeThreadCommand() = %q, want %q", got, want)
	}

	if got := resumeThreadCommand("", "agent-api"); got != "" {
		t.Fatalf("expected empty command for empty thread id, got %q", got)
	}
}
