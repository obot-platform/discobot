package hooks

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/obot-platform/discobot/agent-go/message"
)

const (
	// InlineOutputMaxLines is the max lines to inline in LLM failure messages.
	InlineOutputMaxLines = 200
	// InlineOutputMaxBytes is the max bytes to inline in LLM failure messages.
	InlineOutputMaxBytes = 5 * 1024
	// TruncatedOutputTailLines is the number of trailing lines to show for large hook output.
	TruncatedOutputTailLines = 15
)

// HookFailureMessageMetadata carries structured hook-failure details for UI rendering.
type HookFailureMessageMetadata struct {
	Kind            string   `json:"kind"`
	HookName        string   `json:"hookName"`
	ExitCode        int      `json:"exitCode"`
	Pattern         string   `json:"pattern,omitempty"`
	HookPath        string   `json:"hookPath,omitempty"`
	Files           []string `json:"files,omitempty"`
	ExtraFileCount  int      `json:"extraFileCount,omitempty"`
	Output          string   `json:"output,omitempty"`
	OutputPath      string   `json:"outputPath,omitempty"`
	OutputTail      string   `json:"outputTail,omitempty"`
	OutputTruncated bool     `json:"outputTruncated,omitempty"`
}

// FileHookEvalResult is the result of evaluating file hooks after a completion.
type FileHookEvalResult struct {
	Evaluated      bool
	ShouldReprompt bool
	LLMMessage     string
	FailedResult   *HookResult
	HookFailure    *HookFailureMessageMetadata
}

// HookRunResult is the result of a manual hook run.
type HookRunResult struct {
	Result HookResult
	Eval   FileHookEvalResult
}

// Manager orchestrates hook discovery, execution, and status tracking.
type Manager struct {
	workspaceRoot string
	sessionID     string
	hooksDataDir  string

	mu             sync.Mutex
	fileHooks      []Hook
	preCommitHooks []Hook
	initialized    bool
	chunkEmitter   func(message.MessageChunk)
}

// NewManager creates a new HookManager.
func NewManager(workspaceRoot, sessionID string) *Manager {
	return &Manager{
		workspaceRoot: workspaceRoot,
		sessionID:     sessionID,
		hooksDataDir:  GetHooksDataDir(sessionID),
	}
}

// SetChunkEmitter configures how hook status updates are emitted as message chunks.
func (m *Manager) SetChunkEmitter(fn func(message.MessageChunk)) {
	m.mu.Lock()
	m.chunkEmitter = fn
	m.mu.Unlock()
}

func (m *Manager) statusChunk(status StatusFile) (message.MessageChunk, error) {
	data, err := json.Marshal(status)
	if err != nil {
		return nil, err
	}
	return message.DataChunk{
		DataType: "hooks-status",
		Data:     data,
	}, nil
}

func (m *Manager) emitStatusChunk(status StatusFile) {
	m.mu.Lock()
	emitter := m.chunkEmitter
	m.mu.Unlock()
	if emitter == nil {
		return
	}

	chunk, err := m.statusChunk(status)
	if err != nil {
		log.Printf("hooks: failed to marshal status chunk: %v", err)
		return
	}
	emitter(chunk)
}

func (m *Manager) emitCurrentStatusChunk() {
	m.emitStatusChunk(LoadStatus(m.hooksDataDir))
}

// Init discovers hooks and installs pre-commit hooks if needed.
func (m *Manager) Init() error {
	m.mu.Lock()
	if m.initialized {
		m.mu.Unlock()
		return nil
	}

	if err := m.loadHooks(); err != nil {
		m.mu.Unlock()
		return err
	}

	fileHookIDs := make([]string, 0, len(m.fileHooks))
	for _, hook := range m.fileHooks {
		fileHookIDs = append(fileHookIDs, hook.ID)
	}
	if err := RecoverInterruptedHooks(m.hooksDataDir, fileHookIDs); err != nil {
		m.mu.Unlock()
		return err
	}

	m.initialized = true
	m.mu.Unlock()
	return nil
}

// loadHooks discovers and categorizes hooks. Must be called with m.mu held.
func (m *Manager) loadHooks() error {
	hooksDir := filepath.Join(m.workspaceRoot, HooksDir)
	allHooks, err := DiscoverHooks(hooksDir)
	if err != nil {
		return fmt.Errorf("discover hooks: %w", err)
	}

	m.fileHooks = nil
	m.preCommitHooks = nil

	for _, hook := range allHooks {
		switch hook.Type {
		case HookTypeFile:
			m.fileHooks = append(m.fileHooks, hook)
		case HookTypePreCommit:
			m.preCommitHooks = append(m.preCommitHooks, hook)
		}
	}

	if len(m.preCommitHooks) > 0 {
		if err := InstallPreCommitHook(m.workspaceRoot, m.preCommitHooks, m.sessionID); err != nil {
			log.Printf("Warning: failed to install pre-commit hook: %v", err)
		}
	}

	return nil
}

// reloadHooks re-discovers hooks from disk. Must be called with m.mu held.
func (m *Manager) reloadHooks() {
	if err := m.loadHooks(); err != nil {
		log.Printf("Warning: failed to reload hooks: %v", err)
	}
}

// checkAndReloadHooks checks if hook files have changed and reloads if needed.
// Must be called with m.mu held.
func (m *Manager) checkAndReloadHooks() {
	markerPath := filepath.Join(m.hooksDataDir, ".last-eval")
	markerInfo, err := os.Stat(markerPath)
	if err != nil {
		// No marker yet (first eval) — hooks are already fresh from init()
		return
	}
	markerMtime := markerInfo.ModTime()

	hooksDir := filepath.Join(m.workspaceRoot, HooksDir)

	// Check directory mtime
	dirInfo, err := os.Stat(hooksDir)
	if err != nil {
		return
	}
	if dirInfo.ModTime().After(markerMtime) {
		m.reloadHooks()
		return
	}

	// Check individual file mtimes
	entries, err := os.ReadDir(hooksDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(markerMtime) {
			m.reloadHooks()
			return
		}
	}
}

// HasFileHooks returns whether any file hooks are configured.
func (m *Manager) HasFileHooks() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.fileHooks) > 0
}

// GetStatus returns the current hook status.
func (m *Manager) GetStatus() StatusFile {
	m.mu.Lock()
	m.reloadHooks()
	m.mu.Unlock()
	return LoadStatus(m.hooksDataDir)
}

// GetHookOutput returns the output log for a hook.
func (m *Manager) GetHookOutput(hookID string) (string, error) {
	outputPath := GetHookOutputPath(m.hooksDataDir, hookID)
	data, err := os.ReadFile(outputPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

// RerunHook manually reruns a file hook against current dirty files.
func (m *Manager) RerunHook(hookID string) (*HookRunResult, error) {
	m.mu.Lock()
	m.reloadHooks()
	var hook *Hook
	for i := range m.fileHooks {
		if m.fileHooks[i].ID == hookID {
			hook = &m.fileHooks[i]
			break
		}
	}
	m.mu.Unlock()

	if hook == nil || hook.Pattern == "" {
		return nil, nil
	}

	allDirty := getAllDirtyFiles(m.workspaceRoot)
	matching := matchFiles(allDirty, hook.Pattern)
	if len(matching) == 0 {
		matching = allDirty // run even with no matches
	}

	outputPath := GetHookOutputPath(m.hooksDataDir, hook.ID)
	_ = SetHookRunning(m.hooksDataDir, *hook)
	m.emitCurrentStatusChunk()

	result := ExecuteHook(*hook, ExecuteOptions{
		Cwd:          m.workspaceRoot,
		ChangedFiles: matching,
		SessionID:    m.sessionID,
		OutputPath:   outputPath,
	})

	eval := FileHookEvalResult{}
	if !result.Success {
		eval = buildHookFailureEvalResult(result, matching, outputPath, m.workspaceRoot)
	}

	_ = UpdateHookStatus(m.hooksDataDir, result, outputPath)

	if result.Success {
		_ = RemovePendingHook(m.hooksDataDir, hook.ID)
	}

	_ = UpdateLastEvaluatedAt(m.hooksDataDir)
	m.emitCurrentStatusChunk()

	return &HookRunResult{Result: result, Eval: eval}, nil
}

// EvaluateFileHooks evaluates file hooks after a completion.
func (m *Manager) EvaluateFileHooks() FileHookEvalResult {
	noAction := FileHookEvalResult{}

	m.mu.Lock()
	m.checkAndReloadHooks()
	fileHooks := make([]Hook, len(m.fileHooks))
	copy(fileHooks, m.fileHooks)
	m.mu.Unlock()

	if len(fileHooks) == 0 {
		return noAction
	}

	// Find files changed since marker
	newFiles := m.findChangedFilesSinceMarker()

	addedNewPending := false
	if len(newFiles) > 0 {
		// Match against hook patterns
		var pendingIDs []string
		for _, hook := range fileHooks {
			if hook.Pattern == "" {
				continue
			}
			if len(matchFiles(newFiles, hook.Pattern)) > 0 {
				pendingIDs = append(pendingIDs, hook.ID)
			}
		}
		if len(pendingIDs) > 0 {
			_ = AddPendingHooks(m.hooksDataDir, pendingIDs)
			m.emitCurrentStatusChunk()
			addedNewPending = true
		}
	}

	// Always advance marker
	m.touchMarker()

	// Get pending hooks
	pendingIDs := GetPendingHookIDs(m.hooksDataDir)
	if len(pendingIDs) == 0 {
		return noAction
	}

	// Skip guard: if no new files and didn't add new pending, don't re-run failed hooks
	if len(newFiles) == 0 && !addedNewPending {
		return noAction
	}

	pendingSet := make(map[string]bool, len(pendingIDs))
	for _, id := range pendingIDs {
		pendingSet[id] = true
	}

	// Get all dirty files for pattern matching
	allDirty := getAllDirtyFiles(m.workspaceRoot)

	for _, hook := range fileHooks {
		if !pendingSet[hook.ID] {
			continue
		}

		matching := matchFiles(allDirty, hook.Pattern)
		if len(matching) == 0 {
			// Files were committed/fixed — remove from pending
			_ = RemovePendingHook(m.hooksDataDir, hook.ID)
			m.emitCurrentStatusChunk()
			continue
		}

		outputPath := GetHookOutputPath(m.hooksDataDir, hook.ID)
		_ = SetHookRunning(m.hooksDataDir, hook)
		m.emitCurrentStatusChunk()

		result := ExecuteHook(hook, ExecuteOptions{
			Cwd:          m.workspaceRoot,
			ChangedFiles: matching,
			SessionID:    m.sessionID,
			OutputPath:   outputPath,
		})

		_ = UpdateHookStatus(m.hooksDataDir, result, outputPath)
		m.emitCurrentStatusChunk()

		if result.Success {
			_ = RemovePendingHook(m.hooksDataDir, hook.ID)
			m.emitCurrentStatusChunk()
			continue
		}

		// Hook failed
		return buildHookFailureEvalResult(result, matching, outputPath, m.workspaceRoot)
	}

	// All pending hooks cleared
	return FileHookEvalResult{Evaluated: true}
}

// getAllDirtyFiles returns all dirty files in the workspace (staged, unstaged, untracked).
func getAllDirtyFiles(workspaceRoot string) []string {
	fileSet := make(map[string]bool)

	// git diff --name-only HEAD
	if out, err := gitOutput(workspaceRoot, "diff", "--name-only", "HEAD"); err == nil {
		for _, f := range splitLines(out) {
			fileSet[f] = true
		}
	}

	// git ls-files --others --exclude-standard
	if out, err := gitOutput(workspaceRoot, "ls-files", "--others", "--exclude-standard"); err == nil {
		for _, f := range splitLines(out) {
			fileSet[f] = true
		}
	}

	result := make([]string, 0, len(fileSet))
	for f := range fileSet {
		result = append(result, f)
	}
	return result
}

// findChangedFilesSinceMarker returns dirty files newer than the .last-eval marker.
func (m *Manager) findChangedFilesSinceMarker() []string {
	markerPath := filepath.Join(m.hooksDataDir, ".last-eval")
	markerInfo, err := os.Stat(markerPath)

	allDirty := getAllDirtyFiles(m.workspaceRoot)

	if err != nil {
		// No marker — all dirty files are new
		return allDirty
	}
	markerMtime := markerInfo.ModTime()

	var changed []string
	for _, f := range allDirty {
		fullPath := filepath.Join(m.workspaceRoot, f)
		info, err := os.Stat(fullPath)
		if err != nil {
			continue
		}
		if info.ModTime().After(markerMtime) {
			changed = append(changed, f)
		}
	}
	return changed
}

// touchMarker creates or updates the .last-eval marker file.
func (m *Manager) touchMarker() {
	_ = os.MkdirAll(m.hooksDataDir, 0o755)
	markerPath := filepath.Join(m.hooksDataDir, ".last-eval")

	now := time.Now()
	if err := os.Chtimes(markerPath, now, now); err != nil {
		// File doesn't exist — create it
		_ = os.WriteFile(markerPath, nil, 0o644)
	}

	_ = UpdateLastEvaluatedAt(m.hooksDataDir)
}

// matchFiles returns files that match a glob pattern.
// Supports *, **, ?, [class], and {a,b,c} brace expansion via doublestar.
func matchFiles(files []string, pattern string) []string {
	pattern = filepath.ToSlash(pattern)
	var matched []string
	for _, f := range files {
		ok, err := doublestar.Match(pattern, filepath.ToSlash(f))
		if err == nil && ok {
			matched = append(matched, f)
		}
	}
	return matched
}

// buildHookFailureEvalResult builds the evaluation response for a failed hook.
func buildHookFailureEvalResult(result HookResult, matchingFiles []string, outputPath, workspaceRoot string) FileHookEvalResult {
	if result.Success {
		return FileHookEvalResult{}
	}
	if !result.Hook.NotifyLLM {
		return FileHookEvalResult{
			Evaluated:      true,
			ShouldReprompt: false,
			FailedResult:   &result,
		}
	}

	meta := buildHookFailureMessageMetadata(result, matchingFiles, outputPath, workspaceRoot)
	msg := formatHookFailureMessage(meta)
	return FileHookEvalResult{
		Evaluated:      true,
		ShouldReprompt: true,
		LLMMessage:     msg,
		FailedResult:   &result,
		HookFailure:    &meta,
	}
}

func normalizeHookMessagePath(workspaceRoot, path string) string {
	if path == "" || !filepath.IsAbs(path) || workspaceRoot == "" {
		return path
	}

	relPath, err := filepath.Rel(workspaceRoot, path)
	if err != nil || relPath == "." || strings.HasPrefix(relPath, "..") {
		return path
	}

	return filepath.ToSlash(relPath)
}

// buildHookFailureMessageMetadata builds structured hook-failure metadata for UI rendering.
func buildHookFailureMessageMetadata(result HookResult, matchingFiles []string, outputPath, workspaceRoot string) HookFailureMessageMetadata {
	meta := HookFailureMessageMetadata{
		Kind:     "hook-failure",
		HookName: result.Hook.Name,
		ExitCode: result.ExitCode,
		Pattern:  result.Hook.Pattern,
		HookPath: normalizeHookMessagePath(workspaceRoot, result.Hook.Path),
	}

	if len(matchingFiles) > 0 {
		displayFiles := matchingFiles
		if len(displayFiles) > 20 {
			meta.ExtraFileCount = len(displayFiles) - 20
			displayFiles = displayFiles[:20]
		}
		meta.Files = append([]string(nil), displayFiles...)
	}

	output := strings.TrimSpace(result.Output)
	if output == "" {
		return meta
	}

	lines := strings.Split(output, "\n")
	if len(lines) > InlineOutputMaxLines || len(output) > InlineOutputMaxBytes {
		meta.OutputPath = outputPath
		meta.OutputTail = lastLines(output, TruncatedOutputTailLines)
		meta.OutputTruncated = true
		return meta
	}

	meta.Output = output
	return meta
}

func lastLines(output string, maxLines int) string {
	if maxLines <= 0 || output == "" {
		return ""
	}

	lines := strings.Split(output, "\n")
	if len(lines) <= maxLines {
		return output
	}

	return strings.Join(lines[len(lines)-maxLines:], "\n")
}

// formatHookFailureMessage builds the markdown message for LLM re-prompt.
func formatHookFailureMessage(meta HookFailureMessageMetadata) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("### Hook failed: %s\n\n", meta.HookName))
	b.WriteString(fmt.Sprintf("- Exit code: `%d`\n", meta.ExitCode))
	if meta.Pattern != "" {
		b.WriteString(fmt.Sprintf("- Pattern: `%s`\n", meta.Pattern))
	}

	if len(meta.Files) > 0 {
		filesStr := strings.Join(meta.Files, ", ")
		if meta.ExtraFileCount > 0 {
			filesStr += fmt.Sprintf(", and %d more", meta.ExtraFileCount)
		}
		b.WriteString(fmt.Sprintf("- Files: %s\n", filesStr))
	}
	b.WriteString("\n")

	if meta.OutputTruncated && meta.OutputPath != "" {
		b.WriteString("#### Output\n\n")
		b.WriteString(fmt.Sprintf("Output was too long to inline. Full output was written to `%s`.\n\n", meta.OutputPath))
		if meta.OutputTail != "" {
			b.WriteString(fmt.Sprintf("Last %d lines:\n\n", TruncatedOutputTailLines))
			b.WriteString("```text\n")
			b.WriteString(meta.OutputTail)
			b.WriteString("\n```\n\n")
		}
	} else if meta.OutputTruncated && meta.OutputTail != "" {
		b.WriteString("#### Output\n\n")
		b.WriteString("```text\n")
		b.WriteString(meta.OutputTail)
		b.WriteString("\n```\n\n")
	} else if meta.Output != "" {
		b.WriteString("#### Output\n\n")
		b.WriteString("```text\n")
		b.WriteString(meta.Output)
		b.WriteString("\n```\n\n")
	}

	b.WriteString("Please fix the issues above and ensure the hook passes.")

	return b.String()
}

// gitOutput runs a git command and returns trimmed stdout.
func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// splitLines splits output by newlines, filtering empty lines.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			result = append(result, line)
		}
	}
	return result
}
