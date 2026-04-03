package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// Events handles SSE event streaming for a project.
// GET /api/projects/{projectId}/events
// Query parameters:
//   - since: RFC3339 timestamp to get events after (e.g., "2024-01-15T10:30:00Z")
//   - after: Event ID to get events after (alternative to since)
//
// If neither is provided, only new events from the time of connection are streamed.
func (h *Handler) Events(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := h.withShutdownContext(r.Context())
	defer cancel()

	projectID := chi.URLParam(r, "projectId")
	if projectID == "" {
		h.Error(w, http.StatusBadRequest, "missing project ID")
		return
	}

	// Check if the client supports SSE
	flusher, ok := w.(http.Flusher)
	if !ok {
		h.Error(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	// Parse query parameters
	sinceStr := r.URL.Query().Get("since")
	afterID := r.URL.Query().Get("after")

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

	// Subscribe to events for this project BEFORE sending historical events
	// This ensures we don't miss any events between fetching history and subscribing
	sub := h.eventBroker.Subscribe(projectID)
	defer h.eventBroker.Unsubscribe(sub)

	// Send initial connection event
	_, _ = fmt.Fprintf(w, "event: connected\ndata: {\"projectId\":%q}\n\n", projectID)
	flusher.Flush()

	// Track sent event IDs to avoid duplicates between history and live events
	sentEventIDs := make(map[string]bool)

	// Send historical events if requested
	if afterID != "" {
		// Get events after a specific event ID
		events, err := h.eventBroker.GetEventsAfterID(ctx, projectID, afterID)
		if err != nil {
			_, _ = fmt.Fprintf(w, "event: error\ndata: {\"error\":\"failed to get historical events\"}\n\n")
			flusher.Flush()
		} else {
			for _, event := range events {
				data, err := json.Marshal(event)
				if err != nil {
					continue
				}
				_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, data)
				sentEventIDs[event.ID] = true
			}
			flusher.Flush()
		}
	} else if sinceStr != "" {
		// Parse timestamp and get events since that time
		since, err := time.Parse(time.RFC3339, sinceStr)
		if err != nil {
			// Try parsing as Unix timestamp
			var unixSec int64
			if _, err := fmt.Sscanf(sinceStr, "%d", &unixSec); err == nil {
				since = time.Unix(unixSec, 0)
			} else {
				_, _ = fmt.Fprintf(w, "event: error\ndata: {\"error\":\"invalid since parameter, use RFC3339 format\"}\n\n")
				flusher.Flush()
			}
		}

		if !since.IsZero() {
			events, err := h.eventBroker.GetEventsSince(ctx, projectID, since)
			if err != nil {
				_, _ = fmt.Fprintf(w, "event: error\ndata: {\"error\":\"failed to get historical events\"}\n\n")
				flusher.Flush()
			} else {
				for _, event := range events {
					data, err := json.Marshal(event)
					if err != nil {
						continue
					}
					_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, data)
					sentEventIDs[event.ID] = true
				}
				flusher.Flush()
			}
		}
	}

	// Stream new events until client disconnects
	for {
		select {
		case <-ctx.Done():
			// Client disconnected
			return
		case event, ok := <-sub.Events:
			if !ok {
				// Channel closed
				return
			}

			// Skip if we already sent this event from history
			if sentEventIDs[event.ID] {
				delete(sentEventIDs, event.ID) // Clean up to avoid memory growth
				continue
			}

			// Serialize event data
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}

			// Write SSE format: event: <type>\ndata: <json>\n\n
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, data)
			flusher.Flush()
		}
	}
}
