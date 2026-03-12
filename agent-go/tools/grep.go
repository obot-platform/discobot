package tools

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"

	internalgrep "github.com/obot-platform/discobot/agent-go/internal/grep"
	"github.com/obot-platform/discobot/agent-go/message"
	"github.com/obot-platform/discobot/agent-go/thread"
)

// rgOnce guards the one-time ripgrep availability check.
var (
	rgOnce      sync.Once
	rgAvailable bool
)

// isRgAvailable returns true if the rg binary is on PATH, checked exactly once.
// Set DISCOBOT_NO_RIPGREP=1 to force the pure-Go fallback regardless.
func isRgAvailable() bool {
	rgOnce.Do(func() {
		if os.Getenv("DISCOBOT_NO_RIPGREP") == "1" {
			return
		}
		_, err := exec.LookPath("rg")
		rgAvailable = err == nil
	})
	return rgAvailable
}

type grepInput struct {
	Pattern         string `json:"pattern"`
	Path            string `json:"path"`
	Type            string `json:"type"`        // rg --type
	Glob            string `json:"glob"`        // rg --glob
	OutputMode      string `json:"output_mode"` // "content", "files_with_matches", "count"
	CaseInsensitive bool   `json:"i"`
	Context         int    `json:"context"` // -C lines
	After           int    `json:"A"`       // lines after
	Before          int    `json:"B"`       // lines before
	LineNumbers     bool   `json:"n"`       // show line numbers (default true)
	HeadLimit       int    `json:"head_limit"`
	Offset          int    `json:"offset"`
	Multiline       bool   `json:"multiline"`
}

func (e *Executor) executeGrep(ctx context.Context, call message.ToolCallPart) (thread.ToolExecuteResult, error) {
	var input grepInput
	if err := unmarshalInput(call, &input); err != nil {
		return errResult(call, err.Error()), nil
	}
	if input.Pattern == "" {
		return errResult(call, "pattern is required"), nil
	}

	// Determine search path.
	searchPath := e.cwd
	if input.Path != "" {
		searchPath = resolvePath(e.cwd, input.Path)
	}

	if !isRgAvailable() {
		return e.executeGrepFallback(ctx, call, input, searchPath)
	}

	args := buildRgArgs(input, searchPath)

	cmd := exec.Command("rg", args...)
	cmd.Dir = e.cwd
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	// rg exits 1 when no matches, 2 on error.
	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if ok && exitErr.ExitCode() == 1 {
			return textResult(call, "No matches found"), nil
		}
		if ok && exitErr.ExitCode() == 2 {
			errMsg := strings.TrimSpace(stderr.String())
			if errMsg == "" {
				errMsg = err.Error()
			}
			return errResult(call, "grep error: "+errMsg), nil
		}
		// Unexpected error from rg.
		return errResult(call, fmt.Sprintf("grep error: %v", err)), nil
	}

	output := stdout.String()

	// Apply offset + head_limit filtering.
	if input.Offset > 0 || input.HeadLimit > 0 {
		output = applyOffsetLimit(output, input.Offset, input.HeadLimit)
	}

	if output == "" {
		return textResult(call, "No matches found"), nil
	}
	return textResult(call, output), nil
}

// executeGrepFallback uses the internal pure-Go grep package when rg is unavailable.
func (e *Executor) executeGrepFallback(ctx context.Context, call message.ToolCallPart, input grepInput, searchPath string) (thread.ToolExecuteResult, error) {
	opts := internalgrep.GrepOptions{
		Pattern:         input.Pattern,
		Path:            searchPath,
		Type:            input.Type,
		Glob:            input.Glob,
		OutputMode:      input.OutputMode,
		CaseInsensitive: input.CaseInsensitive,
		Context:         input.Context,
		After:           input.After,
		Before:          input.Before,
		LineNumbers:     true,
		HeadLimit:       input.HeadLimit,
		Offset:          input.Offset,
		Multiline:       input.Multiline,
	}

	results, err := internalgrep.Grep(ctx, opts)
	if err != nil {
		return errResult(call, "grep error: "+err.Error()), nil
	}

	if len(results.Files) == 0 {
		return textResult(call, "No matches found"), nil
	}

	output := formatGrepResults(results, input.OutputMode)
	if output == "" {
		return textResult(call, "No matches found"), nil
	}
	return textResult(call, output), nil
}

// formatGrepResults formats internal grep results as ripgrep-style text output.
func formatGrepResults(results *internalgrep.Results, outputMode string) string {
	var sb strings.Builder
	switch outputMode {
	case "files_with_matches":
		for _, f := range results.Files {
			sb.WriteString(f.Path)
			sb.WriteByte('\n')
		}
	case "count":
		for _, f := range results.Files {
			fmt.Fprintf(&sb, "%s:%d\n", f.Path, f.Count)
		}
	default: // "content"
		for _, f := range results.Files {
			for i, m := range f.Matches {
				withContext := len(m.Before) > 0 || len(m.After) > 0
				if withContext && i > 0 {
					sb.WriteString("--\n")
				}
				for j, line := range m.Before {
					lineNum := m.LineNumber - (len(m.Before) - j)
					fmt.Fprintf(&sb, "%s-%d-%s\n", m.Path, lineNum, line)
				}
				fmt.Fprintf(&sb, "%s:%d:%s\n", m.Path, m.LineNumber, m.Line)
				for j, line := range m.After {
					fmt.Fprintf(&sb, "%s-%d-%s\n", m.Path, m.LineNumber+1+j, line)
				}
			}
		}
	}
	return sb.String()
}

// buildRgArgs constructs the ripgrep command arguments.
func buildRgArgs(input grepInput, searchPath string) []string {
	var args []string

	// Output mode.
	switch input.OutputMode {
	case "files_with_matches":
		args = append(args, "-l")
	case "count":
		args = append(args, "--count")
	default: // "content" or empty
		// default rg behavior: show matching lines
	}

	// Case insensitive.
	if input.CaseInsensitive {
		args = append(args, "-i")
	}

	// Line numbers (default on for content mode).
	if input.OutputMode == "" || input.OutputMode == "content" {
		// Always include line numbers for content mode; -n is on by default in rg
		// when output is not a tty, but we force it.
		args = append(args, "-n")
	}

	// Context flags.
	if input.Context > 0 {
		args = append(args, fmt.Sprintf("-C%d", input.Context))
	} else {
		if input.Before > 0 {
			args = append(args, fmt.Sprintf("-B%d", input.Before))
		}
		if input.After > 0 {
			args = append(args, fmt.Sprintf("-A%d", input.After))
		}
	}

	// Multiline.
	if input.Multiline {
		args = append(args, "-U", "--multiline-dotall")
	}

	// File type filter.
	if input.Type != "" {
		args = append(args, "--type", input.Type)
	}

	// Glob filter.
	if input.Glob != "" {
		args = append(args, "--glob", input.Glob)
	}

	// Pattern.
	args = append(args, "--regexp", input.Pattern)

	// Search path.
	args = append(args, searchPath)

	return args
}

// applyOffsetLimit skips the first `offset` lines and returns at most `limit` lines.
func applyOffsetLimit(output string, offset, limit int) string {
	lines := strings.Split(output, "\n")
	// Remove trailing empty line.
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	if offset > 0 && offset < len(lines) {
		lines = lines[offset:]
	} else if offset >= len(lines) {
		return ""
	}

	if limit > 0 && limit < len(lines) {
		lines = lines[:limit]
	}

	return strings.Join(lines, "\n") + "\n"
}
