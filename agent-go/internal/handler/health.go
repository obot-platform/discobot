package handler

import (
	"net/http"
	"os/user"
	"strconv"

	"github.com/obot-platform/discobot/agent-go/internal/api"
)

// Root handles GET / — service status check.
func (h *Handler) Root(w http.ResponseWriter, _ *http.Request) {
	h.JSON(w, http.StatusOK, api.RootResponse{
		Status:  "ok",
		Service: "agent",
	})
}

// Health handles GET /health — detailed health status.
func (h *Handler) Health(w http.ResponseWriter, _ *http.Request) {
	healthy := h.completions != nil && h.serviceManager != nil
	connected := healthy && h.defaultAgent != nil

	h.JSON(w, http.StatusOK, api.HealthResponse{
		Healthy:   healthy,
		Connected: connected,
	})
}

// User handles GET /user — current user info for terminal sessions.
func (h *Handler) User(w http.ResponseWriter, _ *http.Request) {
	u, err := user.Current()
	if err != nil {
		h.Error(w, http.StatusInternalServerError, "failed to get current user")
		return
	}

	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)

	h.JSON(w, http.StatusOK, api.UserResponse{
		Username: u.Username,
		UID:      uid,
		GID:      gid,
	})
}
