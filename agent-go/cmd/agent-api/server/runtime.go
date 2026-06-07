// Package server implements the HTTP API server mode for agent-api.
package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"maps"
	"net/http"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"

	acpagent "github.com/obot-platform/discobot/agent-go/acp/agent"
	"github.com/obot-platform/discobot/agent-go/agent"
	"github.com/obot-platform/discobot/agent-go/agentimpl"
	"github.com/obot-platform/discobot/agent-go/assets"
	"github.com/obot-platform/discobot/agent-go/browser"
	"github.com/obot-platform/discobot/agent-go/internal/config"
	controlfeatures "github.com/obot-platform/discobot/agent-go/internal/controlfeatures"
	controlsocket "github.com/obot-platform/discobot/agent-go/internal/controlsocket"
	"github.com/obot-platform/discobot/agent-go/internal/credentials"
	"github.com/obot-platform/discobot/agent-go/internal/handler"
	"github.com/obot-platform/discobot/agent-go/internal/hooks"
	"github.com/obot-platform/discobot/agent-go/internal/middleware"
	"github.com/obot-platform/discobot/agent-go/internal/processes"
	"github.com/obot-platform/discobot/agent-go/internal/services"
	"github.com/obot-platform/discobot/agent-go/internal/sudoauth"
	"github.com/obot-platform/discobot/agent-go/internal/workspaceenv"
	"github.com/obot-platform/discobot/agent-go/message"
	"github.com/obot-platform/discobot/agent-go/promptqueue"
	"github.com/obot-platform/discobot/agent-go/providers"
	"github.com/obot-platform/discobot/agent-go/sessionconfig"
	"github.com/obot-platform/discobot/agent-go/thread"
	"github.com/obot-platform/discobot/agent-go/tools"
)

type runtimeInitialCredentials struct {
	Credentials  []credentials.EnvVar
	GitUserName  string
	GitUserEmail string
}

// agentRuntime holds the long-lived objects behind one configured agent API
// instance. Normal startup and bootstrap configuration both build this same
// runtime, then expose it through different outer HTTP servers.
type agentRuntime struct {
	cfg *config.Config

	credentials      *credentials.Manager
	providerRegistry *providers.ProviderRegistry
	threadStore      *thread.Store
	toolExecutor     *tools.Executor
	browserManager   *browser.Manager
	authorizer       *credentials.CredentialUseAuthorizer
	sudoAuthorizer   *credentials.SudoAuthorizer

	defaultAgent   *agentimpl.DefaultAgent
	acpAgent       *acpagent.Agent
	conversations  *agent.ConversationManager
	processManager *processes.Manager
	promptQueue    *promptqueue.Manager

	hookManager    *hooks.Manager
	serviceManager *services.Manager
	controlSocket  *controlsocket.Client
	stopControl    context.CancelFunc
	fileWatcher    *workspaceFileWatcher
	portWatcher    *workspacePortWatcher
}

// buildRuntimeHandler assembles the configured agent API and returns the
// handler plus its cleanup function.
func buildRuntimeHandler(cfg *config.Config, initialCreds runtimeInitialCredentials) (http.Handler, func(), func(func(string)), error) {
	runtime, err := newAgentRuntime(cfg, initialCreds)
	if err != nil {
		return nil, nil, nil, err
	}
	return runtime.routes(), runtime.close, runtime.runStartupTasks, nil
}

// newAgentRuntime wires the runtime in dependency order. The small init methods
// below keep the order readable without hiding the setup details.
func newAgentRuntime(cfg *config.Config, initialCreds runtimeInitialCredentials) (*agentRuntime, error) {
	if err := assets.InstallSystemScripts("/opt/discobot/scripts", cfg.WorkspaceSource); err != nil {
		log.Printf("discobot-agent-api: warning: failed to install embedded system scripts: %v", err)
	}

	runtime := &agentRuntime{cfg: cfg}
	runtime.initCredentials(initialCreds)
	runtime.initTools()
	if err := runtime.initBrowser(); err != nil {
		return nil, err
	}
	runtime.initAuthorizer()
	runtime.initAgent()
	runtime.initPromptQueue()
	runtime.initHooks()
	runtime.initServices()
	runtime.initControlSocket()

	return runtime, nil
}

func (r *agentRuntime) initCredentials(initialCreds runtimeInitialCredentials) {
	// Bootstrap mode can seed credentials before any request-scoped headers
	// exist; later requests may still refresh this manager through middleware.
	r.credentials = credentials.NewManager()
	r.credentials.ApplyEnvVars(initialCreds.Credentials, initialCreds.GitUserName, initialCreds.GitUserEmail)
	r.providerRegistry = providers.NewProviderRegistry(r.credentials)
	r.threadStore = thread.NewStore(r.cfg.ThreadsDir)
}

func (r *agentRuntime) initTools() {
	r.toolExecutor = tools.New(r.cfg.AgentCwd, r.cfg.DataDir, "")
	r.toolExecutor.SetThreadsDir(r.cfg.ThreadsDir)
	// Internal tool lookups may need credentials that are not exposed to the
	// shell environment. Bash receives only the visible snapshot configured
	// after browser state is available.
	r.toolExecutor.SetEnvLookup(func(key string) string {
		if cred := r.credentials.Get(key); cred != nil {
			return cred.Value
		}
		return ""
	})
}

func (r *agentRuntime) initBrowser() error {
	browserManager, err := browser.NewManager(r.cfg.SessionID, r.cfg.DataDir, r.cfg.Port)
	if err != nil {
		return fmt.Errorf("browser manager: %w", err)
	}

	r.browserManager = browserManager
	r.browserManager.SetStore(browser.NewStore(r.cfg.ThreadsDir))
	r.browserManager.SetCurrentTurnLoader(r.threadStore.LoadTurnState)
	r.toolExecutor.SetEnvForThread(r.browserManager.EnvForThread)
	// Shell commands inherit workspace .env values, agent-visible credentials,
	// and browser-specific variables such as the CDP endpoint.
	r.toolExecutor.SetEnvSnapshot(func() map[string]string {
		env := visibleEnvSnapshot(r.cfg.AgentCwd, r.credentials.Snapshot)
		maps.Copy(env, r.browserManager.Env())
		return env
	})
	return nil
}

func (r *agentRuntime) initAuthorizer() {
	// Credential use approval is itself model-backed, so it needs the provider
	// registry after credentials are available.
	r.authorizer = credentials.NewCredentialUseAuthorizer(
		credentialUseAuthorizerResolver{registry: r.providerRegistry},
		r.credentials,
		sessionconfig.CredentialUseAuthorizerSystemPrompt(),
	)
	r.sudoAuthorizer = credentials.NewSudoAuthorizer(r.authorizer, r.credentials)

	r.toolExecutor.SetCredentialUseAuthorizer(func(ctx context.Context, currentProviderID, toolCallID, command, description string, uses []tools.CredentialUseBinding) error {
		bindings := make([]credentials.CredentialUseBinding, 0, len(uses))
		for _, use := range uses {
			bindings = append(bindings, credentials.CredentialUseBinding{
				CredentialID: use.CredentialID,
				UseID:        use.UseID,
				EnvVar:       use.EnvVar,
			})
		}
		return r.authorizer.Authorize(ctx, currentProviderID, toolCallID, command, description, bindings)
	})

	r.toolExecutor.SetCredentialUseEnv(func(uses []tools.CredentialUseBinding) (map[string]string, error) {
		env := make(map[string]string, len(uses))
		for _, use := range uses {
			cred := r.credentials.SessionCredential(use.CredentialID)
			if cred == nil {
				return nil, fmt.Errorf("credential id %s is not available in this session", use.CredentialID)
			}
			if !cred.AgentVisible {
				return nil, fmt.Errorf("credential id %s is not visible to the agent in this session", use.CredentialID)
			}
			if cred.EnvVar != use.EnvVar {
				return nil, fmt.Errorf("credential id %s is not authorized for environment variable %s", use.CredentialID, use.EnvVar)
			}
			if existing, ok := env[use.EnvVar]; ok && existing != cred.Value {
				return nil, fmt.Errorf("multiple credential uses target environment variable %s", use.EnvVar)
			}
			env[use.EnvVar] = cred.Value
		}
		return env, nil
	})
}

func (r *agentRuntime) initAgent() {
	mcpConfig := agentimpl.NewMCPConfig(
		r.cfg.MCPOAuthRedirectBase,
		r.cfg.SessionID,
		r.cfg.DiscobotServerURL,
		r.cfg.DiscobotProjectID,
	)
	r.defaultAgent = agentimpl.NewDefaultAgent(
		r.threadStore,
		r.providerRegistry,
		r.toolExecutor,
		r.cfg.AgentCwd,
		mcpConfig,
	)

	// Spike: optionally route prompt execution through the official Claude Code
	// CLI over ACP so prompts run on a Claude subscription instead of the
	// built-in API-key agent. Thread management, MCP, and hooks stay on the
	// default agent (they share the same thread store); only the conversation
	// agent moves to ACP. Fall back to the default agent if the CLI can't start.
	var convAgent agent.Agent = r.defaultAgent
	if strings.TrimSpace(os.Getenv("DISCOBOT_AGENT_BACKEND")) == acpClaudeCodeBackend {
		if acpAgent, err := connectClaudeCodeACP(r.cfg.AgentCwd, r.threadStore); err != nil {
			log.Printf("discobot-agent-api: %s backend unavailable, using default agent: %v", acpClaudeCodeBackend, err)
		} else {
			r.acpAgent = acpAgent
			convAgent = acpAgent
			log.Printf("discobot-agent-api: routing prompts through the %s ACP backend", acpClaudeCodeBackend)
		}
	}

	r.conversations = agent.NewConversationManager(convAgent)
	r.processManager = processes.NewManager(r.cfg.AgentCwd)
}

func (r *agentRuntime) initPromptQueue() {
	queueStore := promptqueue.NewStore(r.cfg.ThreadsDir)
	r.promptQueue = promptqueue.NewManager(queueStore, r.conversations, nil)
	// Queue updates are surfaced through the same ephemeral stream as normal
	// thread updates so the UI stays in sync while prompts wait to run.
	r.promptQueue.SetChangeFunc(func(threadID string, queue []promptqueue.Prompt) {
		info, err := r.defaultAgent.GetThreadInfo(threadID)
		if err != nil {
			return
		}
		chunk := threadUpdateChunk(r.conversations, info, queue)
		emitThreadUpdate(r.conversations, threadID, chunk)
	})
}

func (r *agentRuntime) initHooks() {
	if !r.cfg.HooksEnabled {
		return
	}

	r.hookManager = hooks.NewManager(r.cfg.AgentCwd, r.cfg.SessionID, r.processManager)
	if err := r.hookManager.Init(); err != nil {
		log.Printf("warn: hooks init: %v", err)
	}
	r.hookManager.SetEnvSnapshot(func() map[string]string {
		return r.credentials.HooksSnapshot()
	})
	r.hookManager.SetChunkEmitter(func(chunk message.MessageChunk) {
		r.conversations.EmitEphemeralChunk(chunk)
	})
	r.hookManager.SetAIHookAgent(r.defaultAgent)
	r.hookManager.SetAIHookEvaluator(hookEvaluationResolver{registry: r.providerRegistry})
	r.hookManager.SetRepromptRunner(r.conversations, r.promptQueue)

	fileWatcher, err := startWorkspaceFileWatcher(r.cfg.AgentCwd, r.conversations.EmitEphemeralChunk)
	if err != nil {
		log.Printf("warn: workspace file watcher: %v", err)
	} else {
		r.fileWatcher = fileWatcher
	}

	portWatcher := startWorkspacePortWatcher(r.conversations.EmitEphemeralChunk)
	r.conversations.AddCompletionListener(portWatcher)
	r.portWatcher = portWatcher
}

func (r *agentRuntime) runStartupTasks(progress func(string)) {
	r.resumeInterruptedTurns(progress)
	r.runStartupHooks(progress)
}

func (r *agentRuntime) resumeInterruptedTurns(progress func(string)) {
	if r.conversations == nil {
		return
	}
	if progress != nil {
		progress("scheduling interrupted agent work recovery")
	}
	go func() {
		if err := r.conversations.ResumeInterruptedTurns(); err != nil {
			log.Printf("warn: resume interrupted turns: %v", err)
			if progress != nil {
				progress("failed to resume interrupted agent work")
			}
		}
	}()
}

func (r *agentRuntime) runStartupHooks(progress func(string)) {
	if r.hookManager == nil {
		return
	}
	// Session hooks are workspace-dependent, so bootstrap mode runs them only
	// after configure has cloned the workspace and built the runtime objects
	// needed to track hook status and process output.
	token, err := randomSudoBootstrapToken()
	if err != nil {
		log.Printf("warn: bootstrap sudo token: %v", err)
		wait := r.hookManager.RunSessionHooks(progress)
		go wait()
		return
	}
	r.sudoAuthorizer.RegisterBootstrapToken(token)
	r.hookManager.SetStartupHookEnv(func(hook hooks.Hook) map[string]string {
		return r.bootstrapSudoEnvForHookWithToken(hook, token)
	})
	wait := r.hookManager.RunSessionHooks(progress)
	r.hookManager.SetStartupHookEnv(nil)
	go func() {
		wait()
		r.sudoAuthorizer.RevokeBootstrapToken(token)
	}()
}

func (r *agentRuntime) bootstrapSudoEnvForHookWithToken(hook hooks.Hook, token string) map[string]string {
	if hook.RunAs != "root" {
		return nil
	}
	return map[string]string{
		sudoauth.TokenEnvVar:             token,
		"DISCOBOT_SUDO_RUNTIME":          "bootstrap",
		"DISCOBOT_SUDO_COMMAND":          hook.Path,
		"DISCOBOT_SUDO_BOOTSTRAP_REASON": "startup hook " + hook.Name,
	}
}

func randomSudoBootstrapToken() (string, error) {
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(token[:]), nil
}

func (r *agentRuntime) initServices() {
	r.serviceManager = services.NewManager(r.cfg.AgentCwd, r.processManager)
	r.serviceManager.SetEnvSnapshot(func() map[string]string {
		return r.credentials.ServicesSnapshot()
	})
}

func (r *agentRuntime) initControlSocket() {
	controlCtx, stopControl := context.WithCancel(context.Background())
	r.stopControl = stopControl

	if !r.cfg.EnableGitControlSocket {
		return
	}

	r.controlSocket = controlsocket.New()
	gitTunnel := controlfeatures.NewGitTunnel(r.controlSocket)
	gitRemoteURL, err := gitTunnel.StartEndpoint(controlCtx)
	if err != nil {
		log.Printf("warn: git control endpoint: %v", err)
		return
	}
	// Local workspaces use the control socket as a git remote so commits can be
	// pushed back through the host-side control plane.
	controlfeatures.ConfigureGitRemote(controlCtx, r.cfg.AgentCwd, gitRemoteURL, r.cfg.SessionID)
}

func (r *agentRuntime) routes() http.Handler {
	apiHandler := handler.New(
		r.cfg.AgentCwd,
		r.conversations,
		r.hookManager,
		r.serviceManager,
		r.defaultAgent,
		r.promptQueue,
		r.browserManager,
		r.processManager,
		r.sudoAuthorizer,
		r.controlSocket,
	)

	router := chi.NewRouter()

	authed := chi.NewRouter()
	authed.Use(middleware.Auth(r.cfg.SecretHash, r.cfg.TrustKey))
	authed.Use(middleware.Credentials(r.credentials, r.promptQueue.EnableTimers))
	apiHandler.RegisterRoutes(authed)
	router.Mount("/", authed)
	return router
}

// close releases runtime resources. The outer HTTP server owns connection
// draining; this only stops background runtime pieces.
func (r *agentRuntime) close() {
	if r.stopControl != nil {
		r.stopControl()
	}
	if r.fileWatcher != nil {
		if err := r.fileWatcher.Close(); err != nil {
			log.Printf("workspace file watcher shutdown: %v", err)
		}
	}
	if r.portWatcher != nil {
		r.portWatcher.Close()
	}
	if r.defaultAgent != nil {
		r.defaultAgent.Close()
	}
	if r.browserManager != nil {
		if err := r.browserManager.Close(); err != nil {
			log.Printf("browser shutdown: %v", err)
		}
	}
}

// visibleEnvSnapshot merges workspace .env values with the current set of
// credentials that are allowed to be visible to agent subprocesses.
func visibleEnvSnapshot(workspaceRoot string, envSnapshot func() map[string]string) map[string]string {
	env := workspaceenv.FileSnapshot(workspaceRoot)
	if env == nil {
		env = map[string]string{}
	}
	if envSnapshot == nil {
		return env
	}
	maps.Copy(env, envSnapshot())
	return env
}

type credentialUseAuthorizerResolver struct {
	registry *providers.ProviderRegistry
}

// ResolveAuthorizationModel chooses the model used to approve requested
// credential bindings for tool calls.
func (r credentialUseAuthorizerResolver) ResolveAuthorizationModel(currentProviderID string) (credentials.AuthorizationModelRef, error) {
	ref, err := r.registry.ResolveModelInProvider(currentProviderID, "", providers.ModelTaskAuthorization, providers.ModelTaskChat)
	if err != nil {
		return credentials.AuthorizationModelRef{}, err
	}
	return credentials.AuthorizationModelRef{ProviderID: ref.ProviderID, ModelID: ref.ModelID}, nil
}

// CompleteText adapts the provider registry to the credential authorizer's
// smaller text-completion interface.
func (r credentialUseAuthorizerResolver) CompleteText(ctx context.Context, model credentials.AuthorizationModelRef, messages []message.Message, maxTokens *int) (string, error) {
	provider, err := r.registry.Get(model.ProviderID)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for chunk, err := range provider.Complete(ctx, providers.CompleteRequest{
		Model:     providers.ModelRef{ProviderID: model.ProviderID, ModelID: model.ModelID},
		Messages:  messages,
		MaxTokens: maxTokens,
	}) {
		if err != nil {
			return "", err
		}
		switch delta := chunk.(type) {
		case message.TextDeltaChunk:
			b.WriteString(delta.Delta)
		}
	}
	return strings.TrimSpace(b.String()), nil
}

type hookEvaluationResolver struct {
	registry *providers.ProviderRegistry
}

// CompleteText adapts the provider registry to the hook evaluator's one-off
// text completion interface.
func (r hookEvaluationResolver) CompleteText(ctx context.Context, model string, messages []message.Message, maxTokens *int) (string, error) {
	ref, err := r.registry.ResolveModel(model, providers.ModelTaskAuthorization, providers.ModelTaskChat)
	if err != nil {
		return "", err
	}
	provider, err := r.registry.Get(ref.ProviderID)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for chunk, err := range provider.Complete(ctx, providers.CompleteRequest{
		Model:     ref,
		Messages:  messages,
		MaxTokens: maxTokens,
		Reasoning: providers.ReasoningNone,
	}) {
		if err != nil {
			return "", err
		}
		switch delta := chunk.(type) {
		case message.TextDeltaChunk:
			b.WriteString(delta.Delta)
		}
	}
	return strings.TrimSpace(b.String()), nil
}

func emitThreadUpdate(conversations *agent.ConversationManager, threadID string, chunk message.ThreadUpdateChunk) {
	if !conversations.EmitChunkIfActive(threadID, chunk) {
		conversations.EmitEphemeralChunk(chunk)
	}
}

func threadUpdateChunk(conversations *agent.ConversationManager, info agent.ThreadInfo, queue []promptqueue.Prompt) message.ThreadUpdateChunk {
	applyThreadStateOverlay(conversations, &info)
	chunk := message.ThreadUpdateChunk{
		Data: message.ThreadUpdateData{
			Thread: message.ThreadUpdateInfo{
				ID:            info.ID,
				Name:          info.Name,
				CWD:           info.CWD,
				LastMessage:   info.LastMessage,
				ErrorMessage:  info.ErrorMessage,
				Model:         info.Model,
				Reasoning:     info.Reasoning,
				ServiceTier:   info.ServiceTier,
				State:         string(info.State),
				ActiveCommand: info.ActiveCommand,
				Metadata:      info.Metadata,
			},
		},
	}
	chunk.Data.Thread.PromptQueue = promptqueue.ToThreadUpdateInfo(queue)
	return chunk
}

func applyThreadStateOverlay(conversations *agent.ConversationManager, info *agent.ThreadInfo) {
	if info == nil || conversations == nil || strings.TrimSpace(info.ID) == "" {
		return
	}
	if conversations.ActiveCompletionID(info.ID) != "" {
		return
	}
	if interrupted, err := conversations.HasInterruptedTurn(info.ID); err == nil && interrupted {
		info.State = agent.ThreadStateInterrupted
	}
}
