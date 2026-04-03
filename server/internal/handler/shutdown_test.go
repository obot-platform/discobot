package handler

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/obot-platform/discobot/server/internal/events"
)

func TestEvents_ShutdownStopsStream(t *testing.T) {
	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
	poller := events.NewPoller(nil, events.DefaultPollerConfig())

	h := &Handler{
		eventBroker:     events.NewBroker(nil, poller),
		shutdownCtx:     shutdownCtx,
		shutdownCancel:  shutdownCancel,
		terminalManager: nil,
	}

	req := httptest.NewRequest("GET", "/api/projects/test-project/events", nil)
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("projectId", "test-project")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		h.Events(w, req)
	}()

	select {
	case <-done:
		t.Fatal("events stream exited before shutdown")
	case <-time.After(100 * time.Millisecond):
	}

	h.BeginShutdown()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("events stream did not stop on shutdown")
	}

	if body := w.Body.String(); !strings.Contains(body, "event: connected") {
		t.Fatalf("expected initial SSE connect event, got %q", body)
	}
}
