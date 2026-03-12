package cli

import (
	"fmt"
	"io"
	"strings"
)

// markdownRenderer provides a tiny streaming markdown formatter for text chunks.
// It intentionally supports only basic formatting and buffers fenced code blocks
// until they are complete.
type markdownRenderer struct {
	out          io.Writer
	enableFormat bool
	enableColor  bool
	atLineStart  bool

	pending string

	inFence        bool
	fenceDelimiter string
	fenceBuf       strings.Builder
}

func newMarkdownRenderer(out io.Writer, enableFormat, enableColor bool) *markdownRenderer {
	if !enableFormat {
		enableColor = false
	}
	return &markdownRenderer{
		out:          out,
		enableFormat: enableFormat,
		enableColor:  enableColor,
		atLineStart:  true,
	}
}

func (r *markdownRenderer) WriteText(delta string) {
	if delta == "" {
		return
	}

	r.pending += delta
	for {
		idx := strings.IndexByte(r.pending, '\n')
		if idx < 0 {
			break
		}
		line := r.pending[:idx+1]
		r.pending = r.pending[idx+1:]
		r.consumeCompleteLine(line)
	}

	r.flushTailIfSafe()
}

// FlushForBoundary flushes pending non-block text before printing non-text chunks.
// If currently inside a fenced code block, data remains buffered until the fence
// closes or Finish() is called.
func (r *markdownRenderer) FlushForBoundary() {
	if r.inFence || r.pending == "" {
		return
	}
	r.emitInlineSegment(r.pending)
	r.pending = ""
}

// Finish flushes all remaining buffered content at end-of-turn.
func (r *markdownRenderer) Finish() {
	if r.pending != "" {
		if r.inFence {
			r.fenceBuf.WriteString(r.pending)
		} else {
			r.emitInlineSegment(r.pending)
		}
		r.pending = ""
	}

	if r.inFence {
		r.emitCodeBlock(r.fenceBuf.String())
		r.fenceBuf.Reset()
		r.inFence = false
		r.fenceDelimiter = ""
	}
}

func (r *markdownRenderer) AtLineStart() bool {
	if r == nil {
		return true
	}
	return r.atLineStart
}

func (r *markdownRenderer) consumeCompleteLine(line string) {
	if r.inFence {
		r.fenceBuf.WriteString(line)
		if r.isFenceCloseLine(line) {
			r.emitCodeBlock(r.fenceBuf.String())
			r.fenceBuf.Reset()
			r.inFence = false
			r.fenceDelimiter = ""
		}
		return
	}

	if delim, ok := detectFenceOpenLine(line); ok {
		r.inFence = true
		r.fenceDelimiter = delim
		r.fenceBuf.Reset()
		r.fenceBuf.WriteString(line)
		return
	}

	r.emitFormattedLine(line)
}

func (r *markdownRenderer) flushTailIfSafe() {
	if r.inFence || r.pending == "" {
		return
	}
	if !r.enableFormat {
		r.write(r.pending)
		r.pending = ""
		return
	}
	if tailNeedsMoreInput(r.pending) {
		return
	}
	r.emitInlineSegment(r.pending)
	r.pending = ""
}

func tailNeedsMoreInput(s string) bool {
	trim := strings.TrimLeft(s, " \t")
	if strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~") {
		return true
	}
	if strings.HasPrefix(trim, "#") || strings.HasPrefix(trim, ">") {
		return true
	}
	if strings.HasPrefix(trim, "- ") || strings.HasPrefix(trim, "* ") || strings.HasPrefix(trim, "+ ") {
		return true
	}
	if _, _, ok := parseOrderedList(trim); ok {
		return true
	}
	return strings.ContainsAny(s, "`*")
}

func detectFenceOpenLine(line string) (string, bool) {
	trim := strings.TrimLeft(line, " \t")
	switch {
	case strings.HasPrefix(trim, "```"):
		return "```", true
	case strings.HasPrefix(trim, "~~~"):
		return "~~~", true
	default:
		return "", false
	}
}

func (r *markdownRenderer) isFenceCloseLine(line string) bool {
	trim := strings.TrimSpace(line)
	return strings.HasPrefix(trim, r.fenceDelimiter)
}

func (r *markdownRenderer) emitFormattedLine(line string) {
	if !r.enableFormat {
		r.write(line)
		return
	}

	content, suffix := splitLineEnding(line)
	r.write(r.formatBlockLine(content))
	r.write(suffix)
}

func (r *markdownRenderer) emitInlineSegment(seg string) {
	if !r.enableFormat {
		r.write(seg)
		return
	}
	r.write(r.formatInline(seg))
}

func (r *markdownRenderer) emitCodeBlock(block string) {
	if !r.enableFormat || !r.enableColor {
		r.write(block)
		return
	}
	r.write(r.style(block, "38;5;245"))
}

func splitLineEnding(line string) (content string, ending string) {
	switch {
	case strings.HasSuffix(line, "\r\n"):
		return strings.TrimSuffix(line, "\r\n"), "\r\n"
	case strings.HasSuffix(line, "\n"):
		return strings.TrimSuffix(line, "\n"), "\n"
	default:
		return line, ""
	}
}

func (r *markdownRenderer) formatBlockLine(line string) string {
	indentLen := len(line) - len(strings.TrimLeft(line, " \t"))
	indent := line[:indentLen]
	trim := line[indentLen:]

	if level, text, ok := parseHeading(trim); ok {
		heading := strings.Repeat("#", level) + " " + text
		return indent + r.style(heading, "1;36")
	}
	if text, ok := parseBlockquote(trim); ok {
		if text == "" {
			return indent + r.style(">", "36")
		}
		return indent + r.style(">", "36") + " " + r.formatInline(text)
	}
	if marker, text, ok := parseUnorderedList(trim); ok {
		return indent + r.style(marker, "33") + " " + r.formatInline(text)
	}
	if marker, text, ok := parseOrderedList(trim); ok {
		return indent + r.style(marker, "33") + " " + r.formatInline(text)
	}

	return indent + r.formatInline(trim)
}

func parseHeading(s string) (int, string, bool) {
	level := 0
	for level < len(s) && s[level] == '#' {
		level++
	}
	if level == 0 || level > 6 {
		return 0, "", false
	}
	if level >= len(s) || s[level] != ' ' {
		return 0, "", false
	}
	return level, strings.TrimSpace(s[level+1:]), true
}

func parseBlockquote(s string) (string, bool) {
	if !strings.HasPrefix(s, ">") {
		return "", false
	}
	text := strings.TrimPrefix(s, ">")
	text = strings.TrimPrefix(text, " ")
	return text, true
}

func parseUnorderedList(s string) (marker, text string, ok bool) {
	if len(s) < 2 {
		return "", "", false
	}
	if (s[0] == '-' || s[0] == '*' || s[0] == '+') && s[1] == ' ' {
		return s[:1], strings.TrimLeft(s[2:], " "), true
	}
	return "", "", false
}

func parseOrderedList(s string) (marker, text string, ok bool) {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == 0 || i+1 >= len(s) || s[i] != '.' || s[i+1] != ' ' {
		return "", "", false
	}
	return s[:i+1], strings.TrimLeft(s[i+2:], " "), true
}

func (r *markdownRenderer) formatInline(text string) string {
	if !r.enableColor {
		return text
	}

	var out strings.Builder
	remaining := text

	for {
		start := strings.Index(remaining, "`")
		if start < 0 {
			out.WriteString(r.formatEmphasis(remaining))
			break
		}
		end := strings.Index(remaining[start+1:], "`")
		if end < 0 {
			out.WriteString(r.formatEmphasis(remaining))
			break
		}

		out.WriteString(r.formatEmphasis(remaining[:start]))
		code := remaining[start+1 : start+1+end]
		out.WriteString(r.style(code, "38;5;214"))
		remaining = remaining[start+1+end+1:]
	}

	return out.String()
}

func (r *markdownRenderer) formatEmphasis(text string) string {
	text = applyPairedStyle(text, "**", func(inner string) string {
		return r.style(inner, "1")
	})
	text = applyPairedStyle(text, "*", func(inner string) string {
		return r.style(inner, "3")
	})
	return text
}

func applyPairedStyle(text, delim string, style func(string) string) string {
	if text == "" {
		return text
	}

	var out strings.Builder
	remaining := text

	for {
		start := strings.Index(remaining, delim)
		if start < 0 {
			out.WriteString(remaining)
			break
		}
		endOffset := strings.Index(remaining[start+len(delim):], delim)
		if endOffset < 0 {
			out.WriteString(remaining)
			break
		}

		out.WriteString(remaining[:start])
		inner := remaining[start+len(delim) : start+len(delim)+endOffset]
		out.WriteString(style(inner))
		remaining = remaining[start+len(delim)+endOffset+len(delim):]
	}

	return out.String()
}

func (r *markdownRenderer) style(text, code string) string {
	if !r.enableColor || text == "" {
		return text
	}
	return "\033[" + code + "m" + text + "\033[0m"
}

func (r *markdownRenderer) write(text string) {
	if text == "" {
		return
	}
	_, _ = fmt.Fprint(r.out, text)
	r.atLineStart = strings.HasSuffix(text, "\n") || strings.HasSuffix(text, "\r")
}
