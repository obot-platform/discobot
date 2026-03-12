// Package tools provides a concrete implementation of thread.ToolExecutor
// that executes all built-in tools natively in Go.
package tools

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/obot-platform/discobot/agent-go/message"
	"github.com/obot-platform/discobot/agent-go/thread"
)

// maxOutputLen is the maximum number of characters returned inline to the LLM.
// Outputs longer than this are written to a file and truncated.
const maxOutputLen = 30_000

// previewLen is the number of characters shown inline when output is spilled.
const previewLen = 5_000

// fileRecord stores the mtime and size of a file at the time it was last read
// via the Read tool. It is used to enforce the read-before-write invariant.
type fileRecord struct {
	modTime time.Time
	size    int64
}

// planModeBlockedTools lists tools that are rejected when plan mode is active.
// Plan mode is read-only: the agent may explore but must not write code or execute commands.
var planModeBlockedTools = map[string]bool{
	"Bash":          true,
	"Write":         true,
	"Edit":          true,
	"apply_patch":   true,
	"NotebookEdit":  true,
	"EnterPlanMode": true, // already in plan mode
}

// Executor implements thread.ToolExecutor with native Go tool implementations.
type Executor struct {
	cwd             string // workspace root for file and shell operations
	dataDir         string // root for persistent data (bash logs, etc.); separate from cwd
	defaultThreadID string

	// cwdMu guards currentCwd, which tracks the shell working directory
	// across Bash calls (cwd persists between commands, shell state does not).
	cwdMu      sync.Mutex
	currentCwd string

	// fileReadsMu guards fileReads, which records the mtime+size of every file
	// read via the Read tool. Write and Edit consult this to enforce
	// read-before-write: an existing file may not be overwritten unless the
	// executor has a matching record for it.
	fileReadsMu sync.RWMutex
	fileReads   map[string]fileRecord // keyed by absolute path

	// bashEnvAllowlist limits which environment variables are passed to Bash.
	// Empty means pass through the full process environment.
	bashEnvAllowlist []string

	// envLookup is an optional secondary source for environment variable
	// lookups (e.g. per-request credentials). It is consulted first; os.Getenv
	// is the fallback.
	envLookup func(key string) string
}

// New creates an Executor rooted at cwd.
// dataDir is the root for persistent storage (bash logs, plan files, spill files, etc.);
// it defaults to the user's home directory if empty.
func New(cwd, dataDir, threadID string) *Executor {
	if dataDir == "" {
		dataDir, _ = os.UserHomeDir()
	}
	return &Executor{
		cwd:             cwd,
		dataDir:         dataDir,
		defaultThreadID: threadID,
		currentCwd:      cwd,
		fileReads:       make(map[string]fileRecord),
	}
}

// recordFileRead saves the mtime and size of a file after a successful Read.
func (e *Executor) recordFileRead(absPath string, info os.FileInfo) {
	e.fileReadsMu.Lock()
	defer e.fileReadsMu.Unlock()
	e.fileReads[absPath] = fileRecord{modTime: info.ModTime(), size: info.Size()}
}

// recordFileWritten updates the stored record for a file after a successful
// Write or Edit, so subsequent writes don't require a re-read.
func (e *Executor) recordFileWritten(absPath string) {
	info, err := os.Stat(absPath)
	if err != nil {
		return
	}
	e.recordFileRead(absPath, info)
}

// checkWriteAllowed returns nil when it is safe to write to absPath:
//   - the file does not exist yet (new file creation is always permitted), or
//   - the file was previously read via the Read tool AND its mtime+size still
//     match the recorded snapshot (the file has not changed underneath us).
//
// displayPath is the user-facing path used in error messages.
func (e *Executor) checkWriteAllowed(absPath, displayPath string) error {
	info, err := os.Stat(absPath)
	if os.IsNotExist(err) {
		return nil // new file — no prior read required
	}
	if err != nil {
		return err
	}

	e.fileReadsMu.RLock()
	rec, ok := e.fileReads[absPath]
	e.fileReadsMu.RUnlock()

	if !ok {
		return fmt.Errorf("you must read %q before writing it", displayPath)
	}
	if rec.modTime != info.ModTime() || rec.size != info.Size() {
		return fmt.Errorf("%q has changed since it was last read — re-read it before writing", displayPath)
	}
	return nil
}

// SetBashEnvAllowlist configures a strict allowlist of env var names passed to
// Bash executions. When empty, Bash receives the full process environment.
func (e *Executor) SetBashEnvAllowlist(keys []string) {
	if len(keys) == 0 {
		e.bashEnvAllowlist = nil
		return
	}
	seen := map[string]struct{}{}
	filtered := make([]string, 0, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		filtered = append(filtered, key)
	}
	e.bashEnvAllowlist = filtered
}

// SetEnvLookup sets an optional function used to look up environment variables
// from a secondary source (e.g. per-request credentials). It is consulted
// before os.Getenv so that request-scoped values take precedence.
func (e *Executor) SetEnvLookup(fn func(key string) string) {
	e.envLookup = fn
}

// getenv returns the value of the environment variable named by key.
// It consults e.envLookup first (if set), then os.Getenv.
func (e *Executor) getenv(key string) string {
	if e.envLookup != nil {
		if v := e.envLookup(key); v != "" {
			return v
		}
	}
	return os.Getenv(key)
}

func (e *Executor) bashEnv() []string {
	if len(e.bashEnvAllowlist) == 0 {
		return os.Environ()
	}
	env := make([]string, 0, len(e.bashEnvAllowlist))
	for _, key := range e.bashEnvAllowlist {
		if value, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+value)
		}
	}
	return env
}

func contextThreadID(toolCtx *thread.ToolContext, fallback string) string {
	if toolCtx != nil && toolCtx.ThreadID != "" {
		return toolCtx.ThreadID
	}
	if fallback != "" {
		return fallback
	}
	return "default"
}

func isPlanMode(toolCtx *thread.ToolContext) bool {
	return toolCtx != nil && toolCtx.PlanMode
}

var planNameAdjectives = []string{
	"clear",
	"focused",
	"steady",
	"calm",
	"bright",
	"swift",
	"bold",
	"practical",
}

var planNameNouns = []string{
	"outline",
	"roadmap",
	"strategy",
	"approach",
	"design",
	"milestone",
	"workflow",
	"steps",
}

func (e *Executor) threadPlansDir(toolCtx *thread.ToolContext) string {
	return filepath.Join(e.dataDir, "plans", contextThreadID(toolCtx, e.defaultThreadID))
}

func (e *Executor) legacyPlanFilePath(toolCtx *thread.ToolContext) string {
	return filepath.Join(e.dataDir, "plan", contextThreadID(toolCtx, e.defaultThreadID)+".md")
}

func randomWord(words []string, fallback string) string {
	if len(words) == 0 {
		return fallback
	}
	b := make([]byte, 1)
	if _, err := rand.Read(b); err != nil {
		return words[int(time.Now().UTC().UnixNano()%int64(len(words)))]
	}
	return words[int(b[0])%len(words)]
}

func randomHex(byteLen int) string {
	if byteLen <= 0 {
		byteLen = 2
	}
	b := make([]byte, byteLen)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%x", time.Now().UTC().UnixNano())
	}
	return hex.EncodeToString(b)
}

func llmFriendlyPlanFileName() string {
	timestamp := time.Now().UTC().Format("20060102-150405")
	adjective := randomWord(planNameAdjectives, "steady")
	noun := randomWord(planNameNouns, "outline")
	return fmt.Sprintf("%s-%s-%s-%s.md", timestamp, adjective, noun, randomHex(2))
}

func (e *Executor) newPlanFilePath(toolCtx *thread.ToolContext) string {
	dir := e.threadPlansDir(toolCtx)
	for range 10 {
		candidate := filepath.Join(dir, llmFriendlyPlanFileName())
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			if toolCtx != nil {
				toolCtx.PlanFilePath = candidate
			}
			return candidate
		}
	}
	fallback := filepath.Join(dir, fmt.Sprintf("%d-%s.md", time.Now().UTC().UnixNano(), randomHex(2)))
	if toolCtx != nil {
		toolCtx.PlanFilePath = fallback
	}
	return fallback
}

func (e *Executor) latestThreadPlanFile(toolCtx *thread.ToolContext) string {
	dir := e.threadPlansDir(toolCtx)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}

	var latestPath string
	var latestModTime time.Time
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			continue
		}
		candidate := filepath.Join(dir, entry.Name())
		if latestPath == "" || info.ModTime().After(latestModTime) {
			latestPath = candidate
			latestModTime = info.ModTime()
		}
	}
	return latestPath
}

func (e *Executor) resolveActivePlanFile(toolCtx *thread.ToolContext) string {
	if toolCtx != nil && toolCtx.PlanFilePath != "" {
		return toolCtx.PlanFilePath
	}
	if latest := e.latestThreadPlanFile(toolCtx); latest != "" {
		if toolCtx != nil {
			toolCtx.PlanFilePath = latest
		}
		return latest
	}
	legacy := e.legacyPlanFilePath(toolCtx)
	if _, err := os.Stat(legacy); err == nil {
		if toolCtx != nil {
			toolCtx.PlanFilePath = legacy
		}
		return legacy
	}
	return e.newPlanFilePath(toolCtx)
}

// Execute dispatches to the appropriate tool handler and enforces output size limits.
func (e *Executor) Execute(ctx context.Context, toolCtx *thread.ToolContext, call message.ToolCallPart) (thread.ToolExecuteResult, error) {
	result, err := e.dispatch(ctx, toolCtx, call)
	if err != nil {
		return result, err
	}
	return e.limitOutput(toolCtx, call, result), nil
}

// dispatch routes a tool call to its handler.
func (e *Executor) dispatch(ctx context.Context, toolCtx *thread.ToolContext, call message.ToolCallPart) (thread.ToolExecuteResult, error) {
	if isPlanMode(toolCtx) && planModeBlockedTools[call.ToolName] && !e.isPlanFileCall(toolCtx, call) {
		if call.ToolName == "EnterPlanMode" {
			return errResult(call, "EnterPlanMode is not available — you are already in plan mode"), nil
		}
		planFile := e.resolveActivePlanFile(toolCtx)
		return errResult(call, fmt.Sprintf("%s is not available in plan mode — use the Write, Edit, or apply_patch tool to write your complete plan to %s (Write, Edit, and apply_patch are allowed for the plan file), then call ExitPlanMode", call.ToolName, planFile)), nil
	}

	switch call.ToolName {
	case "Bash":
		return e.executeBash(ctx, toolCtx, call)
	case "Read":
		return e.executeRead(toolCtx, call)
	case "Write":
		return e.executeWrite(call)
	case "Edit":
		return e.executeEdit(call)
	case "apply_patch":
		return e.executeApplyPatch(call)
	case "Glob":
		return e.executeGlob(call)
	case "Grep":
		return e.executeGrep(ctx, call)
	case "WebFetch":
		return e.executeWebFetch(ctx, call)
	case "WebSearch":
		return e.executeWebSearch(ctx, call)
	case "NotebookEdit":
		return e.executeNotebookEdit(call)
	case "AskUserQuestion":
		return e.executeAskUserQuestion(call)
	case "EnterPlanMode":
		return e.executeEnterPlanMode(toolCtx, call)
	case "ExitPlanMode":
		return e.executeExitPlanMode(toolCtx, call)
	case "Task", "Agent":
		return e.executeTask(ctx, toolCtx, call)
	case "TodoWrite":
		return e.executeTodoWrite(ctx, toolCtx, call)
	case "TaskOutput":
		return e.executeTaskOutput(call)
	case "TaskStop":
		return e.executeTaskStop(call)
	case "Skill":
		return e.executeSkill(ctx, call)
	default:
		return textResult(call, fmt.Sprintf("unknown tool: %s", call.ToolName)), nil
	}
}

// limitOutput checks whether a successful TextOutput exceeds maxOutputLen.
// If it does, the full content is written to a file and the inline value is
// replaced with a short preview plus a path to the full output.
func (e *Executor) limitOutput(toolCtx *thread.ToolContext, call message.ToolCallPart, result thread.ToolExecuteResult) thread.ToolExecuteResult {
	to, ok := result.Result.Output.(message.TextOutput)
	if !ok || len(to.Value) <= maxOutputLen {
		return result
	}

	outPath, writeErr := e.spillToFile(toolCtx, call, to.Value)

	preview := to.Value[:previewLen]
	var truncated string
	if writeErr != nil {
		truncated = fmt.Sprintf(
			"[Output too long (%d chars). Could not write to file: %v]\n\n%s\n\n[...truncated]",
			len(to.Value), writeErr, preview,
		)
	} else {
		truncated = fmt.Sprintf(
			"[Output too long (%d chars). Full output written to: %s]\n\n%s\n\n[...truncated — read %s for the full output]",
			len(to.Value), outPath, preview, outPath,
		)
	}

	result.Result.Output = message.TextOutput{Value: truncated}
	return result
}

// spillToFile writes text to {dataDir}/output/{threadID}/{toolCallID}.txt
// and returns the absolute path.
func (e *Executor) spillToFile(toolCtx *thread.ToolContext, call message.ToolCallPart, text string) (string, error) {
	dir := filepath.Join(e.dataDir, "output", contextThreadID(toolCtx, e.defaultThreadID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, call.ToolCallID+".txt")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// ResolveApproval converts a user's answers into a tool result after an ApprovalRequest.
func (e *Executor) ResolveApproval(toolCtx *thread.ToolContext, call message.ToolCallPart, answers map[string]string) (message.ToolResultPart, error) {
	switch call.ToolName {
	case "AskUserQuestion":
		return e.resolveAskUserQuestion(call, answers)
	case "EnterPlanMode":
		return e.resolveEnterPlanMode(call, answers)
	case "ExitPlanMode":
		return e.resolveExitPlanMode(toolCtx, call, answers)
	default:
		return message.ToolResultPart{}, fmt.Errorf("ResolveApproval not supported for tool %s", call.ToolName)
	}
}

// ResumeAsync re-attaches to a previously launched async background task.
func (e *Executor) ResumeAsync(ctx context.Context, toolCtx *thread.ToolContext, call message.ToolCallPart, taskID string) (thread.ToolExecuteResult, error) {
	switch call.ToolName {
	case "Task", "Agent":
		return e.resumeTask(ctx, toolCtx, call, taskID)
	default:
		return thread.ToolExecuteResult{
			Result: errorResult(call, fmt.Sprintf("async task for %s lost after crash (taskID: %s)", call.ToolName, taskID)),
		}, nil
	}
}

// --- helpers ---

// textResult builds a successful text tool result.
func textResult(call message.ToolCallPart, text string) thread.ToolExecuteResult {
	return thread.ToolExecuteResult{
		Result: message.ToolResultPart{
			ToolCallID: call.ToolCallID,
			ToolName:   call.ToolName,
			Output:     message.TextOutput{Value: text},
		},
	}
}

// errorResult builds an error text tool result.
func errorResult(call message.ToolCallPart, msg string) message.ToolResultPart {
	return message.ToolResultPart{
		ToolCallID: call.ToolCallID,
		ToolName:   call.ToolName,
		Output:     message.ErrorTextOutput{Value: msg},
	}
}

// errResult wraps errorResult in a ToolExecuteResult.
func errResult(call message.ToolCallPart, msg string) thread.ToolExecuteResult {
	return thread.ToolExecuteResult{Result: errorResult(call, msg)}
}

// unmarshalInput decodes the tool call input JSON into dst.
func unmarshalInput(call message.ToolCallPart, dst any) error {
	if err := json.Unmarshal([]byte(call.Input), dst); err != nil {
		return fmt.Errorf("invalid input for %s: %w", call.ToolName, err)
	}
	return nil
}

// isPlanFileCall returns true when a Write, Edit, or apply_patch tool call
// targets only the active plan file for the current thread. These calls are
// allowed even in plan mode so the agent can write its plan.
func (e *Executor) isPlanFileCall(toolCtx *thread.ToolContext, call message.ToolCallPart) bool {
	switch call.ToolName {
	case "Write", "Edit":
		return e.isPlanFileWriteCall(toolCtx, call)
	case "apply_patch":
		return e.isPlanFilePatchCall(toolCtx, call)
	default:
		return false
	}
}

func (e *Executor) isPlanFileWriteCall(toolCtx *thread.ToolContext, call message.ToolCallPart) bool {
	planFile := e.resolveActivePlanFile(toolCtx)

	var input struct {
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal([]byte(call.Input), &input); err != nil || input.FilePath == "" {
		return false
	}
	target := resolvePath(e.cwd, input.FilePath)
	return sameResolvedPath(target, planFile)
}

func (e *Executor) isPlanFilePatchCall(toolCtx *thread.ToolContext, call message.ToolCallPart) bool {
	patchText, err := parseApplyPatchInput(call.Input)
	if err != nil {
		return false
	}
	ops, err := parseApplyPatch(patchText)
	if err != nil {
		return false
	}
	if len(ops) == 0 {
		return false
	}
	planFile := e.resolveActivePlanFile(toolCtx)
	for _, op := range ops {
		if op.kind == patchDeleteFile || op.movePath != "" {
			return false
		}
		if !sameResolvedPath(resolvePath(e.cwd, op.path), planFile) {
			return false
		}
	}
	return true
}

func sameResolvedPath(targetPath, expectedPath string) bool {
	absTarget, err := filepath.Abs(targetPath)
	if err != nil {
		return false
	}
	absExpected, err := filepath.Abs(expectedPath)
	if err != nil {
		return false
	}
	return absTarget == absExpected
}

// resolvePath resolves a file path relative to cwd.
// Absolute paths are returned as-is; relative paths are joined with cwd.
func resolvePath(cwd, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	if cwd == "" || path == "" {
		return path
	}
	return filepath.Join(cwd, path)
}
