package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/obot-platform/discobot/agent-go/agent"
	"github.com/obot-platform/discobot/agent-go/agentimpl"
	"github.com/obot-platform/discobot/agent-go/internal/api"
	"github.com/obot-platform/discobot/agent-go/internal/services"
)

func TestHealth_ReturnsHealthyWhenRuntimeDependenciesExist(t *testing.T) {
	h := &Handler{
		completions:    &agent.CompletionManager{},
		serviceManager: &services.Manager{},
		defaultAgent:   &agentimpl.DefaultAgent{},
	}

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	h.Health(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var resp api.HealthResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode health response: %v", err)
	}
	if !resp.Healthy {
		t.Fatal("expected healthy=true")
	}
	if !resp.Connected {
		t.Fatal("expected connected=true")
	}
}

func TestHealth_ReturnsUnhealthyWhenRuntimeDependenciesMissing(t *testing.T) {
	h := &Handler{}

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	h.Health(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var resp api.HealthResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode health response: %v", err)
	}
	if resp.Healthy {
		t.Fatal("expected healthy=false")
	}
	if resp.Connected {
		t.Fatal("expected connected=false")
	}
}
