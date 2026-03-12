package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/joho/godotenv"

	"github.com/obot-platform/discobot/server/internal/config"
	"github.com/obot-platform/discobot/server/internal/conntrack"
	"github.com/obot-platform/discobot/server/internal/database"
	"github.com/obot-platform/discobot/server/internal/dispatcher"
	"github.com/obot-platform/discobot/server/internal/events"
	"github.com/obot-platform/discobot/server/internal/git"
	"github.com/obot-platform/discobot/server/internal/handler"
	"github.com/obot-platform/discobot/server/internal/jobs"
	"github.com/obot-platform/discobot/server/internal/logfile"
	"github.com/obot-platform/discobot/server/internal/middleware"
	"github.com/obot-platform/discobot/server/internal/model"
	"github.com/obot-platform/discobot/server/internal/routes"
	"github.com/obot-platform/discobot/server/internal/sandbox"
	"github.com/obot-platform/discobot/server/internal/sandbox/docker"
	"github.com/obot-platform/discobot/server/internal/sandbox/local"
	"github.com/obot-platform/discobot/server/internal/sandbox/vm"
	"github.com/obot-platform/discobot/server/internal/sandbox/vz"
	"github.com/obot-platform/discobot/server/internal/service"
	"github.com/obot-platform/discobot/server/internal/ssh"
	"github.com/obot-platform/discobot/server/internal/startup"
	"github.com/obot-platform/discobot/server/internal/store"
	"github.com/obot-platform/discobot/server/internal/version"
	"github.com/obot-platform/discobot/server/static"
)

func main() {
	// Load .env file if present
	_ = godotenv.Load()

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Redirect stdout/stderr to log file if configured (must be before any logging)
	if cfg.LogFile != "" {
		if err := logfile.Truncate(cfg.LogFile); err != nil {
			log.Printf("Warning: failed to truncate log file: %v", err)
		}
		if err := logfile.RedirectStdoutStderr(cfg.LogFile); err != nil {
			log.Printf("Warning: failed to redirect output to %s: %v", cfg.LogFile, err)
		}
	}

	// Log version
	log.Printf("Discobot Server version %s", version.Get())

	// Connect to database
	db, err := database.New(cfg)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Run migrations
	log.Println("Running database migrations...")
	if err := db.Migrate(); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}
	log.Println("Migrations completed successfully")

	// Seed database with anonymous user and default project
	if err := db.Seed(); err != nil {
		log.Fatalf("Failed to seed database: %v", err)
	}

	// Log auth mode
	if cfg.AuthEnabled {
		log.Println("Authentication enabled - users must log in")
	} else {
		log.Println("Authentication disabled - using anonymous user mode")
	}

	// Create store with separate read/write pools for SQLite
	s := store.New(db.DB, db.ReadDB)

	// Initialize git provider (required)
	// Create workspace source for git provider to lookup workspace info
	workspaceSource := git.NewStoreWorkspaceSource(s)
	gitProvider, err := git.NewLocalProvider(cfg.WorkspaceDir, git.WithWorkspaceSource(workspaceSource))
	if err != nil {
		log.Fatalf("Failed to initialize git provider: %v", err)
	}
	log.Printf("Git provider initialized at %s", cfg.WorkspaceDir)

	// Initialize sandbox providers
	// Create a manager that can route to different providers based on workspace configuration
	sandboxManager := sandbox.NewManager()

	// Create event poller and broker for SSE (needed by startup manager)
	eventPoller := events.NewPoller(s, events.DefaultPollerConfig())
	if err := eventPoller.Start(context.Background()); err != nil {
		log.Fatalf("Failed to start event poller: %v", err)
	}
	eventBroker := events.NewBroker(s, eventPoller)

	// Create startup task manager for tracking long-running startup operations
	// Use the default project ID ("local") for startup events
	systemManager := startup.NewSystemManager(eventBroker, model.DefaultProjectID)

	// Shared resolver: looks up project ID for a session from the database.
	// Used by Docker (for cache volumes) and VZ (for project VM routing).
	sessionProjectResolver := func(ctx context.Context, sessionID string) (string, error) {
		session, err := s.GetSessionByID(ctx, sessionID)
		if err != nil {
			return "", err
		}
		return session.ProjectID, nil
	}

	if runtime.GOOS == "darwin" {
		// On macOS, use VZ (Virtualization.framework) provider
		vzCfg := &vm.Config{
			DataDir:       cfg.VZDataDir,
			ConsoleLogDir: cfg.VZConsoleLogDir,
			KernelPath:    cfg.VZKernelPath,
			InitrdPath:    cfg.VZInitrdPath,
			BaseDiskPath:  cfg.VZBaseDiskPath,
			ImageRef:      cfg.VZImageRef,
			HomeDir:       cfg.VZHomeDir,
			CPUCount:      cfg.VZCPUCount,
			MemoryMB:      cfg.VZMemoryMB,
			DataDiskGB:    cfg.VZDataDiskGB,
		}
		if vmProvider, vzErr := vz.NewProvider(cfg, vzCfg, sessionProjectResolver, systemManager); vzErr != nil {
			log.Printf("Warning: Failed to initialize VZ sandbox provider: %v", vzErr)
		} else {
			sandboxManager.RegisterProvider("vz", vmProvider)
			if vmProvider.IsReady() {
				log.Printf("VZ sandbox provider initialized and ready")
			} else {
				log.Printf("VZ sandbox provider registered (images downloading in background)")
			}
		}
	} else {
		// On non-macOS, use Docker provider
		if dockerProvider, dockerErr := docker.NewProvider(cfg, sessionProjectResolver, docker.WithSystemManager(systemManager)); dockerErr != nil {
			log.Printf("Warning: Failed to initialize Docker sandbox provider: %v", dockerErr)
		} else {
			sandboxManager.RegisterProvider("docker", dockerProvider)
			log.Printf("Docker sandbox provider initialized (image: %s)", cfg.SandboxImage)
		}
	}
	//
	// Initialize local provider (only if enabled via config)
	if cfg.LocalProviderEnabled {
		if localProvider, localErr := local.NewProvider(cfg); localErr != nil {
			log.Printf("Warning: Failed to initialize local sandbox provider: %v", localErr)
		} else {
			sandboxManager.RegisterProvider("local", localProvider)
			log.Printf("Local sandbox provider initialized")
		}
	}

	// Create provider proxy that routes based on workspace configuration
	// The proxy will look up the session's workspace and use its provider setting
	var sandboxProvider sandbox.Provider
	if sandboxManager.EnsureDefaultAvailable() {
		log.Printf("Default sandbox provider: %s", sandboxManager.DefaultProviderName())

		// Create a sandbox service for the provider getter function
		// This is a bit of a chicken-and-egg problem, so we'll pass the store directly
		providerGetter := func(ctx context.Context, sessionID string) (string, error) {
			// Get session to retrieve workspace ID
			session, err := s.GetSessionByID(ctx, sessionID)
			if err != nil {
				return "", fmt.Errorf("failed to get session: %w", err)
			}

			// Get workspace to retrieve provider
			workspace, err := s.GetWorkspaceByID(ctx, session.WorkspaceID)
			if err != nil {
				return "", fmt.Errorf("failed to get workspace: %w", err)
			}

			// Use platform default if workspace has no provider set
			if workspace.Provider == "" {
				return sandboxManager.DefaultProviderName(), nil
			}

			return workspace.Provider, nil
		}

		sandboxProvider = sandbox.NewProviderProxy(sandboxManager, providerGetter)
		log.Printf("Sandbox provider proxy initialized with %d providers", len(sandboxManager.ListProviders()))
	}

	// Create job queue early so it can be passed to services
	jobQueue := jobs.NewQueue(s, cfg)

	// Start sandbox watcher to sync session states with sandbox states
	// This handles external changes (e.g., Docker containers deleted outside Discobot)
	var sandboxWatcherCancel context.CancelFunc
	if sandboxProvider != nil {
		sandboxWatcher := service.NewSandboxWatcher(sandboxProvider, s, eventBroker)
		var watcherCtx context.Context
		watcherCtx, sandboxWatcherCancel = context.WithCancel(context.Background())
		go func() {
			if err := sandboxWatcher.Start(watcherCtx); err != nil && err != context.Canceled {
				log.Printf("Sandbox watcher stopped with error: %v", err)
			}
		}()
	}

	// Start session status poller to verify running sessions actually have active completions
	// This catches sessions that are marked "running" but the agent has no active completion
	var sessionStatusPoller *service.SessionStatusPoller
	if sandboxProvider != nil {
		// Create a temporary sandbox service for the poller (will be replaced later)
		pollerSandboxSvc := service.NewSandboxService(s, sandboxProvider, cfg, nil, nil, nil, nil)
		sessionStatusPoller = service.NewSessionStatusPoller(s, pollerSandboxSvc, eventBroker, slog.Default())
		sessionStatusPoller.Start(context.Background())
		log.Println("Session status poller started")
	}

	// Create a shared connection tracker so the idle monitor can see live SSH and
	// service-proxy connections and avoid stopping sandboxes while they are in use.
	connTracker := conntrack.New()

	// Start SSH server for VS Code Remote SSH and other SSH-based workflows
	var sshServer *ssh.Server
	if sandboxProvider != nil && cfg.SSHEnabled {
		// Create sandbox service for UserInfoFetcher
		sshSandboxSvc := service.NewSandboxService(s, sandboxProvider, cfg, nil, nil, nil, nil)
		// Create env set service for EnvVarFetcher (injects active env set vars into SSH sessions)
		var sshEnvVarFetcher ssh.EnvVarFetcher
		if sshEnvSetSvc, envSetErr := service.NewEnvSetService(s, cfg); envSetErr != nil {
			log.Printf("Warning: Failed to create env set service for SSH server: %v", envSetErr)
		} else {
			sshEnvVarFetcher = sshEnvSetSvc
		}
		sshServer, err = ssh.New(&ssh.Config{
			Address:           fmt.Sprintf(":%d", cfg.SSHPort),
			HostKeyPath:       cfg.SSHHostKeyPath,
			SandboxProvider:   sandboxProvider,
			UserInfoFetcher:   &sshUserInfoAdapter{svc: sshSandboxSvc},
			EnvVarFetcher:     sshEnvVarFetcher,
			ConnectionTracker: connTracker,
		})
		if err != nil {
			log.Printf("Warning: Failed to create SSH server: %v", err)
		} else {
			go func() {
				if err := sshServer.Start(); err != nil {
					log.Printf("SSH server stopped: %v", err)
				}
			}()
			log.Printf("SSH server started on port %d", cfg.SSHPort)
		}
	}

	// Initialize and start job dispatcher
	var disp *dispatcher.Service
	var sessionSvc *service.SessionService
	var dispSandboxSvc *service.SandboxService
	var sandboxIdleMonitor *service.SandboxIdleMonitor
	if cfg.DispatcherEnabled {
		disp = dispatcher.NewService(s, cfg, eventBroker)

		// Register workspace init executor
		workspaceSvc := service.NewWorkspaceService(s, gitProvider, eventBroker)
		disp.RegisterExecutor(dispatcher.NewWorkspaceInitExecutor(workspaceSvc))

		// Register session init, delete, and commit executors if sandbox provider is available
		if sandboxProvider != nil {
			gitSvc := service.NewGitService(s, gitProvider)
			credSvc, err := service.NewCredentialService(s, cfg)
			if err != nil {
				log.Fatalf("Failed to create credential service for dispatcher: %v", err)
			}
			envSetSvc, err := service.NewEnvSetService(s, cfg)
			if err != nil {
				log.Fatalf("Failed to create env set service for dispatcher: %v", err)
			}
			credFetcher := service.MakeCredentialFetcher(s, credSvc, envSetSvc)
			dispSandboxSvc = service.NewSandboxService(s, sandboxProvider, cfg, credFetcher, eventBroker, jobQueue, connTracker)
			sessionSvc = service.NewSessionService(s, gitSvc, sandboxProvider, dispSandboxSvc, eventBroker, jobQueue)
			dispSandboxSvc.SetSessionInitializer(sessionSvc)
			disp.RegisterExecutor(dispatcher.NewSessionInitExecutor(sessionSvc))
			disp.RegisterExecutor(dispatcher.NewSessionDeleteExecutor(sessionSvc))
			disp.RegisterExecutor(dispatcher.NewSessionCommitExecutor(sessionSvc))
			disp.RegisterExecutor(dispatcher.NewSessionRebaseExecutor(sessionSvc))
		}

		disp.Start(context.Background())
		log.Printf("Job dispatcher started (server ID: %s)", disp.ServerID())

		// Start sandbox idle monitor to auto-stop idle sessions
		if sandboxProvider != nil && sessionSvc != nil && cfg.SandboxIdleTimeout > 0 {
			sandboxIdleMonitor = service.NewSandboxIdleMonitor(
				s,
				dispSandboxSvc,
				sessionSvc,
				connTracker,
				slog.Default(),
				cfg.SandboxIdleTimeout,
				cfg.IdleCheckInterval,
			)
			sandboxIdleMonitor.Start(context.Background())
			log.Printf("Sandbox idle monitor started (timeout: %s, check interval: %s)",
				cfg.SandboxIdleTimeout, cfg.IdleCheckInterval)
		}

		// Start all reconciliation in background after dispatcher is ready
		// This ensures all reconciliation can properly enqueue jobs if needed
		if dispSandboxSvc != nil && sessionSvc != nil {
			go func() {
				log.Println("Starting reconciliation in background...")

				// 1. Reconcile sandboxes to ensure they use the correct image
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
				if err := dispSandboxSvc.ReconcileSandboxes(ctx); err != nil {
					log.Printf("Warning: Failed to reconcile sandboxes: %v", err)
				} else {
					log.Println("Sandbox reconciliation completed successfully")
				}
				cancel()

				// 2. Reconcile session states with actual sandbox states
				ctx, cancel = context.WithTimeout(context.Background(), 10*time.Minute)
				if err := dispSandboxSvc.ReconcileSessionStates(ctx); err != nil {
					log.Printf("Warning: Failed to reconcile session states: %v", err)
				} else {
					log.Println("Session state reconciliation completed successfully")
				}
				cancel()

				// 3. Reconcile commit states to re-enqueue stuck commits
				ctx, cancel = context.WithTimeout(context.Background(), 5*time.Minute)
				if err := sessionSvc.ReconcileCommitStates(ctx); err != nil {
					log.Printf("Warning: Failed to reconcile commit states: %v", err)
				} else {
					log.Println("Commit state reconciliation completed successfully")
				}
				cancel()

				log.Println("All reconciliation completed")
			}()
		}
	} else {
		log.Println("Job dispatcher disabled")
	}

	// Create router
	r := chi.NewRouter()

	// Global middleware
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(middleware.SanitizedLogger)
	r.Use(chimiddleware.Recoverer)
	// Note: No global timeout - SSE endpoints need long-lived connections

	// Service subdomain proxy - intercepts {session-id}-svc-{service-id}.* domains
	// and proxies to agent-api's HTTP proxy endpoint without credentials.
	// IMPORTANT: This must run BEFORE CORS middleware so that OPTIONS requests
	// are forwarded to the service (which handles its own CORS).
	if sandboxProvider != nil {
		r.Use(middleware.ServiceProxy(sandboxProvider, connTracker))
	}

	if len(cfg.CORSOrigins) > 0 {
		// CORS configuration (only applies to non-service-proxy requests)
		r.Use(cors.Handler(cors.Options{
			AllowedOrigins:   cfg.CORSOrigins,
			AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "HEAD", "PATCH"},
			AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token", "User-Agent"},
			ExposedHeaders:   []string{"Link"},
			AllowCredentials: true,
			MaxAge:           300,
			Debug:            cfg.CORSDebug,
		}))
	}

	// Tauri auth middleware - validates secret cookie when running in Tauri mode
	r.Use(middleware.TauriAuth(cfg))

	// Initialize handlers
	h := handler.New(s, cfg, gitProvider, sandboxProvider, sandboxManager, eventBroker, jobQueue, systemManager)

	// Wire up job queue notification to dispatcher for immediate execution
	if disp != nil {
		h.JobQueue().SetNotifyFunc(disp.NotifyNewJob)
	}

	// Route registry for metadata
	reg := routes.GetRegistry()

	// ===== Debug (pprof) =====
	r.Mount("/debug", chimiddleware.Profiler())

	// ===== Health & Status (no auth) =====
	reg.Register(r, routes.Route{
		Method: "GET", Pattern: "/health",
		Handler: func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		},
		Meta: routes.Meta{Group: "Health", Description: "Health check"},
	})

	reg.Register(r, routes.Route{
		Method: "GET", Pattern: "/api/status",
		Handler: h.GetSystemStatus,
		Meta:    routes.Meta{Group: "Health", Description: "System status (Docker, Git checks)"},
	})

	reg.Register(r, routes.Route{
		Method: "GET", Pattern: "/api/server-config",
		Handler: h.GetServerConfig,
		Meta:    routes.Meta{Group: "Health", Description: "Public server configuration (SSH port, etc.)"},
	})

	reg.Register(r, routes.Route{
		Method: "GET", Pattern: "/api/support-info",
		Handler: h.GetSupportInfo,
		Meta:    routes.Meta{Group: "Health", Description: "Diagnostic information for debugging (version, config, logs)"},
	})

	// API UI - serve the embedded static HTML file
	r.Get("/api/ui", func(w http.ResponseWriter, _ *http.Request) {
		content, err := static.Files.ReadFile("api-ui.html")
		if err != nil {
			http.Error(w, "API UI not found", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(content)
	})

	// API Routes endpoint (returns route metadata for API UI)
	r.Get("/api/routes", h.GetRoutes)

	// ===== Auth routes (no auth required) =====
	r.Route("/auth", func(r chi.Router) {
		authReg := reg.WithPrefix("/auth")

		authReg.Register(r, routes.Route{
			Method: "GET", Pattern: "/login/{provider}",
			Handler: h.AuthLogin,
			Meta: routes.Meta{
				Group:       "Auth",
				Description: "Start OAuth login",
				Params:      []routes.Param{{Name: "provider", Example: "github"}},
			},
		})

		authReg.Register(r, routes.Route{
			Method: "GET", Pattern: "/callback/{provider}",
			Handler: h.AuthCallback,
			Meta: routes.Meta{
				Group:       "Auth",
				Description: "OAuth callback",
				Params:      []routes.Param{{Name: "code", In: "query"}, {Name: "state", In: "query"}},
			},
		})

		authReg.Register(r, routes.Route{
			Method: "POST", Pattern: "/logout",
			Handler: h.AuthLogout,
			Meta:    routes.Meta{Group: "Auth", Description: "Logout"},
		})

		authReg.Register(r, routes.Route{
			Method: "GET", Pattern: "/me",
			Handler: h.AuthMe,
			Meta:    routes.Meta{Group: "Auth", Description: "Get current user"},
		})
	})

	// ===== API routes (auth required) =====
	r.Route("/api", func(r chi.Router) {
		r.Use(middleware.Auth(s, cfg))
		apiReg := reg.WithPrefix("/api")

		// User Preferences (user-scoped, not project-scoped)
		r.Route("/preferences", func(r chi.Router) {
			prefReg := apiReg.WithPrefix("/preferences")

			prefReg.Register(r, routes.Route{
				Method: "GET", Pattern: "/",
				Handler: h.ListPreferences,
				Meta:    routes.Meta{Group: "Preferences", Description: "List all user preferences"},
			})

			prefReg.Register(r, routes.Route{
				Method: "PUT", Pattern: "/",
				Handler: h.SetPreferences,
				Meta: routes.Meta{
					Group:       "Preferences",
					Description: "Set multiple preferences",
					Body:        map[string]any{"preferences": map[string]string{"theme": "dark", "editor": "vim"}},
				},
			})

			prefReg.Register(r, routes.Route{
				Method: "GET", Pattern: "/{key}",
				Handler: h.GetPreference,
				Meta: routes.Meta{
					Group:       "Preferences",
					Description: "Get preference by key",
					Params:      []routes.Param{{Name: "key", Example: "theme"}},
				},
			})

			prefReg.Register(r, routes.Route{
				Method: "PUT", Pattern: "/{key}",
				Handler: h.SetPreference,
				Meta: routes.Meta{
					Group:       "Preferences",
					Description: "Set preference",
					Params:      []routes.Param{{Name: "key", Example: "theme"}},
					Body:        map[string]any{"value": "dark"},
				},
			})

			prefReg.Register(r, routes.Route{
				Method: "DELETE", Pattern: "/{key}",
				Handler: h.DeletePreference,
				Meta: routes.Meta{
					Group:       "Preferences",
					Description: "Delete preference",
					Params:      []routes.Param{{Name: "key", Example: "theme"}},
				},
			})
		})

		// Project list
		apiReg.Register(r, routes.Route{
			Method: "GET", Pattern: "/projects",
			Handler: h.ListProjects,
			Meta:    routes.Meta{Group: "Projects", Description: "List projects"},
		})

		apiReg.Register(r, routes.Route{
			Method: "POST", Pattern: "/projects",
			Handler: h.CreateProject,
			Meta: routes.Meta{
				Group:       "Projects",
				Description: "Create project",
				Body:        map[string]any{"name": "My Project", "slug": "my-project"},
			},
		})

		// Project-specific routes
		r.Route("/projects/{projectId}", func(r chi.Router) {
			r.Use(middleware.ProjectMember(s))
			projReg := apiReg.WithPrefix("/projects/{projectId}")

			// SSE events
			projReg.Register(r, routes.Route{
				Method: "GET", Pattern: "/events",
				Handler: h.Events,
				Meta: routes.Meta{
					Group:       "Events",
					Description: "SSE event stream",
					Params: []routes.Param{
						{Name: "projectId", Example: "local"},
						{Name: "since", In: "query", Example: "2024-01-15T10:30:00Z"},
						{Name: "after", In: "query"},
					},
				},
			})

			// Project CRUD
			projReg.Register(r, routes.Route{
				Method: "GET", Pattern: "/",
				Handler: h.GetProject,
				Meta: routes.Meta{
					Group:       "Projects",
					Description: "Get project",
					Params:      []routes.Param{{Name: "projectId", Example: "local"}},
				},
			})

			projReg.Register(r, routes.Route{
				Method: "PUT", Pattern: "/",
				Handler: h.UpdateProject,
				Meta: routes.Meta{
					Group:       "Projects",
					Description: "Update project",
					Params:      []routes.Param{{Name: "projectId", Example: "local"}},
					Body:        map[string]any{"name": "Updated Name"},
				},
			})

			projReg.Register(r, routes.Route{
				Method: "DELETE", Pattern: "/",
				Handler: h.DeleteProject,
				Meta: routes.Meta{
					Group:       "Projects",
					Description: "Delete project",
					Params:      []routes.Param{{Name: "projectId", Example: "local"}},
				},
			})

			// Members
			projReg.Register(r, routes.Route{
				Method: "GET", Pattern: "/members",
				Handler: h.ListProjectMembers,
				Meta: routes.Meta{
					Group:       "Members",
					Description: "List members",
					Params:      []routes.Param{{Name: "projectId", Example: "local"}},
				},
			})

			projReg.Register(r, routes.Route{
				Method: "DELETE", Pattern: "/members/{userId}",
				Handler: h.RemoveProjectMember,
				Meta: routes.Meta{
					Group:       "Members",
					Description: "Remove member",
					Params:      []routes.Param{{Name: "projectId", Example: "local"}},
				},
			})

			// Invitations
			projReg.Register(r, routes.Route{
				Method: "POST", Pattern: "/invitations",
				Handler: h.CreateInvitation,
				Meta: routes.Meta{
					Group:       "Members",
					Description: "Create invitation",
					Params:      []routes.Param{{Name: "projectId", Example: "local"}},
					Body:        map[string]any{"email": "user@example.com", "role": "member"},
				},
			})

			projReg.Register(r, routes.Route{
				Method: "POST", Pattern: "/invitations/{token}/accept",
				Handler: h.AcceptInvitation,
				Meta: routes.Meta{
					Group:       "Members",
					Description: "Accept invitation",
					Params:      []routes.Param{{Name: "projectId", Example: "local"}},
				},
			})

			// Cache Volumes
			projReg.Register(r, routes.Route{
				Method: "GET", Pattern: "/cache",
				Handler: h.ListProjectCacheVolumes,
				Meta: routes.Meta{
					Group:       "Cache",
					Description: "List cache volumes for project",
					Params:      []routes.Param{{Name: "projectId", Example: "local"}},
				},
			})

			projReg.Register(r, routes.Route{
				Method: "DELETE", Pattern: "/cache",
				Handler: h.DeleteProjectCacheVolume,
				Meta: routes.Meta{
					Group:       "Cache",
					Description: "Delete cache volume for project (clears all caches)",
					Params:      []routes.Param{{Name: "projectId", Example: "local"}},
				},
			})

			projReg.Register(r, routes.Route{
				Method: "GET", Pattern: "/models",
				Handler: h.GetProjectModels,
				Meta: routes.Meta{
					Group:       "Models",
					Description: "Get available models for project credentials",
					Params:      []routes.Param{{Name: "projectId", Example: "local"}},
				},
			})

			// Workspaces
			r.Route("/workspaces", func(r chi.Router) {
				wsReg := projReg.WithPrefix("/workspaces")

				wsReg.Register(r, routes.Route{
					Method: "GET", Pattern: "/providers",
					Handler: h.GetProviders,
					Meta: routes.Meta{
						Group:       "Providers",
						Description: "List sandbox providers with status",
						Params:      []routes.Param{{Name: "projectId", Example: "local"}},
					},
				})

				wsReg.Register(r, routes.Route{
					Method: "GET", Pattern: "/providers/{provider}",
					Handler: h.GetProvider,
					Meta: routes.Meta{
						Group:       "Providers",
						Description: "Get sandbox provider status",
						Params: []routes.Param{
							{Name: "projectId", Example: "local"},
							{Name: "provider", Example: "vz"},
						},
					},
				})

				wsReg.Register(r, routes.Route{
					Method: "GET", Pattern: "/",
					Handler: h.ListWorkspaces,
					Meta: routes.Meta{
						Group:       "Workspaces",
						Description: "List workspaces",
						Params:      []routes.Param{{Name: "projectId", Example: "local"}},
					},
				})

				wsReg.Register(r, routes.Route{
					Method: "POST", Pattern: "/",
					Handler: h.CreateWorkspace,
					Meta: routes.Meta{
						Group:       "Workspaces",
						Description: "Create workspace",
						Params:      []routes.Param{{Name: "projectId", Example: "local"}},
						Body:        map[string]any{"name": "My Workspace", "path": "/home/user/code", "source_type": "local"},
					},
				})

				wsReg.Register(r, routes.Route{
					Method: "POST", Pattern: "/validate",
					Handler: h.ValidateWorkspace,
					Meta: routes.Meta{
						Group:       "Workspaces",
						Description: "Validate workspace input",
						Params:      []routes.Param{{Name: "projectId", Example: "local"}},
						Body:        map[string]any{"path": "~/projects/my-app", "sourceType": "local"},
					},
				})

				wsReg.Register(r, routes.Route{
					Method: "GET", Pattern: "/{workspaceId}",
					Handler: h.GetWorkspace,
					Meta: routes.Meta{
						Group:       "Workspaces",
						Description: "Get workspace",
						Params:      []routes.Param{{Name: "projectId", Example: "local"}},
					},
				})

				wsReg.Register(r, routes.Route{
					Method: "PUT", Pattern: "/{workspaceId}",
					Handler: h.UpdateWorkspace,
					Meta: routes.Meta{
						Group:       "Workspaces",
						Description: "Update workspace",
						Params:      []routes.Param{{Name: "projectId", Example: "local"}},
						Body:        map[string]any{"name": "Updated Name"},
					},
				})

				wsReg.Register(r, routes.Route{
					Method: "DELETE", Pattern: "/{workspaceId}",
					Handler: h.DeleteWorkspace,
					Meta: routes.Meta{
						Group:       "Workspaces",
						Description: "Delete workspace",
						Params:      []routes.Param{{Name: "projectId", Example: "local"}},
					},
				})

				// Git operations
				wsReg.Register(r, routes.Route{
					Method: "GET", Pattern: "/{workspaceId}/git/status",
					Handler: h.GetWorkspaceGitStatus,
					Meta: routes.Meta{
						Group:       "Git",
						Description: "Get git status",
						Params:      []routes.Param{{Name: "projectId", Example: "local"}},
					},
				})

				wsReg.Register(r, routes.Route{
					Method: "POST", Pattern: "/{workspaceId}/git/fetch",
					Handler: h.FetchWorkspace,
					Meta: routes.Meta{
						Group:       "Git",
						Description: "Fetch from remote",
						Params:      []routes.Param{{Name: "projectId", Example: "local"}},
					},
				})

				wsReg.Register(r, routes.Route{
					Method: "POST", Pattern: "/{workspaceId}/git/checkout",
					Handler: h.CheckoutWorkspace,
					Meta: routes.Meta{
						Group:       "Git",
						Description: "Checkout branch/ref",
						Params:      []routes.Param{{Name: "projectId", Example: "local"}},
						Body:        map[string]any{"ref": "main"},
					},
				})

				wsReg.Register(r, routes.Route{
					Method: "GET", Pattern: "/{workspaceId}/git/branches",
					Handler: h.GetWorkspaceBranches,
					Meta: routes.Meta{
						Group:       "Git",
						Description: "List branches",
						Params:      []routes.Param{{Name: "projectId", Example: "local"}},
					},
				})

				wsReg.Register(r, routes.Route{
					Method: "GET", Pattern: "/{workspaceId}/git/diff",
					Handler: h.GetWorkspaceDiff,
					Meta: routes.Meta{
						Group:       "Git",
						Description: "Get diff",
						Params: []routes.Param{
							{Name: "projectId", Example: "local"},
							{Name: "base", In: "query", Example: "HEAD~1"},
							{Name: "target", In: "query", Example: "HEAD"},
						},
					},
				})

				wsReg.Register(r, routes.Route{
					Method: "GET", Pattern: "/{workspaceId}/git/files",
					Handler: h.GetWorkspaceFileTree,
					Meta: routes.Meta{
						Group:       "Git",
						Description: "Get file tree",
						Params: []routes.Param{
							{Name: "projectId", Example: "local"},
							{Name: "ref", In: "query", Example: "HEAD"},
						},
					},
				})

				wsReg.Register(r, routes.Route{
					Method: "GET", Pattern: "/{workspaceId}/git/file",
					Handler: h.GetWorkspaceFileContent,
					Meta: routes.Meta{
						Group:       "Git",
						Description: "Get file content",
						Params: []routes.Param{
							{Name: "projectId", Example: "local"},
							{Name: "path", In: "query", Required: true, Example: "README.md"},
							{Name: "ref", In: "query", Example: "HEAD"},
						},
					},
				})

				wsReg.Register(r, routes.Route{
					Method: "POST", Pattern: "/{workspaceId}/git/file",
					Handler: h.WriteWorkspaceFile,
					Meta: routes.Meta{
						Group:       "Git",
						Description: "Write file",
						Params:      []routes.Param{{Name: "projectId", Example: "local"}},
						Body:        map[string]any{"path": "README.md", "content": "# Hello"},
					},
				})

				wsReg.Register(r, routes.Route{
					Method: "POST", Pattern: "/{workspaceId}/git/stage",
					Handler: h.StageWorkspaceFiles,
					Meta: routes.Meta{
						Group:       "Git",
						Description: "Stage files",
						Params:      []routes.Param{{Name: "projectId", Example: "local"}},
						Body:        map[string]any{"paths": []string{"README.md"}},
					},
				})

				wsReg.Register(r, routes.Route{
					Method: "POST", Pattern: "/{workspaceId}/git/commit",
					Handler: h.CommitWorkspace,
					Meta: routes.Meta{
						Group:       "Git",
						Description: "Commit changes",
						Params:      []routes.Param{{Name: "projectId", Example: "local"}},
						Body:        map[string]any{"message": "Initial commit"},
					},
				})

				wsReg.Register(r, routes.Route{
					Method: "GET", Pattern: "/{workspaceId}/git/log",
					Handler: h.GetWorkspaceLog,
					Meta: routes.Meta{
						Group:       "Git",
						Description: "Get commit log",
						Params: []routes.Param{
							{Name: "projectId", Example: "local"},
							{Name: "limit", In: "query", Example: "10"},
						},
					},
				})
			})

			// Sessions (direct access)
			r.Route("/sessions", func(r chi.Router) {
				sessReg := projReg.WithPrefix("/sessions")

				sessReg.Register(r, routes.Route{
					Method: "GET", Pattern: "/",
					Handler: h.ListSessions,
					Meta: routes.Meta{
						Group:       "Sessions",
						Description: "List sessions",
						Params:      []routes.Param{{Name: "projectId", Example: "local"}},
					},
				})

				sessReg.Register(r, routes.Route{
					Method: "POST", Pattern: "/",
					Handler: h.CreateSession,
					Meta: routes.Meta{
						Group:       "Sessions",
						Description: "Create session (without chat message)",
						Params:      []routes.Param{{Name: "projectId", Example: "local"}},
						Body:        map[string]any{"id": "abc123", "agentId": "", "workspaceId": "optional"},
					},
				})

				// All session-specific routes are nested under /{sessionId} so
				// the SessionBelongsToProject middleware can validate ownership
				// before any handler runs.
				r.Route("/{sessionId}", func(r chi.Router) {
					r.Use(middleware.SessionBelongsToProject(s))
					sidReg := sessReg.WithPrefix("/{sessionId}")

					sidReg.Register(r, routes.Route{
						Method: "GET", Pattern: "/",
						Handler: h.GetSession,
						Meta: routes.Meta{
							Group:       "Sessions",
							Description: "Get session",
							Params:      []routes.Param{{Name: "projectId", Example: "local"}, {Name: "sessionId", Example: "abc123"}},
						},
					})

					sidReg.Register(r, routes.Route{
						Method: "PUT", Pattern: "/",
						Handler: h.UpdateSession,
						Meta: routes.Meta{
							Group:       "Sessions",
							Description: "Update session",
							Params:      []routes.Param{{Name: "projectId", Example: "local"}, {Name: "sessionId", Example: "abc123"}},
							Body:        map[string]any{"name": "Updated Session", "status": "stopped"},
						},
					})

					sidReg.Register(r, routes.Route{
						Method: "PATCH", Pattern: "/",
						Handler: h.UpdateSession,
						Meta: routes.Meta{
							Group:       "Sessions",
							Description: "Patch session (partial update)",
							Params:      []routes.Param{{Name: "projectId", Example: "local"}, {Name: "sessionId", Example: "abc123"}},
							Body:        map[string]any{"displayName": "My Custom Name"},
						},
					})

					sidReg.Register(r, routes.Route{
						Method: "DELETE", Pattern: "/",
						Handler: h.DeleteSession,
						Meta: routes.Meta{
							Group:       "Sessions",
							Description: "Delete session",
							Params:      []routes.Param{{Name: "projectId", Example: "local"}, {Name: "sessionId", Example: "abc123"}},
						},
					})

					sidReg.Register(r, routes.Route{
						Method: "POST", Pattern: "/commit",
						Handler: h.CommitSession,
						Meta: routes.Meta{
							Group:       "Sessions",
							Description: "Commit session changes",
							Params:      []routes.Param{{Name: "projectId", Example: "local"}, {Name: "sessionId", Example: "abc123"}},
						},
					})

					sidReg.Register(r, routes.Route{
						Method: "POST", Pattern: "/rebase",
						Handler: h.RebaseSession,
						Meta: routes.Meta{
							Group:       "Sessions",
							Description: "Rebase session changes onto workspace HEAD",
							Params:      []routes.Param{{Name: "projectId", Example: "local"}, {Name: "sessionId", Example: "abc123"}},
						},
					})

					sidReg.Register(r.With(middleware.EnvSetsOwnedByProject(s)), routes.Route{
						Method: "PUT", Pattern: "/env-set",
						Handler: h.SetSessionActiveEnvSet,
						Meta: routes.Meta{
							Group:       "Sessions",
							Description: "Set active env sets for session",
							Params:      []routes.Param{{Name: "projectId", Example: "local"}, {Name: "sessionId", Example: "abc123"}},
							Body:        map[string]any{"envSetIds": []string{"abc123"}},
						},
					})

					sidReg.Register(r, routes.Route{
						Method: "GET", Pattern: "/files",
						Handler: h.ListSessionFiles,
						Meta: routes.Meta{
							Group:       "Files",
							Description: "List session files",
							Params: []routes.Param{
								{Name: "projectId", Example: "local"},
								{Name: "sessionId", Example: "abc123"},
								{Name: "path", In: "query", Example: "."},
								{Name: "hidden", In: "query", Example: "true"},
							},
						},
					})

					sidReg.Register(r, routes.Route{
						Method: "GET", Pattern: "/files/search",
						Handler: h.SearchSessionFiles,
						Meta: routes.Meta{
							Group:       "Files",
							Description: "Fuzzy-search session workspace files",
							Params: []routes.Param{
								{Name: "projectId", Example: "local"},
								{Name: "sessionId", Example: "abc123"},
								{Name: "q", In: "query", Example: "button"},
								{Name: "limit", In: "query", Example: "50"},
							},
						},
					})

					sidReg.Register(r, routes.Route{
						Method: "GET", Pattern: "/files/read",
						Handler: h.ReadSessionFile,
						Meta: routes.Meta{
							Group:       "Files",
							Description: "Read session file",
							Params: []routes.Param{
								{Name: "projectId", Example: "local"},
								{Name: "sessionId", Example: "abc123"},
								{Name: "path", In: "query", Required: true, Example: "README.md"},
							},
						},
					})

					sidReg.Register(r, routes.Route{
						Method: "PUT", Pattern: "/files/write",
						Handler: h.WriteSessionFile,
						Meta: routes.Meta{
							Group:       "Files",
							Description: "Write session file",
							Params: []routes.Param{
								{Name: "projectId", Example: "local"},
								{Name: "sessionId", Example: "abc123"},
							},
							Body: map[string]any{"path": "README.md", "content": "# Hello"},
						},
					})

					sidReg.Register(r, routes.Route{
						Method: "POST", Pattern: "/files/delete",
						Handler: h.DeleteSessionFile,
						Meta: routes.Meta{
							Group:       "Files",
							Description: "Delete session file or directory",
							Params: []routes.Param{
								{Name: "projectId", Example: "local"},
								{Name: "sessionId", Example: "abc123"},
							},
							Body: map[string]any{"path": "old-file.txt"},
						},
					})

					sidReg.Register(r, routes.Route{
						Method: "POST", Pattern: "/files/rename",
						Handler: h.RenameSessionFile,
						Meta: routes.Meta{
							Group:       "Files",
							Description: "Rename/move session file or directory",
							Params: []routes.Param{
								{Name: "projectId", Example: "local"},
								{Name: "sessionId", Example: "abc123"},
							},
							Body: map[string]any{"oldPath": "old-name.txt", "newPath": "new-name.txt"},
						},
					})

					sidReg.Register(r, routes.Route{
						Method: "GET", Pattern: "/diff",
						Handler: h.GetSessionDiff,
						Meta: routes.Meta{
							Group:       "Files",
							Description: "Get session diff",
							Params: []routes.Param{
								{Name: "projectId", Example: "local"},
								{Name: "sessionId", Example: "abc123"},
								{Name: "path", In: "query", Example: "README.md"},
								{Name: "format", In: "query", Example: "files"},
							},
						},
					})

					sidReg.Register(r, routes.Route{
						Method: "GET", Pattern: "/messages",
						Handler: h.ListMessages,
						Meta: routes.Meta{
							Group:       "Sessions",
							Description: "List messages",
							Params:      []routes.Param{{Name: "projectId", Example: "local"}, {Name: "sessionId", Example: "abc123"}},
						},
					})

					sidReg.Register(r, routes.Route{
						Method: "GET", Pattern: "/threads",
						Handler: h.ListThreads,
						Meta: routes.Meta{
							Group:       "Threads",
							Description: "List session threads",
							Params:      []routes.Param{{Name: "projectId", Example: "local"}, {Name: "sessionId", Example: "abc123"}},
						},
					})

					sidReg.Register(r, routes.Route{
						Method: "POST", Pattern: "/threads",
						Handler: h.CreateThread,
						Meta: routes.Meta{
							Group:       "Threads",
							Description: "Create session thread",
							Params:      []routes.Param{{Name: "projectId", Example: "local"}, {Name: "sessionId", Example: "abc123"}},
							Body:        map[string]any{"id": "thread-1", "name": "Thread 1"},
						},
					})

					sidReg.Register(r, routes.Route{
						Method: "GET", Pattern: "/threads/{threadId}",
						Handler: h.GetThread,
						Meta: routes.Meta{
							Group:       "Threads",
							Description: "Get session thread",
							Params:      []routes.Param{{Name: "projectId", Example: "local"}, {Name: "sessionId", Example: "abc123"}, {Name: "threadId", Example: "thread-1"}},
						},
					})

					sidReg.Register(r, routes.Route{
						Method: "GET", Pattern: "/threads/{threadId}/messages",
						Handler: h.ListThreadMessages,
						Meta: routes.Meta{
							Group:       "Threads",
							Description: "List messages for a specific session thread",
							Params:      []routes.Param{{Name: "projectId", Example: "local"}, {Name: "sessionId", Example: "abc123"}, {Name: "threadId", Example: "thread-1"}},
						},
					})

					sidReg.Register(r, routes.Route{
						Method: "PUT", Pattern: "/threads/{threadId}",
						Handler: h.UpdateThread,
						Meta: routes.Meta{
							Group:       "Threads",
							Description: "Update session thread",
							Params:      []routes.Param{{Name: "projectId", Example: "local"}, {Name: "sessionId", Example: "abc123"}, {Name: "threadId", Example: "thread-1"}},
							Body:        map[string]any{"name": "Renamed thread"},
						},
					})

					sidReg.Register(r, routes.Route{
						Method: "PATCH", Pattern: "/threads/{threadId}",
						Handler: h.UpdateThread,
						Meta: routes.Meta{
							Group:       "Threads",
							Description: "Patch session thread",
							Params:      []routes.Param{{Name: "projectId", Example: "local"}, {Name: "sessionId", Example: "abc123"}, {Name: "threadId", Example: "thread-1"}},
							Body:        map[string]any{"name": "Renamed thread"},
						},
					})

					sidReg.Register(r, routes.Route{
						Method: "DELETE", Pattern: "/threads/{threadId}",
						Handler: h.DeleteThread,
						Meta: routes.Meta{
							Group:       "Threads",
							Description: "Delete session thread",
							Params:      []routes.Param{{Name: "projectId", Example: "local"}, {Name: "sessionId", Example: "abc123"}, {Name: "threadId", Example: "thread-1"}},
						},
					})

					sidReg.Register(r, routes.Route{
						Method: "POST", Pattern: "/threads/{threadId}/chat",
						Handler: h.Chat,
						Meta: routes.Meta{
							Group:       "Chat",
							Description: "Start chat for a specific session thread",
							Params:      []routes.Param{{Name: "projectId", Example: "local"}, {Name: "sessionId", Example: "abc123"}, {Name: "threadId", Example: "thread-1"}},
							Body:        map[string]any{"sessionId": "abc123", "threadId": "thread-1", "messages": []map[string]any{{"role": "user", "content": "Hello"}}},
						},
					})

					sidReg.Register(r, routes.Route{
						Method: "GET", Pattern: "/threads/{threadId}/stream",
						Handler: h.ChatStream,
						Meta: routes.Meta{
							Group:       "Chat",
							Description: "Resume chat stream for a specific session thread",
							Params:      []routes.Param{{Name: "projectId", Example: "local"}, {Name: "sessionId", Example: "abc123"}, {Name: "threadId", Example: "thread-1"}, {Name: "replay", In: "query", Example: "true"}},
						},
					})

					sidReg.Register(r, routes.Route{
						Method: "POST", Pattern: "/threads/{threadId}/cancel",
						Handler: h.ChatCancel,
						Meta: routes.Meta{
							Group:       "Chat",
							Description: "Cancel chat completion for a specific session thread",
							Params:      []routes.Param{{Name: "projectId", Example: "local"}, {Name: "sessionId", Example: "abc123"}, {Name: "threadId", Example: "thread-1"}},
						},
					})

					sidReg.Register(r, routes.Route{
						Method: "GET", Pattern: "/threads/{threadId}/question/{questionId}",
						Handler: h.ChatQuestion,
						Meta: routes.Meta{
							Group:       "Chat",
							Description: "Get pending AskUserQuestion for a specific session thread",
							Params:      []routes.Param{{Name: "projectId", Example: "local"}, {Name: "sessionId", Example: "abc123"}, {Name: "threadId", Example: "thread-1"}, {Name: "questionId", Example: "tool-use-id"}},
						},
					})

					sidReg.Register(r, routes.Route{
						Method: "POST", Pattern: "/threads/{threadId}/answer/{questionId}",
						Handler: h.ChatAnswer,
						Meta: routes.Meta{
							Group:       "Chat",
							Description: "Submit answers to a pending AskUserQuestion for a specific session thread",
							Params:      []routes.Param{{Name: "projectId", Example: "local"}, {Name: "sessionId", Example: "abc123"}, {Name: "threadId", Example: "thread-1"}, {Name: "questionId", Example: "tool-use-id"}},
						},
					})

					// Terminal (session-specific)
					sidReg.Register(r, routes.Route{
						Method: "GET", Pattern: "/terminal/ws",
						Handler: h.TerminalWebSocket,
						Meta: routes.Meta{
							Group:       "Terminal",
							Description: "Terminal WebSocket",
							Params:      []routes.Param{{Name: "projectId", Example: "local"}, {Name: "sessionId", Example: "abc123"}},
						},
					})

					sidReg.Register(r, routes.Route{
						Method: "GET", Pattern: "/terminal/history",
						Handler: h.GetTerminalHistory,
						Meta: routes.Meta{
							Group:       "Terminal",
							Description: "Terminal history",
							Params:      []routes.Param{{Name: "projectId", Example: "local"}, {Name: "sessionId", Example: "abc123"}},
						},
					})

					sidReg.Register(r, routes.Route{
						Method: "GET", Pattern: "/terminal/status",
						Handler: h.GetTerminalStatus,
						Meta: routes.Meta{
							Group:       "Terminal",
							Description: "Terminal status",
							Params:      []routes.Param{{Name: "projectId", Example: "local"}, {Name: "sessionId", Example: "abc123"}},
						},
					})

					// Hooks
					sidReg.Register(r, routes.Route{
						Method: "GET", Pattern: "/hooks/status",
						Handler: h.GetHooksStatus,
						Meta: routes.Meta{
							Group:       "Hooks",
							Description: "Get hook evaluation status",
							Params:      []routes.Param{{Name: "projectId", Example: "local"}, {Name: "sessionId", Example: "abc123"}},
						},
					})

					sidReg.Register(r, routes.Route{
						Method: "GET", Pattern: "/hooks/{hookId}/output",
						Handler: h.GetHookOutput,
						Meta: routes.Meta{
							Group:       "Hooks",
							Description: "Get hook output log",
							Params:      []routes.Param{{Name: "projectId", Example: "local"}, {Name: "sessionId", Example: "abc123"}, {Name: "hookId", Example: "biome-check"}},
						},
					})

					sidReg.Register(r, routes.Route{
						Method: "POST", Pattern: "/hooks/{hookId}/rerun",
						Handler: h.RerunHook,
						Meta: routes.Meta{
							Group:       "Hooks",
							Description: "Rerun a hook",
							Params:      []routes.Param{{Name: "projectId", Example: "local"}, {Name: "sessionId", Example: "abc123"}, {Name: "hookId", Example: "biome-check"}},
						},
					})

					// Services
					sidReg.Register(r, routes.Route{
						Method: "GET", Pattern: "/services",
						Handler: h.ListServices,
						Meta: routes.Meta{
							Group:       "Services",
							Description: "List services",
							Params:      []routes.Param{{Name: "projectId", Example: "local"}, {Name: "sessionId", Example: "abc123"}},
						},
					})

					sidReg.Register(r, routes.Route{
						Method: "POST", Pattern: "/services/{serviceId}/start",
						Handler: h.StartService,
						Meta: routes.Meta{
							Group:       "Services",
							Description: "Start service",
							Params:      []routes.Param{{Name: "projectId", Example: "local"}, {Name: "sessionId", Example: "abc123"}, {Name: "serviceId", Example: "my-server"}},
						},
					})

					sidReg.Register(r, routes.Route{
						Method: "POST", Pattern: "/services/{serviceId}/stop",
						Handler: h.StopService,
						Meta: routes.Meta{
							Group:       "Services",
							Description: "Stop service",
							Params:      []routes.Param{{Name: "projectId", Example: "local"}, {Name: "sessionId", Example: "abc123"}, {Name: "serviceId", Example: "my-server"}},
						},
					})

					sidReg.Register(r, routes.Route{
						Method: "GET", Pattern: "/services/{serviceId}/output",
						Handler: h.GetServiceOutput,
						Meta: routes.Meta{
							Group:       "Services",
							Description: "Stream service output (SSE)",
							Params:      []routes.Param{{Name: "projectId", Example: "local"}, {Name: "sessionId", Example: "abc123"}, {Name: "serviceId", Example: "my-server"}},
						},
					})

					sidReg.Register(r, routes.Route{
						Method: "GET", Pattern: "/models",
						Handler: h.GetSessionModels,
						Meta: routes.Meta{
							Group:       "Sessions",
							Description: "Get available models for session",
							Params:      []routes.Param{{Name: "projectId", Example: "local"}, {Name: "sessionId", Example: "abc123"}},
						},
					})
				})
			})

			// Agents
			r.Route("/agents", func(r chi.Router) {
				agentReg := projReg.WithPrefix("/agents")

				agentReg.Register(r, routes.Route{
					Method: "GET", Pattern: "/",
					Handler: h.ListAgents,
					Meta: routes.Meta{
						Group:       "Agents",
						Description: "List agents",
						Params:      []routes.Param{{Name: "projectId", Example: "local"}},
					},
				})

				agentReg.Register(r, routes.Route{
					Method: "POST", Pattern: "/",
					Handler: h.CreateAgent,
					Meta: routes.Meta{
						Group:       "Agents",
						Description: "Create agent",
						Params:      []routes.Param{{Name: "projectId", Example: "local"}},
						Body:        map[string]any{"name": "My Agent", "agent_type": "claude-code"},
					},
				})

				agentReg.Register(r, routes.Route{
					Method: "GET", Pattern: "/types",
					Handler: h.GetAgentTypes,
					Meta: routes.Meta{
						Group:       "Agents",
						Description: "Get agent types",
						Params:      []routes.Param{{Name: "projectId", Example: "local"}},
					},
				})

				agentReg.Register(r, routes.Route{
					Method: "GET", Pattern: "/auth-providers",
					Handler: h.GetAuthProviders,
					Meta: routes.Meta{
						Group:       "Agents",
						Description: "Get auth providers",
						Params:      []routes.Param{{Name: "projectId", Example: "local"}},
					},
				})

				agentReg.Register(r, routes.Route{
					Method: "POST", Pattern: "/default",
					Handler: h.SetDefaultAgent,
					Meta: routes.Meta{
						Group:       "Agents",
						Description: "Set default agent",
						Params:      []routes.Param{{Name: "projectId", Example: "local"}},
						Body:        map[string]any{"agent_id": ""},
					},
				})

				agentReg.Register(r, routes.Route{
					Method: "GET", Pattern: "/{agentId}",
					Handler: h.GetAgent,
					Meta: routes.Meta{
						Group:       "Agents",
						Description: "Get agent",
						Params:      []routes.Param{{Name: "projectId", Example: "local"}},
					},
				})

				agentReg.Register(r, routes.Route{
					Method: "PUT", Pattern: "/{agentId}",
					Handler: h.UpdateAgent,
					Meta: routes.Meta{
						Group:       "Agents",
						Description: "Update agent",
						Params:      []routes.Param{{Name: "projectId", Example: "local"}},
						Body:        map[string]any{"name": "Updated Agent"},
					},
				})

				agentReg.Register(r, routes.Route{
					Method: "DELETE", Pattern: "/{agentId}",
					Handler: h.DeleteAgent,
					Meta: routes.Meta{
						Group:       "Agents",
						Description: "Delete agent",
						Params:      []routes.Param{{Name: "projectId", Example: "local"}},
					},
				})

				agentReg.Register(r, routes.Route{
					Method: "GET", Pattern: "/{agentId}/models",
					Handler: h.GetAgentModels,
					Meta: routes.Meta{
						Group:       "Agents",
						Description: "Get available models for agent",
						Params:      []routes.Param{{Name: "projectId", Example: "local"}, {Name: "agentId", Example: ""}},
					},
				})
			})

			// Suggestions
			projReg.Register(r, routes.Route{
				Method: "GET", Pattern: "/suggestions",
				Handler: h.GetSuggestions,
				Meta: routes.Meta{
					Group:       "Other",
					Description: "Get suggestions",
					Params: []routes.Param{
						{Name: "projectId", Example: "local"},
						{Name: "q", In: "query", Example: "/home"},
					},
				},
			})

			// Credentials
			r.Route("/credentials", func(r chi.Router) {
				credReg := projReg.WithPrefix("/credentials")

				credReg.Register(r, routes.Route{
					Method: "GET", Pattern: "/",
					Handler: h.ListCredentials,
					Meta: routes.Meta{
						Group:       "Credentials",
						Description: "List credentials",
						Params:      []routes.Param{{Name: "projectId", Example: "local"}},
					},
				})

				credReg.Register(r, routes.Route{
					Method: "POST", Pattern: "/",
					Handler: h.CreateCredential,
					Meta: routes.Meta{
						Group:       "Credentials",
						Description: "Create credential",
						Params:      []routes.Param{{Name: "projectId", Example: "local"}},
						Body:        map[string]any{"provider": "anthropic", "name": "My API Key", "api_key": "sk-..."},
					},
				})

				credReg.Register(r, routes.Route{
					Method: "GET", Pattern: "/{provider}",
					Handler: h.GetCredential,
					Meta: routes.Meta{
						Group:       "Credentials",
						Description: "Get credential",
						Params:      []routes.Param{{Name: "projectId", Example: "local"}, {Name: "provider", Example: "anthropic"}},
					},
				})

				credReg.Register(r, routes.Route{
					Method: "DELETE", Pattern: "/{provider}",
					Handler: h.DeleteCredential,
					Meta: routes.Meta{
						Group:       "Credentials",
						Description: "Delete credential",
						Params:      []routes.Param{{Name: "projectId", Example: "local"}},
					},
				})

				credReg.Register(r, routes.Route{
					Method: "POST", Pattern: "/{provider}/refresh",
					Handler: h.RefreshCredential,
					Meta: routes.Meta{
						Group:       "Credentials",
						Description: "Refresh OAuth tokens",
						Params:      []routes.Param{{Name: "projectId", Example: "local"}, {Name: "provider", Example: "anthropic"}},
					},
				})

				// Anthropic OAuth
				credReg.Register(r, routes.Route{
					Method: "POST", Pattern: "/anthropic/authorize",
					Handler: h.AnthropicAuthorize,
					Meta: routes.Meta{
						Group:       "Credentials",
						Description: "Anthropic OAuth authorize",
						Params:      []routes.Param{{Name: "projectId", Example: "local"}},
						Body:        map[string]any{"redirect_uri": "http://localhost:3000/callback"},
					},
				})

				credReg.Register(r, routes.Route{
					Method: "POST", Pattern: "/anthropic/exchange",
					Handler: h.AnthropicExchange,
					Meta: routes.Meta{
						Group:       "Credentials",
						Description: "Anthropic OAuth exchange",
						Params:      []routes.Param{{Name: "projectId", Example: "local"}},
						Body:        map[string]any{"code": "", "redirect_uri": "", "code_verifier": ""},
					},
				})

				// GitHub Copilot OAuth
				credReg.Register(r, routes.Route{
					Method: "POST", Pattern: "/github-copilot/device-code",
					Handler: h.GitHubCopilotDeviceCode,
					Meta: routes.Meta{
						Group:       "Credentials",
						Description: "GitHub Copilot device code",
						Params:      []routes.Param{{Name: "projectId", Example: "local"}},
					},
				})

				credReg.Register(r, routes.Route{
					Method: "POST", Pattern: "/github-copilot/poll",
					Handler: h.GitHubCopilotPoll,
					Meta: routes.Meta{
						Group:       "Credentials",
						Description: "GitHub Copilot poll",
						Params:      []routes.Param{{Name: "projectId", Example: "local"}},
						Body:        map[string]any{"device_code": ""},
					},
				})

				// GitHub OAuth (git operations: repo scope)
				credReg.Register(r, routes.Route{
					Method: "POST", Pattern: "/github-git/device-code",
					Handler: h.GitHubDeviceCode,
					Meta: routes.Meta{
						Group:       "Credentials",
						Description: "GitHub device code (git operations)",
						Params:      []routes.Param{{Name: "projectId", Example: "local"}},
					},
				})

				credReg.Register(r, routes.Route{
					Method: "POST", Pattern: "/github-git/poll",
					Handler: h.GitHubPoll,
					Meta: routes.Meta{
						Group:       "Credentials",
						Description: "GitHub poll (git operations)",
						Params:      []routes.Param{{Name: "projectId", Example: "local"}},
						Body:        map[string]any{"device_code": ""},
					},
				})

				// Codex OAuth
				credReg.Register(r, routes.Route{
					Method: "POST", Pattern: "/codex/authorize",
					Handler: h.CodexAuthorize,
					Meta: routes.Meta{
						Group:       "Credentials",
						Description: "Codex OAuth authorize",
						Params:      []routes.Param{{Name: "projectId", Example: "local"}},
						Body:        map[string]any{"redirect_uri": "http://localhost:3000/callback"},
					},
				})

				credReg.Register(r, routes.Route{
					Method: "POST", Pattern: "/codex/exchange",
					Handler: h.CodexExchange,
					Meta: routes.Meta{
						Group:       "Credentials",
						Description: "Codex OAuth exchange",
						Params:      []routes.Param{{Name: "projectId", Example: "local"}},
						Body:        map[string]any{"code": "", "redirect_uri": "", "code_verifier": ""},
					},
				})

				// MCP OAuth token storage (called by agent after completing OAuth exchange)
				credReg.Register(r, routes.Route{
					Method: "POST", Pattern: "/mcp",
					Handler: h.PostMCPToken,
					Meta: routes.Meta{
						Group:       "Credentials",
						Description: "Store MCP OAuth token (called by agent after OAuth exchange)",
						Params:      []routes.Param{{Name: "projectId", Example: "local"}},
						Body:        map[string]any{"url": "https://api.example.com/mcp", "accessToken": "...", "refreshToken": "...", "expiresAt": 0},
					},
				})
			})

			// Env Sets
			r.Route("/env-sets", func(r chi.Router) {
				envSetReg := projReg.WithPrefix("/env-sets")

				envSetReg.Register(r, routes.Route{
					Method: "GET", Pattern: "/",
					Handler: h.ListEnvSets,
					Meta: routes.Meta{
						Group:       "EnvSets",
						Description: "List env sets (metadata only)",
						Params:      []routes.Param{{Name: "projectId", Example: "local"}},
					},
				})

				envSetReg.Register(r, routes.Route{
					Method: "POST", Pattern: "/",
					Handler: h.CreateEnvSet,
					Meta: routes.Meta{
						Group:       "EnvSets",
						Description: "Create env set",
						Params:      []routes.Param{{Name: "projectId", Example: "local"}},
						Body:        map[string]any{"name": "Production", "envVars": map[string]string{"API_URL": "https://api.example.com"}},
					},
				})

				envSetReg.Register(r.With(middleware.EnvSetOwnedByProject(s)), routes.Route{
					Method: "GET", Pattern: "/{envSetId}",
					Handler: h.GetEnvSet,
					Meta: routes.Meta{
						Group:       "EnvSets",
						Description: "Get env set with decrypted vars",
						Params:      []routes.Param{{Name: "projectId", Example: "local"}, {Name: "envSetId", Example: "abc123"}},
					},
				})

				envSetReg.Register(r.With(middleware.EnvSetOwnedByProject(s)), routes.Route{
					Method: "PUT", Pattern: "/{envSetId}",
					Handler: h.UpdateEnvSet,
					Meta: routes.Meta{
						Group:       "EnvSets",
						Description: "Update env set",
						Params:      []routes.Param{{Name: "projectId", Example: "local"}, {Name: "envSetId", Example: "abc123"}},
						Body:        map[string]any{"name": "Production", "envVars": map[string]string{"API_URL": "https://api.example.com"}},
					},
				})

				envSetReg.Register(r.With(middleware.EnvSetOwnedByProject(s)), routes.Route{
					Method: "DELETE", Pattern: "/{envSetId}",
					Handler: h.DeleteEnvSet,
					Meta: routes.Meta{
						Group:       "EnvSets",
						Description: "Delete env set",
						Params:      []routes.Param{{Name: "projectId", Example: "local"}, {Name: "envSetId", Example: "abc123"}},
					},
				})
			})

			// TODO: Remove these legacy session-scoped chat routes after all clients
			// migrate to the thread-scoped /sessions/{sessionId}/threads/{threadId}/... APIs.
			// Chat endpoint
			projReg.Register(r, routes.Route{
				Method: "POST", Pattern: "/chat",
				Handler: h.Chat,
				Meta: routes.Meta{
					Group:       "Chat",
					Description: "DEPRECATED: Use /sessions/{sessionId}/threads/{threadId}/chat instead",
					Params:      []routes.Param{{Name: "projectId", Example: "local"}},
					Body:        map[string]any{"messages": []map[string]any{{"role": "user", "content": "Hello"}}},
				},
			})

			// Chat stream resume endpoint
			projReg.Register(r, routes.Route{
				Method: "GET", Pattern: "/chat/{sessionId}/stream",
				Handler: h.ChatStream,
				Meta: routes.Meta{
					Group:       "Chat",
					Description: "DEPRECATED: Use /sessions/{sessionId}/threads/{threadId}/stream instead",
					Params: []routes.Param{
						{Name: "projectId", Example: "local"},
						{Name: "sessionId", Example: "abc123"},
						{Name: "replay", In: "query", Example: "true"},
					},
				},
			})

			// Chat cancel endpoint
			projReg.Register(r, routes.Route{
				Method: "POST", Pattern: "/chat/{sessionId}/cancel",
				Handler: h.ChatCancel,
				Meta: routes.Meta{
					Group:       "Chat",
					Description: "DEPRECATED: Use /sessions/{sessionId}/threads/{threadId}/cancel instead",
					Params: []routes.Param{
						{Name: "projectId", Example: "local"},
						{Name: "sessionId", Example: "abc123"},
					},
				},
			})

			// Chat question endpoint - poll for pending AskUserQuestion
			projReg.Register(r, routes.Route{
				Method: "GET", Pattern: "/chat/{sessionId}/question/{questionId}",
				Handler: h.ChatQuestion,
				Meta: routes.Meta{
					Group:       "Chat",
					Description: "DEPRECATED: Use /sessions/{sessionId}/threads/{threadId}/question/{questionId} instead",
					Params: []routes.Param{
						{Name: "projectId", Example: "local"},
						{Name: "sessionId", Example: "abc123"},
						{Name: "questionId", Example: "tool-use-id"},
					},
				},
			})

			// Chat answer endpoint - submit answer to pending AskUserQuestion
			projReg.Register(r, routes.Route{
				Method: "POST", Pattern: "/chat/{sessionId}/answer/{questionId}",
				Handler: h.ChatAnswer,
				Meta: routes.Meta{
					Group:       "Chat",
					Description: "DEPRECATED: Use /sessions/{sessionId}/threads/{threadId}/answer/{questionId} instead",
					Params: []routes.Param{
						{Name: "projectId", Example: "local"},
						{Name: "sessionId", Example: "abc123"},
						{Name: "questionId", Example: "tool-use-id"},
					},
				},
			})
		})
	})

	// Start debug Docker proxy if enabled
	var debugDockerServer *handler.DebugDockerServer
	if cfg.DebugDocker {
		var err error
		debugDockerServer, err = handler.NewDebugDockerServer(sandboxManager, "local", cfg.DebugDockerPort)
		if err != nil {
			log.Printf("Warning: Failed to create debug Docker proxy: %v", err)
		} else {
			debugDockerServer.Start()
		}
	}

	// Create server
	// Note: No timeouts set - SSE endpoints need long-lived connections
	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Port),
		Handler: r,
	}

	// Start server in a goroutine
	go func() {
		log.Printf("Server starting on port %d", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	// Wait for interrupt signal or stdin close
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	if cfg.StdinKeepalive {
		go func() {
			// Block reading stdin; when the parent process dies, the pipe
			// breaks and Read returns. Send SIGTERM to trigger graceful shutdown.
			buf := make([]byte, 1)
			for {
				if _, err := os.Stdin.Read(buf); err != nil {
					log.Println("Stdin closed, shutting down (parent process died)")
					quit <- syscall.SIGTERM
					return
				}
			}
		}()
	}

	<-quit

	// Hard deadline: if graceful shutdown takes longer than 10s, force exit.
	go func() {
		time.Sleep(10 * time.Second)
		log.Println("Graceful shutdown timed out, forcing exit")
		os.Exit(1)
	}()

	log.Println("Shutting down server...")

	// Stop debug Docker proxy
	if debugDockerServer != nil {
		debugDockerServer.Stop()
	}

	// Stop sandbox watcher
	if sandboxWatcherCancel != nil {
		sandboxWatcherCancel()
	}

	// Shutdown sandbox manager (gracefully stop all VMs and providers)
	if sandboxManager != nil {
		log.Println("Shutting down sandbox providers...")
		sandboxManager.Shutdown()
		log.Println("Sandbox providers stopped")
	}

	// Stop session status poller
	if sessionStatusPoller != nil {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := sessionStatusPoller.Shutdown(shutdownCtx); err != nil {
			log.Printf("Warning: failed to stop session status poller: %v", err)
		}
		shutdownCancel()
	}

	// Stop sandbox idle monitor
	if sandboxIdleMonitor != nil {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := sandboxIdleMonitor.Shutdown(shutdownCtx); err != nil {
			log.Printf("Warning: failed to stop sandbox idle monitor: %v", err)
		}
		shutdownCancel()
	}

	// Stop SSH server
	if sshServer != nil {
		if err := sshServer.Stop(); err != nil {
			log.Printf("Warning: failed to stop SSH server: %v", err)
		}
	}

	// Stop dispatcher first (finish in-flight jobs)
	if disp != nil {
		disp.Stop()
	}

	// Stop event poller
	eventPoller.Stop()

	// Close handler resources (stops Codex callback server, etc.)
	h.Close()

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server stopped")
}

// sshUserInfoAdapter adapts SandboxService.GetClient to the ssh.UserInfoFetcher interface.
type sshUserInfoAdapter struct {
	svc *service.SandboxService
}

func (a *sshUserInfoAdapter) GetUserInfo(ctx context.Context, sessionID string) (string, int, int, error) {
	client, err := a.svc.GetClient(ctx, sessionID)
	if err != nil {
		return "", 0, 0, err
	}
	userInfo, err := client.GetUserInfo(ctx)
	if err != nil {
		return "", 0, 0, err
	}
	return userInfo.Username, userInfo.UID, userInfo.GID, nil
}
