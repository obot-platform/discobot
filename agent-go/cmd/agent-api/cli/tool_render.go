package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/obot-platform/discobot/agent-go/message"
)

// renderChunk prints a MessageChunk to stdout (text) or stderr (tool info).
// Text deltas stream directly to stdout so they can be piped; tool and
// lifecycle events go to stderr to keep them out of pipe output.
func renderChunk(chunk message.MessageChunk, md *markdownRenderer, tools *toolRenderState) {
	switch c := chunk.(type) {
	case message.TextDeltaChunk:
		if md != nil {
			md.WriteText(c.Delta)
		} else {
			fmt.Print(c.Delta)
		}

	case message.ReasoningStartChunk:
		if noColor {
			fmt.Fprint(os.Stderr, "[thinking]\n")
		} else {
			fmt.Fprint(os.Stderr, "\033[2m")
		}

	case message.ReasoningDeltaChunk:
		fmt.Fprint(os.Stderr, c.Delta)

	case message.ReasoningEndChunk:
		if noColor {
			fmt.Fprint(os.Stderr, "\n[/thinking]\n")
		} else {
			fmt.Fprint(os.Stderr, "\033[0m\n")
		}

	case message.ToolInputAvailableChunk:
		tools.rememberInput(c.ToolCallID, c.ToolName, c.Input)
		label := tools.labelFor(c.ToolCallID, c.ToolName)
		summary := toolInputSummary(c.ToolName, c.Input)
		if summary != "" {
			fmt.Fprintf(os.Stderr, "%s [%s] %s\n", styleToolStartArrow(), styleToolLabel(label), summary)
		} else {
			fmt.Fprintf(os.Stderr, "%s [%s]\n", styleToolStartArrow(), styleToolLabel(label))
		}

	case message.ToolOutputAvailableChunk:
		label := tools.labelFor(c.ToolCallID, "")
		text := extractOutputText(c.Output)
		detail := toolOutputDetail(tools.toolNameFor(c.ToolCallID), tools.inputFor(c.ToolCallID))
		renderToolTail(label, false, text, detail)

	case message.ToolOutputErrorChunk:
		label := tools.labelFor(c.ToolCallID, "")
		renderToolTail(label, true, c.ErrorText, toolErrorDetail(c.ErrorText))

	case message.ToolApprovalRequestChunk:
		// The turn will pause after the iterator ends; no action needed here.

	case message.ErrorChunk:
		fmt.Fprintf(os.Stderr, "[error: %s]\n", c.ErrorText)

	case message.AbortChunk:
		if c.Reason != "" {
			fmt.Fprintf(os.Stderr, "[aborted: %s]\n", c.Reason)
		}
	}
}

type toolRenderState struct {
	labels    map[string]string
	toolNames map[string]string
	inputs    map[string]json.RawMessage
}

func newToolRenderState() *toolRenderState {
	return &toolRenderState{
		labels:    map[string]string{},
		toolNames: map[string]string{},
		inputs:    map[string]json.RawMessage{},
	}
}

func (s *toolRenderState) rememberInput(toolCallID, toolName string, input json.RawMessage) {
	if s == nil {
		return
	}
	if toolName != "" {
		s.toolNames[toolCallID] = toolName
	}
	if len(input) > 0 {
		cp := make(json.RawMessage, len(input))
		copy(cp, input)
		s.inputs[toolCallID] = cp
	}
}

func (s *toolRenderState) toolNameFor(toolCallID string) string {
	if s == nil {
		return ""
	}
	return s.toolNames[toolCallID]
}

func (s *toolRenderState) inputFor(toolCallID string) json.RawMessage {
	if s == nil {
		return nil
	}
	return s.inputs[toolCallID]
}

func (s *toolRenderState) labelFor(toolCallID, toolName string) string {
	if s == nil {
		return buildToolLabel(toolCallID, toolName)
	}
	if toolName != "" {
		s.toolNames[toolCallID] = toolName
		label := buildToolLabel(toolCallID, toolName)
		s.labels[toolCallID] = label
		return label
	}
	if label, ok := s.labels[toolCallID]; ok {
		return label
	}
	fallbackName := s.toolNames[toolCallID]
	label := buildToolLabel(toolCallID, fallbackName)
	s.labels[toolCallID] = label
	return label
}

func buildToolLabel(toolCallID, toolName string) string {
	if toolName == "" {
		toolName = "tool"
	}
	return fmt.Sprintf("%s(%s)", toolName, shortToolID(toolCallID))
}

func shortToolID(id string) string {
	if id == "" {
		return "unknown"
	}
	if len(id) <= 8 {
		return id
	}
	return id[len(id)-8:]
}

func styleToolStartArrow() string {
	if noColor {
		return "→"
	}
	return "\033[36m→\033[0m"
}

func styleToolOutputArrow(isError bool) string {
	if noColor {
		return "←"
	}
	if isError {
		return "\033[31m←\033[0m"
	}
	return "\033[32m←\033[0m"
}

func styleToolLabel(label string) string {
	if noColor {
		return label
	}
	return "\033[1m" + label + "\033[0m"
}

func styleToolDivider() string {
	if noColor {
		return "    ------------------------------"
	}
	return "\033[2m    ------------------------------\033[0m"
}

func renderToolTail(label string, isError bool, text, detail string) {
	lineCount := countLines(text)
	kind := "output"
	if isError {
		kind = "error"
	}
	fmt.Fprintf(os.Stderr, "%s [%s] %s: %d lines\n", styleToolOutputArrow(isError), styleToolLabel(label), kind, lineCount)

	detail = strings.TrimSpace(strings.ReplaceAll(detail, "\r\n", "\n"))
	if detail == "" {
		return
	}

	fmt.Fprintln(os.Stderr, styleToolDivider())
	for _, line := range strings.Split(detail, "\n") {
		fmt.Fprintf(os.Stderr, "    %s\n", line)
	}
	fmt.Fprintln(os.Stderr, styleToolDivider())
}

func toolErrorDetail(text string) string {
	text = strings.TrimSpace(normalizeNewlines(text))
	if text == "" {
		return ""
	}
	return "tool output:\n" + text
}

func toolOutputDetail(toolName string, input json.RawMessage) string {
	switch strings.ToLower(toolName) {
	case "write":
		return writeOutputDetail(input)
	case "edit":
		return editOutputDetail(input)
	default:
		return ""
	}
}

func writeOutputDetail(input json.RawMessage) string {
	if len(input) == 0 {
		return ""
	}
	var payload struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return ""
	}
	if payload.Content == "" {
		return "written content:"
	}
	return "written content:\n" + normalizeNewlines(payload.Content)
}

func editOutputDetail(input json.RawMessage) string {
	if len(input) == 0 {
		return ""
	}
	var payload struct {
		OldString string `json:"old_string"`
		NewString string `json:"new_string"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return ""
	}
	if payload.OldString == "" && payload.NewString == "" {
		return ""
	}
	return "applied diff:\n" + renderLineDiff(payload.OldString, payload.NewString)
}

func renderLineDiff(oldText, newText string) string {
	oldLines := splitDiffLines(oldText)
	newLines := splitDiffLines(newText)
	if len(oldLines) == 0 && len(newLines) == 0 {
		return "--- old\n+++ new"
	}

	diffLines := lineDiff(oldLines, newLines)
	var b strings.Builder
	b.WriteString("--- old\n")
	b.WriteString("+++ new\n")
	for i, line := range diffLines {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(line)
	}
	return b.String()
}

func lineDiff(oldLines, newLines []string) []string {
	n := len(oldLines)
	m := len(newLines)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}

	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if oldLines[i] == newLines[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}

	var out []string
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case oldLines[i] == newLines[j]:
			out = append(out, " "+oldLines[i])
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			out = append(out, "-"+oldLines[i])
			i++
		default:
			out = append(out, "+"+newLines[j])
			j++
		}
	}
	for ; i < n; i++ {
		out = append(out, "-"+oldLines[i])
	}
	for ; j < m; j++ {
		out = append(out, "+"+newLines[j])
	}
	return out
}

func splitDiffLines(s string) []string {
	s = normalizeNewlines(s)
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func normalizeNewlines(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return s
}

func isPlanToolName(toolName string) bool {
	return toolName == "EnterPlanMode" || toolName == "ExitPlanMode"
}

func isExitPlanApproved(text string) bool {
	return strings.HasPrefix(strings.TrimSpace(text), "Plan approved")
}

// toolInputSummary extracts a short human-readable summary from tool input JSON.
// Returns "" if no suitable field is found.
func toolInputSummary(toolName string, input json.RawMessage) string {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(input, &obj); err != nil {
		return ""
	}

	switch strings.ToLower(toolName) {
	case "glob":
		return summarizeToolFields(obj, []string{"pattern", "path"})
	case "grep":
		return summarizeToolFields(obj, []string{"pattern", "path", "glob"})
	case "websearch":
		return summarizeToolFields(obj, []string{"query"})
	case "webfetch":
		return summarizeToolFields(obj, []string{"url"})
	}

	return summarizeToolFields(obj, []string{"command", "path", "old_path", "file_path", "url", "query", "pattern", "description"})
}

func summarizeToolFields(obj map[string]json.RawMessage, fields []string) string {
	parts := make([]string, 0, len(fields))
	for _, field := range fields {
		value := summaryStringField(obj, field)
		if value == "" {
			continue
		}
		parts = append(parts, field+": "+abbreviate(value, 80))
	}
	if len(parts) == 0 {
		return ""
	}
	return abbreviate(strings.Join(parts, " "), 120)
}

func summaryStringField(obj map[string]json.RawMessage, field string) string {
	v, ok := obj[field]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(v, &s); err != nil {
		return ""
	}
	return s
}

// extractOutputText pulls the human-readable text from tool output bytes.
//
// The format varies depending on how the chunk was produced:
//   - Bare JSON string: TextOutput marshalled as json.Marshal(value) → "\"...\""
//   - {"type":"text","value":"..."}: from MarshalToolResultOutput
//   - Raw JSON value: JSONOutput
func extractOutputText(output json.RawMessage) string {
	if len(output) == 0 {
		return ""
	}
	// Bare JSON string (most common for TextOutput).
	if output[0] == '"' {
		var s string
		if err := json.Unmarshal(output, &s); err == nil {
			return strings.TrimSpace(s)
		}
	}
	// Structured {"type":"...","value":"..."} from MarshalToolResultOutput.
	var obj struct {
		Type  string `json:"type"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(output, &obj); err == nil && obj.Value != "" {
		return strings.TrimSpace(obj.Value)
	}
	// Fallback: pretty-print whatever JSON we got.
	var v any
	if err := json.Unmarshal(output, &v); err == nil {
		if pretty, err := json.MarshalIndent(v, "", "  "); err == nil {
			return strings.TrimSpace(string(pretty))
		}
	}
	return strings.TrimSpace(string(output))
}

func countLines(text string) int {
	text = strings.TrimRight(text, "\r\n")
	if text == "" {
		return 0
	}
	return len(strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n"))
}

// abbreviate truncates s to maxLen characters, appending "..." if needed.
// Newlines are replaced with "↵" so the output stays on a single line.
func abbreviate(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.ReplaceAll(s, "\n", "↵")
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}
