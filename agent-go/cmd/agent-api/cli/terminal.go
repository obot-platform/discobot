package cli

// terminal.go — interactive line readers and persistent command history.
//
// readLine uses an x/term readline-style editor for the main prompt (history
// enabled) and retains the raw-mode parser for multiline/no-history input where
// byte-for-byte paste handling is required.

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/term"
)

// errInterrupt is returned by readLine when the user presses Ctrl+C.
var errInterrupt = errors.New("interrupted")

const maxHistoryEntries = 500

type pastedChunk struct {
	end     int
	rawLen  int
	dispLen int
}

// cmdHistory stores per-session command history and persists it across restarts.
type cmdHistory struct {
	entries []string
	path    string
}

type stdioReadWriter struct {
	r io.Reader
	w io.Writer
}

type readLineOptions struct {
	slashCommands []string
}

type pasteReplacement struct {
	placeholder string
	raw         []byte
}

type inputTrackingReader struct {
	r            io.Reader
	sawC         *bool
	pendingOut   []byte
	scanBuf      []byte
	pasteBuf     []byte
	pasteActive  bool
	replacements []pasteReplacement
	pendingErr   error
	editSeqState int
}

const sanitizeTriggerKey = byte(0x18)

var (
	bracketPasteStart = []byte{0x1b, '[', '2', '0', '0', '~'}
	bracketPasteEnd   = []byte{0x1b, '[', '2', '0', '1', '~'}

	wellFormedPasteBlockPattern = regexp.MustCompile(`^\[pasted \d+ lines/\d+ bytes\]$`)
	unbracketedPastePattern     = regexp.MustCompile(`pasted \d+ line\w*/\d+ byt\w*\]?`)
)

func (r *inputTrackingReader) Read(p []byte) (int, error) {
	if len(r.pendingOut) == 0 && r.pendingErr != nil {
		err := r.pendingErr
		r.pendingErr = nil
		return 0, err
	}

	for len(r.pendingOut) == 0 {
		buf := make([]byte, 4096)
		n, err := r.r.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			if r.sawC != nil && bytes.IndexByte(chunk, 0x03) >= 0 {
				*r.sawC = true
			}
			r.scanBuf = append(r.scanBuf, chunk...)
			r.processScan()
		}
		if err != nil {
			r.flushAtEOF()
			if len(r.pendingOut) == 0 {
				return 0, err
			}
			r.pendingErr = err
			break
		}
		if n == 0 {
			continue
		}
	}

	n := copy(p, r.pendingOut)
	r.pendingOut = r.pendingOut[n:]
	return n, nil
}

func (r *inputTrackingReader) appendOutput(data []byte) {
	for _, b := range data {
		r.pendingOut = append(r.pendingOut, b)
		r.advanceEditState(b)
	}
}

func (r *inputTrackingReader) advanceEditState(b byte) {
	switch r.editSeqState {
	case 0:
		if b == 0x1b {
			r.editSeqState = 1
			return
		}
		if isDirectEditKeyByte(b) {
			r.pendingOut = append(r.pendingOut, sanitizeTriggerKey)
		}
	case 1:
		if b == '[' {
			r.editSeqState = 2
			return
		}
		r.editSeqState = 0
		if isDirectEditKeyByte(b) {
			r.pendingOut = append(r.pendingOut, sanitizeTriggerKey)
		}
	case 2:
		if b == '3' {
			r.editSeqState = 3
			return
		}
		r.editSeqState = 0
		if isDirectEditKeyByte(b) {
			r.pendingOut = append(r.pendingOut, sanitizeTriggerKey)
		}
	case 3:
		r.editSeqState = 0
		if b == '~' {
			r.pendingOut = append(r.pendingOut, sanitizeTriggerKey)
			return
		}
		if isDirectEditKeyByte(b) {
			r.pendingOut = append(r.pendingOut, sanitizeTriggerKey)
		}
	}
}

func isDirectEditKeyByte(b byte) bool {
	switch b {
	case 0x7f, 0x08, 0x17, 0x15, 0x04, 0x0b:
		return true
	default:
		return false
	}
}

func (r *inputTrackingReader) processScan() {
	for {
		if !r.pasteActive {
			idx := bytes.Index(r.scanBuf, bracketPasteStart)
			if idx < 0 {
				keep := longestSuffixPrefix(r.scanBuf, bracketPasteStart)
				emitLen := len(r.scanBuf) - keep
				if emitLen > 0 {
					r.appendOutput(r.scanBuf[:emitLen])
					r.scanBuf = r.scanBuf[emitLen:]
				}
				return
			}
			if idx > 0 {
				r.appendOutput(r.scanBuf[:idx])
			}
			r.scanBuf = r.scanBuf[idx+len(bracketPasteStart):]
			r.pasteActive = true
			r.pasteBuf = r.pasteBuf[:0]
			continue
		}

		idx := bytes.Index(r.scanBuf, bracketPasteEnd)
		if idx < 0 {
			keep := longestSuffixPrefix(r.scanBuf, bracketPasteEnd)
			emitLen := len(r.scanBuf) - keep
			if emitLen > 0 {
				r.pasteBuf = append(r.pasteBuf, r.scanBuf[:emitLen]...)
				r.scanBuf = r.scanBuf[emitLen:]
			}
			return
		}
		if idx > 0 {
			r.pasteBuf = append(r.pasteBuf, r.scanBuf[:idx]...)
		}
		r.scanBuf = r.scanBuf[idx+len(bracketPasteEnd):]
		r.emitPastePlaceholder(r.pasteBuf)
		r.pasteActive = false
		r.pasteBuf = r.pasteBuf[:0]
	}
}

func (r *inputTrackingReader) flushAtEOF() {
	if r.pasteActive {
		if len(r.scanBuf) > 0 {
			r.pasteBuf = append(r.pasteBuf, r.scanBuf...)
			r.scanBuf = r.scanBuf[:0]
		}
		r.emitPastePlaceholder(r.pasteBuf)
		r.pasteActive = false
		r.pasteBuf = r.pasteBuf[:0]
		return
	}
	if len(r.scanBuf) > 0 {
		r.appendOutput(r.scanBuf)
		r.scanBuf = r.scanBuf[:0]
	}
}

func (r *inputTrackingReader) emitPastePlaceholder(data []byte) {
	if shouldInlinePastedContent(data) {
		r.appendOutput(data)
		return
	}

	summary := pastedSummary(data)
	r.appendOutput([]byte(summary))
	r.replacements = append(r.replacements, pasteReplacement{
		placeholder: summary,
		raw:         append([]byte(nil), data...),
	})
}

func (r *inputTrackingReader) expandPastes(line string) string {
	expanded := line
	for _, rep := range r.replacements {
		expanded = strings.Replace(expanded, rep.placeholder, string(rep.raw), 1)
	}
	return removeMalformedPasteBlocks(expanded)
}

type tabCompletionState struct {
	lastPrefix         string
	lastWasMultiple    bool
	completionRendered bool
}

type textRange struct {
	start int
	end   int
}

func removeMalformedPasteBlocks(line string) string {
	cleaned, _, _ := sanitizeMalformedPasteBlocksWithCursor(line, len(line))
	return cleaned
}

func sanitizeMalformedPasteBlocksWithCursor(line string, pos int) (string, int, bool) {
	ranges := malformedPasteBlockRanges(line)
	if len(ranges) == 0 {
		if pos < 0 {
			pos = 0
		}
		if pos > len(line) {
			pos = len(line)
		}
		return line, pos, false
	}

	merged := mergeTextRanges(ranges)
	var out strings.Builder
	prev := 0
	newPos := pos

	for _, rg := range merged {
		if rg.start < prev {
			rg.start = prev
		}
		if rg.end < rg.start {
			rg.end = rg.start
		}
		if rg.start > len(line) {
			rg.start = len(line)
		}
		if rg.end > len(line) {
			rg.end = len(line)
		}
		out.WriteString(line[prev:rg.start])
		if newPos > rg.end {
			newPos -= rg.end - rg.start
		} else if newPos > rg.start {
			newPos = rg.start
		}
		prev = rg.end
	}
	out.WriteString(line[prev:])

	cleaned := out.String()
	if newPos < 0 {
		newPos = 0
	}
	if newPos > len(cleaned) {
		newPos = len(cleaned)
	}
	return cleaned, newPos, true
}

func malformedPasteBlockRanges(line string) []textRange {
	var ranges []textRange

	for i := 0; i < len(line); {
		if line[i] != '[' {
			i++
			continue
		}
		relEnd := strings.IndexByte(line[i:], ']')
		if relEnd < 0 {
			fragment := line[i:]
			if looksLikePasteFragment(fragment) && !wellFormedPasteBlockPattern.MatchString(fragment) {
				ranges = append(ranges, textRange{start: i, end: len(line)})
			}
			break
		}
		end := i + relEnd + 1
		fragment := line[i:end]
		if looksLikePasteFragment(fragment) && !wellFormedPasteBlockPattern.MatchString(fragment) {
			ranges = append(ranges, textRange{start: i, end: end})
		}
		i = end
	}

	for _, m := range unbracketedPastePattern.FindAllStringIndex(line, -1) {
		start, end := m[0], m[1]
		if start > 0 && line[start-1] == '[' {
			continue
		}
		ranges = append(ranges, textRange{start: start, end: end})
	}

	return ranges
}

func mergeTextRanges(ranges []textRange) []textRange {
	if len(ranges) <= 1 {
		return ranges
	}
	sort.Slice(ranges, func(i, j int) bool {
		if ranges[i].start == ranges[j].start {
			return ranges[i].end < ranges[j].end
		}
		return ranges[i].start < ranges[j].start
	})
	merged := make([]textRange, 0, len(ranges))
	for _, rg := range ranges {
		if len(merged) == 0 {
			merged = append(merged, rg)
			continue
		}
		last := &merged[len(merged)-1]
		if rg.start <= last.end {
			if rg.end > last.end {
				last.end = rg.end
			}
			continue
		}
		merged = append(merged, rg)
	}
	return merged
}

func looksLikePasteFragment(fragment string) bool {
	frag := strings.ToLower(fragment)
	return strings.Contains(frag, "past") || strings.Contains(frag, "line") || strings.Contains(frag, "byt")
}

func longestSuffixPrefix(data, pattern []byte) int {
	maxLen := len(pattern) - 1
	if len(data) < maxLen {
		maxLen = len(data)
	}
	for n := maxLen; n > 0; n-- {
		if bytes.Equal(data[len(data)-n:], pattern[:n]) {
			return n
		}
	}
	return 0
}

func (s stdioReadWriter) Read(p []byte) (int, error) {
	return s.r.Read(p)
}

func (s stdioReadWriter) Write(p []byte) (int, error) {
	return s.w.Write(p)
}

// historyView adapts cmdHistory to x/term's History interface.
// Index 0 is most-recent, while cmdHistory stores oldest→newest.
type historyView struct {
	h *cmdHistory
}

func (h historyView) Add(string) {}

func (h historyView) Len() int {
	if h.h == nil {
		return 0
	}
	return len(h.h.entries)
}

func (h historyView) At(idx int) string {
	if h.h == nil {
		panic("history unavailable")
	}
	return h.h.entries[len(h.h.entries)-1-idx]
}

// loadCmdHistory reads history from path. Missing file is not an error.
func loadCmdHistory(path string) *cmdHistory {
	h := &cmdHistory{path: path}
	if data, err := os.ReadFile(path); err == nil {
		for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
			if line != "" {
				h.entries = append(h.entries, line)
			}
		}
		if len(h.entries) > maxHistoryEntries {
			h.entries = h.entries[len(h.entries)-maxHistoryEntries:]
		}
	}
	return h
}

// push appends line to the history and saves it to disk.
// Adjacent duplicates are skipped.
func (h *cmdHistory) push(line string) {
	if line == "" {
		return
	}
	if len(h.entries) > 0 && h.entries[len(h.entries)-1] == line {
		return
	}
	h.entries = append(h.entries, line)
	if len(h.entries) > maxHistoryEntries {
		h.entries = h.entries[1:]
	}
	_ = h.save()
}

func (h *cmdHistory) save() error {
	if err := os.MkdirAll(filepath.Dir(h.path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(h.path, []byte(strings.Join(h.entries, "\n")+"\n"), 0o600)
}

// readLine reads one interactive line using the readline-style editor. History
// navigation is enabled when hist is non-nil.
//
// Returns:
//   - (line, nil)         — normal input
//   - ("", errInterrupt)  — Ctrl+C
//   - ("", io.EOF)        — Ctrl+D on empty buffer, or stdin closed
func readLine(prompt string, hist *cmdHistory) (string, error) {
	return readLineWithOptions(prompt, hist, nil)
}

func readLineWithOptions(prompt string, hist *cmdHistory, opts *readLineOptions) (string, error) {
	return readLineReadlineWithOptions(prompt, hist, opts)
}

func readLineReadlineWithOptions(prompt string, hist *cmdHistory, opts *readLineOptions) (string, error) {
	// Non-interactive fallback (piped stdin).
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprint(os.Stderr, prompt)
		return readLineSimple()
	}

	width, height := 0, 0
	if w, h, err := term.GetSize(int(os.Stderr.Fd())); err == nil {
		width, height = w, h
	} else if w, h, err := term.GetSize(int(os.Stdin.Fd())); err == nil {
		width, height = w, h
	}

	return readLineReadlineTTYWithOptions(
		prompt,
		hist,
		os.Stdin,
		os.Stderr,
		width,
		height,
		opts,
		func() (*term.State, error) { return term.MakeRaw(int(os.Stdin.Fd())) },
		func(state *term.State) error { return term.Restore(int(os.Stdin.Fd()), state) },
	)
}

func readLineReadlineTTY(
	prompt string,
	hist *cmdHistory,
	stdin io.Reader,
	stdout io.Writer,
	width int,
	height int,
	makeRaw func() (*term.State, error),
	restore func(*term.State) error,
) (string, error) {
	return readLineReadlineTTYWithOptions(prompt, hist, stdin, stdout, width, height, nil, makeRaw, restore)
}

func readLineReadlineTTYWithOptions(
	prompt string,
	hist *cmdHistory,
	stdin io.Reader,
	stdout io.Writer,
	width int,
	height int,
	opts *readLineOptions,
	makeRaw func() (*term.State, error),
	restore func(*term.State) error,
) (string, error) {
	oldState, err := makeRaw()
	if err != nil {
		return "", err
	}
	defer restore(oldState) //nolint:errcheck

	var sawCtrlC bool
	reader := &inputTrackingReader{r: stdin, sawC: &sawCtrlC}
	rw := stdioReadWriter{r: reader, w: stdout}
	t := term.NewTerminal(rw, prompt)
	if width > 0 {
		if height < 1 {
			height = 1
		}
		_ = t.SetSize(width, height)
	}
	t.SetBracketedPasteMode(true)
	defer t.SetBracketedPasteMode(false)
	if hist != nil {
		t.History = historyView{h: hist}
	} else {
		t.History = historyView{h: &cmdHistory{}}
	}
	tabCompletion := &tabCompletionState{}
	t.AutoCompleteCallback = func(line string, pos int, key rune) (string, int, bool) {
		if key == rune(sanitizeTriggerKey) {
			cleaned, newPos, changed := sanitizeMalformedPasteBlocksWithCursor(line, pos)
			if !changed {
				return "", 0, false
			}
			return cleaned, newPos, true
		}

		if key != '\t' {
			tabCompletion.lastPrefix = ""
			tabCompletion.lastWasMultiple = false
			tabCompletion.completionRendered = false
			return "", 0, false
		}
		if opts == nil || len(opts.slashCommands) == 0 {
			tabCompletion.lastPrefix = ""
			tabCompletion.lastWasMultiple = false
			tabCompletion.completionRendered = false
			return "", 0, false
		}

		prefix, ok := slashPrefixAtCursor(line, pos)
		if !ok {
			tabCompletion.lastPrefix = ""
			tabCompletion.lastWasMultiple = false
			return "", 0, false
		}

		completed, newPos, matches, ok := completeSlashCommand(line, pos, opts.slashCommands)
		if !ok {
			tabCompletion.lastPrefix = ""
			tabCompletion.lastWasMultiple = false
			return "", 0, false
		}

		if len(matches) <= 1 {
			tabCompletion.lastPrefix = ""
			tabCompletion.lastWasMultiple = false
			tabCompletion.completionRendered = false
			return completed, newPos, true
		}

		nextPrefix := prefix
		if updatedPrefix, ok := slashPrefixAtCursor(completed, newPos); ok {
			nextPrefix = updatedPrefix
		}
		showMatches := tabCompletion.lastWasMultiple && tabCompletion.lastPrefix == prefix
		tabCompletion.lastPrefix = nextPrefix
		tabCompletion.lastWasMultiple = true

		if showMatches && !tabCompletion.completionRendered {
			displayWidth := width
			if displayWidth <= 0 {
				displayWidth = 80
			}
			printCompletionMatches(stdout, matches, displayWidth, prompt, completed)
			tabCompletion.completionRendered = true
		}
		return completed, newPos, true
	}

	line, err := t.ReadLine()
	line = reader.expandPastes(line)
	if errors.Is(err, term.ErrPasteIndicator) {
		return line, nil
	}
	if err == nil {
		return line, nil
	}
	if errors.Is(err, io.EOF) {
		if sawCtrlC {
			fmt.Fprint(stdout, "^C\r\n")
			return "", errInterrupt
		}
		return "", io.EOF
	}
	return line, err
}

func completeSlashCommand(line string, pos int, commands []string) (string, int, []string, bool) {
	if pos < 0 {
		pos = 0
	}
	if pos > len(line) {
		pos = len(line)
	}

	cmdStart := 0
	for cmdStart < len(line) && line[cmdStart] == ' ' {
		cmdStart++
	}
	if cmdStart >= len(line) || line[cmdStart] != '/' {
		return "", 0, nil, false
	}

	cmdEnd := cmdStart
	for cmdEnd < len(line) && line[cmdEnd] != ' ' && line[cmdEnd] != '\t' {
		cmdEnd++
	}
	if pos < cmdStart || pos > cmdEnd {
		return "", 0, nil, false
	}

	prefix := line[cmdStart:pos]
	if !strings.HasPrefix(prefix, "/") {
		return "", 0, nil, false
	}

	matches := make([]string, 0, len(commands))
	for _, cmd := range commands {
		if strings.HasPrefix(cmd, prefix) {
			matches = append(matches, cmd)
		}
	}
	if len(matches) == 0 {
		return "", 0, nil, false
	}

	replacement := prefix
	if len(matches) == 1 {
		replacement = matches[0]
		if cmdEnd == len(line) {
			replacement += " "
		}
	} else {
		common := longestCommonPrefix(matches)
		if len(common) > len(prefix) {
			replacement = common
		}
	}

	completed := line[:cmdStart] + replacement + line[cmdEnd:]
	newPos := cmdStart + len(replacement)
	return completed, newPos, matches, true
}

func longestCommonPrefix(values []string) string {
	if len(values) == 0 {
		return ""
	}
	prefix := values[0]
	for _, v := range values[1:] {
		for !strings.HasPrefix(v, prefix) {
			if prefix == "" {
				return ""
			}
			prefix = prefix[:len(prefix)-1]
		}
	}
	return prefix
}

func slashPrefixAtCursor(line string, pos int) (string, bool) {
	if pos < 0 {
		pos = 0
	}
	if pos > len(line) {
		pos = len(line)
	}

	cmdStart := 0
	for cmdStart < len(line) && line[cmdStart] == ' ' {
		cmdStart++
	}
	if cmdStart >= len(line) || line[cmdStart] != '/' {
		return "", false
	}

	cmdEnd := cmdStart
	for cmdEnd < len(line) && line[cmdEnd] != ' ' && line[cmdEnd] != '\t' {
		cmdEnd++
	}
	if pos < cmdStart || pos > cmdEnd {
		return "", false
	}

	prefix := line[cmdStart:pos]
	if !strings.HasPrefix(prefix, "/") {
		return "", false
	}
	return prefix, true
}

func printCompletionMatches(stdout io.Writer, matches []string, maxWidth int, prompt string, buffer string) {
	line := strings.Join(matches, "  ")
	line = truncateCompletionLine(line, maxWidth)
	displayPrompt := strings.TrimLeft(prompt, "\n")
	fmt.Fprintf(stdout, "\r\n%s\r\n%s%s", line, displayPrompt, buffer)
}

func truncateCompletionLine(line string, maxWidth int) string {
	if maxWidth <= 0 {
		return line
	}
	runes := []rune(line)
	if len(runes) <= maxWidth {
		return line
	}
	suffix := []rune(" ...")
	if maxWidth <= len(suffix) {
		return string(suffix[:maxWidth])
	}
	cut := maxWidth - len(suffix)
	return strings.TrimRight(string(runes[:cut]), " ") + string(suffix)
}

func normalizePastedChunks(chunks []pastedChunk, bufLen int) []pastedChunk {
	for len(chunks) > 0 {
		last := chunks[len(chunks)-1]
		start := last.end - last.rawLen
		if start >= 0 && last.end <= bufLen {
			break
		}
		chunks = chunks[:len(chunks)-1]
	}
	return chunks
}

func pastedSummary(data []byte) string {
	return fmt.Sprintf("[pasted %d lines/%d bytes]", pastedLineCount(data), len(data))
}

func shouldInlinePastedContent(data []byte) bool {
	if len(data) == 0 {
		return true
	}
	if !utf8.Valid(data) {
		return false
	}
	s := string(data)
	if strings.ContainsAny(s, "\r\n") {
		return false
	}
	if utf8.RuneCountInString(s) >= 100 {
		return false
	}
	for _, r := range s {
		if !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

func pastedLineCount(data []byte) int {
	if len(data) == 0 {
		return 0
	}
	lines := bytes.Count(data, []byte{'\n'}) + 1
	if data[len(data)-1] == '\n' {
		lines--
	}
	if lines < 1 {
		lines = 1
	}
	return lines
}

// readLineSimple reads a line from os.Stdin byte-by-byte without raw mode.
// Used as fallback when stdin is not a terminal or MakeRaw fails.
func readLineSimple() (string, error) {
	var buf []byte
	b := make([]byte, 1)
	for {
		_, err := os.Stdin.Read(b)
		if err != nil {
			if len(buf) > 0 {
				return strings.TrimRight(string(buf), "\r\n"), nil
			}
			return "", err
		}
		if b[0] == '\n' {
			return strings.TrimRight(string(buf), "\r"), nil
		}
		buf = append(buf, b[0])
	}
}
