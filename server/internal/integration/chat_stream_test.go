package integration

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type chatSSEFrame struct {
	Event string
	Data  string
}

func readChatSSEFrames(body io.Reader) ([]chatSSEFrame, error) {
	scanner := bufio.NewScanner(body)
	frames := []chatSSEFrame{}
	current := chatSSEFrame{}
	hasCurrent := false

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if hasCurrent {
				frames = append(frames, current)
				current = chatSSEFrame{}
				hasCurrent = false
			}
			continue
		}

		hasCurrent = true
		switch {
		case strings.HasPrefix(line, "event: "):
			current.Event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			data := strings.TrimPrefix(line, "data: ")
			if current.Data == "" {
				current.Data = data
			} else {
				current.Data += "\n" + data
			}
		}
	}

	if hasCurrent {
		frames = append(frames, current)
	}

	if err := scanner.Err(); err != nil && err != io.EOF {
		return nil, err
	}

	return frames, nil
}

func threadChatStreamPath(projectID, sessionID, threadID string) string {
	return "/api/projects/" + projectID + "/sessions/" + sessionID + "/threads/" + threadID + "/stream"
}

func TestChatStream_SessionNotFound(t *testing.T) {
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	client := ts.AuthenticatedClient(user)

	// Request stream for non-existent session - the nested session route rejects it before the stream handler runs.
	resp := client.Get(threadChatStreamPath(project.ID, "nonexistent-session", "nonexistent-thread"))
	defer resp.Body.Close()

	// Missing session = 404 Not Found from session route validation
	AssertStatus(t, resp, http.StatusNotFound)
}

func TestChatStream_SessionBelongsToOtherProject(t *testing.T) {
	ts := NewTestServer(t)

	// Create two users with their own projects
	user1 := ts.CreateTestUser("user1@example.com")
	project1 := ts.CreateTestProject(user1, "Project 1")
	workspace1 := ts.CreateTestWorkspace(project1, "/home/user1/code")
	session1 := ts.CreateTestSession(workspace1, "Session 1")

	user2 := ts.CreateTestUser("user2@example.com")
	project2 := ts.CreateTestProject(user2, "Project 2")

	// User2 tries to access user1's session via their own project
	client2 := ts.AuthenticatedClient(user2)
	resp := client2.Get(threadChatStreamPath(project2.ID, session1.ID, session1.ID))
	defer resp.Body.Close()

	// Should return 403 Forbidden
	AssertStatus(t, resp, http.StatusForbidden)
}

func TestChatStream_ValidSession_NoActiveStream(t *testing.T) {
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	workspace := ts.CreateTestWorkspaceWithGitRepo(project)
	session := ts.CreateTestSessionWithSandbox(workspace, "Test Session")
	client := ts.AuthenticatedClient(user)

	// Request stream for a valid session with no active completion.
	// The stream endpoint should still be available and return an SSE response.
	resp := client.Get(threadChatStreamPath(project.ID, session.ID, session.ID))
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusOK)
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("expected Content-Type text/event-stream, got %s", ct)
	}
	if stream := resp.Header.Get("x-vercel-ai-ui-message-stream"); stream != "v1" {
		t.Fatalf("expected x-vercel-ai-ui-message-stream v1, got %s", stream)
	}

	frames, err := readChatSSEFrames(resp.Body)
	if err != nil {
		t.Fatalf("Error reading SSE stream: %v", err)
	}
	if len(frames) != 0 {
		t.Fatalf("expected idle stream to close without forwarding events, got %d frames", len(frames))
	}
}

func TestChatStream_RequiresAuthentication(t *testing.T) {
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	workspace := ts.CreateTestWorkspace(project, "/home/user/code")
	session := ts.CreateTestSession(workspace, "Test Session")

	// Make unauthenticated request
	resp, err := http.Get(ts.Server.URL + threadChatStreamPath(project.ID, session.ID, session.ID))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	// Should return 401 Unauthorized
	AssertStatus(t, resp, http.StatusUnauthorized)
}

func TestChatStream_MissingThreadId(t *testing.T) {
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	workspace := ts.CreateTestWorkspace(project, "/home/user/code")
	session := ts.CreateTestSession(workspace, "Test Session")
	client := ts.AuthenticatedClient(user)

	// Request stream without thread ID.
	// chi router treats /threads//stream as /threads/{threadId}/stream with an empty threadId.
	resp := client.Get("/api/projects/" + project.ID + "/sessions/" + session.ID + "/threads//stream")
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusBadRequest)
}

// TestChatStream_ActiveStream_FirstMessageConsumed verifies the stream forwards
// the first buffered message instead of dropping it.
func TestChatStream_ActiveStream_FirstMessageConsumed(t *testing.T) {
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	workspace := ts.CreateTestWorkspaceWithGitRepo(project)
	client := ts.AuthenticatedClient(user)

	// Create session with sandbox
	session := ts.CreateTestSessionWithSandbox(workspace, "Test Session")

	// Configure mock sandbox with a custom HTTP handler that simulates
	// an active SSE stream with multiple messages
	messages := []string{
		`{"type":"text","text":"First message"}`,
		`{"type":"text","text":"Second message"}`,
		`{"type":"text","text":"Third message"}`,
	}

	ts.MockSandbox.HTTPHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only handle GET /chat/stream for SSE streams
		if r.Method != "GET" || !strings.HasSuffix(r.URL.Path, "/chat/stream") {
			http.NotFound(w, r)
			return
		}

		// Check if this is an SSE stream request
		if r.Header.Get("Accept") == "text/event-stream" {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("x-vercel-ai-ui-message-stream", "v1")
			w.WriteHeader(http.StatusOK)

			// Write all messages immediately so the first event is already buffered.
			for _, msg := range messages {
				_, _ = fmt.Fprintf(w, "event: chunk\n")
				_, _ = fmt.Fprintf(w, "data: %s\n\n", msg)
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
			}

			_, _ = fmt.Fprintf(w, "event: done\n")
			_, _ = fmt.Fprintf(w, "data: {}\n\n")
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			return
		}

		// Non-SSE GET returns empty messages
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"messages":[]}`))
	})

	// Request the stream
	resp := client.Get(threadChatStreamPath(project.ID, session.ID, session.ID))
	defer resp.Body.Close()

	// Verify we got 200 OK with SSE headers
	AssertStatus(t, resp, http.StatusOK)
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Expected Content-Type text/event-stream, got %s", ct)
	}
	if stream := resp.Header.Get("x-vercel-ai-ui-message-stream"); stream != "v1" {
		t.Errorf("Expected x-vercel-ai-ui-message-stream v1, got %s", stream)
	}

	frames, err := readChatSSEFrames(resp.Body)
	if err != nil {
		t.Fatalf("Error reading SSE stream: %v", err)
	}
	receivedMessages := []string{}
	for _, frame := range frames {
		if frame.Event == "chunk" {
			receivedMessages = append(receivedMessages, frame.Data)
		}
	}

	// Verify we received all messages including the first one
	if len(receivedMessages) != len(messages) {
		t.Errorf("Expected %d messages, got %d", len(messages), len(receivedMessages))
	}

	// Verify each message was received correctly
	for i, expected := range messages {
		if i >= len(receivedMessages) {
			t.Errorf("Missing message %d: %s", i, expected)
			continue
		}
		if receivedMessages[i] != expected {
			t.Errorf("Message %d mismatch:\nExpected: %s\nGot: %s", i, expected, receivedMessages[i])
		}
	}

	// Most importantly: verify the first message was NOT lost
	if len(receivedMessages) > 0 && !strings.Contains(receivedMessages[0], "First message") {
		t.Error("First buffered message was lost")
	}
}

// TestChatStream_ActiveStream_SlowMessages tests that the stream properly
// handles messages that arrive slowly (not all buffered at once).
func TestChatStream_ActiveStream_SlowMessages(t *testing.T) {
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	workspace := ts.CreateTestWorkspaceWithGitRepo(project)
	client := ts.AuthenticatedClient(user)

	session := ts.CreateTestSessionWithSandbox(workspace, "Test Session")

	// Configure mock sandbox to send messages with delays
	ts.MockSandbox.HTTPHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || !strings.HasSuffix(r.URL.Path, "/chat/stream") {
			http.NotFound(w, r)
			return
		}

		if r.Header.Get("Accept") == "text/event-stream" {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("x-vercel-ai-ui-message-stream", "v1")
			w.WriteHeader(http.StatusOK)

			// Send first message immediately
			_, _ = fmt.Fprintf(w, "event: chunk\n")
			_, _ = fmt.Fprintf(w, "data: %s\n\n", `{"type":"text","text":"Message 1"}`)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}

			// Wait a bit before sending second message
			time.Sleep(10 * time.Millisecond)
			_, _ = fmt.Fprintf(w, "event: chunk\n")
			_, _ = fmt.Fprintf(w, "data: %s\n\n", `{"type":"text","text":"Message 2"}`)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}

			_, _ = fmt.Fprintf(w, "event: done\n")
			_, _ = fmt.Fprintf(w, "data: {}\n\n")
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"messages":[]}`))
	})

	resp := client.Get(threadChatStreamPath(project.ID, session.ID, session.ID))
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusOK)

	// Read messages with a reasonable timeout
	receivedMessages := []string{}
	done := make(chan struct{})
	errCh := make(chan error, 1)

	go func() {
		defer close(done)
		frames, err := readChatSSEFrames(resp.Body)
		if err != nil {
			errCh <- err
			return
		}
		for _, frame := range frames {
			if frame.Event == "chunk" {
				receivedMessages = append(receivedMessages, frame.Data)
			}
		}
	}()

	// Wait for messages or timeout
	select {
	case <-done:
		// Success
		select {
		case err := <-errCh:
			t.Fatalf("Error reading SSE stream: %v", err)
		default:
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for stream messages")
	}

	if len(receivedMessages) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(receivedMessages))
	}

	if len(receivedMessages) > 0 && !strings.Contains(receivedMessages[0], "Message 1") {
		t.Error("First message was not received correctly")
	}
}

// TestChatStream_ActiveStream_OnlyDone tests the edge case where the sandbox
// immediately closes the stream with only a terminal done event.
func TestChatStream_ActiveStream_OnlyDone(t *testing.T) {
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	workspace := ts.CreateTestWorkspaceWithGitRepo(project)
	client := ts.AuthenticatedClient(user)

	session := ts.CreateTestSessionWithSandbox(workspace, "Test Session")

	// Configure mock sandbox to send only DONE signal
	ts.MockSandbox.HTTPHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || !strings.HasSuffix(r.URL.Path, "/chat/stream") {
			http.NotFound(w, r)
			return
		}

		if r.Header.Get("Accept") == "text/event-stream" {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("x-vercel-ai-ui-message-stream", "v1")
			w.WriteHeader(http.StatusOK)

			_, _ = fmt.Fprintf(w, "event: done\n")
			_, _ = fmt.Fprintf(w, "data: {}\n\n")
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"messages":[]}`))
	})

	resp := client.Get(threadChatStreamPath(project.ID, session.ID, session.ID))
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusOK)

	// Read and verify the server does not forward the terminal done event.
	frames, err := readChatSSEFrames(resp.Body)
	if err != nil {
		t.Fatalf("Error reading SSE stream: %v", err)
	}
	if len(frames) != 0 {
		t.Fatalf("expected no forwarded events, got %d", len(frames))
	}
}
