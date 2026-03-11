package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/obot-platform/discobot/agent-go/message"
	"github.com/obot-platform/discobot/agent-go/thread"
)

const (
	beginPatchMarker = "*** Begin Patch"
	endPatchMarker   = "*** End Patch"
	addFileMarker    = "*** Add File: "
	deleteFileMarker = "*** Delete File: "
	updateFileMarker = "*** Update File: "
	moveToMarker     = "*** Move to: "
	endOfFileMarker  = "*** End of File"
	changeCtxMarker  = "@@ "
	emptyCtxMarker   = "@@"
)

type applyPatchInput struct {
	Input string `json:"input"`
}

type patchOperationKind int

const (
	patchAddFile patchOperationKind = iota
	patchDeleteFile
	patchUpdateFile
)

type patchOperation struct {
	kind     patchOperationKind
	path     string
	movePath string
	addLines []string
	chunks   []patchChunk
}

type patchChunk struct {
	changeContext *string
	oldLines      []string
	newLines      []string
	isEndOfFile   bool
}

type patchAffectedPaths struct {
	added    []string
	modified []string
	deleted  []string
}

func (e *Executor) executeApplyPatch(call message.ToolCallPart) (thread.ToolExecuteResult, error) {
	patchText, err := parseApplyPatchInput(call.Input)
	if err != nil {
		return errResult(call, err.Error()), nil
	}

	ops, err := parseApplyPatch(patchText)
	if err != nil {
		return errResult(call, err.Error()), nil
	}

	affected, err := e.applyPatchOperations(ops)
	if err != nil {
		return errResult(call, err.Error()), nil
	}

	if len(affected.added) == 0 && len(affected.modified) == 0 && len(affected.deleted) == 0 {
		return errResult(call, "No files were modified."), nil
	}

	var b strings.Builder
	b.WriteString("Success. Updated the following files:\n")
	for _, path := range affected.added {
		b.WriteString("A ")
		b.WriteString(path)
		b.WriteByte('\n')
	}
	for _, path := range affected.modified {
		b.WriteString("M ")
		b.WriteString(path)
		b.WriteByte('\n')
	}
	for _, path := range affected.deleted {
		b.WriteString("D ")
		b.WriteString(path)
		b.WriteByte('\n')
	}

	return textResult(call, strings.TrimSuffix(b.String(), "\n")), nil
}

func parseApplyPatchInput(rawInput string) (string, error) {
	trimmed := strings.TrimSpace(rawInput)
	if trimmed == "" {
		return "", fmt.Errorf("input is required")
	}

	var structured applyPatchInput
	if err := json.Unmarshal([]byte(trimmed), &structured); err == nil {
		if strings.TrimSpace(structured.Input) == "" {
			return "", fmt.Errorf("input is required")
		}
		return structured.Input, nil
	}

	var textInput string
	if err := json.Unmarshal([]byte(trimmed), &textInput); err == nil {
		if strings.TrimSpace(textInput) == "" {
			return "", fmt.Errorf("input is required")
		}
		return textInput, nil
	}

	return trimmed, nil
}

func (e *Executor) applyPatchOperations(ops []patchOperation) (*patchAffectedPaths, error) {
	affected := &patchAffectedPaths{}

	for _, op := range ops {
		srcPath := resolvePath(e.cwd, op.path)

		switch op.kind {
		case patchAddFile:
			if err := e.checkWriteAllowed(srcPath, op.path); err != nil {
				return nil, err
			}
			if err := os.MkdirAll(filepath.Dir(srcPath), 0o755); err != nil {
				return nil, fmt.Errorf("failed to create parent directory: %v", err)
			}
			content := ""
			if len(op.addLines) > 0 {
				content = strings.Join(op.addLines, "\n") + "\n"
			}
			if err := os.WriteFile(srcPath, []byte(content), 0o644); err != nil {
				return nil, fmt.Errorf("failed to write file: %v", err)
			}
			e.recordFileWritten(srcPath)
			affected.added = append(affected.added, op.path)

		case patchDeleteFile:
			if err := e.checkWriteAllowed(srcPath, op.path); err != nil {
				return nil, err
			}
			if err := os.Remove(srcPath); err != nil {
				return nil, fmt.Errorf("failed to delete file: %v", err)
			}
			e.removeFileRecord(srcPath)
			affected.deleted = append(affected.deleted, op.path)

		case patchUpdateFile:
			if err := e.checkWriteAllowed(srcPath, op.path); err != nil {
				return nil, err
			}

			data, err := os.ReadFile(srcPath)
			if err != nil {
				return nil, fmt.Errorf("failed to read file to update %s: %v", op.path, err)
			}

			newContent, err := applyUpdateChunks(data, op.path, op.chunks)
			if err != nil {
				return nil, err
			}

			destPath := srcPath
			displayPath := op.path
			if op.movePath != "" {
				destPath = resolvePath(e.cwd, op.movePath)
				displayPath = op.movePath
				if destPath != srcPath {
					if err := e.checkWriteAllowed(destPath, op.movePath); err != nil {
						return nil, err
					}
				}
			}

			if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
				return nil, fmt.Errorf("failed to create parent directory: %v", err)
			}
			if err := os.WriteFile(destPath, []byte(newContent), 0o644); err != nil {
				return nil, fmt.Errorf("failed to write file: %v", err)
			}

			if op.movePath != "" && destPath != srcPath {
				if err := os.Remove(srcPath); err != nil {
					return nil, fmt.Errorf("failed to remove original %s: %v", op.path, err)
				}
				e.removeFileRecord(srcPath)
			}

			e.recordFileWritten(destPath)
			affected.modified = append(affected.modified, displayPath)
		}
	}

	return affected, nil
}

func (e *Executor) removeFileRecord(absPath string) {
	e.fileReadsMu.Lock()
	defer e.fileReadsMu.Unlock()
	delete(e.fileReads, absPath)
}

func parseApplyPatch(patch string) ([]patchOperation, error) {
	lines, err := parseApplyPatchLines(patch)
	if err != nil {
		return nil, err
	}

	body := lines[1 : len(lines)-1]
	ops := make([]patchOperation, 0)
	for len(body) > 0 {
		if strings.TrimSpace(body[0]) == "" {
			body = body[1:]
			continue
		}
		op, consumed, err := parsePatchOperation(body)
		if err != nil {
			return nil, err
		}
		ops = append(ops, op)
		body = body[consumed:]
	}

	return ops, nil
}

func parseApplyPatchLines(patch string) ([]string, error) {
	patch = normalizePatchNewlines(patch)
	lines := strings.Split(strings.TrimSpace(patch), "\n")

	err := validatePatchBoundaries(lines)
	if err == nil {
		return lines, nil
	}
	if len(lines) >= 4 {
		first := strings.TrimSpace(lines[0])
		last := strings.TrimSpace(lines[len(lines)-1])
		if (first == "<<EOF" || first == "<<'EOF'" || first == "<<\"EOF\"") && strings.HasSuffix(last, "EOF") {
			inner := lines[1 : len(lines)-1]
			innerErr := validatePatchBoundaries(inner)
			if innerErr == nil {
				return inner, nil
			}
			return nil, innerErr
		}
	}
	return nil, err
}

func validatePatchBoundaries(lines []string) error {
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != beginPatchMarker {
		return fmt.Errorf("the first line of the patch must be '%s'", beginPatchMarker)
	}
	if strings.TrimSpace(lines[len(lines)-1]) != endPatchMarker {
		return fmt.Errorf("the last line of the patch must be '%s'", endPatchMarker)
	}
	return nil
}

func parsePatchOperation(lines []string) (patchOperation, int, error) {
	header := strings.TrimSpace(lines[0])

	if strings.HasPrefix(header, addFileMarker) {
		path := strings.TrimSpace(strings.TrimPrefix(header, addFileMarker))
		if err := ensureRelativePatchPath(path); err != nil {
			return patchOperation{}, 0, err
		}
		consumed := 1
		addLines := make([]string, 0)
		for consumed < len(lines) {
			line := lines[consumed]
			if !strings.HasPrefix(line, "+") {
				break
			}
			addLines = append(addLines, line[1:])
			consumed++
		}
		return patchOperation{kind: patchAddFile, path: path, addLines: addLines}, consumed, nil
	}

	if strings.HasPrefix(header, deleteFileMarker) {
		path := strings.TrimSpace(strings.TrimPrefix(header, deleteFileMarker))
		if err := ensureRelativePatchPath(path); err != nil {
			return patchOperation{}, 0, err
		}
		return patchOperation{kind: patchDeleteFile, path: path}, 1, nil
	}

	if strings.HasPrefix(header, updateFileMarker) {
		path := strings.TrimSpace(strings.TrimPrefix(header, updateFileMarker))
		if err := ensureRelativePatchPath(path); err != nil {
			return patchOperation{}, 0, err
		}
		consumed := 1
		movePath := ""
		if consumed < len(lines) {
			next := strings.TrimSpace(lines[consumed])
			if strings.HasPrefix(next, moveToMarker) {
				movePath = strings.TrimSpace(strings.TrimPrefix(next, moveToMarker))
				if err := ensureRelativePatchPath(movePath); err != nil {
					return patchOperation{}, 0, err
				}
				consumed++
			}
		}

		chunks := make([]patchChunk, 0)
		for consumed < len(lines) {
			if strings.TrimSpace(lines[consumed]) == "" {
				consumed++
				continue
			}
			if strings.HasPrefix(strings.TrimSpace(lines[consumed]), "***") {
				break
			}

			chunk, used, err := parsePatchChunk(lines[consumed:], len(chunks) == 0)
			if err != nil {
				return patchOperation{}, 0, err
			}
			chunks = append(chunks, chunk)
			consumed += used
		}

		if len(chunks) == 0 {
			return patchOperation{}, 0, fmt.Errorf("update file hunk for path '%s' is empty", path)
		}

		return patchOperation{
			kind:     patchUpdateFile,
			path:     path,
			movePath: movePath,
			chunks:   chunks,
		}, consumed, nil
	}

	return patchOperation{}, 0, fmt.Errorf(
		"'%s' is not a valid hunk header. Valid hunk headers: '*** Add File: {path}', '*** Delete File: {path}', '*** Update File: {path}'",
		header,
	)
}

func parsePatchChunk(lines []string, allowMissingContext bool) (patchChunk, int, error) {
	if len(lines) == 0 {
		return patchChunk{}, 0, fmt.Errorf("update hunk does not contain any lines")
	}

	startIndex := 0
	var changeContext *string
	first := strings.TrimSpace(lines[0])
	if first == emptyCtxMarker {
		startIndex = 1
	} else if strings.HasPrefix(first, changeCtxMarker) {
		changeContext = new(strings.TrimPrefix(first, changeCtxMarker))
		startIndex = 1
	} else if !allowMissingContext {
		return patchChunk{}, 0, fmt.Errorf(
			"expected update hunk to start with a @@ context marker, got: '%s'",
			lines[0],
		)
	}

	if startIndex >= len(lines) {
		return patchChunk{}, 0, fmt.Errorf("update hunk does not contain any lines")
	}

	chunk := patchChunk{changeContext: changeContext}
	parsed := 0
	for _, line := range lines[startIndex:] {
		if strings.TrimSpace(line) == endOfFileMarker {
			if parsed == 0 {
				return patchChunk{}, 0, fmt.Errorf("update hunk does not contain any lines")
			}
			chunk.isEndOfFile = true
			parsed++
			break
		}

		if line == "" {
			chunk.oldLines = append(chunk.oldLines, "")
			chunk.newLines = append(chunk.newLines, "")
			parsed++
			continue
		}

		switch line[0] {
		case ' ':
			chunk.oldLines = append(chunk.oldLines, line[1:])
			chunk.newLines = append(chunk.newLines, line[1:])
		case '+':
			chunk.newLines = append(chunk.newLines, line[1:])
		case '-':
			chunk.oldLines = append(chunk.oldLines, line[1:])
		default:
			if parsed == 0 {
				return patchChunk{}, 0, fmt.Errorf(
					"unexpected line found in update hunk: '%s'. Every line should start with ' ' (context line), '+' (added line), or '-' (removed line)",
					line,
				)
			}
			return chunk, parsed + startIndex, nil
		}

		parsed++
	}

	if parsed == 0 {
		return patchChunk{}, 0, fmt.Errorf("update hunk does not contain any lines")
	}

	return chunk, parsed + startIndex, nil
}

func applyUpdateChunks(data []byte, displayPath string, chunks []patchChunk) (string, error) {
	content := normalizePatchNewlines(string(data))
	originalLines := strings.Split(content, "\n")
	if len(originalLines) > 0 && originalLines[len(originalLines)-1] == "" {
		originalLines = originalLines[:len(originalLines)-1]
	}

	replacements, err := computePatchReplacements(originalLines, displayPath, chunks)
	if err != nil {
		return "", err
	}

	updatedLines := applyPatchReplacements(originalLines, replacements)
	if len(updatedLines) == 0 || updatedLines[len(updatedLines)-1] != "" {
		updatedLines = append(updatedLines, "")
	}

	return strings.Join(updatedLines, "\n"), nil
}

func computePatchReplacements(originalLines []string, displayPath string, chunks []patchChunk) ([]patchReplacement, error) {
	replacements := make([]patchReplacement, 0, len(chunks))
	lineIndex := 0

	for _, chunk := range chunks {
		if chunk.changeContext != nil {
			ctx := []string{*chunk.changeContext}
			idx := seekPatchSequence(originalLines, ctx, lineIndex, false)
			if idx < 0 {
				return nil, fmt.Errorf("failed to find context '%s' in %s", *chunk.changeContext, displayPath)
			}
			lineIndex = idx + 1
		}

		if len(chunk.oldLines) == 0 {
			insertionIdx := len(originalLines)
			if len(originalLines) > 0 && originalLines[len(originalLines)-1] == "" {
				insertionIdx = len(originalLines) - 1
			}
			replacements = append(replacements, patchReplacement{
				start:    insertionIdx,
				oldLen:   0,
				newLines: append([]string(nil), chunk.newLines...),
			})
			continue
		}

		pattern := append([]string(nil), chunk.oldLines...)
		newSlice := append([]string(nil), chunk.newLines...)
		found := seekPatchSequence(originalLines, pattern, lineIndex, chunk.isEndOfFile)

		if found < 0 && len(pattern) > 0 && pattern[len(pattern)-1] == "" {
			pattern = pattern[:len(pattern)-1]
			if len(newSlice) > 0 && newSlice[len(newSlice)-1] == "" {
				newSlice = newSlice[:len(newSlice)-1]
			}
			found = seekPatchSequence(originalLines, pattern, lineIndex, chunk.isEndOfFile)
		}

		if found < 0 {
			return nil, fmt.Errorf(
				"failed to find expected lines in %s:\n%s",
				displayPath,
				strings.Join(chunk.oldLines, "\n"),
			)
		}

		replacements = append(replacements, patchReplacement{
			start:    found,
			oldLen:   len(pattern),
			newLines: newSlice,
		})
		lineIndex = found + len(pattern)
	}

	sort.Slice(replacements, func(i, j int) bool {
		return replacements[i].start < replacements[j].start
	})

	return replacements, nil
}

type patchReplacement struct {
	start    int
	oldLen   int
	newLines []string
}

func applyPatchReplacements(lines []string, replacements []patchReplacement) []string {
	out := append([]string(nil), lines...)
	for i := len(replacements) - 1; i >= 0; i-- {
		r := replacements[i]
		if r.start < 0 {
			continue
		}
		if r.start > len(out) {
			r.start = len(out)
		}
		end := r.start + r.oldLen
		if end > len(out) {
			end = len(out)
		}

		segment := append([]string(nil), r.newLines...)
		updated := make([]string, 0, len(out)-maxInt(0, end-r.start)+len(segment))
		updated = append(updated, out[:r.start]...)
		updated = append(updated, segment...)
		updated = append(updated, out[end:]...)
		out = updated
	}
	return out
}

func seekPatchSequence(lines []string, pattern []string, start int, eof bool) int {
	if len(pattern) == 0 {
		if start < 0 {
			return 0
		}
		return start
	}
	if len(pattern) > len(lines) {
		return -1
	}

	searchStart := start
	if searchStart < 0 {
		searchStart = 0
	}
	if eof && len(lines) >= len(pattern) {
		searchStart = len(lines) - len(pattern)
	}

	maxStart := len(lines) - len(pattern)
	if searchStart > maxStart {
		return -1
	}

	for i := searchStart; i <= maxStart; i++ {
		if patchExactMatch(lines, pattern, i) {
			return i
		}
	}
	for i := searchStart; i <= maxStart; i++ {
		if patchTrimEndMatch(lines, pattern, i) {
			return i
		}
	}
	for i := searchStart; i <= maxStart; i++ {
		if patchTrimMatch(lines, pattern, i) {
			return i
		}
	}
	for i := searchStart; i <= maxStart; i++ {
		if patchNormalizedMatch(lines, pattern, i) {
			return i
		}
	}

	return -1
}

func patchExactMatch(lines []string, pattern []string, start int) bool {
	for i := range pattern {
		if lines[start+i] != pattern[i] {
			return false
		}
	}
	return true
}

func patchTrimEndMatch(lines []string, pattern []string, start int) bool {
	for i := range pattern {
		if strings.TrimRight(lines[start+i], " \t") != strings.TrimRight(pattern[i], " \t") {
			return false
		}
	}
	return true
}

func patchTrimMatch(lines []string, pattern []string, start int) bool {
	for i := range pattern {
		if strings.TrimSpace(lines[start+i]) != strings.TrimSpace(pattern[i]) {
			return false
		}
	}
	return true
}

func patchNormalizedMatch(lines []string, pattern []string, start int) bool {
	for i := range pattern {
		if normalizePatchLine(lines[start+i]) != normalizePatchLine(pattern[i]) {
			return false
		}
	}
	return true
}

func normalizePatchLine(s string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(s) {
		switch r {
		case '\u2010', '\u2011', '\u2012', '\u2013', '\u2014', '\u2015', '\u2212':
			b.WriteRune('-')
		case '\u2018', '\u2019', '\u201A', '\u201B':
			b.WriteRune('\'')
		case '\u201C', '\u201D', '\u201E', '\u201F':
			b.WriteRune('"')
		case '\u00A0', '\u2002', '\u2003', '\u2004', '\u2005', '\u2006', '\u2007', '\u2008', '\u2009', '\u200A', '\u202F', '\u205F', '\u3000':
			b.WriteRune(' ')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func normalizePatchNewlines(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return s
}

func ensureRelativePatchPath(path string) error {
	if path == "" {
		return fmt.Errorf("path is required")
	}
	return nil
}

func maxInt(lhs, rhs int) int {
	if lhs > rhs {
		return lhs
	}
	return rhs
}
