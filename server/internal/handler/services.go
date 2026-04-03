package handler

import (
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/obot-platform/discobot/server/internal/middleware"
)

// ============================================================================
// Service Endpoints
// ============================================================================

// ListServices lists all services in the session's sandbox.
// GET /api/projects/{projectId}/sessions/{sessionId}/services
func (h *Handler) ListServices(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	projectID := middleware.GetProjectID(ctx)
	sessionID := chi.URLParam(r, "sessionId")

	if sessionID == "" {
		h.Error(w, http.StatusBadRequest, "sessionId is required")
		return
	}

	result, err := h.chatService.ListServices(ctx, projectID, sessionID)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		h.Error(w, status, err.Error())
		return
	}

	h.JSON(w, http.StatusOK, result)
}

// StartService starts a service in the session's sandbox.
// POST /api/projects/{projectId}/sessions/{sessionId}/services/{serviceId}/start
func (h *Handler) StartService(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	projectID := middleware.GetProjectID(ctx)
	sessionID := chi.URLParam(r, "sessionId")
	serviceID := chi.URLParam(r, "serviceId")

	if sessionID == "" {
		h.Error(w, http.StatusBadRequest, "sessionId is required")
		return
	}
	if serviceID == "" {
		h.Error(w, http.StatusBadRequest, "serviceId is required")
		return
	}

	result, err := h.chatService.StartService(ctx, projectID, sessionID, serviceID)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "service_not_found") {
			status = http.StatusNotFound
		} else if strings.Contains(err.Error(), "already_running") {
			status = http.StatusConflict
		}
		h.Error(w, status, err.Error())
		return
	}

	h.JSON(w, http.StatusAccepted, result)
}

// StopService stops a service in the session's sandbox.
// POST /api/projects/{projectId}/sessions/{sessionId}/services/{serviceId}/stop
func (h *Handler) StopService(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	projectID := middleware.GetProjectID(ctx)
	sessionID := chi.URLParam(r, "sessionId")
	serviceID := chi.URLParam(r, "serviceId")

	if sessionID == "" {
		h.Error(w, http.StatusBadRequest, "sessionId is required")
		return
	}
	if serviceID == "" {
		h.Error(w, http.StatusBadRequest, "serviceId is required")
		return
	}

	result, err := h.chatService.StopService(ctx, projectID, sessionID, serviceID)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "service_not_found") {
			status = http.StatusNotFound
		} else if strings.Contains(err.Error(), "not_running") {
			status = http.StatusBadRequest
		}
		h.Error(w, status, err.Error())
		return
	}

	h.JSON(w, http.StatusOK, result)
}

// GetServiceOutput streams the output of a service via SSE.
// GET /api/projects/{projectId}/sessions/{sessionId}/services/{serviceId}/output
func (h *Handler) GetServiceOutput(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := h.withShutdownContext(r.Context())
	defer cancel()

	projectID := middleware.GetProjectID(ctx)
	sessionID := chi.URLParam(r, "sessionId")
	serviceID := chi.URLParam(r, "serviceId")

	if sessionID == "" {
		h.Error(w, http.StatusBadRequest, "sessionId is required")
		return
	}
	if serviceID == "" {
		h.Error(w, http.StatusBadRequest, "serviceId is required")
		return
	}

	// Set up SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

	// Get the stream from sandbox
	sseCh, err := h.chatService.GetServiceOutput(ctx, projectID, sessionID, serviceID)
	if err != nil {
		writeServiceSSEError(w, err.Error())
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		h.Error(w, http.StatusInternalServerError, "Streaming not supported")
		return
	}

	// Pass through raw SSE lines from sandbox
	for {
		select {
		case <-ctx.Done():
			// Client disconnected
			log.Printf("[ServiceOutput] Client disconnected, stopping SSE stream")
			return
		case line, ok := <-sseCh:
			if !ok {
				// Channel closed without explicit DONE
				_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
				flusher.Flush()
				return
			}
			if line.Done {
				log.Printf("[ServiceOutput] Received [DONE] signal from sandbox")
				_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
				flusher.Flush()
				return
			}
			// Pass through raw data line without parsing
			_, _ = fmt.Fprintf(w, "data: %s\n\n", line.Data)
			flusher.Flush()
		}
	}
}

// writeServiceSSEError sends an error SSE event followed by the [DONE] signal.
func writeServiceSSEError(w http.ResponseWriter, errorText string) {
	_, _ = fmt.Fprintf(w, "data: {\"type\":\"error\",\"error\":\"%s\"}\n\n", errorText)
	_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}
