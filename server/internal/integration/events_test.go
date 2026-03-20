package integration

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/obot-platform/discobot/server/internal/events"
)

func TestEvents_SSE_Connection(t *testing.T) {
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	client := ts.AuthenticatedClient(user)

	// Connect to SSE endpoint
	req, err := http.NewRequest("GET", ts.Server.URL+"/api/projects/"+project.ID+"/events", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.AddCookie(&http.Cookie{Name: "discobot_session", Value: user.Token})

	// Use a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := client.ts.Client().Do(req)
	if err != nil {
		t.Fatalf("Failed to connect to SSE: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", resp.StatusCode)
	}

	// Check SSE headers
	contentType := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "text/event-stream") {
		t.Errorf("Expected Content-Type text/event-stream, got %s", contentType)
	}

	// Read the connected event
	scanner := bufio.NewScanner(resp.Body)
	var foundConnected bool
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: connected") {
			foundConnected = true
			break
		}
	}

	if !foundConnected {
		t.Error("Did not receive connected event")
	}
}

func TestEvents_ReceivesSessionUpdates(t *testing.T) {
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")

	// Connect to SSE endpoint
	req, err := http.NewRequest("GET", ts.Server.URL+"/api/projects/"+project.ID+"/events", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.AddCookie(&http.Cookie{Name: "discobot_session", Value: user.Token})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("Failed to connect to SSE: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", resp.StatusCode)
	}

	// Read events in a goroutine
	eventsCh := make(chan string, 10)
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data: ") {
				eventsCh <- line[6:] // Remove "data: " prefix
			}
		}
		close(eventsCh)
	}()

	// Wait a bit for connection to be established
	time.Sleep(100 * time.Millisecond)

	// Publish a session_updated event
	err = ts.Handler.EventBroker().PublishSessionUpdated(context.Background(), project.ID, "session-123", "ready", "")
	if err != nil {
		t.Fatalf("Failed to publish event: %v", err)
	}

	// Wait for the session_updated event (skip any other events like 'connected')
	timeout := time.After(5 * time.Second)
	for {
		select {
		case data := <-eventsCh:
			var event events.Event
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				// Skip events that don't parse as Event (e.g., connected event)
				continue
			}
			if event.Type != events.EventTypeSessionUpdated {
				// Skip non-session_updated events
				continue
			}

			var sessionData events.SessionUpdatedData
			if err := json.Unmarshal(event.Data, &sessionData); err != nil {
				t.Fatalf("Failed to unmarshal session data: %v", err)
			}
			if sessionData.SessionID != "session-123" {
				t.Errorf("Expected session ID 'session-123', got '%s'", sessionData.SessionID)
			}
			if sessionData.Status != "ready" {
				t.Errorf("Expected status 'ready', got '%s'", sessionData.Status)
			}
			return // Test passed
		case <-timeout:
			t.Error("Timeout waiting for session_updated event")
			return
		}
	}
}

func TestEvents_FiltersEventsByProject(t *testing.T) {
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	projectA := ts.CreateTestProject(user, "Project A")
	projectB := ts.CreateTestProject(user, "Project B")

	// Connect to SSE for project A
	req, err := http.NewRequest("GET", ts.Server.URL+"/api/projects/"+projectA.ID+"/events", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.AddCookie(&http.Cookie{Name: "discobot_session", Value: user.Token})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("Failed to connect to SSE: %v", err)
	}
	defer resp.Body.Close()

	// Read events in a goroutine
	eventsCh := make(chan string, 10)
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data: ") {
				eventsCh <- line[6:]
			}
		}
		close(eventsCh)
	}()

	// Wait for connection
	time.Sleep(100 * time.Millisecond)

	// Publish event to project B (should NOT be received by project A subscriber)
	err = ts.Handler.EventBroker().PublishSessionUpdated(context.Background(), projectB.ID, "session-b", "ready", "")
	if err != nil {
		t.Fatalf("Failed to publish event to project B: %v", err)
	}

	// Publish event to project A (SHOULD be received)
	err = ts.Handler.EventBroker().PublishSessionUpdated(context.Background(), projectA.ID, "session-a", "ready", "")
	if err != nil {
		t.Fatalf("Failed to publish event to project A: %v", err)
	}

	// Wait for the event - should only receive project A's event
	timeout := time.After(2 * time.Second)
	for {
		select {
		case data := <-eventsCh:
			var event events.Event
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				// Skip events that don't parse as Event (e.g., connected event)
				continue
			}
			if event.Type != events.EventTypeSessionUpdated {
				// Skip non-session_updated events
				continue
			}

			var sessionData events.SessionUpdatedData
			if err := json.Unmarshal(event.Data, &sessionData); err != nil {
				t.Fatalf("Failed to unmarshal session data: %v", err)
			}
			if sessionData.SessionID != "session-a" {
				t.Errorf("Expected session-a, got %s (might have received project B's event)", sessionData.SessionID)
			}
			return // Test passed
		case <-timeout:
			t.Error("Timeout waiting for event")
			return
		}
	}
}

func TestEvents_EventsPersistedToDatabase(t *testing.T) {
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")

	ctx := context.Background()

	// Publish some events
	err := ts.Handler.EventBroker().PublishSessionUpdated(ctx, project.ID, "session-1", "initializing", "")
	if err != nil {
		t.Fatalf("Failed to publish event 1: %v", err)
	}

	err = ts.Handler.EventBroker().PublishSessionUpdated(ctx, project.ID, "session-1", "ready", "")
	if err != nil {
		t.Fatalf("Failed to publish event 2: %v", err)
	}

	// Query events from database
	events, err := ts.Store.ListEventsAfterSeq(ctx, 0, 100)
	if err != nil {
		t.Fatalf("Failed to list events: %v", err)
	}

	if len(events) < 2 {
		t.Errorf("Expected at least 2 events, got %d", len(events))
	}

	// Verify events are for the correct project
	for _, event := range events {
		if event.ProjectID != project.ID {
			t.Errorf("Event has wrong project ID: %s (expected %s)", event.ProjectID, project.ID)
		}
	}

	// Verify events have sequential seq numbers
	for i := 1; i < len(events); i++ {
		if events[i].Seq <= events[i-1].Seq {
			t.Errorf("Events not in sequential order: seq %d followed by %d", events[i-1].Seq, events[i].Seq)
		}
	}
}

func TestEvents_SessionCreationEmitsEvents(t *testing.T) {
	SkipIfShort(t) // Slow test: ~2.2s
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	workspace := ts.CreateTestWorkspace(project, "/home/user/code")
	client := ts.AuthenticatedClient(user)

	// Connect to SSE
	req, err := http.NewRequest("GET", ts.Server.URL+"/api/projects/"+project.ID+"/events", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.AddCookie(&http.Cookie{Name: "discobot_session", Value: user.Token})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("Failed to connect to SSE: %v", err)
	}
	defer resp.Body.Close()

	// Collect events
	eventsCh := make(chan events.Event, 20)
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data: ") {
				var event events.Event
				if err := json.Unmarshal([]byte(line[6:]), &event); err == nil {
					eventsCh <- event
				}
			}
		}
		close(eventsCh)
	}()

	// Wait for connection
	time.Sleep(100 * time.Millisecond)

	// Create a session via the chat endpoint (which should emit status update events)
	// Format matches AI SDK's DefaultChatTransport with UIMessage format
	createResp := client.Post(threadChatPath(project.ID, "test-events-session-1", "test-events-session-1"), map[string]interface{}{
		"messages": []map[string]interface{}{
			{
				"id":   "msg-1",
				"role": "user",
				"parts": []map[string]interface{}{
					{"type": "text", "text": "Hello"},
				},
			},
		},
		"workspaceId": workspace.ID,
	})
	defer createResp.Body.Close()

	AssertStatus(t, createResp, http.StatusOK)

	// Wait for session initialization to complete and events to be emitted
	time.Sleep(500 * time.Millisecond)

	// Collect received events
	var receivedEvents []events.Event
	timeout := time.After(5 * time.Second)
loop:
	for {
		select {
		case event, ok := <-eventsCh:
			if !ok {
				break loop
			}
			receivedEvents = append(receivedEvents, event)
		case <-timeout:
			break loop
		default:
			// No more events immediately available
			if len(receivedEvents) > 0 {
				break loop
			}
			time.Sleep(50 * time.Millisecond)
		}
	}

	// Should have received at least one session_updated event
	var hasSessionUpdated bool
	for _, event := range receivedEvents {
		if event.Type == events.EventTypeSessionUpdated {
			hasSessionUpdated = true
			break
		}
	}

	if !hasSessionUpdated {
		t.Error("Expected at least one session_updated event during session creation")
	}
}
