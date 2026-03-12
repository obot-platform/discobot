package integration

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/obot-platform/discobot/server/internal/model"
)

// createWorkspaceTestGitRepo creates a test git repository for workspace initialization tests
func createWorkspaceTestGitRepo(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()

	// Initialize git repo
	runWorkspaceGit(t, dir, "init")
	runWorkspaceGit(t, dir, "config", "user.email", "test@example.com")
	runWorkspaceGit(t, dir, "config", "user.name", "Test User")

	// Create initial file
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Test Repo\n"), 0644); err != nil {
		t.Fatalf("Failed to create README.md: %v", err)
	}

	// Commit
	runWorkspaceGit(t, dir, "add", ".")
	runWorkspaceGit(t, dir, "commit", "-m", "Initial commit")

	return dir
}

func runWorkspaceGit(t *testing.T, dir string, args ...string) string {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\nOutput: %s", args, err, output)
	}
	return string(output)
}

func TestListWorkspaces_Empty(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	client := ts.AuthenticatedClient(user)

	resp := client.Get("/api/projects/" + project.ID + "/workspaces")
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusOK)

	var result struct {
		Workspaces []interface{} `json:"workspaces"`
	}
	ParseJSON(t, resp, &result)

	if len(result.Workspaces) != 0 {
		t.Errorf("Expected 0 workspaces, got %d", len(result.Workspaces))
	}
}

func TestCreateWorkspace(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	client := ts.AuthenticatedClient(user)

	// Create a valid git repository for testing
	testPath := createWorkspaceTestGitRepo(t)

	resp := client.Post("/api/projects/"+project.ID+"/workspaces", map[string]string{
		"path":       testPath,
		"sourceType": "local",
	})
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusCreated)

	var workspace map[string]interface{}
	ParseJSON(t, resp, &workspace)

	if workspace["path"] != testPath {
		t.Errorf("Expected path '%s', got '%v'", testPath, workspace["path"])
	}
	if workspace["sourceType"] != "local" {
		t.Errorf("Expected sourceType 'local', got '%v'", workspace["sourceType"])
	}
	// New workspace should start in initializing status
	if workspace["status"] != model.WorkspaceStatusInitializing {
		t.Errorf("Expected status '%s', got '%v'", model.WorkspaceStatusInitializing, workspace["status"])
	}
}

func TestCreateWorkspace_MissingPath(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	client := ts.AuthenticatedClient(user)

	resp := client.Post("/api/projects/"+project.ID+"/workspaces", map[string]string{})
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusBadRequest)
}

func TestCreateWorkspace_DefaultSourceType(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	client := ts.AuthenticatedClient(user)

	// Create a valid git repository for testing
	testPath := createWorkspaceTestGitRepo(t)

	resp := client.Post("/api/projects/"+project.ID+"/workspaces", map[string]string{
		"path": testPath,
	})
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusCreated)

	var workspace map[string]interface{}
	ParseJSON(t, resp, &workspace)

	if workspace["sourceType"] != "local" {
		t.Errorf("Expected default sourceType 'local', got '%v'", workspace["sourceType"])
	}
}

func TestValidateWorkspace_LocalPath(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	client := ts.AuthenticatedClient(user)

	repoPath := createWorkspaceTestGitRepo(t)

	resp := client.Post("/api/projects/"+project.ID+"/workspaces/validate", map[string]string{
		"path":       repoPath,
		"sourceType": "local",
	})
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusOK)

	var result struct {
		Path           string `json:"path"`
		SourceType     string `json:"sourceType"`
		Valid          bool   `json:"valid"`
		Classification string `json:"classification"`
	}
	ParseJSON(t, resp, &result)

	if result.Path != repoPath {
		t.Errorf("Expected path %q, got %q", repoPath, result.Path)
	}
	if result.SourceType != "local" {
		t.Errorf("Expected sourceType local, got %q", result.SourceType)
	}
	if !result.Valid {
		t.Error("Expected local git repo path to be valid")
	}
	if result.Classification != "existing_git" {
		t.Errorf("Expected classification existing_git, got %q", result.Classification)
	}
}

func TestValidateWorkspace_GitRemote(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	client := ts.AuthenticatedClient(user)

	repoPath := createWorkspaceTestGitRepo(t)
	remoteURL := "file://" + repoPath

	resp := client.Post("/api/projects/"+project.ID+"/workspaces/validate", map[string]string{
		"path":       remoteURL,
		"sourceType": "git",
	})
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusOK)

	var result struct {
		Path           string `json:"path"`
		SourceType     string `json:"sourceType"`
		Valid          bool   `json:"valid"`
		Classification string `json:"classification"`
	}
	ParseJSON(t, resp, &result)

	if result.Path != remoteURL {
		t.Errorf("Expected path %q, got %q", remoteURL, result.Path)
	}
	if result.SourceType != "git" {
		t.Errorf("Expected sourceType git, got %q", result.SourceType)
	}
	if result.Valid {
		t.Error("Expected local file:// remote to be rejected")
	}
	if result.Classification != "invalid" {
		t.Errorf("Expected classification invalid, got %q", result.Classification)
	}
}

func TestGetWorkspace(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	workspace := ts.CreateTestWorkspace(project, "/home/user/code")
	client := ts.AuthenticatedClient(user)

	resp := client.Get("/api/projects/" + project.ID + "/workspaces/" + workspace.ID)
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusOK)

	var result map[string]interface{}
	ParseJSON(t, resp, &result)

	if result["id"] != workspace.ID {
		t.Errorf("Expected id '%s', got '%v'", workspace.ID, result["id"])
	}
}

func TestUpdateWorkspace(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	workspace := ts.CreateTestWorkspace(project, "/home/user/code")
	client := ts.AuthenticatedClient(user)

	resp := client.Put("/api/projects/"+project.ID+"/workspaces/"+workspace.ID, map[string]string{
		"path": "/home/user/new-path",
	})
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusOK)

	var result map[string]interface{}
	ParseJSON(t, resp, &result)

	if result["path"] != "/home/user/new-path" {
		t.Errorf("Expected path '/home/user/new-path', got '%v'", result["path"])
	}
}

func TestDeleteWorkspace(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	workspace := ts.CreateTestWorkspace(project, "/home/user/code")
	client := ts.AuthenticatedClient(user)

	resp := client.Delete("/api/projects/" + project.ID + "/workspaces/" + workspace.ID)
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusOK)

	// Verify workspace is deleted
	resp = client.Get("/api/projects/" + project.ID + "/workspaces/" + workspace.ID)
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusNotFound)
}

func TestListWorkspaces_WithData(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	ts.CreateTestWorkspace(project, "/home/user/project1")
	ts.CreateTestWorkspace(project, "/home/user/project2")
	client := ts.AuthenticatedClient(user)

	resp := client.Get("/api/projects/" + project.ID + "/workspaces")
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusOK)

	var result struct {
		Workspaces []interface{} `json:"workspaces"`
	}
	ParseJSON(t, resp, &result)

	if len(result.Workspaces) != 2 {
		t.Errorf("Expected 2 workspaces, got %d", len(result.Workspaces))
	}
}

func TestCreateWorkspace_TildeExpansion(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	client := ts.AuthenticatedClient(user)

	// Create a real git repo under ~ so path validation passes
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("Failed to get home directory: %v", err)
	}
	testSubdir := fmt.Sprintf(".discobot-test-tilde-%d", time.Now().UnixNano())
	testPath := filepath.Join(homeDir, testSubdir)
	if err := os.MkdirAll(testPath, 0755); err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(testPath) })

	// Initialize as a git repo
	runWorkspaceGit(t, testPath, "init")
	runWorkspaceGit(t, testPath, "config", "user.email", "test@example.com")
	runWorkspaceGit(t, testPath, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(testPath, "README.md"), []byte("# Test\n"), 0644); err != nil {
		t.Fatalf("Failed to create README: %v", err)
	}
	runWorkspaceGit(t, testPath, "add", ".")
	runWorkspaceGit(t, testPath, "commit", "-m", "Initial commit")

	tildeRelPath := "~/" + testSubdir
	resp := client.Post("/api/projects/"+project.ID+"/workspaces", map[string]string{
		"path":       tildeRelPath,
		"sourceType": "local",
	})
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusCreated)

	var workspace map[string]interface{}
	ParseJSON(t, resp, &workspace)

	path := workspace["path"].(string)
	// Path should not contain ~
	if path == tildeRelPath || path[0] == '~' {
		t.Errorf("Expected tilde to be expanded, got '%v'", path)
	}
	// Path should be absolute
	if !filepath.IsAbs(path) {
		t.Errorf("Expected absolute path, got '%v'", path)
	}
	// Path should resolve to the test directory
	expected := testPath
	if path != expected {
		t.Errorf("Expected path '%s', got '%s'", expected, path)
	}
}

func TestUpdateWorkspace_TildeExpansion(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	workspace := ts.CreateTestWorkspace(project, "/home/user/code")
	client := ts.AuthenticatedClient(user)

	resp := client.Put("/api/projects/"+project.ID+"/workspaces/"+workspace.ID, map[string]string{
		"path": "~/projects/newpath",
	})
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusOK)

	var result map[string]interface{}
	ParseJSON(t, resp, &result)

	path := result["path"].(string)
	// Path should not contain ~
	if path == "~/projects/newpath" || path[0] == '~' {
		t.Errorf("Expected tilde to be expanded, got '%v'", path)
	}
	// Path should be absolute
	if !filepath.IsAbs(path) {
		t.Errorf("Expected absolute path, got '%v'", path)
	}
	// Path should end with /projects/newpath
	homeDir, _ := os.UserHomeDir()
	expected := filepath.Join(homeDir, "projects/newpath")
	if path != expected {
		t.Errorf("Expected path '%s', got '%s'", expected, path)
	}
}

func TestWorkspaceInitialization_Local(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	client := ts.AuthenticatedClient(user)

	// Create a test git repo to use as the workspace source
	localPath := createWorkspaceTestGitRepo(t)
	resp := client.Post("/api/projects/"+project.ID+"/workspaces", map[string]string{
		"path":       localPath,
		"sourceType": "local",
	})
	AssertStatus(t, resp, http.StatusCreated)

	var workspace struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	ParseJSON(t, resp, &workspace)

	// Should start as initializing
	if workspace.Status != model.WorkspaceStatusInitializing {
		t.Errorf("Expected initial status '%s', got '%s'", model.WorkspaceStatusInitializing, workspace.Status)
	}

	// Wait for initialization job to complete (poll for ready status)
	var finalStatus string
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp = client.Get("/api/projects/" + project.ID + "/workspaces/" + workspace.ID)
		var ws struct {
			Status string `json:"status"`
			Commit string `json:"commit"`
		}
		ParseJSON(t, resp, &ws)
		finalStatus = ws.Status

		if ws.Status == model.WorkspaceStatusReady {
			return
		}
		if ws.Status == model.WorkspaceStatusError {
			t.Fatalf("Workspace initialization failed with error status")
		}
		time.Sleep(50 * time.Millisecond)
	}

	t.Errorf("Workspace did not reach ready status within timeout, final status: %s", finalStatus)
}

func TestCreateWorkspace_WithDisplayName(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	client := ts.AuthenticatedClient(user)

	// Create a valid git repository for testing
	testPath := createWorkspaceTestGitRepo(t)

	resp := client.Post("/api/projects/"+project.ID+"/workspaces", map[string]interface{}{
		"path":        testPath,
		"sourceType":  "local",
		"displayName": "My Project",
	})
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusCreated)

	var workspace map[string]interface{}
	ParseJSON(t, resp, &workspace)

	if workspace["path"] != testPath {
		t.Errorf("Expected path '%s', got '%v'", testPath, workspace["path"])
	}
	if workspace["displayName"] != "My Project" {
		t.Errorf("Expected displayName 'My Project', got '%v'", workspace["displayName"])
	}
}

func TestUpdateWorkspace_SetDisplayName(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	workspace := ts.CreateTestWorkspace(project, "/home/user/code")
	client := ts.AuthenticatedClient(user)

	// Set display name
	resp := client.Put("/api/projects/"+project.ID+"/workspaces/"+workspace.ID, map[string]interface{}{
		"displayName": "My Renamed Project",
	})
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusOK)

	var result map[string]interface{}
	ParseJSON(t, resp, &result)

	if result["displayName"] != "My Renamed Project" {
		t.Errorf("Expected displayName 'My Renamed Project', got '%v'", result["displayName"])
	}
	// Path should remain unchanged
	if result["path"] != "/home/user/code" {
		t.Errorf("Expected path to remain '/home/user/code', got '%v'", result["path"])
	}
}

func TestUpdateWorkspace_ClearDisplayName(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	workspace := ts.CreateTestWorkspace(project, "/home/user/code")
	client := ts.AuthenticatedClient(user)

	// First set a display name
	resp := client.Put("/api/projects/"+project.ID+"/workspaces/"+workspace.ID, map[string]interface{}{
		"displayName": "My Project",
	})
	resp.Body.Close()
	AssertStatus(t, resp, http.StatusOK)

	// Now clear it by setting to null
	resp = client.Put("/api/projects/"+project.ID+"/workspaces/"+workspace.ID, map[string]interface{}{
		"displayName": nil,
	})
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusOK)

	var result map[string]interface{}
	ParseJSON(t, resp, &result)

	// displayName should be nil/absent
	if val, ok := result["displayName"]; ok && val != nil {
		t.Errorf("Expected displayName to be nil, got '%v'", val)
	}
}

func TestUpdateWorkspace_UpdateBothPathAndDisplayName(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	workspace := ts.CreateTestWorkspace(project, "/home/user/code")
	client := ts.AuthenticatedClient(user)

	// Update both path and display name
	resp := client.Put("/api/projects/"+project.ID+"/workspaces/"+workspace.ID, map[string]interface{}{
		"path":        "/home/user/new-code",
		"displayName": "New Project Name",
	})
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusOK)

	var result map[string]interface{}
	ParseJSON(t, resp, &result)

	if result["path"] != "/home/user/new-code" {
		t.Errorf("Expected path '/home/user/new-code', got '%v'", result["path"])
	}
	if result["displayName"] != "New Project Name" {
		t.Errorf("Expected displayName 'New Project Name', got '%v'", result["displayName"])
	}
}

func TestGetWorkspace_WithDisplayName(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	workspace := ts.CreateTestWorkspace(project, "/home/user/code")
	client := ts.AuthenticatedClient(user)

	// Set display name first
	resp := client.Put("/api/projects/"+project.ID+"/workspaces/"+workspace.ID, map[string]interface{}{
		"displayName": "Project Display Name",
	})
	resp.Body.Close()
	AssertStatus(t, resp, http.StatusOK)

	// Retrieve workspace and verify display name is included
	resp = client.Get("/api/projects/" + project.ID + "/workspaces/" + workspace.ID)
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusOK)

	var result map[string]interface{}
	ParseJSON(t, resp, &result)

	if result["displayName"] != "Project Display Name" {
		t.Errorf("Expected displayName 'Project Display Name', got '%v'", result["displayName"])
	}
}

func TestListWorkspaces_WithDisplayNames(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	ws1 := ts.CreateTestWorkspace(project, "/home/user/project1")
	ws2 := ts.CreateTestWorkspace(project, "/home/user/project2")
	client := ts.AuthenticatedClient(user)

	// Set display name on first workspace
	resp := client.Put("/api/projects/"+project.ID+"/workspaces/"+ws1.ID, map[string]interface{}{
		"displayName": "First Project",
	})
	resp.Body.Close()
	AssertStatus(t, resp, http.StatusOK)

	// List workspaces
	resp = client.Get("/api/projects/" + project.ID + "/workspaces")
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusOK)

	var result struct {
		Workspaces []map[string]interface{} `json:"workspaces"`
	}
	ParseJSON(t, resp, &result)

	if len(result.Workspaces) != 2 {
		t.Fatalf("Expected 2 workspaces, got %d", len(result.Workspaces))
	}

	// Find workspace with display name and verify it's present
	foundDisplayName := false
	for _, ws := range result.Workspaces {
		if ws["id"] == ws1.ID {
			if ws["displayName"] != "First Project" {
				t.Errorf("Expected displayName 'First Project' for ws1, got '%v'", ws["displayName"])
			}
			foundDisplayName = true
		}
		if ws["id"] == ws2.ID {
			// ws2 should not have a displayName
			if val, ok := ws["displayName"]; ok && val != nil {
				t.Errorf("Expected ws2 to have no displayName, got '%v'", val)
			}
		}
	}

	if !foundDisplayName {
		t.Error("Did not find workspace with display name")
	}
}

func TestCreateWorkspace_WithProvider(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	client := ts.AuthenticatedClient(user)

	// Create a test git repo
	localPath := createWorkspaceTestGitRepo(t)

	resp := client.Post("/api/projects/"+project.ID+"/workspaces", map[string]interface{}{
		"path":       localPath,
		"sourceType": "local",
		"provider":   "docker",
	})
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusCreated)

	var workspace map[string]interface{}
	ParseJSON(t, resp, &workspace)

	if workspace["path"] != localPath {
		t.Errorf("Expected path '%s', got '%v'", localPath, workspace["path"])
	}
	if workspace["provider"] != "docker" {
		t.Errorf("Expected provider 'docker', got '%v'", workspace["provider"])
	}
}

func TestCreateWorkspace_WithoutProvider(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	client := ts.AuthenticatedClient(user)

	// Create a test git repo
	localPath := createWorkspaceTestGitRepo(t)

	resp := client.Post("/api/projects/"+project.ID+"/workspaces", map[string]interface{}{
		"path":       localPath,
		"sourceType": "local",
	})
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusCreated)

	var workspace map[string]interface{}
	ParseJSON(t, resp, &workspace)

	if workspace["path"] != localPath {
		t.Errorf("Expected path '%s', got '%v'", localPath, workspace["path"])
	}
	// Provider should be empty string or omitted when not specified
	if provider, ok := workspace["provider"]; ok && provider != "" {
		t.Errorf("Expected provider to be empty or omitted, got '%v'", provider)
	}
}

func TestCreateWorkspace_WithVZProvider(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	client := ts.AuthenticatedClient(user)

	// Create a test git repo
	localPath := createWorkspaceTestGitRepo(t)

	resp := client.Post("/api/projects/"+project.ID+"/workspaces", map[string]interface{}{
		"path":       localPath,
		"sourceType": "local",
		"provider":   "vz",
	})
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusCreated)

	var workspace map[string]interface{}
	ParseJSON(t, resp, &workspace)

	if workspace["provider"] != "vz" {
		t.Errorf("Expected provider 'vz', got '%v'", workspace["provider"])
	}
}

func TestCreateWorkspace_WithLocalProvider(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	client := ts.AuthenticatedClient(user)

	// Create a test git repo
	localPath := createWorkspaceTestGitRepo(t)

	resp := client.Post("/api/projects/"+project.ID+"/workspaces", map[string]interface{}{
		"path":       localPath,
		"sourceType": "local",
		"provider":   "local",
	})
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusCreated)

	var workspace map[string]interface{}
	ParseJSON(t, resp, &workspace)

	if workspace["provider"] != "local" {
		t.Errorf("Expected provider 'local', got '%v'", workspace["provider"])
	}
}

func TestUpdateWorkspace_ProviderImmutable(t *testing.T) {
	t.Parallel()
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	client := ts.AuthenticatedClient(user)

	// Create a test git repo
	localPath := createWorkspaceTestGitRepo(t)

	// Create workspace with docker provider
	resp := client.Post("/api/projects/"+project.ID+"/workspaces", map[string]interface{}{
		"path":       localPath,
		"sourceType": "local",
		"provider":   "docker",
	})
	AssertStatus(t, resp, http.StatusCreated)

	var workspace map[string]interface{}
	ParseJSON(t, resp, &workspace)
	workspaceID := workspace["id"].(string)

	// Attempt to update provider
	resp = client.Put("/api/projects/"+project.ID+"/workspaces/"+workspaceID, map[string]interface{}{
		"provider": "vz",
	})
	defer resp.Body.Close()

	AssertStatus(t, resp, http.StatusOK)

	var updated map[string]interface{}
	ParseJSON(t, resp, &updated)

	// Provider should remain unchanged (docker, not vz)
	if updated["provider"] != "docker" {
		t.Errorf("Expected provider to remain 'docker', got '%v'", updated["provider"])
	}
}
