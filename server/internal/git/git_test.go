package git

import (
	"testing"
)

// TestDefaultRef verifies defaultRef returns HEAD when empty.
func TestDefaultRef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ref  string
		want string
	}{
		{name: "empty string returns HEAD", ref: "", want: "HEAD"},
		{name: "non-empty passes through", ref: "main", want: "main"},
		{name: "SHA passes through", ref: "abc123def456", want: "abc123def456"},
		{name: "refs/heads passes through", ref: "refs/heads/main", want: "refs/heads/main"},
		{name: "HEAD passes through", ref: "HEAD", want: "HEAD"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := defaultRef(tt.ref)
			if got != tt.want {
				t.Errorf("defaultRef(%q) = %q, want %q", tt.ref, got, tt.want)
			}
		})
	}
}

// TestBranchReferenceName validates branch reference name parsing.
func TestBranchReferenceName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		ref      string
		wantName string
		wantOK   bool
	}{
		{
			name:   "empty string returns false",
			ref:    "",
			wantOK: false,
		},
		{
			name:     "simple branch name",
			ref:      "main",
			wantName: "refs/heads/main",
			wantOK:   true,
		},
		{
			name:   "branch with slash returns false",
			ref:    "feature/my-feature",
			wantOK: false,
		},
		{
			name:     "explicit refs/heads prefix",
			ref:      "refs/heads/main",
			wantName: "refs/heads/main",
			wantOK:   true,
		},
		{
			name:   "refs/tags prefix returns false",
			ref:    "refs/tags/v1.0.0",
			wantOK: false,
		},
		{
			name:   "refs/remotes returns false",
			ref:    "refs/remotes/origin/main",
			wantOK: false,
		},
		{
			name:     "branch with hyphen",
			ref:      "my-branch",
			wantName: "refs/heads/my-branch",
			wantOK:   true,
		},
		{
			name:     "branch with numbers",
			ref:      "release-1234",
			wantName: "refs/heads/release-1234",
			wantOK:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotName, gotOK := branchReferenceName(tt.ref)
			if gotOK != tt.wantOK {
				t.Errorf("branchReferenceName(%q) ok = %v, want %v", tt.ref, gotOK, tt.wantOK)
			}
			if tt.wantOK && string(gotName) != tt.wantName {
				t.Errorf("branchReferenceName(%q) name = %q, want %q", tt.ref, gotName, tt.wantName)
			}
		})
	}
}
