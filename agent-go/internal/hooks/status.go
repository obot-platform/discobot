package hooks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// HookRunStatus is the status of a single hook's runs.
type HookRunStatus struct {
	HookID              string `json:"hookId"`
	HookName            string `json:"hookName"`
	Type                string `json:"type"`
	LastRunAt           string `json:"lastRunAt"`
	LastResult          string `json:"lastResult"` // "success", "failure", "running", or "pending"
	LastExitCode        int    `json:"lastExitCode"`
	OutputPath          string `json:"outputPath"`
	RunCount            int    `json:"runCount"`
	FailCount           int    `json:"failCount"`
	ConsecutiveFailures int    `json:"consecutiveFailures"`
}

// StatusFile is the JSON structure persisted to status.json.
type StatusFile struct {
	Hooks           map[string]HookRunStatus `json:"hooks"`
	PendingHooks    []string                 `json:"pendingHooks"`
	LastEvaluatedAt string                   `json:"lastEvaluatedAt"`
}

// GetHooksDataDir returns the hooks data directory for a session.
func GetHooksDataDir(sessionID string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".discobot", "threads", sessionID, "hooks")
}

func statusFilePath(hooksDataDir string) string {
	return filepath.Join(hooksDataDir, "status.json")
}

// LoadStatus reads the status file. Returns an empty status on error.
func LoadStatus(hooksDataDir string) StatusFile {
	data, err := os.ReadFile(statusFilePath(hooksDataDir))
	if err != nil {
		return StatusFile{
			Hooks:        make(map[string]HookRunStatus),
			PendingHooks: []string{},
		}
	}
	var status StatusFile
	if err := json.Unmarshal(data, &status); err != nil {
		return StatusFile{
			Hooks:        make(map[string]HookRunStatus),
			PendingHooks: []string{},
		}
	}
	if status.Hooks == nil {
		status.Hooks = make(map[string]HookRunStatus)
	}
	if status.PendingHooks == nil {
		status.PendingHooks = []string{}
	}
	return status
}

// SaveStatus atomically writes the status file.
func SaveStatus(hooksDataDir string, status StatusFile) error {
	_ = os.MkdirAll(hooksDataDir, 0o755)

	data, err := json.MarshalIndent(status, "", "\t")
	if err != nil {
		return err
	}

	tmpPath := fmt.Sprintf("%s.tmp.%d", statusFilePath(hooksDataDir), time.Now().UnixNano())
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, statusFilePath(hooksDataDir))
}

// SetHookRunning marks a hook as currently running in the status file.
func SetHookRunning(hooksDataDir string, hook Hook) error {
	status := LoadStatus(hooksDataDir)

	existing, ok := status.Hooks[hook.ID]
	if !ok {
		existing = HookRunStatus{
			HookID:   hook.ID,
			HookName: hook.Name,
			Type:     string(hook.Type),
		}
	}

	existing.LastRunAt = time.Now().UTC().Format(time.RFC3339)
	existing.LastResult = "running"
	existing.OutputPath = GetHookOutputPath(hooksDataDir, hook.ID)

	status.Hooks[hook.ID] = existing
	return SaveStatus(hooksDataDir, status)
}

// RecoverInterruptedHooks resets stale running hooks after an agent-go restart.
// Hooks that can be automatically rerun (file hooks) are also re-added to pendingHooks.
func RecoverInterruptedHooks(hooksDataDir string, rerunnableHookIDs []string) error {
	status := LoadStatus(hooksDataDir)

	rerunnable := make(map[string]struct{}, len(rerunnableHookIDs))
	for _, hookID := range rerunnableHookIDs {
		rerunnable[hookID] = struct{}{}
	}

	pending := make(map[string]struct{}, len(status.PendingHooks))
	for _, hookID := range status.PendingHooks {
		pending[hookID] = struct{}{}
	}

	changed := false
	for hookID, hookStatus := range status.Hooks {
		if hookStatus.LastResult != "running" {
			continue
		}

		hookStatus.LastResult = "pending"
		status.Hooks[hookID] = hookStatus
		changed = true

		if _, ok := rerunnable[hookID]; ok {
			if _, ok := pending[hookID]; !ok {
				status.PendingHooks = append(status.PendingHooks, hookID)
				pending[hookID] = struct{}{}
			}
		}
	}

	if !changed {
		return nil
	}

	return SaveStatus(hooksDataDir, status)
}

// UpdateHookStatus updates the status after a hook execution.
func UpdateHookStatus(hooksDataDir string, result HookResult, outputPath string) error {
	status := LoadStatus(hooksDataDir)

	existing, ok := status.Hooks[result.Hook.ID]
	if !ok {
		existing = HookRunStatus{
			HookID:   result.Hook.ID,
			HookName: result.Hook.Name,
			Type:     string(result.Hook.Type),
		}
	}

	existing.RunCount++
	existing.LastExitCode = result.ExitCode
	existing.OutputPath = outputPath

	if result.Success {
		existing.LastResult = "success"
		existing.ConsecutiveFailures = 0
	} else {
		existing.LastResult = "failure"
		existing.FailCount++
		existing.ConsecutiveFailures++
	}

	status.Hooks[result.Hook.ID] = existing
	return SaveStatus(hooksDataDir, status)
}

// UpdateLastEvaluatedAt sets the lastEvaluatedAt timestamp.
func UpdateLastEvaluatedAt(hooksDataDir string) error {
	status := LoadStatus(hooksDataDir)
	status.LastEvaluatedAt = time.Now().UTC().Format(time.RFC3339)
	return SaveStatus(hooksDataDir, status)
}

// AddPendingHooks adds hook IDs to the pending set (deduped).
func AddPendingHooks(hooksDataDir string, hookIDs []string) error {
	if len(hookIDs) == 0 {
		return nil
	}
	status := LoadStatus(hooksDataDir)

	existing := make(map[string]bool, len(status.PendingHooks))
	for _, id := range status.PendingHooks {
		existing[id] = true
	}
	for _, id := range hookIDs {
		if !existing[id] {
			status.PendingHooks = append(status.PendingHooks, id)
			existing[id] = true
		}
	}

	return SaveStatus(hooksDataDir, status)
}

// RemovePendingHook removes a hook ID from the pending set.
func RemovePendingHook(hooksDataDir, hookID string) error {
	status := LoadStatus(hooksDataDir)

	filtered := make([]string, 0, len(status.PendingHooks))
	for _, id := range status.PendingHooks {
		if id != hookID {
			filtered = append(filtered, id)
		}
	}
	status.PendingHooks = filtered

	return SaveStatus(hooksDataDir, status)
}

// GetPendingHookIDs returns the list of pending hook IDs.
func GetPendingHookIDs(hooksDataDir string) []string {
	status := LoadStatus(hooksDataDir)
	return status.PendingHooks
}
