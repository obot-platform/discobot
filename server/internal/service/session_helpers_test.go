package service

import (
	"strings"
	"testing"
)

// TestNormalizeSessionStatus verifies status normalization.
func TestNormalizeSessionStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status string
		want   string
	}{
		{
			name:   "legacy running maps to ready",
			status: "running",
			want:   "ready",
		},
		{
			name:   "ready passes through",
			status: "ready",
			want:   "ready",
		},
		{
			name:   "stopped passes through",
			status: "stopped",
			want:   "stopped",
		},
		{
			name:   "failed passes through",
			status: "failed",
			want:   "failed",
		},
		{
			name:   "empty passes through",
			status: "",
			want:   "",
		},
		{
			name:   "removing passes through",
			status: "removing",
			want:   "removing",
		},
		{
			name:   "removed passes through",
			status: "removed",
			want:   "removed",
		},
		{
			name:   "unknown status passes through",
			status: "initializing",
			want:   "initializing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := normalizeSessionStatus(tt.status)
			if got != tt.want {
				t.Errorf("normalizeSessionStatus(%q) = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

// TestCommitsNoOpError verifies the CommitsNoOpError.Error() output.
func TestCommitsNoOpError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        *CommitsNoOpError
		wantSubstr string
	}{
		{
			name: "clean workspace",
			err: &CommitsNoOpError{
				IsClean:    true,
				HeadCommit: "abc123def456",
			},
			wantSubstr: "abc123def456",
		},
		{
			name: "dirty workspace",
			err: &CommitsNoOpError{
				IsClean:    false,
				HeadCommit: "deadbeef",
			},
			wantSubstr: "deadbeef",
		},
		{
			name: "contains no_commits marker",
			err: &CommitsNoOpError{
				IsClean:    true,
				HeadCommit: "sha123",
			},
			wantSubstr: "no_commits",
		},
		{
			name: "clean flag is reflected",
			err: &CommitsNoOpError{
				IsClean:    true,
				HeadCommit: "sha456",
			},
			wantSubstr: "isClean=true",
		},
		{
			name: "not clean flag is reflected",
			err: &CommitsNoOpError{
				IsClean:    false,
				HeadCommit: "sha789",
			},
			wantSubstr: "isClean=false",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			msg := tt.err.Error()
			if !strings.Contains(msg, tt.wantSubstr) {
				t.Errorf("CommitsNoOpError.Error() = %q, want to contain %q", msg, tt.wantSubstr)
			}
		})
	}
}

// TestValidateSessionID_AdditionalCases tests additional session ID validation scenarios.
func TestValidateSessionID_AdditionalCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		sessionID string
		wantErr   bool
		errSubstr string
	}{
		{
			name:      "single character",
			sessionID: "a",
			wantErr:   false,
		},
		{
			name:      "all uppercase",
			sessionID: "ABCDEF",
			wantErr:   false,
		},
		{
			name:      "mixed case with hyphens and numbers",
			sessionID: "Session-123-ABC",
			wantErr:   false,
		},
		{
			name:      "underscore not allowed",
			sessionID: "session_123",
			wantErr:   true,
			errSubstr: "alphanumeric",
		},
		{
			name:      "space not allowed",
			sessionID: "session 123",
			wantErr:   true,
			errSubstr: "alphanumeric",
		},
		{
			name:      "dot not allowed",
			sessionID: "session.123",
			wantErr:   true,
			errSubstr: "alphanumeric",
		},
		{
			name:      "slash not allowed",
			sessionID: "session/123",
			wantErr:   true,
			errSubstr: "alphanumeric",
		},
		{
			name:      "exactly max length (65 chars)",
			sessionID: strings.Repeat("a", 65),
			wantErr:   false,
		},
		{
			name:      "one over max length (66 chars)",
			sessionID: strings.Repeat("a", 66),
			wantErr:   true,
			errSubstr: "maximum length",
		},
		{
			name:      "leading hyphen",
			sessionID: "-session",
			wantErr:   false, // hyphens are allowed anywhere
		},
		{
			name:      "trailing hyphen",
			sessionID: "session-",
			wantErr:   false, // hyphens are allowed anywhere
		},
		{
			name:      "only hyphens",
			sessionID: "---",
			wantErr:   false, // hyphens are valid chars
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateSessionID(tt.sessionID)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateSessionID(%q) error = %v, wantErr %v", tt.sessionID, err, tt.wantErr)
			}
			if err != nil && tt.errSubstr != "" && !strings.Contains(err.Error(), tt.errSubstr) {
				t.Errorf("expected error containing %q, got %q", tt.errSubstr, err.Error())
			}
		})
	}
}
