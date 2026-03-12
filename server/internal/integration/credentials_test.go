package integration

import (
	"net/http"
	"testing"
)

func TestListCredentials_Empty(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("cred@test.com")
	project := ts.CreateTestProject(user, "cred-project")

	client := ts.AuthenticatedClient(user)
	resp := client.Get("/api/projects/" + project.ID + "/credentials")
	AssertStatus(t, resp, http.StatusOK)

	var result struct {
		Credentials []map[string]interface{} `json:"credentials"`
	}
	ParseJSON(t, resp, &result)

	if len(result.Credentials) != 0 {
		t.Errorf("Expected empty credentials list, got %d", len(result.Credentials))
	}
}

func TestCreateCredential_APIKey(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("cred@test.com")
	project := ts.CreateTestProject(user, "cred-project")

	client := ts.AuthenticatedClient(user)
	resp := client.Post("/api/projects/"+project.ID+"/credentials", map[string]string{
		"provider": "anthropic",
		"name":     "My Anthropic Key",
		"apiKey":   "sk-ant-test-123456",
	})
	AssertStatus(t, resp, http.StatusOK)

	var cred map[string]interface{}
	ParseJSON(t, resp, &cred)

	if cred["provider"] != "anthropic" {
		t.Errorf("Expected provider 'anthropic', got %v", cred["provider"])
	}
	if cred["name"] != "My Anthropic Key" {
		t.Errorf("Expected name 'My Anthropic Key', got %v", cred["name"])
	}
	if cred["authType"] != "api_key" {
		t.Errorf("Expected authType 'api_key', got %v", cred["authType"])
	}
	if cred["isConfigured"] != true {
		t.Errorf("Expected isConfigured true, got %v", cred["isConfigured"])
	}
	// Verify the API key is NOT returned
	if _, ok := cred["apiKey"]; ok {
		t.Error("API key should not be returned in response")
	}
}

func TestCreateCredential_ID(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("cred@test.com")
	project := ts.CreateTestProject(user, "cred-project")

	client := ts.AuthenticatedClient(user)
	resp := client.Post("/api/projects/"+project.ID+"/credentials", map[string]string{
		"provider": "discobot",
		"name":     "My Discobot ID",
		"authType": "id",
		"apiKey":   "discobot-test-id-123",
	})
	AssertStatus(t, resp, http.StatusOK)

	var cred map[string]interface{}
	ParseJSON(t, resp, &cred)

	if cred["provider"] != "discobot" {
		t.Errorf("Expected provider 'discobot', got %v", cred["provider"])
	}
	if cred["name"] != "My Discobot ID" {
		t.Errorf("Expected name 'My Discobot ID', got %v", cred["name"])
	}
	if cred["authType"] != "id" {
		t.Errorf("Expected authType 'id', got %v", cred["authType"])
	}
	if cred["isConfigured"] != true {
		t.Errorf("Expected isConfigured true, got %v", cred["isConfigured"])
	}
	if _, ok := cred["apiKey"]; ok {
		t.Error("credential secret should not be returned in response")
	}
}

func TestCreateCredential_MissingProvider(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("cred@test.com")
	project := ts.CreateTestProject(user, "cred-project")

	client := ts.AuthenticatedClient(user)
	resp := client.Post("/api/projects/"+project.ID+"/credentials", map[string]string{
		"apiKey": "sk-test-123",
	})
	AssertStatus(t, resp, http.StatusBadRequest)
}

func TestCreateCredential_MissingAPIKey(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("cred@test.com")
	project := ts.CreateTestProject(user, "cred-project")

	client := ts.AuthenticatedClient(user)
	resp := client.Post("/api/projects/"+project.ID+"/credentials", map[string]string{
		"provider": "anthropic",
	})
	AssertStatus(t, resp, http.StatusBadRequest)
}

func TestCreateCredential_InvalidProvider(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("cred@test.com")
	project := ts.CreateTestProject(user, "cred-project")

	client := ts.AuthenticatedClient(user)
	resp := client.Post("/api/projects/"+project.ID+"/credentials", map[string]string{
		"provider": "invalid-provider",
		"apiKey":   "sk-test-123",
	})
	AssertStatus(t, resp, http.StatusBadRequest)
}

func TestGetCredential(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("cred@test.com")
	project := ts.CreateTestProject(user, "cred-project")

	client := ts.AuthenticatedClient(user)

	// Create credential first
	resp := client.Post("/api/projects/"+project.ID+"/credentials", map[string]string{
		"provider": "openai",
		"name":     "My OpenAI Key",
		"apiKey":   "sk-test-openai-123",
	})
	AssertStatus(t, resp, http.StatusOK)

	// Get the credential
	resp = client.Get("/api/projects/" + project.ID + "/credentials/openai")
	AssertStatus(t, resp, http.StatusOK)

	var cred map[string]interface{}
	ParseJSON(t, resp, &cred)

	if cred["provider"] != "openai" {
		t.Errorf("Expected provider 'openai', got %v", cred["provider"])
	}
	if cred["name"] != "My OpenAI Key" {
		t.Errorf("Expected name 'My OpenAI Key', got %v", cred["name"])
	}
}

func TestGetCredential_NotFound(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("cred@test.com")
	project := ts.CreateTestProject(user, "cred-project")

	client := ts.AuthenticatedClient(user)
	resp := client.Get("/api/projects/" + project.ID + "/credentials/anthropic")
	AssertStatus(t, resp, http.StatusNotFound)
}

func TestDeleteCredential(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("cred@test.com")
	project := ts.CreateTestProject(user, "cred-project")

	client := ts.AuthenticatedClient(user)

	// Create credential first
	resp := client.Post("/api/projects/"+project.ID+"/credentials", map[string]string{
		"provider": "anthropic",
		"apiKey":   "sk-test-123",
	})
	AssertStatus(t, resp, http.StatusOK)

	// Delete it
	resp = client.Delete("/api/projects/" + project.ID + "/credentials/anthropic")
	AssertStatus(t, resp, http.StatusOK)

	// Verify it's gone
	resp = client.Get("/api/projects/" + project.ID + "/credentials/anthropic")
	AssertStatus(t, resp, http.StatusNotFound)
}

func TestUpdateCredential(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("cred@test.com")
	project := ts.CreateTestProject(user, "cred-project")

	client := ts.AuthenticatedClient(user)

	// Create credential
	resp := client.Post("/api/projects/"+project.ID+"/credentials", map[string]string{
		"provider": "anthropic",
		"name":     "Original Name",
		"apiKey":   "sk-old-key",
	})
	AssertStatus(t, resp, http.StatusOK)

	// Update it (same provider creates/updates)
	resp = client.Post("/api/projects/"+project.ID+"/credentials", map[string]string{
		"provider": "anthropic",
		"name":     "Updated Name",
		"apiKey":   "sk-new-key",
	})
	AssertStatus(t, resp, http.StatusOK)

	var cred map[string]interface{}
	ParseJSON(t, resp, &cred)

	if cred["name"] != "Updated Name" {
		t.Errorf("Expected name 'Updated Name', got %v", cred["name"])
	}

	// Verify only one credential exists
	resp = client.Get("/api/projects/" + project.ID + "/credentials")
	AssertStatus(t, resp, http.StatusOK)

	var credList struct {
		Credentials []map[string]interface{} `json:"credentials"`
	}
	ParseJSON(t, resp, &credList)

	if len(credList.Credentials) != 1 {
		t.Errorf("Expected 1 credential, got %d", len(credList.Credentials))
	}
}

func TestListCredentials_WithData(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("cred@test.com")
	project := ts.CreateTestProject(user, "cred-project")

	client := ts.AuthenticatedClient(user)

	// Create multiple credentials
	providers := []string{"anthropic", "openai", "github-copilot"}
	for _, provider := range providers {
		resp := client.Post("/api/projects/"+project.ID+"/credentials", map[string]string{
			"provider": provider,
			"apiKey":   "sk-test-" + provider,
		})
		AssertStatus(t, resp, http.StatusOK)
	}

	// List all
	resp := client.Get("/api/projects/" + project.ID + "/credentials")
	AssertStatus(t, resp, http.StatusOK)

	var result struct {
		Credentials []map[string]interface{} `json:"credentials"`
	}
	ParseJSON(t, resp, &result)

	if len(result.Credentials) != 3 {
		t.Errorf("Expected 3 credentials, got %d", len(result.Credentials))
	}
}
