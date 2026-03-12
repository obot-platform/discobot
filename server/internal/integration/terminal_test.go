package integration

import (
	"net/http"
	"testing"
	"time"

	"github.com/obot-platform/discobot/server/internal/sandbox"
)

func TestGetTerminalStatus_NotCreated(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	workspace := ts.CreateTestWorkspace(project, "/home/user/code")
	session := ts.CreateTestSession(workspace, "Test Session")
	client := ts.AuthenticatedClient(user)

	resp := client.Get("/api/projects/" + project.ID + "/sessions/" + session.ID + "/terminal/status")
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusOK)

	var status map[string]interface{}
	ParseJSON(t, resp, &status)

	if status["status"] != "not_created" {
		t.Errorf("Expected status 'not_created', got '%v'", status["status"])
	}
}

func TestGetTerminalStatus_Running(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	workspace := ts.CreateTestWorkspace(project, "/home/user/code")
	session := ts.CreateTestSession(workspace, "Test Session")
	client := ts.AuthenticatedClient(user)

	// Create and start sandbox via mock
	ts.MockSandbox.Create(t.Context(), session.ID, sandbox.CreateOptions{})
	ts.MockSandbox.Start(t.Context(), session.ID)

	resp := client.Get("/api/projects/" + project.ID + "/sessions/" + session.ID + "/terminal/status")
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusOK)

	var status map[string]interface{}
	ParseJSON(t, resp, &status)

	if status["status"] != "running" {
		t.Errorf("Expected status 'running', got '%v'", status["status"])
	}
}

func TestGetTerminalHistory_Empty(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	workspace := ts.CreateTestWorkspace(project, "/home/user/code")
	session := ts.CreateTestSession(workspace, "Test Session")
	client := ts.AuthenticatedClient(user)

	resp := client.Get("/api/projects/" + project.ID + "/sessions/" + session.ID + "/terminal/history")
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusOK)

	var result map[string]interface{}
	ParseJSON(t, resp, &result)

	history, ok := result["history"].([]interface{})
	if !ok {
		t.Fatalf("Expected history to be an array")
	}

	if len(history) != 0 {
		t.Errorf("Expected 0 history entries, got %d", len(history))
	}
}

func TestGetTerminalStatus_SessionNotFound(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	client := ts.AuthenticatedClient(user)

	resp := client.Get("/api/projects/" + project.ID + "/sessions/nonexistent-session/terminal/status")
	defer resp.Body.Close()

	// Should return not_created since container doesn't exist
	// (The session check happens in the WebSocket handler, not status)
	AssertStatus(t, resp, http.StatusOK)

	var status map[string]interface{}
	ParseJSON(t, resp, &status)

	if status["status"] != "not_created" {
		t.Errorf("Expected status 'not_created', got '%v'", status["status"])
	}
}

func TestTerminalWebSocket_ServiceUnavailable(t *testing.T) {
	t.Parallel()
	// This test verifies that the WebSocket endpoint returns an error
	// when trying to connect without a valid session setup
	// Note: Full WebSocket testing would require a WebSocket client
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	workspace := ts.CreateTestWorkspace(project, "/home/user/code")
	session := ts.CreateTestSession(workspace, "Test Session")
	client := ts.AuthenticatedClient(user)

	// Regular HTTP GET to WebSocket endpoint (won't upgrade)
	resp := client.Get("/api/projects/" + project.ID + "/sessions/" + session.ID + "/terminal/ws")
	defer resp.Body.Close()

	// Should fail because it's not a proper WebSocket upgrade
	// The exact status depends on the WebSocket library behavior
	// gorilla/websocket typically returns 400 Bad Request for non-WebSocket requests
	if resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("Expected status 400 or 500 for non-WebSocket request, got %d", resp.StatusCode)
	}
}

func TestCreateSession_CreatesSandbox(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	workspace := ts.CreateTestWorkspaceWithGitRepo(project) // Local workspaces need real git repos
	agent := ts.CreateTestAgent(project, "Claude", "claude-code")
	client := ts.AuthenticatedClient(user)

	// Sessions are created implicitly via the chat endpoint
	// Format matches AI SDK's DefaultChatTransport with UIMessage format
	sessionID := "test-sandbox-session-1"
	resp := client.Post("/api/projects/"+project.ID+"/chat", map[string]interface{}{
		"id": sessionID,
		"messages": []map[string]interface{}{
			{
				"id":   "msg-1",
				"role": "user",
				"parts": []map[string]interface{}{
					{"type": "text", "text": "Hello sandbox"},
				},
			},
		},
		"workspaceId": workspace.ID,
		"agentId":     agent.ID,
	})
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusOK)

	// Get the session that was created
	listResp := client.Get("/api/projects/" + project.ID + "/sessions")
	defer listResp.Body.Close()

	var result struct {
		Sessions []map[string]interface{} `json:"sessions"`
	}
	ParseJSON(t, listResp, &result)

	if len(result.Sessions) != 1 {
		t.Errorf("Expected 1 session, got %d", len(result.Sessions))
		return
	}

	// Wait for async sandbox creation via job queue
	// The dispatcher polls every 10ms in tests, so wait a bit for the job to be processed
	time.Sleep(100 * time.Millisecond)

	// Check that a sandbox was created for this session
	sandboxes := ts.MockSandbox.GetSandboxes()
	if _, exists := sandboxes[sessionID]; !exists {
		t.Errorf("Expected sandbox to be created for session %s", sessionID)
	}
}
