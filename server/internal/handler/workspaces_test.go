package handler

import (
	"strings"
	"testing"
)

// TestNormalizeGitPath verifies git path normalization.
func TestNormalizeGitPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "github shorthand owner/repo",
			input: "owner/repo",
			want:  "https://github.com/owner/repo",
		},
		{
			name:  "github shorthand with dots",
			input: "owner/my.repo",
			want:  "https://github.com/owner/my.repo",
		},
		{
			name:  "github.com/ prefix gets https",
			input: "github.com/owner/repo",
			want:  "https://github.com/owner/repo",
		},
		{
			name:  "www.github.com prefix normalized",
			input: "www.github.com/owner/repo",
			want:  "https://github.com/owner/repo",
		},
		{
			name:  "https URL passes through",
			input: "https://github.com/owner/repo.git",
			want:  "https://github.com/owner/repo.git",
		},
		{
			name:  "git@ SSH URL passes through",
			input: "git@github.com:owner/repo.git",
			want:  "git@github.com:owner/repo.git",
		},
		{
			name:  "whitespace is trimmed",
			input: "  owner/repo  ",
			want:  "https://github.com/owner/repo",
		},
		{
			name:  "local path passes through",
			input: "/local/path/to/repo",
			want:  "/local/path/to/repo",
		},
		{
			name:  "arbitrary URL passes through",
			input: "https://gitlab.com/group/repo.git",
			want:  "https://gitlab.com/group/repo.git",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := normalizeGitPath(tt.input)
			if got != tt.want {
				t.Errorf("normalizeGitPath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestLooksLikeGitRepositoryInput verifies git repository input detection.
func TestLooksLikeGitRepositoryInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		// Should look like git repos
		{name: "github shorthand", input: "owner/repo", want: true},
		{name: "github shorthand with dot", input: "owner/my.repo", want: true},
		{name: "github.com/ prefix", input: "github.com/owner/repo", want: true},
		{name: "www.github.com prefix", input: "www.github.com/owner/repo", want: true},
		{name: "https URL", input: "https://github.com/owner/repo.git", want: true},
		{name: "http URL", input: "http://gitlab.com/group/repo.git", want: true},
		{name: "git@ SSH with colon", input: "git@github.com:owner/repo.git", want: true},
		{name: "ssh:// URL", input: "ssh://git@github.com/owner/repo.git", want: true},

		// Should NOT look like git repos
		{name: "empty string", input: "", want: false},
		{name: "whitespace only", input: "   ", want: false},
		{name: "local path", input: "/local/path", want: false},
		{name: "relative path", input: "./project", want: false},
		{name: "plain name", input: "myproject", want: false},
		{name: "git@ without colon", input: "git@github.com", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := looksLikeGitRepositoryInput(tt.input)
			if got != tt.want {
				t.Errorf("looksLikeGitRepositoryInput(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// TestSplitGitHubOwnerRepoPrefix verifies owner/repo splitting.
func TestSplitGitHubOwnerRepoPrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		input          string
		wantOwner      string
		wantRepoPrefix string
		wantOK         bool
	}{
		{
			name:           "owner/repo",
			input:          "owner/repo",
			wantOwner:      "owner",
			wantRepoPrefix: "repo",
			wantOK:         true,
		},
		{
			name:           "owner/partial-repo",
			input:          "owner/my-app",
			wantOwner:      "owner",
			wantRepoPrefix: "my-app",
			wantOK:         true,
		},
		{
			name:           "owner/ no repo prefix",
			input:          "owner/",
			wantOwner:      "owner",
			wantRepoPrefix: "",
			wantOK:         true,
		},
		{
			name:   "no slash",
			input:  "owner",
			wantOK: false,
		},
		{
			name:   "empty string",
			input:  "",
			wantOK: false,
		},
		{
			name:   "whitespace only",
			input:  "   ",
			wantOK: false,
		},
		{
			name:   "slash at start",
			input:  "/repo",
			wantOK: false, // empty owner
		},
		{
			name:           "whitespace trimmed",
			input:          "  owner / repo  ",
			wantOwner:      "owner",
			wantRepoPrefix: "repo",
			wantOK:         true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotOwner, gotRepo, gotOK := splitGitHubOwnerRepoPrefix(tt.input)
			if gotOK != tt.wantOK {
				t.Errorf("splitGitHubOwnerRepoPrefix(%q) ok = %v, want %v", tt.input, gotOK, tt.wantOK)
			}
			if tt.wantOK {
				if gotOwner != tt.wantOwner {
					t.Errorf("owner = %q, want %q", gotOwner, tt.wantOwner)
				}
				if gotRepo != tt.wantRepoPrefix {
					t.Errorf("repoPrefix = %q, want %q", gotRepo, tt.wantRepoPrefix)
				}
			}
		})
	}
}

// TestBuildGitHubSearchQuery verifies search query construction.
func TestBuildGitHubSearchQuery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "simple name searches in name",
			input: "myrepo",
			want:  "myrepo in:name",
		},
		{
			name:  "owner/repo searches by owner",
			input: "owner/repo",
			want:  "repo in:name user:owner",
		},
		{
			name:  "owner/ with no repo prefix normalizes to just owner",
			input: "owner/",
			want:  "owner in:name",
		},
		{
			name:  "empty string returns empty",
			input: "",
			want:  "",
		},
		{
			name:  "whitespace only returns empty",
			input: "   ",
			want:  "",
		},
		{
			name:  "git@ URL gets normalized to owner/repo",
			input: "git@github.com:owner/repo.git",
			want:  "repo in:name user:owner",
		},
		{
			name:  "https github URL gets normalized",
			input: "https://github.com/owner/repo.git",
			want:  "repo in:name user:owner",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := buildGitHubSearchQuery(tt.input)
			if got != tt.want {
				t.Errorf("buildGitHubSearchQuery(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestNormalizeGitHubRepoQuery verifies GitHub repo query normalization.
func TestNormalizeGitHubRepoQuery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty string returns empty",
			input: "",
			want:  "",
		},
		{
			name:  "whitespace returns empty",
			input: "   ",
			want:  "",
		},
		{
			name:  "plain name passes through",
			input: "myrepo",
			want:  "myrepo",
		},
		{
			name:  "owner/repo passes through",
			input: "owner/repo",
			want:  "owner/repo",
		},
		{
			name:  "git@ normalized",
			input: "git@github.com:owner/repo.git",
			want:  "owner/repo",
		},
		{
			name:  "https github URL normalized",
			input: "https://github.com/owner/repo.git",
			want:  "owner/repo",
		},
		{
			name:  "https github URL without .git",
			input: "https://github.com/owner/repo",
			want:  "owner/repo",
		},
		{
			name:  ".git suffix removed",
			input: "owner/repo.git",
			want:  "owner/repo",
		},
		{
			name:  "leading/trailing slashes removed",
			input: "/owner/repo/",
			want:  "owner/repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := normalizeGitHubRepoQuery(tt.input)
			if got != tt.want {
				t.Errorf("normalizeGitHubRepoQuery(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestIsGitHubRepositoryURL verifies GitHub URL detection.
func TestIsGitHubRepositoryURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "https github.com", input: "https://github.com/owner/repo", want: true},
		{name: "http github.com", input: "http://github.com/owner/repo", want: true},
		{name: "git@ github.com", input: "git@github.com:owner/repo.git", want: true},
		{name: "www.github.com", input: "https://www.github.com/owner/repo", want: true},
		{name: "uppercase GitHub.com", input: "https://GitHub.com/owner/repo", want: true},
		{name: "gitlab URL", input: "https://gitlab.com/owner/repo", want: false},
		{name: "bitbucket URL", input: "https://bitbucket.org/owner/repo", want: false},
		{name: "local path", input: "/local/path", want: false},
		{name: "empty string", input: "", want: false},
		{name: "arbitrary domain", input: "https://example.com/repo", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isGitHubRepositoryURL(tt.input)
			if got != tt.want {
				t.Errorf("isGitHubRepositoryURL(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// TestIsGitHubRepositoryNotFound verifies error message detection.
func TestIsGitHubRepositoryNotFound(t *testing.T) {
	t.Parallel()

	t.Run("nil error returns false", func(t *testing.T) {
		t.Parallel()
		if isGitHubRepositoryNotFound(nil) {
			t.Error("expected false for nil error")
		}
	})

	tests := []struct {
		name   string
		errMsg string
		want   bool
	}{
		{name: "repository not found lowercase", errMsg: "repository not found", want: true},
		{name: "repository not found uppercase", errMsg: "Repository Not Found", want: true},
		{name: "other error", errMsg: "connection refused", want: false},
		{name: "empty error", errMsg: "", want: false},
		{name: "partial match", errMsg: "not found", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := &mockError{msg: tt.errMsg}
			got := isGitHubRepositoryNotFound(err)
			if got != tt.want {
				t.Errorf("isGitHubRepositoryNotFound(%q) = %v, want %v", tt.errMsg, got, tt.want)
			}
		})
	}
}

type mockError struct{ msg string }

func (e *mockError) Error() string { return e.msg }

// TestNormalizeGitHubRepoQuery_EdgeCases tests edge cases in normalization.
func TestNormalizeGitHubRepoQuery_EdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		input       string
		wantContain string
	}{
		{
			name:        "github SSH URL with multiple dots",
			input:       "git@github.com:org/my.dotted.repo.git",
			wantContain: "org/my.dotted.repo",
		},
		{
			name:        "https with trailing slash",
			input:       "https://github.com/owner/repo/",
			wantContain: "owner/repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := normalizeGitHubRepoQuery(tt.input)
			if !strings.Contains(got, tt.wantContain) {
				t.Errorf("normalizeGitHubRepoQuery(%q) = %q, expected to contain %q", tt.input, got, tt.wantContain)
			}
		})
	}
}
