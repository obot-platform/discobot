package tools

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/obot-platform/discobot/agent-go/message"
	"github.com/obot-platform/discobot/agent-go/thread"
)

type globInput struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path"` // optional directory to search in
}

// globSkipDirs are directory names that are always skipped during glob walks.
// This mirrors the skip logic in internal/grep/walker.go.
var globSkipDirs = map[string]bool{
	"node_modules": true,
	"__pycache__":  true,
	".git":         true,
}

func (e *Executor) executeGlob(call message.ToolCallPart) (thread.ToolExecuteResult, error) {
	var input globInput
	if err := unmarshalInput(call, &input); err != nil {
		return errResult(call, err.Error()), nil
	}
	if input.Pattern == "" {
		return errResult(call, "pattern is required"), nil
	}

	// Validate the pattern before walking.
	if !doublestar.ValidatePattern(input.Pattern) {
		return errResult(call, "invalid glob pattern"), nil
	}

	// Determine search root.
	root := e.cwd
	if input.Path != "" {
		root = resolvePath(e.cwd, input.Path)
	}

	// For absolute patterns, extract the relative portion against root if
	// possible, otherwise fall back to the pattern as-is.
	matchPattern := input.Pattern
	if filepath.IsAbs(input.Pattern) {
		rel, relErr := filepath.Rel(root, input.Pattern)
		if relErr == nil {
			matchPattern = rel
		}
	}

	// Walk the filesystem using filepath.WalkDir, which does not follow
	// symbolic links. This avoids traversing node_modules symlinks (and
	// similar) that would otherwise flood results with thousands of
	// irrelevant package files.
	var matched []string
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if d.IsDir() {
			name := d.Name()
			// Skip common non-source directories and hidden directories.
			if globSkipDirs[name] || (strings.HasPrefix(name, ".") && name != ".") {
				return filepath.SkipDir
			}
			return nil
		}
		// Match the relative path against the pattern.
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		ok, matchErr := doublestar.Match(matchPattern, rel)
		if matchErr == nil && ok {
			matched = append(matched, rel)
		}
		return nil
	})
	if walkErr != nil {
		return errResult(call, "error walking directory: "+walkErr.Error()), nil
	}

	if len(matched) == 0 {
		return textResult(call, "No files found"), nil
	}

	sort.Strings(matched)

	var sb strings.Builder
	for _, p := range matched {
		sb.WriteString(p)
		sb.WriteByte('\n')
	}

	return textResult(call, strings.TrimRight(sb.String(), "\n")), nil
}
