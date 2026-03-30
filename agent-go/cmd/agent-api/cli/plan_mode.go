package cli

import (
	"strings"

	"github.com/obot-platform/discobot/agent-go/thread"
)

// getThreadPlanMode reads the persisted plan mode state for a thread.
func getThreadPlanMode(store *thread.Store, threadID string) bool {
	cfg, err := store.LoadConfig(threadID)
	if err != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(cfg.Mode.Value), "plan")
}

// saveThreadPlanMode persists the plan mode state for a thread, preserving other config fields.
func saveThreadPlanMode(store *thread.Store, threadID string, enabled bool) {
	cfg, _ := store.LoadConfig(threadID)
	if enabled {
		cfg.Mode.Value = "plan"
		cfg.Mode.SetBy = "user"
	} else {
		cfg.Mode.Value = "build"
		cfg.Mode.SetBy = "user"
	}
	_ = store.SaveConfig(threadID, cfg)
}

// planModeStr converts a planMode bool to the Mode string expected by PromptRequest.
func planModeStr(enabled bool) string {
	if enabled {
		return "plan"
	}
	return ""
}
