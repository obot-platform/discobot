package cli

import "github.com/obot-platform/discobot/agent-go/thread"

// getThreadPlanMode reads the persisted plan mode state for a thread.
func getThreadPlanMode(store *thread.Store, threadID string) bool {
	cfg, err := store.LoadConfig(threadID)
	if err != nil {
		return false
	}
	return cfg.PlanMode
}

// saveThreadPlanMode persists the plan mode state for a thread, preserving other config fields.
func saveThreadPlanMode(store *thread.Store, threadID string, enabled bool) {
	cfg, _ := store.LoadConfig(threadID)
	cfg.PlanMode = enabled
	_ = store.SaveConfig(threadID, cfg)
}

// planModeStr converts a planMode bool to the Mode string expected by PromptRequest.
func planModeStr(enabled bool) string {
	if enabled {
		return "plan"
	}
	return ""
}
