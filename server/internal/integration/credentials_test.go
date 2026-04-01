package integration

import (
	"net/http"
	"testing"
)

type credentialResponse struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Name     string `json:"name"`
}

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

func TestCreateCredential_BlankNameRemainsEmptyForBuiltInProvider(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("cred@test.com")
	project := ts.CreateTestProject(user, "cred-project")

	client := ts.AuthenticatedClient(user)
	resp := client.Post("/api/projects/"+project.ID+"/credentials", map[string]string{
		"provider": "anthropic",
		"apiKey":   "sk-ant-test-123456",
	})
	AssertStatus(t, resp, http.StatusOK)

	var cred credentialResponse
	ParseJSON(t, resp, &cred)
	if cred.Name != "" {
		t.Fatalf("expected blank credential name, got %q", cred.Name)
	}
}

func TestCreateCredential_BlankNameRemainsEmptyForCustomEnvVars(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("cred@test.com")
	project := ts.CreateTestProject(user, "cred-project")

	client := ts.AuthenticatedClient(user)
	resp := client.Post("/api/projects/"+project.ID+"/credentials", map[string]any{
		"envVars": []map[string]string{{
			"key":   "FOO_TOKEN",
			"value": "foo-secret",
		}, {
			"key":   "BAR_TOKEN",
			"value": "bar-secret",
		}},
	})
	AssertStatus(t, resp, http.StatusOK)

	var cred credentialResponse
	ParseJSON(t, resp, &cred)
	if cred.Name != "" {
		t.Fatalf("expected blank credential name, got %q", cred.Name)
	}
	if cred.Provider == "" {
		t.Fatal("expected generated custom credential provider")
	}
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

func TestGetCredentialByID_DoesNotReturnSecretValues(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("cred@test.com")
	project := ts.CreateTestProject(user, "cred-project")

	client := ts.AuthenticatedClient(user)
	createResp := client.Post("/api/projects/"+project.ID+"/credentials", map[string]any{
		"provider": "custom",
		"name":     "Secrets",
		"envVars": []map[string]string{{
			"key":   "FOO_TOKEN",
			"value": "foo-secret",
		}, {
			"key":   "BAR_TOKEN",
			"value": "bar-secret",
		}},
	})
	AssertStatus(t, createResp, http.StatusOK)

	var created credentialResponse
	ParseJSON(t, createResp, &created)

	resp := client.Get("/api/projects/" + project.ID + "/credentials/" + created.ID)
	AssertStatus(t, resp, http.StatusOK)

	var credential map[string]any
	ParseJSON(t, resp, &credential)
	if _, ok := credential["envVars"]; ok {
		t.Fatal("expected envVars to be omitted from credential response")
	}
	if credential["envKeys"] == nil {
		t.Fatal("expected envKeys to remain available in credential response")
	}
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

func TestUpdateCredentialByForeignIDReturnsNotFound(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("cred@test.com")
	projectA := ts.CreateTestProject(user, "cred-project-a")
	projectB := ts.CreateTestProject(user, "cred-project-b")

	client := ts.AuthenticatedClient(user)
	createResp := client.Post("/api/projects/"+projectB.ID+"/credentials", map[string]string{
		"provider": "anthropic",
		"name":     "Project B credential",
		"apiKey":   "sk-project-b",
	})
	AssertStatus(t, createResp, http.StatusOK)

	var created credentialResponse
	ParseJSON(t, createResp, &created)

	resp := client.Post("/api/projects/"+projectA.ID+"/credentials", map[string]string{
		"credentialId": created.ID,
		"provider":     "anthropic",
		"name":         "Cross-project overwrite",
		"apiKey":       "sk-project-a",
	})
	AssertStatus(t, resp, http.StatusNotFound)

	resp = client.Get("/api/projects/" + projectA.ID + "/credentials/anthropic")
	AssertStatus(t, resp, http.StatusNotFound)

	resp = client.Get("/api/projects/" + projectB.ID + "/credentials/" + created.ID)
	AssertStatus(t, resp, http.StatusOK)
	ParseJSON(t, resp, &created)
	if created.Name != "Project B credential" {
		t.Fatalf("expected project B credential to remain unchanged, got %q", created.Name)
	}
}

func TestUpdateCustomCredentialByForeignIDReturnsNotFound(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("cred@test.com")
	projectA := ts.CreateTestProject(user, "cred-project-a")
	projectB := ts.CreateTestProject(user, "cred-project-b")

	client := ts.AuthenticatedClient(user)
	createResp := client.Post("/api/projects/"+projectB.ID+"/credentials", map[string]any{
		"provider": "custom",
		"name":     "Project B custom credential",
		"envVars": []map[string]string{{
			"key":   "PROJECT_B_TOKEN",
			"value": "secret-b",
		}},
	})
	AssertStatus(t, createResp, http.StatusOK)

	var created credentialResponse
	ParseJSON(t, createResp, &created)

	resp := client.Post("/api/projects/"+projectA.ID+"/credentials", map[string]any{
		"credentialId": created.ID,
		"provider":     "custom",
		"name":         "Cross-project custom overwrite",
		"envVars": []map[string]string{{
			"key":   "PROJECT_A_TOKEN",
			"value": "secret-a",
		}},
	})
	AssertStatus(t, resp, http.StatusNotFound)

	resp = client.Get("/api/projects/" + projectB.ID + "/credentials/" + created.ID)
	AssertStatus(t, resp, http.StatusOK)
	ParseJSON(t, resp, &created)
	if created.Name != "Project B custom credential" {
		t.Fatalf("expected project B custom credential to remain unchanged, got %q", created.Name)
	}
}

func TestDeleteCredentialByForeignIDReturnsNotFound(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("cred@test.com")
	projectA := ts.CreateTestProject(user, "cred-project-a")
	projectB := ts.CreateTestProject(user, "cred-project-b")

	client := ts.AuthenticatedClient(user)
	createResp := client.Post("/api/projects/"+projectB.ID+"/credentials", map[string]string{
		"provider": "openai",
		"name":     "Project B OpenAI",
		"apiKey":   "sk-project-b-openai",
	})
	AssertStatus(t, createResp, http.StatusOK)

	var created credentialResponse
	ParseJSON(t, createResp, &created)

	resp := client.Delete("/api/projects/" + projectA.ID + "/credentials/" + created.ID)
	AssertStatus(t, resp, http.StatusNotFound)

	resp = client.Get("/api/projects/" + projectB.ID + "/credentials/" + created.ID)
	AssertStatus(t, resp, http.StatusOK)
}

func TestSetSessionCredentialsRejectsForeignCredentialID(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("cred@test.com")
	projectA := ts.CreateTestProject(user, "cred-project-a")
	projectB := ts.CreateTestProject(user, "cred-project-b")

	workspace := ts.CreateTestWorkspace(projectA, "/tmp/project-a")
	session := ts.CreateTestSession(workspace, "project-a-session")
	client := ts.AuthenticatedClient(user)

	createResp := client.Post("/api/projects/"+projectB.ID+"/credentials", map[string]string{
		"provider": "anthropic",
		"name":     "Project B Anthropic",
		"apiKey":   "sk-project-b-anthropic",
	})
	AssertStatus(t, createResp, http.StatusOK)

	var created credentialResponse
	ParseJSON(t, createResp, &created)

	resp := client.Put("/api/projects/"+projectA.ID+"/sessions/"+session.ID+"/credentials", map[string]any{
		"credentials": []map[string]any{{
			"credentialId": created.ID,
			"agentVisible": true,
		}},
	})
	AssertStatus(t, resp, http.StatusNotFound)
}
