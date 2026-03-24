package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestListSessionsByProject_Empty(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	client := ts.AuthenticatedClient(user)

	resp := client.Get("/api/projects/" + project.ID + "/sessions")
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusOK)

	var result struct {
		Sessions []interface{} `json:"sessions"`
	}
	ParseJSON(t, resp, &result)

	if len(result.Sessions) != 0 {
		t.Errorf("Expected 0 sessions, got %d", len(result.Sessions))
	}
}

func TestCreateSession_ViaChat(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	workspace := ts.CreateTestWorkspace(project, "/home/user/code")
	client := ts.AuthenticatedClient(user)

	// Sessions are created implicitly via the chat endpoint
	// Format matches AI SDK's DefaultChatTransport with UIMessage format
	sessionID := "test-session-id-1"
	resp := client.Post(threadChatPath(project.ID, sessionID, sessionID), map[string]interface{}{
		"messages": []map[string]interface{}{
			{
				"id":   "msg-1",
				"role": "user",
				"parts": []map[string]interface{}{
					{"type": "text", "text": "Create a new session"},
				},
			},
		},
		"workspaceId": workspace.ID,
	})
	defer resp.Body.Close()

	// Chat endpoint returns a normal JSON response.
	AssertStatus(t, resp, http.StatusOK)

	var chatResult map[string]interface{}
	ParseJSON(t, resp, &chatResult)
	if chatResult["sessionId"] != sessionID {
		t.Fatalf("Expected sessionId %q, got %v", sessionID, chatResult["sessionId"])
	}
	if chatResult["threadId"] != sessionID {
		t.Fatalf("Expected default threadId %q, got %v", sessionID, chatResult["threadId"])
	}
	if chatResult["workspaceId"] != workspace.ID {
		t.Fatalf("Expected workspaceId %q, got %v", workspace.ID, chatResult["workspaceId"])
	}

	// Verify session was created by listing sessions
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

	// Session name is derived from the prompt
	if result.Sessions[0]["name"] != "Create a new session" {
		t.Errorf("Expected name derived from prompt, got '%v'", result.Sessions[0]["name"])
	}
}

func TestCreateSession_ViaChatWithSessionIDPath(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	workspace := ts.CreateTestWorkspace(project, "/home/user/code")
	client := ts.AuthenticatedClient(user)

	sessionID := "test-session-id-session-field"
	resp := client.Post(threadChatPath(project.ID, sessionID, sessionID), map[string]interface{}{
		"messages": []map[string]interface{}{
			{
				"id":   "msg-1",
				"role": "user",
				"parts": []map[string]interface{}{
					{"type": "text", "text": "Create a session using sessionId"},
				},
			},
		},
		"workspaceId": workspace.ID,
	})
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusOK)

	var chatResult map[string]interface{}
	ParseJSON(t, resp, &chatResult)
	if chatResult["sessionId"] != sessionID {
		t.Fatalf("Expected sessionId %q, got %v", sessionID, chatResult["sessionId"])
	}
	if chatResult["threadId"] != sessionID {
		t.Fatalf("Expected default threadId %q, got %v", sessionID, chatResult["threadId"])
	}
	if chatResult["workspaceId"] != workspace.ID {
		t.Fatalf("Expected workspaceId %q, got %v", workspace.ID, chatResult["workspaceId"])
	}
}

func TestCreateSession_ViaLegacyChatEndpoint_NotFound(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	client := ts.AuthenticatedClient(user)

	resp := client.Post("/api/projects/"+project.ID+"/chat", map[string]interface{}{
		"messages": []map[string]interface{}{},
	})
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusNotFound)
}

func TestCreateSession_ViaEmptyChat(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	workspace := ts.CreateTestWorkspace(project, "/home/user/code")
	client := ts.AuthenticatedClient(user)

	sessionID := "test-session-id-empty-chat"
	resp := client.Post(threadChatPath(project.ID, sessionID, sessionID), map[string]interface{}{
		"messages":    []map[string]interface{}{},
		"workspaceId": workspace.ID,
	})
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusOK)

	var chatResult map[string]interface{}
	ParseJSON(t, resp, &chatResult)
	if chatResult["sessionId"] != sessionID {
		t.Fatalf("Expected sessionId %q, got %v", sessionID, chatResult["sessionId"])
	}
	if chatResult["threadId"] != sessionID {
		t.Fatalf("Expected default threadId %q, got %v", sessionID, chatResult["threadId"])
	}
	if chatResult["workspaceId"] != workspace.ID {
		t.Fatalf("Expected workspaceId %q, got %v", workspace.ID, chatResult["workspaceId"])
	}
	if chatResult["messageId"] != nil {
		t.Fatalf("Expected empty messageId for empty chat submission, got %v", chatResult["messageId"])
	}

	sessionResp := client.Get("/api/projects/" + project.ID + "/sessions/" + sessionID)
	defer sessionResp.Body.Close()
	AssertStatus(t, sessionResp, http.StatusOK)

	var sessionResult map[string]interface{}
	ParseJSON(t, sessionResp, &sessionResult)
	if sessionResult["name"] != "" {
		t.Fatalf("expected empty session name for empty chat submission, got %v", sessionResult["name"])
	}
}

func TestCreateSession_ViaCreateSessionEndpointWithoutWorkspace(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	client := ts.AuthenticatedClient(user)

	sessionID := "test-session-id-no-workspace-1"
	resp := client.Post(threadChatPath(project.ID, sessionID, sessionID), map[string]interface{}{
		"messages": []map[string]interface{}{},
	})
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusOK)

	var createResult map[string]string
	ParseJSON(t, resp, &createResult)
	if createResult["sessionId"] != sessionID {
		t.Fatalf("Expected created session id %q, got %q", sessionID, createResult["sessionId"])
	}

	sessionResp := client.Get("/api/projects/" + project.ID + "/sessions/" + sessionID)
	defer sessionResp.Body.Close()
	AssertStatus(t, sessionResp, http.StatusOK)

	var sessionResult map[string]interface{}
	ParseJSON(t, sessionResp, &sessionResult)

	workspaceID, ok := sessionResult["workspaceId"].(string)
	if !ok || workspaceID == "" {
		t.Fatalf("Expected non-empty workspaceId on created session, got %v", sessionResult["workspaceId"])
	}

	workspaceResp := client.Get("/api/projects/" + project.ID + "/workspaces/" + workspaceID)
	defer workspaceResp.Body.Close()
	AssertStatus(t, workspaceResp, http.StatusOK)

	var workspaceResult map[string]interface{}
	ParseJSON(t, workspaceResp, &workspaceResult)

	path, ok := workspaceResult["path"].(string)
	if !ok || path == "" {
		t.Fatalf("Expected non-empty workspace path, got %v", workspaceResult["path"])
	}
	if !strings.HasPrefix(path, ts.Config.WorkspaceDir) {
		t.Fatalf("Expected workspace path %q to be under %q", path, ts.Config.WorkspaceDir)
	}
	if workspaceResult["sourceType"] != "local" {
		t.Fatalf("Expected sourceType local, got %v", workspaceResult["sourceType"])
	}
	if workspaceResult["autoGenerated"] != true {
		t.Fatalf("Expected autoGenerated true, got %v", workspaceResult["autoGenerated"])
	}
}

func TestCreateSession_ViaChatWithoutWorkspace(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	client := ts.AuthenticatedClient(user)

	sessionID := "test-session-id-no-workspace-2"
	resp := client.Post(threadChatPath(project.ID, sessionID, sessionID), map[string]interface{}{
		"messages": []map[string]interface{}{
			{
				"id":   "msg-1",
				"role": "user",
				"parts": []map[string]interface{}{
					{"type": "text", "text": "Create a session without selecting a workspace"},
				},
			},
		},
	})
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusOK)

	sessionResp := client.Get("/api/projects/" + project.ID + "/sessions/" + sessionID)
	defer sessionResp.Body.Close()
	AssertStatus(t, sessionResp, http.StatusOK)

	var sessionResult map[string]interface{}
	ParseJSON(t, sessionResp, &sessionResult)

	workspaceID, ok := sessionResult["workspaceId"].(string)
	if !ok || workspaceID == "" {
		t.Fatalf("Expected non-empty workspaceId on chat-created session, got %v", sessionResult["workspaceId"])
	}

	workspaceResp := client.Get("/api/projects/" + project.ID + "/workspaces/" + workspaceID)
	defer workspaceResp.Body.Close()
	AssertStatus(t, workspaceResp, http.StatusOK)

	var workspaceResult map[string]interface{}
	ParseJSON(t, workspaceResp, &workspaceResult)

	path, ok := workspaceResult["path"].(string)
	if !ok || path == "" {
		t.Fatalf("Expected non-empty workspace path, got %v", workspaceResult["path"])
	}
	if !strings.HasPrefix(path, ts.Config.WorkspaceDir) {
		t.Fatalf("Expected workspace path %q to be under %q", path, ts.Config.WorkspaceDir)
	}
	if workspaceResult["sourceType"] != "local" {
		t.Fatalf("Expected sourceType local, got %v", workspaceResult["sourceType"])
	}
	if workspaceResult["autoGenerated"] != true {
		t.Fatalf("Expected autoGenerated true, got %v", workspaceResult["autoGenerated"])
	}
}

func TestCreateSession_ViaChatWithWorkspace(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	workspace := ts.CreateTestWorkspace(project, "/home/user/code")
	client := ts.AuthenticatedClient(user)

	// Sessions are created implicitly via the chat endpoint with workspace
	// Format matches AI SDK's DefaultChatTransport with UIMessage format
	sessionID := "test-session-id-2"
	resp := client.Post(threadChatPath(project.ID, sessionID, sessionID), map[string]interface{}{
		"messages": []map[string]interface{}{
			{
				"id":   "msg-1",
				"role": "user",
				"parts": []map[string]interface{}{
					{"type": "text", "text": "Hello agent"},
				},
			},
		},
		"workspaceId": workspace.ID,
	})
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusOK)

	// Verify session was created by listing sessions
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
}

func TestCreateSession_NameFromLongPrompt(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	workspace := ts.CreateTestWorkspace(project, "/home/user/code")
	client := ts.AuthenticatedClient(user)

	// Session name is derived from the full prompt text (no truncation)
	longPrompt := "This is a very long prompt that should be truncated to fit within the 50 character limit for session names"
	sessionID := "test-session-id-3"
	resp := client.Post(threadChatPath(project.ID, sessionID, sessionID), map[string]interface{}{
		"messages": []map[string]interface{}{
			{
				"id":   "msg-1",
				"role": "user",
				"parts": []map[string]interface{}{
					{"type": "text", "text": longPrompt},
				},
			},
		},
		"workspaceId": workspace.ID,
	})
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusOK)

	// Verify session name matches the full prompt
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

	name := result.Sessions[0]["name"].(string)
	if name != longPrompt {
		t.Errorf("Expected name to match prompt, got %q", name)
	}
}

func TestGetSession(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	workspace := ts.CreateTestWorkspace(project, "/home/user/code")
	session := ts.CreateTestSession(workspace, "Test Session")
	client := ts.AuthenticatedClient(user)

	resp := client.Get("/api/projects/" + project.ID + "/sessions/" + session.ID)
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusOK)

	var result map[string]interface{}
	ParseJSON(t, resp, &result)

	if result["id"] != session.ID {
		t.Errorf("Expected id '%s', got '%v'", session.ID, result["id"])
	}
	if result["name"] != "Test Session" {
		t.Errorf("Expected name 'Test Session', got '%v'", result["name"])
	}
}

func TestUpdateSession(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	workspace := ts.CreateTestWorkspace(project, "/home/user/code")
	session := ts.CreateTestSession(workspace, "Test Session")
	client := ts.AuthenticatedClient(user)

	resp := client.Put("/api/projects/"+project.ID+"/sessions/"+session.ID, map[string]string{
		"name":   "Updated Session",
		"status": "stopped",
	})
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusOK)

	var result map[string]interface{}
	ParseJSON(t, resp, &result)

	if result["name"] != "Updated Session" {
		t.Errorf("Expected name 'Updated Session', got '%v'", result["name"])
	}
	if result["status"] != "stopped" {
		t.Errorf("Expected status 'stopped', got '%v'", result["status"])
	}
}

func TestDeleteSession(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	workspace := ts.CreateTestWorkspace(project, "/home/user/code")
	session := ts.CreateTestSession(workspace, "Test Session")
	client := ts.AuthenticatedClient(user)

	resp := client.Delete("/api/projects/" + project.ID + "/sessions/" + session.ID)
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusOK)

	// Verify session status is "removing" (async deletion)
	resp = client.Get("/api/projects/" + project.ID + "/sessions/" + session.ID)
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusOK)
	var result map[string]interface{}
	ParseJSON(t, resp, &result)
	if result["status"] != "removing" {
		t.Errorf("Expected status 'removing', got '%v'", result["status"])
	}
}

func TestListSessionsByProject_WithData(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	workspace := ts.CreateTestWorkspace(project, "/home/user/code")
	ts.CreateTestSession(workspace, "Session 1")
	ts.CreateTestSession(workspace, "Session 2")
	ts.CreateTestSession(workspace, "Session 3")
	client := ts.AuthenticatedClient(user)

	resp := client.Get("/api/projects/" + project.ID + "/sessions")
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusOK)

	var result struct {
		Sessions []struct {
			ID        string `json:"id"`
			CreatedAt string `json:"createdAt"`
			Timestamp string `json:"timestamp"`
		} `json:"sessions"`
	}
	ParseJSON(t, resp, &result)

	if len(result.Sessions) != 3 {
		t.Errorf("Expected 3 sessions, got %d", len(result.Sessions))
	}

	for _, session := range result.Sessions {
		if session.CreatedAt == "" {
			t.Fatalf("Expected session %s to include createdAt", session.ID)
		}
		if _, err := time.Parse(time.RFC3339, session.CreatedAt); err != nil {
			t.Fatalf("Expected session %s createdAt to be RFC3339, got %q: %v", session.ID, session.CreatedAt, err)
		}
		if session.Timestamp == "" {
			t.Fatalf("Expected session %s to include timestamp", session.ID)
		}
	}
}

func TestListSessionFiles(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	workspace := ts.CreateTestWorkspace(project, "/home/user/code")
	client := ts.AuthenticatedClient(user)

	// Create a session with sandbox (uses mock provider's default handler which supports /files)
	session := ts.CreateTestSessionWithSandbox(workspace, "Test Session")

	resp := client.Get("/api/projects/" + project.ID + "/sessions/" + session.ID + "/files?path=.")
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusOK)

	var result struct {
		Path    string `json:"path"`
		Entries []struct {
			Name string `json:"name"`
			Type string `json:"type"`
			Size int64  `json:"size,omitempty"`
		} `json:"entries"`
	}
	ParseJSON(t, resp, &result)

	// Mock returns README.md and src directory
	if len(result.Entries) != 2 {
		t.Errorf("Expected 2 entries, got %d", len(result.Entries))
	}
	if result.Path != "." {
		t.Errorf("Expected path '.', got %s", result.Path)
	}
}

func TestListMessages(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	workspace := ts.CreateTestWorkspace(project, "/home/user/code")

	// Set up mock sandbox HTTP server that responds to /chat
	mockSandboxServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/chat") && r.Method == "GET" && r.Header.Get("Accept") != "text/event-stream" {
			// Return empty messages array
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"messages":[]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer mockSandboxServer.Close()

	// Create session with sandbox using mock server
	session := ts.CreateTestSessionWithMockSandbox(workspace, "Test Session", mockSandboxServer.URL)
	client := ts.AuthenticatedClient(user)

	// Get messages from sandbox - returns empty since no messages have been sent
	resp := client.Get("/api/projects/" + project.ID + "/sessions/" + session.ID + "/messages")
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusOK)

	var result struct {
		Messages []interface{} `json:"messages"`
	}
	ParseJSON(t, resp, &result)

	if len(result.Messages) != 0 {
		t.Errorf("Expected 0 messages, got %d", len(result.Messages))
	}
}

// ============================================================================
// Session Commit Tests
// ============================================================================

func TestCommitSession_NoWorkspaceCommit(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	workspace := ts.CreateTestWorkspace(project, "/home/user/code")

	// Create a session with a running sandbox
	session := ts.CreateTestSessionWithSandbox(workspace, "Test Session")
	client := ts.AuthenticatedClient(user)

	// First verify the session exists and can be fetched
	getResp := client.Get("/api/projects/" + project.ID + "/sessions/" + session.ID)
	AssertStatus(t, getResp, http.StatusOK)
	getResp.Body.Close()

	// Initiate commit - the request is accepted synchronously and any workspace/git
	// failure will happen during async job processing.
	resp := client.Post("/api/projects/"+project.ID+"/sessions/"+session.ID+"/commit", nil)
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusOK)
}

func TestCommitSession_NotFound(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	client := ts.AuthenticatedClient(user)

	// Try to commit a non-existent session
	resp := client.Post("/api/projects/"+project.ID+"/sessions/nonexistent-session/commit", nil)
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusNotFound)
}

func TestCommitSession_AlreadyInProgress(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	workspace := ts.CreateTestWorkspace(project, "/home/user/code")
	session := ts.CreateTestSessionWithSandbox(workspace, "Test Session")
	client := ts.AuthenticatedClient(user)

	// Manually set commit status to pending to simulate in-progress commit
	session.CommitStatus = "pending"
	if err := ts.Store.UpdateSession(context.Background(), session); err != nil {
		t.Fatalf("Failed to update session: %v", err)
	}

	// Try to commit again
	resp := client.Post("/api/projects/"+project.ID+"/sessions/"+session.ID+"/commit", nil)
	defer resp.Body.Close()

	// Should return conflict
	AssertStatus(t, resp, http.StatusConflict)
}

func TestRebaseSession_NotFound(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	client := ts.AuthenticatedClient(user)

	resp := client.Post("/api/projects/"+project.ID+"/sessions/nonexistent-session/rebase", nil)
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusNotFound)
}

func TestRebaseSession_AlreadyInProgress(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	workspace := ts.CreateTestWorkspace(project, "/home/user/code")
	session := ts.CreateTestSessionWithSandbox(workspace, "Test Session")
	client := ts.AuthenticatedClient(user)

	session.CommitStatus = "pending"
	operation := "commit"
	session.CommitOperation = &operation
	if err := ts.Store.UpdateSession(context.Background(), session); err != nil {
		t.Fatalf("Failed to update session: %v", err)
	}

	resp := client.Post("/api/projects/"+project.ID+"/sessions/"+session.ID+"/rebase", nil)
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusConflict)
}

func TestGetSession_IncludesCommitStatus(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	workspace := ts.CreateTestWorkspace(project, "/home/user/code")
	session := ts.CreateTestSessionWithSandbox(workspace, "Test Session")
	client := ts.AuthenticatedClient(user)

	// Set commit status to test it's included in response
	session.CommitStatus = "committing"
	operation := "rebase"
	session.CommitOperation = &operation
	baseCommit := "abc123"
	session.BaseCommit = &baseCommit
	if err := ts.Store.UpdateSession(context.Background(), session); err != nil {
		t.Fatalf("Failed to update session: %v", err)
	}

	// Get session and verify commit fields are included
	resp := client.Get("/api/projects/" + project.ID + "/sessions/" + session.ID)
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusOK)

	var result map[string]interface{}
	ParseJSON(t, resp, &result)

	if result["commitStatus"] != "committing" {
		t.Errorf("Expected commitStatus 'committing', got %v", result["commitStatus"])
	}
	if result["commitOperation"] != "rebase" {
		t.Errorf("Expected commitOperation 'rebase', got %v", result["commitOperation"])
	}
	if result["baseCommit"] != "abc123" {
		t.Errorf("Expected baseCommit 'abc123', got %v", result["baseCommit"])
	}
}

func TestGetSession_MapsFailedCommitIntoStatusAndError(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	workspace := ts.CreateTestWorkspace(project, "/home/user/code")
	session := ts.CreateTestSessionWithSandbox(workspace, "Test Session")
	client := ts.AuthenticatedClient(user)

	// Set commit status to failed with error.
	session.CommitStatus = "failed"
	commitError := "Patch conflict on file.txt"
	session.CommitError = &commitError
	if err := ts.Store.UpdateSession(context.Background(), session); err != nil {
		t.Fatalf("Failed to update session: %v", err)
	}

	resp := client.Get("/api/projects/" + project.ID + "/sessions/" + session.ID)
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusOK)

	var result map[string]interface{}
	ParseJSON(t, resp, &result)

	if result["status"] != "error" {
		t.Errorf("Expected status 'error', got %v", result["status"])
	}
	if result["errorMessage"] != "Patch conflict on file.txt" {
		t.Errorf("Expected errorMessage 'Patch conflict on file.txt', got %v", result["errorMessage"])
	}
	if _, ok := result["commitStatus"]; ok {
		t.Errorf("Expected commitStatus to be omitted, got %v", result["commitStatus"])
	}
	if _, ok := result["commitError"]; ok {
		t.Errorf("Expected commitError to be omitted, got %v", result["commitError"])
	}
}

func TestListSessions_MapsCommitStatusIntoStatus(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	workspace := ts.CreateTestWorkspace(project, "/home/user/code")
	session := ts.CreateTestSessionWithSandbox(workspace, "Test Session")
	client := ts.AuthenticatedClient(user)

	// Set commit status.
	session.CommitStatus = "completed"
	appliedCommit := "def456"
	session.AppliedCommit = &appliedCommit
	if err := ts.Store.UpdateSession(context.Background(), session); err != nil {
		t.Fatalf("Failed to update session: %v", err)
	}

	resp := client.Get("/api/projects/" + project.ID + "/sessions")
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusOK)

	var result struct {
		Sessions []map[string]interface{} `json:"sessions"`
	}
	ParseJSON(t, resp, &result)

	if len(result.Sessions) != 1 {
		t.Fatalf("Expected 1 session, got %d", len(result.Sessions))
	}

	if result.Sessions[0]["status"] != "completed" {
		t.Errorf("Expected status 'completed', got %v", result.Sessions[0]["status"])
	}
	if result.Sessions[0]["appliedCommit"] != "def456" {
		t.Errorf("Expected appliedCommit 'def456', got %v", result.Sessions[0]["appliedCommit"])
	}
	if _, ok := result.Sessions[0]["commitStatus"]; ok {
		t.Errorf("Expected commitStatus to be omitted, got %v", result.Sessions[0]["commitStatus"])
	}
	if _, ok := result.Sessions[0]["commitError"]; ok {
		t.Errorf("Expected commitError to be omitted, got %v", result.Sessions[0]["commitError"])
	}
}

func TestCommitSession_SendsCommitMessageToAgent(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	client := ts.AuthenticatedClient(user)

	// Create a real git repo to get a valid base commit
	repoPath := createTestGitRepo(t)
	workspace := ts.CreateTestWorkspace(project, repoPath)

	// Get the current commit SHA via the API (this also ensures workspace is indexed)
	statusResp := client.Get("/api/projects/" + project.ID + "/workspaces/" + workspace.ID + "/git/status")
	AssertStatus(t, statusResp, http.StatusOK)

	var gitStatus struct {
		Commit string `json:"commit"`
	}
	ParseJSON(t, statusResp, &gitStatus)
	statusResp.Body.Close()
	baseCommit := gitStatus.Commit

	// Track messages sent to the agent
	var capturedMessages []map[string]interface{}
	var messagesMu sync.Mutex

	// Set up a custom HTTP handler to capture messages sent to /chat
	ts.MockSandbox.HTTPHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/chat") && r.Method == "POST" {
			// Capture the request body
			body, _ := io.ReadAll(r.Body)
			r.Body.Close()

			var req map[string]interface{}
			if err := json.Unmarshal(body, &req); err == nil {
				messagesMu.Lock()
				capturedMessages = append(capturedMessages, req)
				messagesMu.Unlock()
			}

			// Return 202 Accepted
			w.WriteHeader(http.StatusAccepted)
			return
		}

		if strings.HasSuffix(r.URL.Path, "/chat") && r.Method == "GET" {
			// Return SSE stream with DONE
			if r.Header.Get("Accept") == "text/event-stream" {
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("data: [DONE]\n\n"))
				return
			}
			// Return empty messages
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"messages":[]}`))
			return
		}

		if r.URL.Path == "/commits" && r.Method == "GET" {
			// Return mock commits response (no commits - will fail but we want to test the message was sent)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":"no_commits","message":"No commits found"}`))
			return
		}

		http.NotFound(w, r)
	})

	// Create session with sandbox
	session := ts.CreateTestSessionWithSandbox(workspace, "Test Session")

	// The session should be in ready state, call commit API to trigger the full flow
	// This will set baseCommit, status to pending, and enqueue the job
	resp := client.Post("/api/projects/"+project.ID+"/sessions/"+session.ID+"/commit", nil)
	resp.Body.Close()

	// Give the job time to be picked up and start processing
	// (The commit API should return 202 Accepted)

	// Wait for the job to process (with timeout)
	timeout := time.After(5 * time.Second)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	var foundCommitMessage bool
waitLoop:
	for {
		select {
		case <-timeout:
			break waitLoop
		case <-ticker.C:
			messagesMu.Lock()
			for _, msg := range capturedMessages {
				if messages, ok := msg["messages"].([]interface{}); ok {
					for _, m := range messages {
						if msgMap, ok := m.(map[string]interface{}); ok {
							if parts, ok := msgMap["parts"].([]interface{}); ok {
								for _, p := range parts {
									if partMap, ok := p.(map[string]interface{}); ok {
										if text, ok := partMap["text"].(string); ok {
											expectedMsg := "/discobot-commit " + baseCommit
											if text == expectedMsg {
												foundCommitMessage = true
												break waitLoop
											}
										}
									}
								}
							}
						}
					}
				}
			}
			messagesMu.Unlock()
		}
	}

	if !foundCommitMessage {
		messagesMu.Lock()
		t.Errorf("Expected /discobot-commit %s message to be sent to agent, captured messages: %+v", baseCommit, capturedMessages)
		messagesMu.Unlock()
	}
}
