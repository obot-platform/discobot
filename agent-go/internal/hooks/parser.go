// Package hooks provides the hook system for post-completion evaluation.
// Hooks are executable scripts in .discobot/hooks/ with YAML front matter.
package hooks

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
)

// HooksDir is the directory within the workspace where hooks are defined.
const HooksDir = ".discobot/hooks"

// HookType is the type of hook.
type HookType string

const (
	HookTypeSession   HookType = "session"
	HookTypeFile      HookType = "file"
	HookTypePreCommit HookType = "pre-commit"
)

// Hook represents a discovered hook definition.
type Hook struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Type        HookType `json:"type"`
	Description string   `json:"description,omitempty"`
	Path        string   `json:"path"`
	RunAs       string   `json:"runAs"` // "root" or "user"
	Pattern     string   `json:"pattern,omitempty"`
	NotifyLLM   bool     `json:"notifyLlm"`
}

// hookConfig is the raw front matter config parsed from a hook file.
type hookConfig struct {
	Name        string
	Type        string
	Description string
	RunAs       string
	Pattern     string
	NotifyLLM   *bool // nil = default (true)
}

// Script extensions to strip when normalizing IDs.
var scriptExtensions = []string{
	".sh", ".bash", ".zsh", ".py", ".js", ".ts", ".rb", ".pl", ".php",
}

var nonAlphanumericRe = regexp.MustCompile(`[^a-z0-9_-]`)
var leadingTrailingHyphens = regexp.MustCompile(`^-+|-+$`)

// normalizeID converts a filename to a hook ID.
func normalizeID(filename string) string {
	id := filename
	lower := strings.ToLower(filename)
	for _, ext := range scriptExtensions {
		if strings.HasSuffix(lower, ext) {
			id = id[:len(id)-len(ext)]
			break
		}
	}
	id = strings.ReplaceAll(id, ".", "-")
	id = strings.ToLower(id)
	id = nonAlphanumericRe.ReplaceAllString(id, "")
	id = leadingTrailingHyphens.ReplaceAllString(id, "")
	return id
}

// delimiterStyle describes the front matter delimiter format.
type delimiterStyle struct {
	prefix    string // "" for plain, "#" for hash, "//" for slash
	delimiter string // "---", "#---", or "//---"
}

// detectDelimiter checks if a line is a front matter delimiter.
func detectDelimiter(line string) *delimiterStyle {
	trimmed := strings.TrimSpace(line)
	switch trimmed {
	case "---":
		return &delimiterStyle{prefix: "", delimiter: "---"}
	case "#---":
		return &delimiterStyle{prefix: "#", delimiter: "#---"}
	case "//---":
		return &delimiterStyle{prefix: "//", delimiter: "//---"}
	}
	return nil
}

// stripPrefix removes the comment prefix and leading whitespace from a content line.
func stripPrefix(line, prefix string) string {
	if prefix == "" {
		return line
	}
	idx := strings.Index(line, prefix)
	if idx == -1 {
		return line
	}
	return strings.TrimLeft(line[idx+len(prefix):], " \t")
}

// parseHookFrontMatter parses the front matter from a hook file's content.
func parseHookFrontMatter(content string) *hookConfig {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 {
		return nil
	}

	// Check for shebang
	startLine := 0
	if strings.HasPrefix(lines[0], "#!") {
		startLine = 1
	}

	if len(lines) <= startLine {
		return nil
	}

	delim := detectDelimiter(lines[startLine])
	if delim == nil {
		return nil
	}

	// Find closing delimiter
	var yamlLines []string
	found := false
	for i := startLine + 1; i < len(lines); i++ {
		if detectDelimiter(lines[i]) != nil {
			found = true
			break
		}
		yamlLines = append(yamlLines, stripPrefix(lines[i], delim.prefix))
	}

	if !found {
		return nil
	}

	// Parse simple YAML key-value pairs
	cfg := &hookConfig{}
	for _, line := range yamlLines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		colonIdx := strings.Index(trimmed, ":")
		if colonIdx == -1 {
			continue
		}
		key := strings.TrimSpace(trimmed[:colonIdx])
		value := strings.TrimSpace(trimmed[colonIdx+1:])

		// Remove quotes
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') ||
				(value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}

		switch key {
		case "name":
			cfg.Name = value
		case "type":
			cfg.Type = value
		case "description":
			cfg.Description = value
		case "run_as":
			cfg.RunAs = value
		case "pattern":
			cfg.Pattern = value
		case "notify_llm":
			v := strings.ToLower(value)
			b := v != "false" && v != "no" && v != "0"
			cfg.NotifyLLM = &b
		}
	}

	return cfg
}

// DiscoverHooks finds all valid hooks in the given hooks directory.
func DiscoverHooks(hooksDir string) ([]Hook, error) {
	entries, err := os.ReadDir(hooksDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var hooks []Hook
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		// Must be executable (Windows has no Unix-style execute bits, so all files qualify).
		if runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
			continue
		}

		filePath := filepath.Join(hooksDir, entry.Name())
		content, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		lines := strings.Split(string(content), "\n")
		if len(lines) == 0 || !strings.HasPrefix(lines[0], "#!") {
			continue
		}

		cfg := parseHookFrontMatter(string(content))
		if cfg == nil {
			continue
		}

		// Must have a valid type
		hookType := HookType(cfg.Type)
		switch hookType {
		case HookTypeSession, HookTypeFile, HookTypePreCommit:
			// valid
		default:
			continue
		}

		// File hooks must have a pattern
		if hookType == HookTypeFile && cfg.Pattern == "" {
			continue
		}

		id := normalizeID(entry.Name())
		name := cfg.Name
		if name == "" {
			name = id
		}
		runAs := cfg.RunAs
		if runAs == "" {
			runAs = "user"
		}
		notifyLLM := true
		if cfg.NotifyLLM != nil {
			notifyLLM = *cfg.NotifyLLM
		}

		hooks = append(hooks, Hook{
			ID:          id,
			Name:        name,
			Type:        hookType,
			Description: cfg.Description,
			Path:        filePath,
			RunAs:       runAs,
			Pattern:     cfg.Pattern,
			NotifyLLM:   notifyLLM,
		})
	}

	sort.Slice(hooks, func(i, j int) bool {
		return hooks[i].Name < hooks[j].Name
	})

	return hooks, nil
}
