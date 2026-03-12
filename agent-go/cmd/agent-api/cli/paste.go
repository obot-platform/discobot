package cli

import (
	"bytes"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

type pasteReplacement struct {
	placeholder string
	raw         []byte
}

type inputTrackingReader struct {
	r            io.Reader
	sawC         *bool
	pendingOut   []byte
	scanBuf      []byte
	normalizeBuf []byte
	pasteBuf     []byte
	pasteActive  bool
	replacements []pasteReplacement
	pendingErr   error
	editSeqState int
}

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

func (r *inputTrackingReader) emitEditorInput(data []byte) {
	r.normalizeBuf = append(r.normalizeBuf, data...)
	for len(r.normalizeBuf) > 0 {
		if idx := bytes.IndexByte(r.normalizeBuf, 0x1b); idx != 0 {
			if idx < 0 {
				r.appendOutput(r.normalizeBuf)
				r.normalizeBuf = r.normalizeBuf[:0]
				return
			}
			r.appendOutput(r.normalizeBuf[:idx])
			r.normalizeBuf = r.normalizeBuf[idx:]
		}

		normalized, consumed, incomplete := normalizeReadlineEscapeSequence(r.normalizeBuf)
		if incomplete {
			return
		}
		r.appendOutput(normalized)
		r.normalizeBuf = r.normalizeBuf[consumed:]
	}
}

func (r *inputTrackingReader) flushEditorInput() {
	if len(r.normalizeBuf) == 0 {
		return
	}
	r.appendOutput(r.normalizeBuf)
	r.normalizeBuf = r.normalizeBuf[:0]
}

func normalizeReadlineEscapeSequence(data []byte) ([]byte, int, bool) {
	if len(data) < 2 {
		return nil, 0, true
	}

	switch data[1] {
	case 'b':
		return []byte{0x1b, '[', '1', ';', '3', 'D'}, 2, false
	case 'f':
		return []byte{0x1b, '[', '1', ';', '3', 'C'}, 2, false
	case 0x7f, 0x08:
		return []byte{0x17}, 2, false
	case 'O':
		if len(data) < 3 {
			return nil, 0, true
		}
		switch data[2] {
		case 'H':
			return []byte{0x1b, '[', 'H'}, 3, false
		case 'F':
			return []byte{0x1b, '[', 'F'}, 3, false
		default:
			return data[:3], 3, false
		}
	case '[':
		end := 2
		for end < len(data) {
			c := data[end]
			if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '~' {
				seq := data[:end+1]
				return normalizeReadlineCSISequence(seq), len(seq), false
			}
			end++
		}
		return nil, 0, true
	default:
		return data[:2], 2, false
	}
}

func normalizeReadlineCSISequence(seq []byte) []byte {
	switch string(seq) {
	case "\x1b[5D", "\x1b[1;5D", "\x1b[1;7D", "\x1b[1;9D":
		return []byte{0x1b, '[', '1', ';', '3', 'D'}
	case "\x1b[5C", "\x1b[1;5C", "\x1b[1;7C", "\x1b[1;9C":
		return []byte{0x1b, '[', '1', ';', '3', 'C'}
	case "\x1b[1~", "\x1b[7~":
		return []byte{0x1b, '[', 'H'}
	case "\x1b[4~", "\x1b[8~":
		return []byte{0x1b, '[', 'F'}
	case "\x1b[3;2~", "\x1b[3;3~", "\x1b[3;5~":
		return []byte{0x1b, '[', '3', '~'}
	default:
		return seq
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
					r.emitEditorInput(r.scanBuf[:emitLen])
					r.scanBuf = r.scanBuf[emitLen:]
				}
				return
			}
			if idx > 0 {
				r.emitEditorInput(r.scanBuf[:idx])
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
		r.flushEditorInput()
		return
	}
	if len(r.scanBuf) > 0 {
		r.emitEditorInput(r.scanBuf)
		r.scanBuf = r.scanBuf[:0]
	}
	r.flushEditorInput()
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
	return strings.Contains(frag, "past") || strings.Contains(frag, "line") || strings.Contains(frag, "char") || strings.Contains(frag, "chr")
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
	return fmt.Sprintf("[pasted %d lines/%d chars]", pastedLineCount(data), pastedCharCount(data))
}

func pastedCharCount(data []byte) int {
	count := 0
	for i := 0; i < len(data); {
		switch data[i] {
		case '\r':
			count++
			i++
			if i < len(data) && data[i] == '\n' {
				i++
			}
			continue
		case '\n':
			count++
			i++
			continue
		}
		_, size := utf8.DecodeRune(data[i:])
		count++
		i += size
	}
	return count
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

	lines := 1
	for i := 0; i < len(data); i++ {
		switch data[i] {
		case '\r':
			lines++
			if i+1 < len(data) && data[i+1] == '\n' {
				i++
			}
		case '\n':
			lines++
		}
	}

	if len(data) > 0 {
		last := data[len(data)-1]
		if last == '\n' || last == '\r' {
			lines--
		}
	}
	if lines < 1 {
		lines = 1
	}
	return lines
}
