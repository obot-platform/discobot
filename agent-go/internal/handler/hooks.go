package handler

import (
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/obot-platform/discobot/agent-go/internal/api"
)

// HooksStatus handles GET /hooks/status — returns hook evaluation status.
func (h *Handler) HooksStatus(w http.ResponseWriter, _ *http.Request) {
	if h.hookManager == nil {
		h.JSON(w, http.StatusOK, api.HooksStatusResponse{
			Hooks:           map[string]api.HookRunStatus{},
			PendingHooks:    []string{},
			LastEvaluatedAt: "",
		})
		return
	}

	status := h.hookManager.GetStatus()
	resp := api.HooksStatusResponse{
		Hooks:           make(map[string]api.HookRunStatus, len(status.Hooks)),
		PendingHooks:    status.PendingHooks,
		LastEvaluatedAt: status.LastEvaluatedAt,
	}
	if resp.PendingHooks == nil {
		resp.PendingHooks = []string{}
	}

	for id, s := range status.Hooks {
		resp.Hooks[id] = api.HookRunStatus{
			HookID:              s.HookID,
			HookName:            s.HookName,
			Type:                s.Type,
			LastRunAt:           s.LastRunAt,
			LastResult:          s.LastResult,
			LastExitCode:        s.LastExitCode,
			OutputPath:          s.OutputPath,
			RunCount:            s.RunCount,
			FailCount:           s.FailCount,
			ConsecutiveFailures: s.ConsecutiveFailures,
		}
	}

	h.JSON(w, http.StatusOK, resp)
}

// HookOutput handles GET /hooks/{hookId}/output — returns hook output log.
func (h *Handler) HookOutput(w http.ResponseWriter, r *http.Request) {
	hookID := chi.URLParam(r, "hookId")

	if h.hookManager == nil {
		h.Error(w, http.StatusNotFound, "Hooks not enabled")
		return
	}

	output, err := h.hookManager.GetHookOutput(hookID)
	if err != nil {
		h.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.JSON(w, http.StatusOK, api.HookOutputResponse{
		Output: output,
	})
}

// RerunHook handles POST /hooks/{hookId}/rerun — manually reruns a hook.
func (h *Handler) RerunHook(w http.ResponseWriter, r *http.Request) {
	hookID := chi.URLParam(r, "hookId")

	if h.hookManager == nil {
		h.Error(w, http.StatusNotFound, "Hooks not enabled")
		return
	}

	result, err := h.hookManager.RerunHook(hookID)
	if err != nil {
		h.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if result == nil {
		h.Error(w, http.StatusNotFound, "Hook not found")
		return
	}

	if result.Eval.ShouldReprompt {
		threadID := chi.URLParam(r, "id")
		if err := h.startHookFailureReprompt(threadID, result.Eval); err != nil {
			log.Printf("hooks: failed to start re-prompt for manual rerun: %v", err)
		}
	}

	h.JSON(w, http.StatusOK, api.HookRerunResponse{
		Success:  result.Result.Success,
		ExitCode: result.Result.ExitCode,
	})
}
