package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	// hooksDir is the directory within the workspace containing hook files
	hooksDir = ".discobot/hooks"

	// sessionHookTimeout is the maximum execution time per session hook
	sessionHookTimeout = 5 * time.Minute

	// sessionHookMaxAttempts is the maximum number of times a session hook is retried
	// before its overall run is recorded as a failure.
	sessionHookMaxAttempts = 10
)

// hookConfig represents parsed hook front matter
type hookConfig struct {
	Name     string // Display name
	Type     string // "session", "file", "pre-commit"
	RunAs    string // "root" or "user" (default: "user")
	Blocking bool   // If true, session hook blocks agent startup (default: false)
}

// hookRunStatus represents the persisted status of a single hook's runs.
// Schema matches the TypeScript HookRunStatus in agent-api/src/hooks/status.ts.
type hookRunStatus struct {
	HookID              string `json:"hookId"`
	HookName            string `json:"hookName"`
	Type                string `json:"type"`
	LastRunAt           string `json:"lastRunAt"`
	LastResult          string `json:"lastResult"`
	LastExitCode        int    `json:"lastExitCode"`
	OutputPath          string `json:"outputPath"`
	RunCount            int    `json:"runCount"`
	FailCount           int    `json:"failCount"`
	ConsecutiveFailures int    `json:"consecutiveFailures"`
}

// hookStatusFile represents the top-level status file schema.
// Schema matches the TypeScript HookStatusFile in agent-api/src/hooks/status.ts.
type hookStatusFile struct {
	Hooks           map[string]hookRunStatus `json:"hooks"`
	PendingHooks    []string                 `json:"pendingHooks"`
	LastEvaluatedAt string                   `json:"lastEvaluatedAt"`
}

// hooksDataDir returns the hooks data directory for a session: ~/.discobot/threads/{sessionId}/hooks/
func hooksDataDir(homeDir, sessionID string) string {
	return filepath.Join(homeDir, ".discobot", "threads", sessionID, "hooks")
}

// hookOutputPath returns the output log path for a hook: {hooksDataDir}/output/{hookId}.log
func hookOutputPath(dataDir, hookID string) string {
	return filepath.Join(dataDir, "output", hookID+".log")
}

// normalizeHookID converts a filename to a hook ID.
// Matches normalizeServiceId in agent-api/src/services/parser.ts.
func normalizeHookID(filename string) string {
	extensions := []string{".sh", ".bash", ".zsh", ".py", ".js", ".ts", ".rb", ".pl", ".php"}

	id := filename
	lower := strings.ToLower(filename)
	for _, ext := range extensions {
		if strings.HasSuffix(lower, ext) {
			id = id[:len(id)-len(ext)]
			break
		}
	}

	// Replace dots with hyphens
	id = strings.ReplaceAll(id, ".", "-")

	// Convert to lowercase
	id = strings.ToLower(id)

	// Remove any characters that aren't a-z0-9_-
	var result strings.Builder
	for _, c := range id {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' || c == '-' {
			result.WriteRune(c)
		}
	}
	id = result.String()

	// Remove leading/trailing hyphens
	id = strings.Trim(id, "-")

	return id
}

// loadHookStatus reads the status file from disk. Returns empty status if not found.
func loadHookStatus(dataDir string) hookStatusFile {
	filePath := filepath.Join(dataDir, "status.json")

	data, err := os.ReadFile(filePath)
	if err != nil {
		return hookStatusFile{Hooks: make(map[string]hookRunStatus)}
	}

	var status hookStatusFile
	if err := json.Unmarshal(data, &status); err != nil {
		return hookStatusFile{Hooks: make(map[string]hookRunStatus)}
	}

	if status.Hooks == nil {
		status.Hooks = make(map[string]hookRunStatus)
	}
	if status.PendingHooks == nil {
		status.PendingHooks = []string{}
	}

	return status
}

// saveHookStatus writes the status file atomically (write-to-temp + rename).
func saveHookStatus(dataDir string, status hookStatusFile) error {
	filePath := filepath.Join(dataDir, "status.json")

	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(status, "", "\t")
	if err != nil {
		return err
	}

	tmpPath := fmt.Sprintf("%s.tmp.%d", filePath, time.Now().UnixMilli())
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		os.Remove(tmpPath)
		return err
	}

	return os.Rename(tmpPath, filePath)
}

// updateSessionHookStatus updates the status for a session hook after execution.
func updateSessionHookStatus(dataDir, hookID, hookName string, success bool, exitCode int, outputPath string) {
	status := loadHookStatus(dataDir)

	existing, exists := status.Hooks[hookID]

	runCount := 1
	failCount := 0
	consecutiveFailures := 0
	if exists {
		runCount = existing.RunCount + 1
		failCount = existing.FailCount
		consecutiveFailures = existing.ConsecutiveFailures
	}

	if !success {
		failCount++
		consecutiveFailures++
	} else {
		consecutiveFailures = 0
	}

	resultStr := "success"
	if !success {
		resultStr = "failure"
	}

	status.Hooks[hookID] = hookRunStatus{
		HookID:              hookID,
		HookName:            hookName,
		Type:                "session",
		LastRunAt:           time.Now().UTC().Format(time.RFC3339Nano),
		LastResult:          resultStr,
		LastExitCode:        exitCode,
		OutputPath:          outputPath,
		RunCount:            runCount,
		FailCount:           failCount,
		ConsecutiveFailures: consecutiveFailures,
	}

	if err := saveHookStatus(dataDir, status); err != nil {
		fmt.Fprintf(os.Stderr, "discobot-agent: failed to save hook status: %v\n", err)
	}
}

// parseHookFrontMatter extracts hook configuration from file content.
// Supports the same #--- delimited YAML front matter as the TypeScript services parser.
func parseHookFrontMatter(content string) hookConfig {
	config := hookConfig{}
	lines := strings.Split(content, "\n")

	if len(lines) == 0 {
		return config
	}

	// Determine where front matter starts (skip shebang)
	startLine := 0
	if strings.HasPrefix(lines[0], "#!") {
		startLine = 1
	}

	if startLine >= len(lines) {
		return config
	}

	// Detect delimiter — only support #--- for shell scripts (most common for hooks)
	trimmed := strings.TrimSpace(lines[startLine])
	var delimiter string
	var prefix string

	switch trimmed {
	case "---":
		delimiter = "---"
		prefix = ""
	case "#---":
		delimiter = "#---"
		prefix = "#"
	case "//---":
		delimiter = "//---"
		prefix = "//"
	default:
		return config // No front matter
	}

	// Extract YAML lines between delimiters
	var yamlLines []string
	found := false
	for i := startLine + 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == delimiter {
			found = true
			break
		}
		line := lines[i]
		if prefix != "" {
			idx := strings.Index(line, prefix)
			if idx != -1 {
				line = strings.TrimSpace(line[idx+len(prefix):])
			}
		}
		yamlLines = append(yamlLines, line)
	}

	if !found {
		return config // No closing delimiter
	}

	// Parse simple key: value pairs
	for _, line := range yamlLines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		colonIdx := strings.Index(line, ":")
		if colonIdx == -1 {
			continue
		}

		key := strings.TrimSpace(line[:colonIdx])
		value := strings.TrimSpace(line[colonIdx+1:])

		// Remove quotes
		if (strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`)) ||
			(strings.HasPrefix(value, `'`) && strings.HasSuffix(value, `'`)) {
			value = value[1 : len(value)-1]
		}

		switch key {
		case "name":
			config.Name = value
		case "type":
			config.Type = value
		case "run_as":
			config.RunAs = value
		case "blocking":
			config.Blocking = strings.EqualFold(value, "true")
		}
	}

	return config
}

// discoverSessionHooks scans the hooks directory and returns session hooks sorted by filename.
func discoverSessionHooks(workspacePath string) ([]string, []hookConfig) {
	dir := filepath.Join(workspacePath, hooksDir)

	entries, err := os.ReadDir(dir)
	if err != nil {
		// Directory doesn't exist — not an error
		return nil, nil
	}

	var paths []string
	var configs []hookConfig

	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		filePath := filepath.Join(dir, entry.Name())

		// Check executable bit
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.Mode()&0o111 == 0 {
			continue
		}

		// Read and parse content
		content, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		contentStr := string(content)

		// Must have shebang
		if !strings.HasPrefix(contentStr, "#!") {
			continue
		}

		config := parseHookFrontMatter(contentStr)
		if config.Type != "session" {
			continue
		}

		if config.Name == "" {
			config.Name = entry.Name()
		}

		paths = append(paths, filePath)
		configs = append(configs, config)
	}

	// Sort by filename for deterministic order
	if len(paths) > 1 {
		type hookEntry struct {
			path   string
			config hookConfig
		}
		entries := make([]hookEntry, len(paths))
		for i := range paths {
			entries[i] = hookEntry{paths[i], configs[i]}
		}
		sort.Slice(entries, func(i, j int) bool {
			return filepath.Base(entries[i].path) < filepath.Base(entries[j].path)
		})
		for i := range entries {
			paths[i] = entries[i].path
			configs[i] = entries[i].config
		}
	}

	return paths, configs
}

// runSessionHook executes a single session hook, captures output, and updates status.json.
// Returns true if the hook succeeded, false otherwise.
func runSessionHook(hookPath string, config hookConfig, workspacePath, sessionID, dataDir string, u *userInfo) bool {
	name := config.Name
	hookID := normalizeHookID(filepath.Base(hookPath))

	runAs := config.RunAs
	if runAs == "" {
		runAs = "user"
	}

	result := runSessionHookWithRetry(name, func(attempt int) sessionHookAttemptResult {
		return runSessionHookAttempt(hookPath, name, runAs, workspacePath, sessionID, u, attempt)
	})

	outPath := hookOutputPath(dataDir, hookID)
	if err := os.WriteFile(outPath, result.output, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "discobot-agent: failed to save hook output: %v\n", err)
	} else {
		_ = os.Chown(outPath, u.uid, u.gid)
	}

	// Update status.json
	updateSessionHookStatus(dataDir, hookID, name, result.success, result.exitCode, outPath)
	// Chown status file so agent-api can update it later
	_ = os.Chown(filepath.Join(dataDir, "status.json"), u.uid, u.gid)

	return result.success
}

type sessionHookAttemptResult struct {
	success  bool
	exitCode int
	output   []byte
}

func runSessionHookWithRetry(name string, runner func(attempt int) sessionHookAttemptResult) sessionHookAttemptResult {
	var finalResult sessionHookAttemptResult
	var combinedOutput bytes.Buffer

	for attempt := 1; attempt <= sessionHookMaxAttempts; attempt++ {
		attemptResult := runner(attempt)
		finalResult = attemptResult

		if !attemptResult.success || attempt > 1 {
			if combinedOutput.Len() > 0 {
				combinedOutput.WriteByte('\n')
			}
			fmt.Fprintf(&combinedOutput, "=== Attempt %d/%d ===\n", attempt, sessionHookMaxAttempts)
			combinedOutput.Write(attemptResult.output)
			if len(attemptResult.output) > 0 && attemptResult.output[len(attemptResult.output)-1] != '\n' {
				combinedOutput.WriteByte('\n')
			}
		} else {
			combinedOutput.Write(attemptResult.output)
		}

		if attemptResult.success {
			finalResult.output = combinedOutput.Bytes()
			return finalResult
		}

		if attempt < sessionHookMaxAttempts {
			fmt.Fprintf(os.Stderr, "discobot-agent: retrying session hook %q (attempt %d/%d)\n", name, attempt+1, sessionHookMaxAttempts)
		}
	}

	finalResult.output = combinedOutput.Bytes()
	fmt.Fprintf(os.Stderr, "discobot-agent: session hook %q failed after %d attempts\n", name, sessionHookMaxAttempts)
	return finalResult
}

func runSessionHookAttempt(hookPath, name, runAs, workspacePath, sessionID string, u *userInfo, attempt int) sessionHookAttemptResult {
	fmt.Printf("discobot-agent: running session hook %q (run_as: %s, attempt %d/%d)\n", name, runAs, attempt, sessionHookMaxAttempts)

	ctx, cancel := context.WithTimeout(context.Background(), sessionHookTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, hookPath)
	cmd.Dir = workspacePath
	cmd.Env = buildHookEnv(u, sessionID, workspacePath)

	// Run as root or discobot user
	if runAs == "user" {
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Credential: &syscall.Credential{
				Uid:    uint32(u.uid),
				Gid:    uint32(u.gid),
				Groups: u.groups,
			},
		}
	}
	// run_as: root — no credential switching needed (already running as root)

	// Capture output for status tracking while streaming to stdout/stderr
	var outputBuf bytes.Buffer
	cmd.Stdout = io.MultiWriter(&outputBuf, &prefixWriter{prefix: fmt.Sprintf("  [%s] ", name), w: os.Stdout})
	cmd.Stderr = io.MultiWriter(&outputBuf, &prefixWriter{prefix: fmt.Sprintf("  [%s] ", name), w: os.Stderr})

	startTime := time.Now()
	runErr := cmd.Run()
	duration := time.Since(startTime)

	if runErr != nil {
		exitCode := 1
		if ctx.Err() == context.DeadlineExceeded {
			exitCode = 124
			fmt.Fprintf(os.Stderr, "discobot-agent: session hook %q attempt %d/%d timed out after %s\n", name, attempt, sessionHookMaxAttempts, sessionHookTimeout)
		} else if exitErr, ok := runErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
			fmt.Fprintf(os.Stderr, "discobot-agent: session hook %q attempt %d/%d failed (%.1fs): %v\n", name, attempt, sessionHookMaxAttempts, duration.Seconds(), runErr)
		} else {
			fmt.Fprintf(os.Stderr, "discobot-agent: session hook %q attempt %d/%d failed (%.1fs): %v\n", name, attempt, sessionHookMaxAttempts, duration.Seconds(), runErr)
		}

		return sessionHookAttemptResult{
			success:  false,
			exitCode: exitCode,
			output:   outputBuf.Bytes(),
		}
	}

	if attempt == 1 {
		fmt.Printf("discobot-agent: session hook %q completed (%.1fs)\n", name, duration.Seconds())
	} else {
		fmt.Printf("discobot-agent: session hook %q completed on attempt %d/%d (%.1fs)\n", name, attempt, sessionHookMaxAttempts, duration.Seconds())
	}

	return sessionHookAttemptResult{
		success:  true,
		exitCode: 0,
		output:   outputBuf.Bytes(),
	}
}

// runSessionHooks discovers and executes session hooks from .discobot/hooks/.
// Hooks with type: session run at container startup.
// By default, hooks are non-blocking: they run in a background goroutine sequentially
// but do not block the agent from starting. Hooks with blocking: true in their front
// matter run synchronously before the agent starts.
// Failures are logged and persisted to ~/.discobot/threads/{sessionId}/hooks/status.json.
//
// Returns a wait function that blocks until all background (non-blocking) hooks
// have completed. Callers that exit shortly after (e.g. oneshot systemd services)
// must call the returned function to avoid killing in-flight hooks.
func runSessionHooks(workspacePath string, u *userInfo) func() {
	noop := func() {}

	paths, configs := discoverSessionHooks(workspacePath)
	if len(paths) == 0 {
		return noop
	}

	fmt.Printf("discobot-agent: found %d session hook(s)\n", len(paths))

	sessionID := os.Getenv("SESSION_ID")
	dataDir := hooksDataDir(u.homeDir, sessionID)

	// Ensure hooks data dir and output dir exist, owned by the discobot user
	// so the agent-api (which runs as discobot) can also write to it later.
	outputDir := filepath.Join(dataDir, "output")
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "discobot-agent: failed to create hooks data dir: %v\n", err)
	} else {
		// Chown the entire tree to discobot user
		for _, dir := range []string{
			filepath.Join(u.homeDir, ".discobot"),
			filepath.Join(u.homeDir, ".discobot", "threads"),
			filepath.Join(u.homeDir, ".discobot", "threads", sessionID),
			dataDir,
			outputDir,
		} {
			_ = os.Chown(dir, u.uid, u.gid)
		}
	}

	// Separate blocking and non-blocking hooks (preserving filename order within each group)
	type hookEntry struct {
		path   string
		config hookConfig
	}
	var blockingHooks, backgroundHooks []hookEntry
	for i, hookPath := range paths {
		entry := hookEntry{path: hookPath, config: configs[i]}
		if configs[i].Blocking {
			blockingHooks = append(blockingHooks, entry)
		} else {
			backgroundHooks = append(backgroundHooks, entry)
		}
	}

	// Phase 1: Run blocking hooks synchronously — these gate startup
	if len(blockingHooks) > 0 {
		fmt.Printf("discobot-agent: running %d blocking session hook(s)\n", len(blockingHooks))
		succeeded, failed := 0, 0
		for _, h := range blockingHooks {
			if runSessionHook(h.path, h.config, workspacePath, sessionID, dataDir, u) {
				succeeded++
			} else {
				failed++
			}
		}
		fmt.Printf("discobot-agent: blocking session hooks completed (%d succeeded, %d failed)\n", succeeded, failed)
	}

	// Phase 2: Launch non-blocking hooks in a background goroutine
	if len(backgroundHooks) == 0 {
		return noop
	}

	var wg sync.WaitGroup
	wg.Add(1)
	fmt.Printf("discobot-agent: launching %d non-blocking session hook(s) in background\n", len(backgroundHooks))
	go func() {
		defer wg.Done()
		succeeded, failed := 0, 0
		for _, h := range backgroundHooks {
			if runSessionHook(h.path, h.config, workspacePath, sessionID, dataDir, u) {
				succeeded++
			} else {
				failed++
			}
		}
		fmt.Printf("discobot-agent: background session hooks completed (%d succeeded, %d failed)\n", succeeded, failed)
	}()

	return wg.Wait
}

// buildHookEnv creates the environment for session hooks.
func buildHookEnv(u *userInfo, sessionID, workspacePath string) []string {
	env := os.Environ()
	env = append(env,
		"DISCOBOT_HOOK_TYPE=session",
		"DISCOBOT_SESSION_ID="+sessionID,
		"DISCOBOT_WORKSPACE="+workspacePath,
		"HOME="+u.homeDir,
		"USER="+u.username,
	)
	return env
}

// prefixWriter adds a prefix to each line of output for readability.
type prefixWriter struct {
	prefix string
	w      *os.File
	buf    []byte // incomplete line buffer
}

func (pw *prefixWriter) Write(p []byte) (n int, err error) {
	pw.buf = append(pw.buf, p...)

	scanner := bufio.NewScanner(strings.NewReader(string(pw.buf)))
	var remaining []byte

	for scanner.Scan() {
		line := scanner.Text()
		fmt.Fprintf(pw.w, "%s%s\n", pw.prefix, line)
	}

	// Check if the last byte is not a newline — keep it for next Write
	if len(pw.buf) > 0 && pw.buf[len(pw.buf)-1] != '\n' {
		lines := strings.Split(string(pw.buf), "\n")
		remaining = []byte(lines[len(lines)-1])
	}

	pw.buf = remaining
	return len(p), nil
}
