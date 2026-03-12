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

func TestChatStream_SessionNotFound(t *testing.T) {
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	client := ts.AuthenticatedClient(user)

	// Request stream for non-existent session - should return 204 No Content
	resp := client.Get("/api/projects/" + project.ID + "/chat/nonexistent-session/stream")
	defer resp.Body.Close()

	// No session = no stream = 204 No Content
	AssertStatus(t, resp, http.StatusNoContent)
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
	resp := client2.Get("/api/projects/" + project2.ID + "/chat/" + session1.ID + "/stream")
	defer resp.Body.Close()

	// Should return 403 Forbidden
	AssertStatus(t, resp, http.StatusForbidden)
}

func TestChatStream_ValidSession_NoActiveStream(t *testing.T) {
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	workspace := ts.CreateTestWorkspace(project, "/home/user/code")
	session := ts.CreateTestSession(workspace, "Test Session")
	client := ts.AuthenticatedClient(user)

	// Request stream for valid session but no active completion
	// The mock sandbox will return an error (no sandbox running), which
	// should be handled gracefully as 204 No Content
	resp := client.Get("/api/projects/" + project.ID + "/chat/" + session.ID + "/stream")
	defer resp.Body.Close()

	// No sandbox/no active stream = 204 No Content
	AssertStatus(t, resp, http.StatusNoContent)
}

func TestChatStream_RequiresAuthentication(t *testing.T) {
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	workspace := ts.CreateTestWorkspace(project, "/home/user/code")
	session := ts.CreateTestSession(workspace, "Test Session")

	// Make unauthenticated request
	resp, err := http.Get(ts.Server.URL + "/api/projects/" + project.ID + "/chat/" + session.ID + "/stream")
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	// Should return 401 Unauthorized
	AssertStatus(t, resp, http.StatusUnauthorized)
}

func TestChatStream_MissingSessionId(t *testing.T) {
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	client := ts.AuthenticatedClient(user)

	// Request stream without session ID
	// chi router treats /chat//stream as /chat/{sessionId}/stream with empty sessionId
	// The handler validates and returns 400 Bad Request
	resp := client.Get("/api/projects/" + project.ID + "/chat//stream")
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusBadRequest)
}

// TestChatStream_ActiveStream_FirstMessageConsumed tests the bug fix for losing
// the first message when checking if a stream is active. This verifies that
// the message consumed during the channel check is properly sent to the client.
func TestChatStream_ActiveStream_FirstMessageConsumed(t *testing.T) {
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	workspace := ts.CreateTestWorkspace(project, "/home/user/code")
	agent := ts.CreateTestAgent(project, "Test Agent", "claude-code")
	client := ts.AuthenticatedClient(user)

	// Create session with agent
	session := ts.CreateTestSessionWithAgent(workspace, agent, "Test Session")

	// Create a sandbox so the session has a running container
	ts.CreateAndStartSandbox(session.ID)

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

			// Write all messages immediately so they're available when
			// the handler checks the channel
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
	resp := client.Get("/api/projects/" + project.ID + "/chat/" + session.ID + "/stream")
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
		t.Error("First message was lost during channel check")
	}
}

// TestChatStream_ActiveStream_SlowMessages tests that the stream properly
// handles messages that arrive slowly (not all buffered at once).
func TestChatStream_ActiveStream_SlowMessages(t *testing.T) {
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	workspace := ts.CreateTestWorkspace(project, "/home/user/code")
	agent := ts.CreateTestAgent(project, "Test Agent", "claude-code")
	client := ts.AuthenticatedClient(user)

	session := ts.CreateTestSessionWithAgent(workspace, agent, "Test Session")
	ts.CreateAndStartSandbox(session.ID)

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

	resp := client.Get("/api/projects/" + project.ID + "/chat/" + session.ID + "/stream")
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

// TestChatStream_ActiveStream_OnlyDone tests edge case where stream
// immediately sends DONE without any messages.
func TestChatStream_ActiveStream_OnlyDone(t *testing.T) {
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	workspace := ts.CreateTestWorkspace(project, "/home/user/code")
	agent := ts.CreateTestAgent(project, "Test Agent", "claude-code")
	client := ts.AuthenticatedClient(user)

	session := ts.CreateTestSessionWithAgent(workspace, agent, "Test Session")
	ts.CreateAndStartSandbox(session.ID)

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

	resp := client.Get("/api/projects/" + project.ID + "/chat/" + session.ID + "/stream")
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusOK)

	// Read and verify we get the done event
	gotDone := false
	frames, err := readChatSSEFrames(resp.Body)
	if err != nil {
		t.Fatalf("Error reading SSE stream: %v", err)
	}
	for _, frame := range frames {
		if frame.Event == "done" {
			gotDone = true
			break
		}
	}

	if !gotDone {
		t.Error("expected to receive done event")
	}
}
