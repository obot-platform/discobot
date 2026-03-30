package integration

import (
	"net/http"
	"testing"
)

func TestSessionThreadCRUD(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	workspace := ts.CreateTestWorkspace(project, "/home/user/code")
	session := ts.CreateTestSessionWithSandbox(workspace, "Test Session")
	client := ts.AuthenticatedClient(user)

	basePath := "/api/projects/" + project.ID + "/sessions/" + session.ID + "/threads"

	// Initially no threads.
	listResp := client.Get(basePath)
	defer listResp.Body.Close()
	AssertStatus(t, listResp, http.StatusOK)

	var listResult struct {
		Threads []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"threads"`
	}
	ParseJSON(t, listResp, &listResult)
	if len(listResult.Threads) != 0 {
		t.Fatalf("expected 0 threads, got %d", len(listResult.Threads))
	}

	// Create thread.
	createResp := client.Post(basePath, map[string]string{
		"id":   "thread-1",
		"name": "Thread 1",
	})
	defer createResp.Body.Close()
	AssertStatus(t, createResp, http.StatusCreated)

	var created struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	ParseJSON(t, createResp, &created)
	if created.ID != "thread-1" {
		t.Fatalf("expected thread id thread-1, got %s", created.ID)
	}
	if created.Name != "Thread 1" {
		t.Fatalf("expected thread name Thread 1, got %s", created.Name)
	}

	// Get thread.
	getResp := client.Get(basePath + "/thread-1")
	defer getResp.Body.Close()
	AssertStatus(t, getResp, http.StatusOK)

	var got struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	ParseJSON(t, getResp, &got)
	if got.ID != "thread-1" || got.Name != "Thread 1" {
		t.Fatalf("unexpected thread payload: %+v", got)
	}

	// Update thread via PATCH.
	updateResp := client.Patch(basePath+"/thread-1", map[string]string{"name": "Renamed Thread"})
	defer updateResp.Body.Close()
	AssertStatus(t, updateResp, http.StatusOK)

	var updated struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	ParseJSON(t, updateResp, &updated)
	if updated.Name != "Renamed Thread" {
		t.Fatalf("expected updated name Renamed Thread, got %s", updated.Name)
	}

	// List threads should include renamed thread.
	listResp2 := client.Get(basePath)
	defer listResp2.Body.Close()
	AssertStatus(t, listResp2, http.StatusOK)
	ParseJSON(t, listResp2, &listResult)
	if len(listResult.Threads) != 1 {
		t.Fatalf("expected 1 thread, got %d", len(listResult.Threads))
	}
	if listResult.Threads[0].Name != "Renamed Thread" {
		t.Fatalf("expected list name Renamed Thread, got %s", listResult.Threads[0].Name)
	}

	// Delete thread.
	deleteResp := client.Delete(basePath + "/thread-1")
	defer deleteResp.Body.Close()
	AssertStatus(t, deleteResp, http.StatusOK)

	var deleted struct {
		Success bool `json:"success"`
	}
	ParseJSON(t, deleteResp, &deleted)
	if !deleted.Success {
		t.Fatal("expected success=true on delete")
	}

	// Thread should no longer exist.
	getMissingResp := client.Get(basePath + "/thread-1")
	defer getMissingResp.Body.Close()
	AssertStatus(t, getMissingResp, http.StatusNotFound)
}

func TestGetPrimaryThreadPendingPlaceholderIncludesSessionName(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	workspace := ts.CreateTestWorkspace(project, "/home/user/code")
	session := ts.CreateTestSessionWithSandbox(workspace, "Test Session")
	client := ts.AuthenticatedClient(user)

	basePath := "/api/projects/" + project.ID + "/sessions/" + session.ID + "/threads"

	resp := client.Get(basePath + "/" + session.ID)
	defer resp.Body.Close()
	AssertStatus(t, resp, http.StatusOK)

	var got struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Pending bool   `json:"pending"`
	}
	ParseJSON(t, resp, &got)
	if got.ID != session.ID {
		t.Fatalf("expected thread id %s, got %s", session.ID, got.ID)
	}
	if got.Name != "Test Session" {
		t.Fatalf("expected pending thread name Test Session, got %q", got.Name)
	}
	if !got.Pending {
		t.Fatal("expected pending=true for unmaterialized primary thread")
	}
}
