package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/obot-platform/discobot/server/internal/config"
	"github.com/obot-platform/discobot/server/internal/database"
	"github.com/obot-platform/discobot/server/internal/dispatcher"
	"github.com/obot-platform/discobot/server/internal/events"
	"github.com/obot-platform/discobot/server/internal/git"
	"github.com/obot-platform/discobot/server/internal/handler"
	"github.com/obot-platform/discobot/server/internal/jobs"
	"github.com/obot-platform/discobot/server/internal/middleware"
	"github.com/obot-platform/discobot/server/internal/model"
	"github.com/obot-platform/discobot/server/internal/sandbox"
	"github.com/obot-platform/discobot/server/internal/sandbox/mock"
	"github.com/obot-platform/discobot/server/internal/service"
	"github.com/obot-platform/discobot/server/internal/store"
)

// TestServer wraps a test HTTP server with helpers
type TestServer struct {
	Server          *httptest.Server
	Store           *store.Store
	Config          *config.Config
	Handler         *handler.Handler
	DB              *database.DB
	GitProvider     git.Provider
	SandboxProvider sandbox.Provider
	MockSandbox     *mock.Provider // Access to mock for test assertions
	Dispatcher      *dispatcher.Service
	EventPoller     *events.Poller
	T               *testing.T
}

// NewTestServer creates a new test server with in-memory SQLite or PostgreSQL
func NewTestServer(t *testing.T) *TestServer {
	t.Helper()

	// Determine DSN based on test configuration
	var dsn string
	var driver string
	var skipMigrate bool

	if PostgresEnabled() {
		// Use PostgreSQL container
		dsn = PostgresDSN()
		driver = "postgres"
	} else if envDSN := os.Getenv("TEST_DATABASE_DSN"); envDSN != "" {
		// Use explicit DSN from environment
		dsn = envDSN
		if len(dsn) >= 8 && dsn[:8] == "postgres" {
			driver = "postgres"
		} else {
			driver = "sqlite"
		}
	} else if templatePath := GetTemplateDBPath(); templatePath != "" {
		// Use pre-migrated template database (copy it for this test)
		// This is much faster than running AutoMigrate for each test
		testDBPath := fmt.Sprintf("%s/test.db", t.TempDir())
		if err := copyFile(templatePath, testDBPath); err != nil {
			t.Fatalf("Failed to copy template database: %v", err)
		}
		dsn = "sqlite3://" + testDBPath
		driver = "sqlite"
		skipMigrate = true // Already migrated
	} else {
		// Fallback: file-based SQLite in temp directory with migration
		// (in-memory SQLite creates separate databases per connection,
		// which doesn't work well with the dispatcher using separate goroutines)
		dsn = fmt.Sprintf("sqlite3://%s/test.db", t.TempDir())
		driver = "sqlite"
	}

	workspaceDir := t.TempDir()
	cfg := &config.Config{
		Port:           8080,
		CORSOrigins:    []string{"*"},
		DatabaseDSN:    dsn,
		DatabaseDriver: driver,
		AuthEnabled:    true, // Enable auth for testing the full auth flow
		SessionSecret:  []byte("test-session-secret-32-bytes-long!!"),
		EncryptionKey:  []byte("01234567890123456789012345678901"), // 32 bytes
		WorkspaceDir:   workspaceDir,
	}

	db, err := database.New(cfg)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}

	if skipMigrate {
		// Template DBs can occasionally miss schema changes if WAL state isn't fully
		// materialized into the copied file. Verify required tables and run migration
		// only when needed.
		migrator := db.Migrator()
		for _, m := range model.AllModels() {
			if !migrator.HasTable(m) {
				if err := db.Migrate(); err != nil {
					t.Fatalf("Failed to run migrations: %v", err)
				}
				break
			}
		}
	} else {
		if err := db.Migrate(); err != nil {
			t.Fatalf("Failed to run migrations: %v", err)
		}
	}

	// For PostgreSQL, clean tables before each test to ensure isolation
	if driver == "postgres" {
		cleanTables(db)
	}

	s := store.New(db.DB, db.ReadDB)

	// Create git provider with workspace source for lookup
	workspaceSource := git.NewStoreWorkspaceSource(s)
	gitProvider, err := git.NewLocalProvider(workspaceDir, git.WithWorkspaceSource(workspaceSource))
	if err != nil {
		t.Fatalf("Failed to create git provider: %v", err)
	}

	// Create mock sandbox provider
	mockSandbox := mock.NewProvider()

	// Create event poller and broker for SSE
	eventPollerCfg := events.DefaultPollerConfig()
	eventPollerCfg.PollInterval = 10 * time.Millisecond // Fast polling for tests
	eventPoller := events.NewPoller(s, eventPollerCfg)
	if err := eventPoller.Start(context.Background()); err != nil {
		t.Fatalf("Failed to start event poller: %v", err)
	}
	eventBroker := events.NewBroker(s, eventPoller)

	// Create sandbox manager and register mock provider
	sandboxManager := sandbox.NewManager()
	sandboxManager.RegisterProvider("mock", mockSandbox)

	// Create job queue early so it can be passed to services
	jobQueue := jobs.NewQueue(s, cfg)

	h := handler.New(s, cfg, gitProvider, mockSandbox, sandboxManager, eventBroker, jobQueue, nil)

	// Create and start dispatcher for job processing
	cfg.DispatcherEnabled = true
	cfg.DispatcherPollInterval = 10 * time.Millisecond // Fast polling for tests
	cfg.DispatcherHeartbeatInterval = 50 * time.Millisecond
	cfg.DispatcherHeartbeatTimeout = 500 * time.Millisecond
	cfg.DispatcherJobTimeout = 30 * time.Second
	cfg.DispatcherStaleJobTimeout = 1 * time.Minute

	workspaceSvc := service.NewWorkspaceService(s, gitProvider, eventBroker)

	gitSvc := service.NewGitService(s, gitProvider)
	sandboxSvc := service.NewSandboxService(s, mockSandbox, cfg, nil, eventBroker, jobQueue, nil)
	sessionSvc := service.NewSessionService(s, gitSvc, mockSandbox, sandboxSvc, eventBroker, jobQueue)
	sandboxSvc.SetSessionInitializer(sessionSvc)

	disp := dispatcher.NewService(s, cfg, eventBroker)
	disp.RegisterExecutor(dispatcher.NewWorkspaceInitExecutor(workspaceSvc))
	disp.RegisterExecutor(dispatcher.NewSessionInitExecutor(sessionSvc))
	disp.RegisterExecutor(dispatcher.NewSessionCommitExecutor(sessionSvc))
	disp.RegisterExecutor(dispatcher.NewSessionRebaseExecutor(sessionSvc))
	disp.Start(context.Background())

	// Wire up job queue notification for immediate execution
	h.JobQueue().SetNotifyFunc(disp.NotifyNewJob)

	r := setupRouter(s, cfg, h)
	server := httptest.NewServer(r)

	ts := &TestServer{
		Server:          server,
		Store:           s,
		Config:          cfg,
		Handler:         h,
		DB:              db,
		GitProvider:     gitProvider,
		SandboxProvider: mockSandbox,
		MockSandbox:     mockSandbox,
		Dispatcher:      disp,
		EventPoller:     eventPoller,
		T:               t,
	}

	t.Cleanup(func() {
		disp.Stop()
		eventPoller.Stop()
		server.Close()
		_ = db.Close()
	})

	return ts
}

// setupRouter creates the router with all routes (matches main.go)
func setupRouter(s *store.Store, cfg *config.Config, h *handler.Handler) *chi.Mux {
	r := chi.NewRouter()

	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(chimiddleware.Recoverer)
	// Note: No global timeout - SSE endpoints need long-lived connections

	// Health check
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// Auth routes (no auth required)
	r.Route("/auth", func(r chi.Router) {
		r.Get("/login/{provider}", h.AuthLogin)
		r.Get("/callback/{provider}", h.AuthCallback)
		r.Post("/logout", h.AuthLogout)
		r.Get("/me", h.AuthMe)
	})

	// API routes (auth required)
	r.Route("/api", func(r chi.Router) {
		r.Use(middleware.Auth(s, cfg))

		// User Preferences (user-scoped, not project-scoped)
		r.Route("/preferences", func(r chi.Router) {
			r.Get("/", h.ListPreferences)
			r.Put("/", h.SetPreferences)
			r.Get("/{key}", h.GetPreference)
			r.Put("/{key}", h.SetPreference)
			r.Delete("/{key}", h.DeletePreference)
		})

		r.Get("/projects", h.ListProjects)
		r.Post("/projects", h.CreateProject)

		r.Route("/projects/{projectId}", func(r chi.Router) {
			r.Use(middleware.ProjectMember(s))

			r.Get("/", h.GetProject)
			r.Put("/", h.UpdateProject)
			r.Delete("/", h.DeleteProject)

			r.Get("/members", h.ListProjectMembers)
			r.Delete("/members/{userId}", h.RemoveProjectMember)

			r.Post("/invitations", h.CreateInvitation)
			r.Post("/invitations/{token}/accept", h.AcceptInvitation)

			r.Route("/workspaces", func(r chi.Router) {
				r.Get("/", h.ListWorkspaces)
				r.Post("/", h.CreateWorkspace)
				r.Post("/validate", h.ValidateWorkspace)
				r.Get("/{workspaceId}", h.GetWorkspace)
				r.Put("/{workspaceId}", h.UpdateWorkspace)
				r.Delete("/{workspaceId}", h.DeleteWorkspace)

				// Sessions within workspace (list only - creation via /chat endpoint)
				r.Get("/{workspaceId}/sessions", h.ListSessionsByWorkspace)

				// Git operations
				r.Get("/{workspaceId}/git/status", h.GetWorkspaceGitStatus)
				r.Post("/{workspaceId}/git/fetch", h.FetchWorkspace)
				r.Post("/{workspaceId}/git/checkout", h.CheckoutWorkspace)
				r.Get("/{workspaceId}/git/branches", h.GetWorkspaceBranches)
				r.Get("/{workspaceId}/git/diff", h.GetWorkspaceDiff)
				r.Get("/{workspaceId}/git/files", h.GetWorkspaceFileTree)
				r.Get("/{workspaceId}/git/file", h.GetWorkspaceFileContent)
				r.Post("/{workspaceId}/git/file", h.WriteWorkspaceFile)
				r.Post("/{workspaceId}/git/stage", h.StageWorkspaceFiles)
				r.Post("/{workspaceId}/git/commit", h.CommitWorkspace)
				r.Get("/{workspaceId}/git/log", h.GetWorkspaceLog)
			})

			r.Route("/sessions", func(r chi.Router) {
				r.Route("/{sessionId}", func(r chi.Router) {
					r.Use(middleware.SessionBelongsToProject(s))
					r.Get("/", h.GetSession)
					r.Put("/", h.UpdateSession)
					r.Patch("/", h.UpdateSession)
					r.Delete("/", h.DeleteSession)
					r.Post("/commit", h.CommitSession)
					r.Post("/rebase", h.RebaseSession)
					r.Get("/files", h.ListSessionFiles)
					r.Get("/files/read", h.ReadSessionFile)
					r.Put("/files/write", h.WriteSessionFile)
					r.Get("/diff", h.GetSessionDiff)
					r.Get("/messages", h.ListMessages)
					r.Get("/threads", h.ListThreads)
					r.Post("/threads", h.CreateThread)
					r.Get("/threads/{threadId}", h.GetThread)
					r.Put("/threads/{threadId}", h.UpdateThread)
					r.Patch("/threads/{threadId}", h.UpdateThread)
					r.Delete("/threads/{threadId}", h.DeleteThread)
					r.Get("/terminal/ws", h.TerminalWebSocket)
					r.Get("/terminal/history", h.GetTerminalHistory)
					r.Get("/terminal/status", h.GetTerminalStatus)
				})
			})

			r.Route("/agents", func(r chi.Router) {
				r.Get("/", h.ListAgents)
				r.Post("/", h.CreateAgent)
				r.Get("/types", h.GetAgentTypes)
				r.Post("/default", h.SetDefaultAgent)
				r.Get("/{agentId}", h.GetAgent)
				r.Put("/{agentId}", h.UpdateAgent)
				r.Delete("/{agentId}", h.DeleteAgent)
			})

			r.Get("/suggestions", h.GetSuggestions)
			r.Get("/events", h.Events)

			r.Route("/credentials", func(r chi.Router) {
				r.Get("/", h.ListCredentials)
				r.Post("/", h.CreateCredential)
				r.Get("/{provider}", h.GetCredential)
				r.Delete("/{provider}", h.DeleteCredential)

				r.Post("/anthropic/authorize", h.AnthropicAuthorize)
				r.Post("/anthropic/exchange", h.AnthropicExchange)

				r.Post("/github-copilot/device-code", h.GitHubCopilotDeviceCode)
				r.Post("/github-copilot/poll", h.GitHubCopilotPoll)

				r.Post("/github-git/device-code", h.GitHubDeviceCode)
				r.Post("/github-git/poll", h.GitHubPoll)

				r.Post("/codex/authorize", h.CodexAuthorize)
				r.Post("/codex/exchange", h.CodexExchange)
			})

			// AI Chat endpoints (streaming)
			r.Post("/chat", h.Chat)
			r.Get("/chat/{sessionId}/stream", h.ChatStream)
		})
	})

	return r
}

// NewTestServerNoAuth creates a test server with auth disabled (anonymous user mode)
func NewTestServerNoAuth(t *testing.T) *TestServer {
	t.Helper()

	workspaceDir := t.TempDir()
	dbDir := t.TempDir()

	var dsn string
	var skipMigrate bool

	if templatePath := GetTemplateDBPath(); templatePath != "" {
		// Use pre-migrated template database (copy it for this test)
		testDBPath := fmt.Sprintf("%s/test.db", dbDir)
		if err := copyFile(templatePath, testDBPath); err != nil {
			t.Fatalf("Failed to copy template database: %v", err)
		}
		dsn = "sqlite3://" + testDBPath
		skipMigrate = true
	} else {
		dsn = fmt.Sprintf("sqlite3://%s/test.db", dbDir)
	}

	cfg := &config.Config{
		Port:           8080,
		CORSOrigins:    []string{"*"},
		DatabaseDSN:    dsn,
		DatabaseDriver: "sqlite",
		AuthEnabled:    false, // Disable auth - use anonymous user
		SessionSecret:  []byte("test-session-secret-32-bytes-long!!"),
		EncryptionKey:  []byte("01234567890123456789012345678901"),
		WorkspaceDir:   workspaceDir,
	}

	db, err := database.New(cfg)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}

	if skipMigrate {
		// Template DBs can occasionally miss schema changes if WAL state isn't fully
		// materialized into the copied file. Verify required tables and run migration
		// only when needed.
		migrator := db.Migrator()
		for _, m := range model.AllModels() {
			if !migrator.HasTable(m) {
				if err := db.Migrate(); err != nil {
					t.Fatalf("Failed to run migrations: %v", err)
				}
				break
			}
		}
	} else {
		if err := db.Migrate(); err != nil {
			t.Fatalf("Failed to run migrations: %v", err)
		}
	}

	// Seed the anonymous user and default project
	if err := db.Seed(); err != nil {
		t.Fatalf("Failed to seed database: %v", err)
	}

	s := store.New(db.DB, db.ReadDB)

	// Create git provider with workspace source for lookup
	workspaceSource := git.NewStoreWorkspaceSource(s)
	gitProvider, err := git.NewLocalProvider(workspaceDir, git.WithWorkspaceSource(workspaceSource))
	if err != nil {
		t.Fatalf("Failed to create git provider: %v", err)
	}

	// Create mock sandbox provider
	mockSandbox := mock.NewProvider()

	// Create event poller and broker for SSE
	eventPollerCfg := events.DefaultPollerConfig()
	eventPollerCfg.PollInterval = 10 * time.Millisecond // Fast polling for tests
	eventPoller := events.NewPoller(s, eventPollerCfg)
	if err := eventPoller.Start(context.Background()); err != nil {
		t.Fatalf("Failed to start event poller: %v", err)
	}
	eventBroker := events.NewBroker(s, eventPoller)

	// Create sandbox manager and register mock provider
	sandboxManager := sandbox.NewManager()
	sandboxManager.RegisterProvider("mock", mockSandbox)

	// Create job queue early so it can be passed to services
	jobQueue := jobs.NewQueue(s, cfg)

	h := handler.New(s, cfg, gitProvider, mockSandbox, sandboxManager, eventBroker, jobQueue, nil)

	// Create and start dispatcher for job processing
	cfg.DispatcherEnabled = true
	cfg.DispatcherPollInterval = 10 * time.Millisecond // Fast polling for tests
	cfg.DispatcherHeartbeatInterval = 50 * time.Millisecond
	cfg.DispatcherHeartbeatTimeout = 500 * time.Millisecond
	cfg.DispatcherJobTimeout = 30 * time.Second
	cfg.DispatcherStaleJobTimeout = 1 * time.Minute

	workspaceSvc := service.NewWorkspaceService(s, gitProvider, eventBroker)

	gitSvc := service.NewGitService(s, gitProvider)
	sandboxSvc := service.NewSandboxService(s, mockSandbox, cfg, nil, eventBroker, jobQueue, nil)
	sessionSvc := service.NewSessionService(s, gitSvc, mockSandbox, sandboxSvc, eventBroker, jobQueue)
	sandboxSvc.SetSessionInitializer(sessionSvc)

	disp := dispatcher.NewService(s, cfg, eventBroker)
	disp.RegisterExecutor(dispatcher.NewWorkspaceInitExecutor(workspaceSvc))
	disp.RegisterExecutor(dispatcher.NewSessionInitExecutor(sessionSvc))
	disp.RegisterExecutor(dispatcher.NewSessionCommitExecutor(sessionSvc))
	disp.RegisterExecutor(dispatcher.NewSessionRebaseExecutor(sessionSvc))
	disp.Start(context.Background())

	// Wire up job queue notification for immediate execution
	h.JobQueue().SetNotifyFunc(disp.NotifyNewJob)

	r := setupRouter(s, cfg, h)
	server := httptest.NewServer(r)

	ts := &TestServer{
		Server:          server,
		Store:           s,
		Config:          cfg,
		Handler:         h,
		DB:              db,
		GitProvider:     gitProvider,
		SandboxProvider: mockSandbox,
		MockSandbox:     mockSandbox,
		Dispatcher:      disp,
		EventPoller:     eventPoller,
		T:               t,
	}

	t.Cleanup(func() {
		disp.Stop()
		eventPoller.Stop()
		server.Close()
		_ = db.Close()
	})

	return ts
}

// TestUser represents a test user with session
type TestUser struct {
	User    *model.User
	Session *model.UserSession
	Token   string
}

// CreateTestUser creates a test user and returns it with a valid session
func (ts *TestServer) CreateTestUser(email string) *TestUser {
	ts.T.Helper()

	user := &model.User{
		Email:      email,
		Name:       strPtr(fmt.Sprintf("Test User %s", email)),
		Provider:   "github",
		ProviderID: fmt.Sprintf("gh_%s", email),
	}

	if err := ts.Store.CreateUser(context.Background(), user); err != nil {
		ts.T.Fatalf("Failed to create test user: %v", err)
	}

	// Generate a plain token and hash it for storage (like the auth service does)
	plainToken := fmt.Sprintf("test-token-%s-%d", email, time.Now().UnixNano())
	hash := sha256.Sum256([]byte(plainToken))
	tokenHash := hex.EncodeToString(hash[:])

	session := &model.UserSession{
		UserID:    user.ID,
		TokenHash: tokenHash,
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}

	if err := ts.Store.CreateUserSession(context.Background(), session); err != nil {
		ts.T.Fatalf("Failed to create test session: %v", err)
	}

	return &TestUser{
		User:    user,
		Session: session,
		Token:   plainToken, // Return the plain token for use in cookies
	}
}

// CreateTestProject creates a test project with the user as owner
func (ts *TestServer) CreateTestProject(user *TestUser, name string) *model.Project {
	ts.T.Helper()

	project := &model.Project{
		Name: name,
		Slug: fmt.Sprintf("%s-%d", name, time.Now().UnixNano()),
	}

	if err := ts.Store.CreateProject(context.Background(), project); err != nil {
		ts.T.Fatalf("Failed to create test project: %v", err)
	}

	member := &model.ProjectMember{
		ProjectID: project.ID,
		UserID:    user.User.ID,
		Role:      "owner",
	}

	if err := ts.Store.CreateProjectMember(context.Background(), member); err != nil {
		ts.T.Fatalf("Failed to create project member: %v", err)
	}

	return project
}

// CreateTestWorkspace creates a test workspace
func (ts *TestServer) CreateTestWorkspace(project *model.Project, path string) *model.Workspace {
	ts.T.Helper()

	workspace := &model.Workspace{
		ProjectID:  project.ID,
		Path:       path,
		SourceType: "local",
	}

	if err := ts.Store.CreateWorkspace(context.Background(), workspace); err != nil {
		ts.T.Fatalf("Failed to create test workspace: %v", err)
	}

	return workspace
}

// CreateTestGitRepo creates a temporary git repository for testing and returns its path.
func (ts *TestServer) CreateTestGitRepo() string {
	ts.T.Helper()

	// Create a temp directory for the repo
	repoDir := ts.T.TempDir()

	// Initialize git repo
	if err := execGit(repoDir, "init"); err != nil {
		ts.T.Fatalf("Failed to init git repo: %v", err)
	}

	// Configure git user for commits
	if err := execGit(repoDir, "config", "user.email", "test@example.com"); err != nil {
		ts.T.Fatalf("Failed to configure git email: %v", err)
	}
	if err := execGit(repoDir, "config", "user.name", "Test User"); err != nil {
		ts.T.Fatalf("Failed to configure git name: %v", err)
	}

	// Create initial commit
	readmePath := repoDir + "/README.md"
	if err := os.WriteFile(readmePath, []byte("# Test Repo\n"), 0644); err != nil {
		ts.T.Fatalf("Failed to create README: %v", err)
	}
	if err := execGit(repoDir, "add", "."); err != nil {
		ts.T.Fatalf("Failed to git add: %v", err)
	}
	if err := execGit(repoDir, "commit", "-m", "Initial commit"); err != nil {
		ts.T.Fatalf("Failed to create initial commit: %v", err)
	}

	return repoDir
}

// CreateTestWorkspaceWithGitRepo creates a test workspace with a real git repository.
// This is needed for tests that exercise the git provider with local workspaces.
func (ts *TestServer) CreateTestWorkspaceWithGitRepo(project *model.Project) *model.Workspace {
	ts.T.Helper()

	repoPath := ts.CreateTestGitRepo()
	return ts.CreateTestWorkspace(project, repoPath)
}

// execGit runs a git command in the specified directory.
func execGit(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run()
}

// CreateTestSession creates a test session
func (ts *TestServer) CreateTestSession(workspace *model.Workspace, name string) *model.Session {
	ts.T.Helper()

	workspacePath := workspace.Path
	session := &model.Session{
		ProjectID:     workspace.ProjectID,
		WorkspaceID:   workspace.ID,
		Name:          name,
		Status:        "open",
		WorkspacePath: &workspacePath, // Set workspace path for CreateForSession
	}

	if err := ts.Store.CreateSession(context.Background(), session); err != nil {
		ts.T.Fatalf("Failed to create test session: %v", err)
	}

	return session
}

// CreateTestSessionWithAgent creates a test session with an agent assigned.
func (ts *TestServer) CreateTestSessionWithAgent(workspace *model.Workspace, agent *model.Agent, name string) *model.Session {
	ts.T.Helper()

	workspacePath := workspace.Path
	session := &model.Session{
		ProjectID:     workspace.ProjectID,
		WorkspaceID:   workspace.ID,
		AgentID:       &agent.ID,
		Name:          name,
		Status:        model.SessionStatusReady,
		WorkspacePath: &workspacePath,
	}

	if err := ts.Store.CreateSession(context.Background(), session); err != nil {
		ts.T.Fatalf("Failed to create test session: %v", err)
	}

	return session
}

// CreateTestSessionWithMockSandbox creates a test session with a properly configured
// mock sandbox that points to the provided mock server URL.
func (ts *TestServer) CreateTestSessionWithMockSandbox(workspace *model.Workspace, agent *model.Agent, name string, mockServerURL string) *model.Session {
	ts.T.Helper()

	// Parse mock server URL to get host and port
	u, err := url.Parse(mockServerURL)
	if err != nil {
		ts.T.Fatalf("Failed to parse mock server URL: %v", err)
	}
	hostPort := strings.Split(u.Host, ":")
	host := hostPort[0]
	port := 80
	if len(hostPort) > 1 {
		port, _ = strconv.Atoi(hostPort[1])
	}

	workspacePath := workspace.Path
	session := &model.Session{
		ProjectID:     workspace.ProjectID,
		WorkspaceID:   workspace.ID,
		AgentID:       &agent.ID,
		Name:          name,
		Status:        model.SessionStatusReady, // Session is ready
		WorkspacePath: &workspacePath,
	}

	if err := ts.Store.CreateSession(context.Background(), session); err != nil {
		ts.T.Fatalf("Failed to create test session: %v", err)
	}

	// Create and start a mock sandbox that points to the test server
	ctx := context.Background()
	_, err = ts.MockSandbox.Create(ctx, session.ID, sandbox.CreateOptions{
		SharedSecret: "test-secret",
	})
	if err != nil {
		ts.T.Fatalf("Failed to create mock sandbox: %v", err)
	}

	if err := ts.MockSandbox.Start(ctx, session.ID); err != nil {
		ts.T.Fatalf("Failed to start mock sandbox: %v", err)
	}

	// Override the sandbox's port mapping to point to mock server
	ts.MockSandbox.SetSandboxPort(session.ID, host, port)

	return session
}

// CreateTestSessionWithSandbox creates a test session with a running mock sandbox
// that uses the mock provider's default handler (supports /files, /chat, /diff endpoints).
func (ts *TestServer) CreateTestSessionWithSandbox(workspace *model.Workspace, agent *model.Agent, name string) *model.Session {
	ts.T.Helper()

	workspacePath := workspace.Path
	session := &model.Session{
		ProjectID:     workspace.ProjectID,
		WorkspaceID:   workspace.ID,
		AgentID:       &agent.ID,
		Name:          name,
		Status:        model.SessionStatusReady, // Session is ready
		WorkspacePath: &workspacePath,
	}

	if err := ts.Store.CreateSession(context.Background(), session); err != nil {
		ts.T.Fatalf("Failed to create test session: %v", err)
	}

	// Create and start a mock sandbox (uses default handler which supports all endpoints)
	ctx := context.Background()
	_, err := ts.MockSandbox.Create(ctx, session.ID, sandbox.CreateOptions{
		SharedSecret: "test-secret",
	})
	if err != nil {
		ts.T.Fatalf("Failed to create mock sandbox: %v", err)
	}

	if err := ts.MockSandbox.Start(ctx, session.ID); err != nil {
		ts.T.Fatalf("Failed to start mock sandbox: %v", err)
	}

	return session
}

// CreateAndStartSandbox creates and starts a mock sandbox for the given session.
// This is useful for tests that need a running sandbox but don't need to configure it.
func (ts *TestServer) CreateAndStartSandbox(sessionID string) {
	ts.T.Helper()

	ctx := context.Background()
	_, err := ts.MockSandbox.Create(ctx, sessionID, sandbox.CreateOptions{
		SharedSecret: "test-secret",
	})
	if err != nil {
		ts.T.Fatalf("Failed to create mock sandbox: %v", err)
	}

	if err := ts.MockSandbox.Start(ctx, sessionID); err != nil {
		ts.T.Fatalf("Failed to start mock sandbox: %v", err)
	}
}

// CreateTestAgent creates a test agent
func (ts *TestServer) CreateTestAgent(project *model.Project, _, agentType string) *model.Agent {
	ts.T.Helper()

	agent := &model.Agent{
		ProjectID: project.ID,
		AgentType: agentType,
		IsDefault: false,
	}

	if err := ts.Store.CreateAgent(context.Background(), agent); err != nil {
		ts.T.Fatalf("Failed to create test agent: %v", err)
	}

	return agent
}

// Client returns an HTTP client with cookie jar for the test server
func (ts *TestServer) Client() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{
		Jar: jar,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse // Don't follow redirects
		},
	}
}

// AuthenticatedClient returns an HTTP client that sends auth cookies
func (ts *TestServer) AuthenticatedClient(user *TestUser) *TestClient {
	return &TestClient{
		ts:    ts,
		token: user.Token,
	}
}

// TestClient is a helper for making authenticated requests
type TestClient struct {
	ts    *TestServer
	token string
}

// Get makes an authenticated GET request
func (tc *TestClient) Get(path string) *http.Response {
	tc.ts.T.Helper()
	return tc.do("GET", path, nil)
}

// Post makes an authenticated POST request
func (tc *TestClient) Post(path string, body interface{}) *http.Response {
	tc.ts.T.Helper()
	return tc.do("POST", path, body)
}

// Put makes an authenticated PUT request
func (tc *TestClient) Put(path string, body interface{}) *http.Response {
	tc.ts.T.Helper()
	return tc.do("PUT", path, body)
}

// Patch makes an authenticated PATCH request
func (tc *TestClient) Patch(path string, body interface{}) *http.Response {
	tc.ts.T.Helper()
	return tc.do("PATCH", path, body)
}

// Delete makes an authenticated DELETE request
func (tc *TestClient) Delete(path string) *http.Response {
	tc.ts.T.Helper()
	return tc.do("DELETE", path, nil)
}

func (tc *TestClient) do(method, path string, body interface{}) *http.Response {
	tc.ts.T.Helper()

	var bodyReader io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			tc.ts.T.Fatalf("Failed to marshal request body: %v", err)
		}
		bodyReader = bytes.NewReader(jsonBody)
	}

	req, err := http.NewRequest(method, tc.ts.Server.URL+path, bodyReader)
	if err != nil {
		tc.ts.T.Fatalf("Failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{
		Name:  "discobot_session",
		Value: tc.token,
	})

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		tc.ts.T.Fatalf("Request failed: %v", err)
	}

	return resp
}

// ParseJSON parses the response body as JSON
func ParseJSON(t *testing.T, resp *http.Response, v interface{}) {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	if err := json.Unmarshal(body, v); err != nil {
		t.Fatalf("Failed to parse JSON: %v\nBody: %s", err, string(body))
	}
}

// AssertStatus checks the response status code
func AssertStatus(t *testing.T, resp *http.Response, expected int) {
	t.Helper()
	if resp.StatusCode != expected {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		t.Errorf("Expected status %d, got %d\nBody: %s", expected, resp.StatusCode, string(body))
	}
}

func strPtr(s string) *string {
	return &s
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}

// cleanTables truncates all tables for test isolation (PostgreSQL only)
func cleanTables(db *database.DB) {
	// Order matters due to foreign key constraints (delete children first)
	tables := []string{
		"terminal_history",
		"messages",
		"sessions",
		"workspaces",
		"agent_mcp_servers",
		"agents",
		"credentials",
		"project_invitations",
		"project_members",
		"projects",
		"user_preferences",
		"user_sessions",
		"users",
	}

	for _, table := range tables {
		db.Exec("TRUNCATE TABLE " + table + " CASCADE")
	}
}
