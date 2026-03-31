package handler

import (
	"net/http"

	"github.com/obot-platform/discobot/agent-go/internal/api"
	"github.com/obot-platform/discobot/agent-go/internal/gitops"
)

// GetCommits handles GET /commits — returns a serialized commit replay bundle for commits since a parent.
// Query params:
//   - parent: required, the parent commit hash
func (h *Handler) GetCommits(w http.ResponseWriter, r *http.Request) {
	parent := r.URL.Query().Get("parent")
	if parent == "" {
		h.JSON(w, http.StatusBadRequest, api.CommitsErrorResponse{
			Error:   "invalid_parent",
			Message: "parent query parameter is required",
		})
		return
	}

	result, commitsErr := gitops.GetCommitReplayBundle(h.agentCwd, parent)
	if commitsErr != nil {
		status := http.StatusInternalServerError
		switch commitsErr.Code {
		case "invalid_parent":
			status = http.StatusBadRequest
		case "not_git_repo":
			status = http.StatusBadRequest
		case "parent_mismatch":
			status = http.StatusConflict
		case "no_commits":
			status = http.StatusNotFound
		}

		h.JSON(w, status, api.CommitsErrorResponse{
			Error:      commitsErr.Code,
			Message:    commitsErr.Message,
			IsClean:    commitsErr.IsClean,
			HeadCommit: commitsErr.HeadCommit,
		})
		return
	}

	h.JSON(w, http.StatusOK, api.CommitsResponse{
		ReplayBundle: result.ReplayBundle,
		CommitCount:  result.CommitCount,
	})
}
