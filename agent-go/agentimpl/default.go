// Package agentimpl provides the default Agent implementation.
// The Agent interface and PromptRequest type live in the agent package;
// this package contains the concrete DefaultAgent that implements them.
package agentimpl

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"log"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/obot-platform/discobot/agent-go/agent"
	"github.com/obot-platform/discobot/agent-go/internal/api"
	agenthooks "github.com/obot-platform/discobot/agent-go/internal/hooks"
	"github.com/obot-platform/discobot/agent-go/mcp"
	"github.com/obot-platform/discobot/agent-go/message"
	"github.com/obot-platform/discobot/agent-go/providers"
	"github.com/obot-platform/discobot/agent-go/sessionconfig"
	"github.com/obot-platform/discobot/agent-go/thread"
	"github.com/obot-platform/discobot/modelsdev"
)

// MCPConfig holds MCP OAuth and connectivity settings.
type MCPConfig struct {
	redirectBase      string // base URL for OAuth callbacks (MCPOAuthRedirectBase)
	sessionID         string // session ID used in OAuth redirect URL
	discobotServerURL string // Discobot server URL for token persistence
	projectID         string // project ID for token persistence
}

// DefaultAgent is the built-in Agent implementation that uses the thread
// package for file-based persistence and the crash-resilient step loop.
type DefaultAgent struct {
	store    *thread.Store
	registry *providers.ProviderRegistry
	executor thread.ToolExecutor
	cwd      string // working directory for session config discovery
	mcpCfg   MCPConfig

	mu      sync.Mutex
	cancels map[string]context.CancelFunc

	mcpMu    sync.Mutex
	mcpMgr   *mcp.Manager // nil until first Prompt with MCP servers
	mcpToken string
}

type threadNameResult struct {
	name string
}

type backgroundThreadName struct {
	agent    *DefaultAgent
	threadID string
	resultCh <-chan threadNameResult
}

type promptEnvironment struct {
	threadCfg        thread.Config
	useThreadConfig  bool
	sessionCfg       *sessionconfig.SessionConfig
	subAgentCfg      *sessionconfig.SubAgentConfig
	tools            []providers.ToolDefinition
	modelRef         providers.ModelRef
	threadSummaryRef providers.ModelRef
	displayName      string
	systemPrompt     string
	mcpMgr           *mcp.Manager
	executor         thread.ToolExecutor
	currentDepth     int
	maxSteps         int
}

func (a *DefaultAgent) resolveThreadCWD(cwd string) string {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		cwd = a.cwd
	}
	cwd = filepath.Clean(cwd)
	if abs, err := filepath.Abs(cwd); err == nil {
		cwd = abs
	}
	return cwd
}

func (a *DefaultAgent) ensureThreadCWD(threadID string, cfg thread.Config) (thread.Config, bool, error) {
	resolved := a.resolveThreadCWD(cfg.CWD)
	if resolved == cfg.CWD {
		return cfg, false, nil
	}
	cfg.CWD = resolved
	if err := a.store.SaveConfig(threadID, cfg); err != nil {
		return cfg, false, fmt.Errorf("save thread config cwd: %w", err)
	}
	return cfg, true, nil
}

func (a *DefaultAgent) newToolContext(threadID string, maxSubagentDepth int, providerID, modelID, cwd string) *thread.ToolContext {
	toolCtx := &thread.ToolContext{
		ThreadID:                threadID,
		CurrentWorkingDirectory: a.resolveThreadCWD(cwd),
		MaxSubagentDepth:        maxSubagentDepth,
		ProviderResolver:        a.registry,
		Agent:                   a,
		ProviderID:              providerID,
		ModelID:                 modelID,
	}
	toolCtx.SetCurrentWorkingDirectory = func(next string) error {
		resolved := a.resolveThreadCWD(next)
		if resolved == toolCtx.CurrentWorkingDirectory {
			return nil
		}
		cfg, err := a.store.LoadConfig(threadID)
		if err != nil {
			return fmt.Errorf("load thread config cwd: %w", err)
		}
		cfg.CWD = resolved
		if err := a.store.SaveConfig(threadID, cfg); err != nil {
			return fmt.Errorf("save thread config cwd: %w", err)
		}
		toolCtx.CurrentWorkingDirectory = resolved
		return nil
	}
	return toolCtx
}

// NewDefaultAgent creates a DefaultAgent. Session configuration (system prompt,
// tools, instructions) is loaded fresh from the cwd when a new thread is created.
func NewDefaultAgent(
	store *thread.Store,
	registry *providers.ProviderRegistry,
	executor thread.ToolExecutor,
	cwd string,
	mcpCfg MCPConfig,
) *DefaultAgent {
	agent := &DefaultAgent{
		store:    store,
		registry: registry,
		executor: executor,
		cwd:      cwd,
		mcpCfg:   mcpCfg,
		cancels:  make(map[string]context.CancelFunc),
	}
	agent.ensureHelperScripts()
	return agent
}

// NewMCPConfig creates an MCPConfig from individual configuration values.
func NewMCPConfig(redirectBase, sessionID, discobotServerURL, projectID string) MCPConfig {
	return MCPConfig{
		redirectBase:      redirectBase,
		sessionID:         sessionID,
		discobotServerURL: discobotServerURL,
		projectID:         projectID,
	}
}

// MCPManager returns the lazily-initialized MCP manager, or nil if MCP has
// not been started yet (no Prompt with MCP servers has been called).
func (a *DefaultAgent) MCPManager() *mcp.Manager {
	a.mcpMu.Lock()
	defer a.mcpMu.Unlock()
	return a.mcpMgr
}

// Close shuts down the MCP manager (closes all server connections).
func (a *DefaultAgent) Close() {
	a.mcpMu.Lock()
	mgr := a.mcpMgr
	a.mcpMu.Unlock()
	if mgr != nil {
		mgr.Close()
	}
}

// Prompt sends a user message and streams the response as an iterator.
//
// If req.SubagentType is set, the named SubAgentConfig from session config is
// used to restrict tools, override the model, and set the system prompt.
//
// Model references may be empty, a bare provider ID, a bare model/supporting
// type relative to the current provider, or a full "providerId/modelId" ref.
// For resume (empty req), the provider is resolved from the persisted turn state.
func (a *DefaultAgent) Prompt(ctx context.Context, threadID string, req agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
	thread.ClearError(a.store, threadID)
	env, err := a.resolvePromptEnvironment(ctx, threadID, req)
	if err != nil {
		return a.withThreadErrorPersistence(threadID, errorIter(err))
	}
	return a.withThreadErrorPersistence(threadID, a.promptStream(ctx, threadID, req, env))
}

func (a *DefaultAgent) Compact(ctx context.Context, threadID string, req agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
	thread.ClearError(a.store, threadID)
	return a.withThreadErrorPersistence(threadID, a.handleCompactCommand(ctx, threadID, req))
}

func (a *DefaultAgent) Reset(_ context.Context, threadID string) (agent.ThreadInfo, error) {
	thread.ClearError(a.store, threadID)
	cfg, err := a.store.LoadConfig(threadID)
	if err != nil {
		return agent.ThreadInfo{}, fmt.Errorf("load thread config: %w", err)
	}
	cfg.ActiveLeafID = ""
	cfg.ContextReset = true
	cfg.ActiveCommand = ""
	cfg.LastMessage = ""
	cfg.LastTurnState = ""
	cfg.CommunicatedCredentials = nil
	cfg.CommunicatedSkillLikeEntries = nil
	if err := a.store.SaveConfig(threadID, cfg); err != nil {
		return agent.ThreadInfo{}, fmt.Errorf("save thread config: %w", err)
	}
	info, err := a.store.GetThreadInfo(threadID)
	if err != nil {
		return agent.ThreadInfo{}, err
	}
	return threadInfoToAgent(info), nil
}

func (a *DefaultAgent) withThreadErrorPersistence(threadID string, seq iter.Seq2[message.MessageChunk, error]) iter.Seq2[message.MessageChunk, error] {
	return func(yield func(message.MessageChunk, error) bool) {
		for chunk, err := range seq {
			thread.PersistError(a.store, threadID, err)
			if !yield(chunk, err) {
				return
			}
		}
	}
}

func (a *DefaultAgent) promptStream(
	ctx context.Context,
	threadID string,
	req agent.PromptRequest,
	env *promptEnvironment,
) iter.Seq2[message.MessageChunk, error] {
	toolCtx := a.newToolContext(threadID, env.sessionCfg.MaxSubagentDepth, env.modelRef.ProviderID, env.modelRef.ModelID, env.threadCfg.CWD)
	toolCtx.SubagentDepth = env.currentDepth
	toolCtx.CurrentTaskID = req.ParentTaskID
	toolCtx.ResolveTools = func(ctx context.Context) ([]providers.ToolDefinition, error) {
		tools := resolvePromptTools(req, env.sessionCfg, env.subAgentCfg, env.currentDepth)
		mcpMgr := a.resolveMCPManager(ctx)
		if mcpMgr != nil {
			tools = append(tools, mcpMgr.Tools()...)
		}
		return tools, nil
	}

	resolvedUserParts, originalText, activeCommand, slashCommand := resolveSlashCommand(a.cwd, req.UserParts)

	return func(yield func(message.MessageChunk, error) bool) {
		effectiveLeafID, err := a.resolveEffectiveLeafID(threadID, req.FreshContext, env.systemPrompt, env.displayName, env.sessionCfg, env.subAgentCfg, env.tools)
		if err != nil {
			yield(nil, err)
			return
		}

		// Create a child context so Cancel(threadID) can stop this prompt.
		promptCtx, cancel := context.WithCancel(ctx)
		defer func() {
			a.mu.Lock()
			delete(a.cancels, threadID)
			a.mu.Unlock()
			cancel()
		}()

		a.mu.Lock()
		a.cancels[threadID] = cancel
		a.mu.Unlock()

		// Check for interrupted turn first.
		state, err := a.store.LoadTurnState(threadID)
		if err != nil {
			yield(nil, err)
			return
		}

		if state != nil {
			if state.Phase == thread.PhaseWaitingForAnswer {
				yield(nil, agent.ErrPendingQuestionRequiresAnswer)
				return
			}
			yield(nil, agent.ErrInterruptedTurnRequiresResume)
			return
		}

		threadNameBg := a.startBackgroundThreadName(promptCtx, threadID, env.threadCfg, req.UserParts, env.threadSummaryRef)

		actualSlashCommand := slashCommand
		actualUserParts, updatedSlashCommand, execErr := a.executeScriptSlashCommand(promptCtx, resolvedUserParts, originalText, actualSlashCommand)
		if execErr != nil {
			yield(nil, execErr)
			return
		}
		actualSlashCommand = updatedSlashCommand

		cfg := thread.TurnConfig{
			ProviderID:       env.modelRef.ProviderID,
			Model:            env.modelRef.ModelID,
			SupportingModels: compactSupportingModels(env.modelRef, map[providers.SupportingModelType]providers.ModelRef{providers.SupportingModelThreadSummarization: env.threadSummaryRef}),
			Reasoning:        providers.Reasoning(req.Reasoning),
			ServiceTier:      req.ServiceTier,
			Metadata:         req.Metadata,
			UserParts:        message.UIPartsToParts(actualUserParts),
			OriginalUserText: originalText,
			SlashCommand:     actualSlashCommand,
			Tools:            env.tools,
			MaxSteps:         env.maxSteps,
		}
		if modelInfo := modelsdev.Lookup(env.modelRef.ProviderID, env.modelRef.ModelID); modelInfo != nil {
			cfg.ContextWindow = modelInfo.ContextWindow
			cfg.MaxOutputTokens = modelInfo.MaxOutputTokens
			cfg.TokenPrices = message.TokenPrices{
				Input:  modelInfo.Cost.Input,
				Output: modelInfo.Cost.Output,
			}
		}

		currentCommunicatedCredentials, credentialReminder := a.buildCredentialChangeReminder(
			env.threadCfg.CommunicatedCredentials,
		)
		currentSkillLikeEntries := currentVisibleSkillLikeEntries(env.sessionCfg, env.tools)
		addedSkills, removedSkills, changedSkills := thread.DiffCommunicatedSkillLikeEntries(
			env.threadCfg.CommunicatedSkillLikeEntries,
			currentSkillLikeEntries,
		)
		skillLikeChangeReminder := buildSkillLikeChangeReminder(addedSkills, removedSkills, changedSkills)
		if env.threadCfg.ActiveLeafID == "" {
			skillLikeChangeReminder = ""
		}
		workspaceChangeReminder := a.buildWorkspaceChangeReminder(threadID)
		cfg.PreludeMessages = buildTurnPreludeMessages(
			credentialReminder,
			skillLikeChangeReminder,
			workspaceChangeReminder,
		)

		// Resolve provider for new turn.
		provider, resolveErr := a.registry.Get(cfg.ProviderID)
		if resolveErr != nil {
			yield(nil, fmt.Errorf("resolve provider: %w", resolveErr))
			return
		}

		// Persist the resolved model and cwd so new sessions can resume by directory.
		cwd := a.resolveThreadCWD(env.threadCfg.CWD)
		// Persist the resolved model and working directory.
		cfgToSave := thread.Config{
			Name:                         env.threadCfg.Name,
			NameSource:                   env.threadCfg.NameSource,
			LastMessage:                  lastUserPromptFromUIParts(req.UserParts),
			Model:                        cfg.ProviderID + "/" + cfg.Model,
			Reasoning:                    cfg.Reasoning,
			ServiceTier:                  cfg.ServiceTier,
			CWD:                          cwd,
			LastTurnState:                "",
			ActiveLeafID:                 effectiveLeafID,
			ContextReset:                 false,
			ActiveCommand:                activeCommand,
			CommunicatedCredentials:      currentCommunicatedCredentials,
			CommunicatedSkillLikeEntries: currentSkillLikeEntries,
			Metadata:                     env.threadCfg.Metadata,
		}
		if err := a.store.SaveConfig(threadID, cfgToSave); err != nil {
			yield(nil, fmt.Errorf("save thread config: %w", err))
			return
		}
		if activeCommand != "" {
			if !yield(thread.UpdateChunkFromConfig(threadID, cfgToSave), nil) {
				return
			}
		}

		// Some script slash commands intentionally return no LLM-visible text on
		// success. In that case we still persist the user message and slash-command
		// metadata, but we skip starting a model turn entirely because there is no
		// prompt content for the provider to respond to.
		if actualSlashCommand != nil &&
			actualSlashCommand.Kind == agent.CommandKindScript &&
			actualSlashCommand.Script != nil &&
			actualSlashCommand.Script.SuppressedLLM {
			if !a.persistUserOnlyTurn(threadID, effectiveLeafID, cfg, cfgToSave, yield) {
				return
			}
			if !a.clearActiveCommand(threadID, yield) {
				return
			}
			a.recordTurnBoundaryIfComplete(threadID)
			return
		}

		// Start new turn.
		startEmitted := false
		for chunk, chunkErr := range thread.RunTurn(promptCtx, provider, env.executor, a.store, threadID, effectiveLeafID, cfg, toolCtx) {
			// Only flush the background thread name after the start chunk has
			// been emitted so the name update always follows the start of
			// streaming. Flushing before the start chunk can race on slow
			// schedulers (e.g. Windows arm64 CI) and emit the name before the
			// assistant even begins its response.
			if startEmitted {
				if !threadNameBg.flush(false, yield) {
					return
				}
			}
			// Before the response-finish chunk, block until the background
			// name is available.  This guarantees the name update precedes the
			// finish marker regardless of how quickly the naming completed.
			if _, isFinish := chunk.(message.ResponseFinishChunk); isFinish {
				if !threadNameBg.flush(true, yield) {
					return
				}
			}
			if !yield(chunk, chunkErr) {
				return
			}
			if !startEmitted {
				if _, ok := chunk.(message.StartChunk); ok {
					startEmitted = true
				}
			}
		}
		if promptCtx.Err() != nil {
			a.persistLastTurnState(threadID, thread.StateCancelled)
		}
		if !a.clearActiveCommand(threadID, yield) {
			return
		}
		a.persistActiveLeaf(threadID)
		a.recordTurnBoundaryIfComplete(threadID)
		if !threadNameBg.flush(true, yield) {
			return
		}
	}
}

// Resume continues or finalizes an interrupted turn from persisted disk state.
func (a *DefaultAgent) Resume(ctx context.Context, threadID string, req agent.PromptRequest) (agent.ResumeResult, error) {
	state, err := a.store.LoadTurnState(threadID)
	if err != nil {
		return agent.ResumeResult{}, fmt.Errorf("load turn state: %w", err)
	}
	if state == nil {
		return agent.ResumeResult{}, agent.ErrInterruptedTurnRequiresResume
	}
	if state.Phase == thread.PhaseWaitingForAnswer {
		answer, err := a.store.LoadAnswer(threadID, state.ID, state.PendingApprovalID)
		if err != nil {
			return agent.ResumeResult{}, fmt.Errorf("load answer: %w", err)
		}
		if answer == nil {
			return agent.ResumeResult{}, agent.ErrPendingQuestionRequiresAnswer
		}
	}
	thread.ClearError(a.store, threadID)
	if len(req.UserParts) > 0 {
		env, err := a.resolvePromptEnvironment(ctx, threadID, req)
		if err != nil {
			return agent.ResumeResult{}, err
		}
		replayLeafID, err := a.closeInterruptedTurnForPrompt(ctx, threadID)
		if err != nil {
			return agent.ResumeResult{}, err
		}
		return agent.ResumeResult{
			ReplayLeafID: replayLeafID,
			Stream:       a.withThreadErrorPersistence(threadID, a.promptStream(ctx, threadID, req, env)),
		}, nil
	}

	threadCfg, threadCfgErr := a.store.LoadConfig(threadID)
	if threadCfgErr != nil {
		log.Printf("agent: warning: thread config: %v", threadCfgErr)
		threadCfg = thread.Config{}
	}
	threadCfg, cfgUpdated, err := a.applyResumeOverrides(threadID, state, threadCfg, req)
	if err != nil {
		return agent.ResumeResult{}, err
	}
	threadCfg, cwdUpdated, err := a.ensureThreadCWD(threadID, threadCfg)
	if err != nil {
		return agent.ResumeResult{}, err
	}
	cfgUpdated = cfgUpdated || cwdUpdated

	replayLeafID := resumeReplayLeafID(threadID, state, threadCfg, a.store)
	if a.registry == nil {
		return agent.ResumeResult{}, fmt.Errorf("provider registry unavailable for resume")
	}

	provider, err := a.registry.Get(state.Config.ProviderID)
	if err != nil {
		return agent.ResumeResult{}, fmt.Errorf("resolve provider for resume: %w", err)
	}

	sessionCfg, err := sessionconfig.Load(a.cwd)
	if err != nil {
		log.Printf("agent: warning: session config: %v", err)
		sessionCfg = &sessionconfig.SessionConfig{
			MaxSubagentDepth: sessionconfig.DefaultMaxSubagentDepth,
		}
	}

	toolCtx := a.newToolContext(threadID, sessionCfg.MaxSubagentDepth, state.Config.ProviderID, state.Config.Model, threadCfg.CWD)
	resumeMessageID := a.resolveResumeMessageID(threadID, state)

	executor := thread.ToolExecutor(a.executor)
	if mcpMgr := a.resolveMCPManager(ctx); mcpMgr != nil {
		executor = mcp.NewExecutor(a.executor, a.MCPManager)
	}

	return agent.ResumeResult{
		ReplayLeafID: replayLeafID,
		Stream: a.withThreadErrorPersistence(threadID, func(yield func(message.MessageChunk, error) bool) {
			log.Printf("agent: resuming interrupted turn %s for thread %s (step %d, phase %s)",
				state.ID, threadID, state.CurrentStep, state.Phase)

			promptCtx, cancel := context.WithCancel(ctx)
			defer func() {
				a.mu.Lock()
				delete(a.cancels, threadID)
				a.mu.Unlock()
				cancel()
			}()

			a.mu.Lock()
			a.cancels[threadID] = cancel
			a.mu.Unlock()

			if cfgUpdated {
				if !yield(thread.UpdateChunkFromConfig(threadID, threadCfg), nil) {
					return
				}
			}
			if resumeMessageID != "" {
				if !yield(message.ThreadResumeChunk{
					Data: message.ThreadResumeData{ThreadID: threadID, MessageID: resumeMessageID},
				}, nil) {
					return
				}
			} else if state.AssistantMsgID != "" {
				if !yield(message.StartChunk{
					MessageID:       state.AssistantMsgID,
					MessageMetadata: resumeStartMessageMetadata(state),
				}, nil) {
					return
				}
			}

			for chunk, chunkErr := range thread.ResumeTurn(promptCtx, provider, executor, a.store, state, toolCtx) {
				if !yield(chunk, chunkErr) {
					return
				}
			}
			if promptCtx.Err() != nil {
				a.persistLastTurnState(threadID, thread.StateCancelled)
			}
			if !a.clearActiveCommand(threadID, yield) {
				return
			}
			a.persistActiveLeaf(threadID)
		}),
	}, nil
}

func (a *DefaultAgent) closeInterruptedTurnForPrompt(ctx context.Context, threadID string) (string, error) {
	state, err := a.store.LoadTurnState(threadID)
	if err != nil {
		return "", fmt.Errorf("load turn state: %w", err)
	}
	if state == nil {
		return a.resolveCurrentLeaf(threadID)
	}
	if state.Phase == thread.PhaseWaitingForAnswer {
		return "", agent.ErrPendingQuestionRequiresAnswer
	}
	if a.registry == nil {
		return "", fmt.Errorf("provider registry unavailable for interrupted turn close")
	}

	provider, err := a.registry.Get(state.Config.ProviderID)
	if err != nil {
		return "", fmt.Errorf("resolve provider for interrupted turn close: %w", err)
	}

	sessionCfg, err := sessionconfig.Load(a.cwd)
	if err != nil {
		log.Printf("agent: warning: session config: %v", err)
		sessionCfg = &sessionconfig.SessionConfig{
			MaxSubagentDepth: sessionconfig.DefaultMaxSubagentDepth,
		}
	}

	threadCfg, err := a.store.LoadConfig(threadID)
	if err != nil {
		log.Printf("agent: warning: thread config: %v", err)
		threadCfg = thread.Config{}
	}
	toolCtx := a.newToolContext(threadID, sessionCfg.MaxSubagentDepth, state.Config.ProviderID, state.Config.Model, threadCfg.CWD)

	executor := thread.ToolExecutor(a.executor)
	if mcpMgr := a.resolveMCPManager(ctx); mcpMgr != nil {
		executor = mcp.NewExecutor(a.executor, a.MCPManager)
	}

	abortCtx, cancel := context.WithCancel(ctx)
	cancel()
	for _, chunkErr := range thread.ResumeTurn(abortCtx, provider, executor, a.store, state, toolCtx) {
		if chunkErr == nil || errors.Is(chunkErr, context.Canceled) || errors.Is(chunkErr, context.DeadlineExceeded) {
			continue
		}
		return "", chunkErr
	}

	a.persistLastTurnState(threadID, thread.StateCancelled)
	_ = a.clearActiveCommand(threadID, func(message.MessageChunk, error) bool { return true })
	a.persistActiveLeaf(threadID)
	return a.resolveCurrentLeaf(threadID)
}

func resumeReplayLeafID(threadID string, state *thread.TurnState, cfg thread.Config, store *thread.Store) string {
	if state != nil {
		if leafID := strings.TrimSpace(state.LeafMsgID); leafID != "" {
			return leafID
		}
	}
	if leafID := strings.TrimSpace(cfg.ActiveLeafID); leafID != "" {
		return leafID
	}
	leafID, err := store.FindLeaf(threadID)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(leafID)
}

func resumeStartMessageMetadata(state *thread.TurnState) json.RawMessage {
	if state == nil {
		return nil
	}
	payload := map[string]any{}
	if state.Config.ProviderID != "" && state.Config.Model != "" {
		payload["model"] = state.Config.ProviderID + "/" + state.Config.Model
		payload["reasoning"] = string(state.Config.Reasoning)
	}
	if state.StartedAt != nil {
		payload["startedAt"] = state.StartedAt.UTC().Format(time.RFC3339Nano)
	}
	if len(payload) == 0 {
		return nil
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	return data
}

func (a *DefaultAgent) applyResumeOverrides(
	threadID string,
	state *thread.TurnState,
	threadCfg thread.Config,
	req agent.PromptRequest,
) (thread.Config, bool, error) {
	if ref, err := providers.ParseModelRef(state.Config.Model); err == nil {
		state.Config.ProviderID = ref.ProviderID
		state.Config.Model = ref.ModelID
	}

	updated := false
	currentProviderID := state.Config.ProviderID
	if currentProviderID == "" {
		currentProviderID = providers.CurrentProviderFromRef(threadCfg.Model)
	}

	if model := strings.TrimSpace(req.Model); model != "" {
		ref, err := a.registry.ResolveModelInProvider(currentProviderID, model, providers.ModelTaskChat)
		if err != nil {
			return threadCfg, false, fmt.Errorf("invalid model: %w", err)
		}
		if state.Config.ProviderID != ref.ProviderID || state.Config.Model != ref.ModelID {
			state.Config.ProviderID = ref.ProviderID
			state.Config.Model = ref.ModelID
			updated = true
		}
		if threadCfg.Model != ref.String() {
			threadCfg.Model = ref.String()
			updated = true
		}
	}

	if reasoning := strings.TrimSpace(req.Reasoning); reasoning != "" {
		resolved := providers.Reasoning(reasoning)
		if state.Config.Reasoning != resolved {
			state.Config.Reasoning = resolved
			updated = true
		}
		if threadCfg.Reasoning != resolved {
			threadCfg.Reasoning = resolved
			updated = true
		}
	}

	if !updated {
		return threadCfg, false, nil
	}
	if err := a.store.SaveTurnState(threadID, *state); err != nil {
		return threadCfg, false, fmt.Errorf("save turn state for resume: %w", err)
	}
	if err := a.store.SaveConfig(threadID, threadCfg); err != nil {
		return threadCfg, false, fmt.Errorf("save thread config for resume: %w", err)
	}
	return threadCfg, true, nil
}

func (a *DefaultAgent) resolvePromptEnvironment(ctx context.Context, threadID string, req agent.PromptRequest) (*promptEnvironment, error) {
	threadCfg, threadCfgErr := a.store.LoadConfig(threadID)
	if threadCfgErr != nil {
		log.Printf("agent: warning: thread config: %v", threadCfgErr)
		threadCfg = thread.Config{}
	}
	useThreadConfig := threadCfgErr == nil
	currentDepth := max(req.SubagentDepth, 0)

	sessionCfg, err := sessionconfig.Load(a.cwd)
	if err != nil {
		log.Printf("agent: warning: session config: %v", err)
		sessionCfg = &sessionconfig.SessionConfig{MaxSubagentDepth: sessionconfig.DefaultMaxSubagentDepth}
	}

	subAgentCfg, err := resolveSubAgentConfig(sessionCfg, req.SubagentType)
	if err != nil {
		return nil, err
	}

	tools := resolvePromptTools(req, sessionCfg, subAgentCfg, currentDepth)
	supportingModels := req.SupportingModels
	if subAgentCfg != nil && len(subAgentCfg.SupportingModels) > 0 {
		supportingModels = mergeSupportingModels(req.SupportingModels, subAgentCfg.SupportingModels)
	}

	systemPrompt := sessionCfg.SystemPrompt
	if subAgentCfg != nil && subAgentCfg.Prompt != "" {
		systemPrompt = subAgentCfg.Prompt
	}

	mcpMgr := a.resolveMCPManager(ctx)
	if mcpMgr != nil {
		tools = append(tools, mcpMgr.Tools()...)
	}
	executor := thread.ToolExecutor(a.executor)
	if mcpMgr != nil {
		executor = mcp.NewExecutor(a.executor, a.MCPManager)
	}

	model := req.Model
	if model == "" && useThreadConfig && threadCfg.Model != "" {
		model = threadCfg.Model
	}

	currentProviderID := ""
	if useThreadConfig {
		currentProviderID = providers.CurrentProviderFromRef(threadCfg.Model)
	}

	ref, err := a.resolvePromptModel(threadID, currentProviderID, model, req)
	if err != nil {
		return nil, fmt.Errorf("invalid model: %w", err)
	}
	if subAgentCfg != nil && subAgentCfg.Model != "" {
		ref, err = a.resolveSubAgentPromptModel(threadID, ref, subAgentCfg.Model, req)
		if err != nil {
			return nil, fmt.Errorf("invalid sub-agent model: %w", err)
		}
	}
	threadSummaryRef, err := a.registry.ResolveSupportingModel(ref, supportingModels, providers.SupportingModelThreadSummarization)
	if err != nil {
		return nil, fmt.Errorf("invalid thread summarization model: %w", err)
	}

	displayName := resolveModelDisplayName(ref.ProviderID, ref.ModelID)

	maxSteps := req.MaxTurns
	if subAgentCfg != nil && subAgentCfg.MaxTurns > 0 {
		if maxSteps == 0 || subAgentCfg.MaxTurns < maxSteps {
			maxSteps = subAgentCfg.MaxTurns
		}
	}

	return &promptEnvironment{
		threadCfg:        threadCfg,
		useThreadConfig:  useThreadConfig,
		sessionCfg:       sessionCfg,
		subAgentCfg:      subAgentCfg,
		tools:            tools,
		modelRef:         ref,
		threadSummaryRef: threadSummaryRef,
		displayName:      displayName,
		systemPrompt:     systemPrompt,
		mcpMgr:           mcpMgr,
		executor:         executor,
		currentDepth:     currentDepth,
		maxSteps:         maxSteps,
	}, nil
}

func (a *DefaultAgent) resolvePromptModel(
	threadID, currentProviderID, model string,
	req agent.PromptRequest,
) (providers.ModelRef, error) {
	ref, err := a.registry.ResolveModelInProvider(currentProviderID, model, providers.ModelTaskChat)
	if err == nil {
		return ref, nil
	}
	if !shouldFallbackPromptModel(req) {
		return providers.ModelRef{}, err
	}
	fallback, fallbackErr := a.registry.ResolveModelInProvider(currentProviderID, "", providers.ModelTaskChat)
	if fallbackErr != nil {
		return providers.ModelRef{}, err
	}
	log.Printf(
		"agent: warning: thread %s: could not resolve task model %q: %v; falling back to %s",
		threadID,
		model,
		err,
		fallback.String(),
	)
	return fallback, nil
}

func (a *DefaultAgent) resolveSubAgentPromptModel(
	threadID string,
	current providers.ModelRef,
	model string,
	req agent.PromptRequest,
) (providers.ModelRef, error) {
	ref, err := a.registry.ResolveModelInProvider(current.ProviderID, model, providers.ModelTaskChat)
	if err == nil {
		return ref, nil
	}
	if !shouldFallbackPromptModel(req) {
		return providers.ModelRef{}, err
	}
	log.Printf(
		"agent: warning: thread %s: could not resolve sub-agent model %q: %v; keeping %s",
		threadID,
		model,
		err,
		current.String(),
	)
	return current, nil
}

func shouldFallbackPromptModel(req agent.PromptRequest) bool {
	return req.SubagentType != "" || req.SubagentDepth > 0 || req.ParentTaskID != ""
}

func resolveSubAgentConfig(sessionCfg *sessionconfig.SessionConfig, subAgentType string) (*sessionconfig.SubAgentConfig, error) {
	if subAgentType == "" {
		return nil, nil
	}
	for i := range sessionCfg.SubAgents {
		if sessionCfg.SubAgents[i].Name == subAgentType {
			return &sessionCfg.SubAgents[i], nil
		}
	}
	return nil, fmt.Errorf("sub-agent type %q not found in session config", subAgentType)
}

// ValidateSubagentType checks that subAgentType exists in the session config.
func (a *DefaultAgent) ValidateSubagentType(subAgentType string) error {
	sessionCfg, err := sessionconfig.Load(a.cwd)
	if err != nil {
		return fmt.Errorf("load session config: %w", err)
	}
	_, err = resolveSubAgentConfig(sessionCfg, subAgentType)
	return err
}

func resolvePromptTools(req agent.PromptRequest, sessionCfg *sessionconfig.SessionConfig, subAgentCfg *sessionconfig.SubAgentConfig, currentDepth int) []providers.ToolDefinition {
	tools := req.Tools
	if tools == nil {
		tools = sessionCfg.Tools
	}
	if sessionCfg.MaxSubagentDepth > 0 && currentDepth >= sessionCfg.MaxSubagentDepth {
		tools = filterTools(tools, nil, []string{"Task", "Agent"})
	}
	if subAgentCfg != nil {
		tools = filterTools(tools, subAgentCfg.AllowedTools, subAgentCfg.DisallowedTools)
	}
	return sessionconfig.AdaptToolsForRuntime(runtime.GOOS, tools)
}

func (a *DefaultAgent) resolveMCPManager(ctx context.Context) *mcp.Manager {
	a.mcpMu.Lock()
	defer a.mcpMu.Unlock()
	state, err := sessionconfig.DiscoverMCPState(a.cwd)
	if err != nil {
		log.Printf("agent: warning: MCP discovery: %v", err)
		return a.mcpMgr
	}
	if a.mcpToken != state.ReloadToken {
		if a.mcpMgr != nil {
			log.Printf("agent: .mcp.json changed, reloading MCP manager")
			a.mcpMgr.Close()
			a.mcpMgr = nil
		}
		if len(state.Servers) > 0 {
			callback := mcp.MakeTokenCallback(a.mcpCfg.discobotServerURL, a.mcpCfg.projectID)
			a.mcpMgr = mcp.NewManager(a.cwd, callback)
			a.mcpMgr.Connect(ctx, state.Servers, a.mcpCfg.redirectBase, a.mcpCfg.sessionID)
		}
		a.mcpToken = state.ReloadToken
	}
	return a.mcpMgr
}

func lastUserPromptFromUIParts(parts []message.UIPart) string {
	textParts := make([]string, 0, len(parts))
	for _, part := range parts {
		textPart, ok := part.(message.UITextPart)
		if !ok {
			continue
		}
		text := strings.TrimSpace(textPart.Text)
		if text == "" {
			continue
		}
		textParts = append(textParts, text)
	}
	return strings.TrimSpace(strings.Join(textParts, "\n"))
}

func buildTurnPreludeMessages(credentialReminder, skillLikeChangeReminder, workspaceChangeReminder string) []message.Message {
	prelude := make([]message.Message, 0, 3)
	if strings.TrimSpace(credentialReminder) != "" {
		prelude = append(prelude, message.Message{
			ID:        "credentials-" + agent.GenerateID(),
			Role:      "user",
			Synthetic: true,
			Parts: []message.Part{message.TextPart{
				Text: credentialReminder,
				ProviderMetadata: message.MarshalProviderMetadata(message.DiscobotPartMetadata{
					ReminderKind: "credentials",
				}),
			}},
		})
	}
	if strings.TrimSpace(skillLikeChangeReminder) != "" {
		prelude = append(prelude, message.Message{
			ID:        "skills-" + agent.GenerateID(),
			Role:      "user",
			Synthetic: true,
			Parts: []message.Part{message.TextPart{
				Text: skillLikeChangeReminder,
				ProviderMetadata: message.MarshalProviderMetadata(message.DiscobotPartMetadata{
					ReminderKind: "skills",
				}),
			}},
		})
	}
	if strings.TrimSpace(workspaceChangeReminder) != "" {
		prelude = append(prelude, message.Message{
			ID:        "workspace-changes-" + agent.GenerateID(),
			Role:      "user",
			Synthetic: true,
			Parts: []message.Part{message.TextPart{
				Text: workspaceChangeReminder,
				ProviderMetadata: message.MarshalProviderMetadata(message.DiscobotPartMetadata{
					ReminderKind: "workspace-changes",
				}),
			}},
		})
	}
	if len(prelude) == 0 {
		return nil
	}
	return prelude
}

const workspaceChangeReminderLimit = 10

func (a *DefaultAgent) workspaceChangesDir(threadID string) string {
	return filepath.Join(a.store.ThreadDir(threadID), "workspace-changes")
}

func (a *DefaultAgent) workspaceChangeMarkerPath(threadID string) string {
	return filepath.Join(a.workspaceChangesDir(threadID), "last-turn-ended-at")
}

func (a *DefaultAgent) workspaceChangeListPath(threadID string, changedFiles []string) string {
	sum := sha256.Sum256([]byte(strings.Join(changedFiles, "\n")))
	return filepath.Join(
		a.workspaceChangesDir(threadID),
		fmt.Sprintf("changed-since-last-turn-%x.txt", sum[:8]),
	)
}

func (a *DefaultAgent) buildWorkspaceChangeReminder(threadID string) string {
	if a.store == nil {
		return ""
	}

	sinceInfo, err := os.Stat(a.workspaceChangeMarkerPath(threadID))
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("agent: warning: stat workspace change marker for %s: %v", threadID, err)
		}
		return ""
	}

	_ = sinceInfo
	changedFiles := agenthooks.DirtyFilesSinceMarker(a.cwd, a.workspaceChangeMarkerPath(threadID))
	listPath, err := a.writeWorkspaceChangeList(threadID, changedFiles)
	if err != nil {
		log.Printf("agent: warning: save workspace change list for %s: %v", threadID, err)
	}
	return sessionconfig.FormatWorkspaceChangeReminder(
		listPath,
		changedFiles,
		workspaceChangeReminderLimit,
	)
}

func (a *DefaultAgent) writeWorkspaceChangeList(threadID string, changedFiles []string) (string, error) {
	dir := a.workspaceChangesDir(threadID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create workspace change dir: %w", err)
	}
	content := strings.Join(changedFiles, "\n")
	if content != "" {
		content += "\n"
	}
	path := a.workspaceChangeListPath(threadID, changedFiles)
	if err := thread.WriteFileAtomic(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func (a *DefaultAgent) recordTurnBoundaryIfComplete(threadID string) {
	if a.store == nil {
		return
	}
	state, err := a.store.LoadTurnState(threadID)
	if err != nil {
		log.Printf("agent: warning: load turn state before workspace change marker for %s: %v", threadID, err)
		return
	}
	if state != nil {
		return
	}
	if err := a.recordTurnBoundary(threadID, time.Now().UTC()); err != nil {
		log.Printf("agent: warning: record workspace change marker for %s: %v", threadID, err)
	}
}

func (a *DefaultAgent) recordTurnBoundary(threadID string, at time.Time) error {
	dir := a.workspaceChangesDir(threadID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create workspace change dir: %w", err)
	}
	path := a.workspaceChangeMarkerPath(threadID)
	if err := thread.WriteFileAtomic(path, []byte(at.UTC().Format(time.RFC3339Nano)+"\n"), 0o644); err != nil {
		return fmt.Errorf("write workspace change marker: %w", err)
	}
	if err := os.Chtimes(path, at, at); err != nil {
		return fmt.Errorf("set workspace change marker time: %w", err)
	}
	return nil
}

const generatedThreadNameMaxRunes = 72

func shouldGenerateThreadName(cfg thread.Config) bool {
	return strings.TrimSpace(cfg.Name) == ""
}

func generatedThreadName(parts []message.UIPart) string {
	text := firstThreadNameText(parts)
	if text == "" {
		return ""
	}

	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return ""
	}

	if commandArgs, ok := stripLeadingSlashCommand(text); ok {
		text = commandArgs
		if text == "" {
			return ""
		}
	}

	runes := []rune(text)
	if len(runes) <= generatedThreadNameMaxRunes {
		return text
	}
	return strings.TrimSpace(string(runes[:generatedThreadNameMaxRunes-1])) + "…"
}

func mergeSupportingModels(base, override providers.SupportingModels) providers.SupportingModels {
	if len(base) == 0 && len(override) == 0 {
		return nil
	}
	merged := make(providers.SupportingModels, len(base)+len(override))
	maps.Copy(merged, base)
	maps.Copy(merged, override)
	return merged
}

func compactSupportingModels(main providers.ModelRef, resolved map[providers.SupportingModelType]providers.ModelRef) providers.SupportingModels {
	if len(resolved) == 0 {
		return nil
	}
	compacted := make(providers.SupportingModels, len(resolved))
	for taskType, ref := range resolved {
		if ref == main || ref.ModelID == "" {
			continue
		}
		compacted[taskType] = ref.String()
	}
	if len(compacted) == 0 {
		return nil
	}
	return compacted
}

func (a *DefaultAgent) generateThreadName(ctx context.Context, parts []message.UIPart, modelRef providers.ModelRef) (string, error) {
	starter := generatedThreadName(parts)
	if starter == "" {
		return "", nil
	}

	provider, err := a.registry.Get(modelRef.ProviderID)
	if err != nil {
		return "", err
	}

	maxTokens := 48
	req := providers.CompleteRequest{
		Model: providers.ModelRef{
			ProviderID: modelRef.ProviderID,
			ModelID:    modelRef.ModelID,
		},
		Messages: []message.Message{{
			Role: "user",
			Parts: []message.Part{message.TextPart{Text: fmt.Sprintf(
				"Generate a concise thread title for this conversation starter.\n\nRules:\n- Return only the title.\n- Do not use quotes.\n- Keep it under %d characters.\n- Preserve important technical terms.\n\nConversation starter:\n%s",
				generatedThreadNameMaxRunes,
				starter,
			)}},
		}},
		MaxTokens: &maxTokens,
		Reasoning: providers.ReasoningNone,
	}

	acc := message.NewChunkAccumulator()
	for chunk, chunkErr := range provider.Complete(ctx, req) {
		if chunkErr != nil {
			acc.Close()
			return "", chunkErr
		}
		acc.Push(chunk)
	}
	acc.Close()

	result := acc.Message()
	var sb strings.Builder
	for _, part := range result.Parts {
		if tp, ok := part.(message.TextPart); ok {
			sb.WriteString(tp.Text)
		}
	}

	name := strings.TrimSpace(sb.String())
	if idx := strings.IndexByte(name, '\n'); idx >= 0 {
		name = strings.TrimSpace(name[:idx])
	}
	name = strings.Trim(name, "\"'`")
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		return "", nil
	}

	runes := []rune(name)
	if len(runes) > generatedThreadNameMaxRunes {
		name = strings.TrimSpace(string(runes[:generatedThreadNameMaxRunes-1])) + "…"
	}
	return name, nil
}

func (a *DefaultAgent) startBackgroundThreadName(
	ctx context.Context,
	threadID string,
	cfg thread.Config,
	parts []message.UIPart,
	modelRef providers.ModelRef,
) *backgroundThreadName {
	if !shouldGenerateThreadName(cfg) {
		return nil
	}

	fallbackName := cfg.Name
	if strings.TrimSpace(fallbackName) == "" {
		fallbackName = generatedThreadName(parts)
	}

	resultCh := make(chan threadNameResult, 1)
	go func() {
		generatedName := fallbackName
		if aiName, err := a.generateThreadName(ctx, parts, modelRef); err == nil && aiName != "" {
			generatedName = aiName
		}
		resultCh <- threadNameResult{name: generatedName}
	}()

	return &backgroundThreadName{
		agent:    a,
		threadID: threadID,
		resultCh: resultCh,
	}
}

func (a *DefaultAgent) saveGeneratedThreadName(threadID, generatedName string) (string, bool, error) {
	if strings.TrimSpace(generatedName) == "" {
		return "", false, nil
	}

	cfg, err := a.store.LoadConfig(threadID)
	if err != nil {
		return "", false, err
	}
	if !shouldGenerateThreadName(cfg) {
		return cfg.Name, false, nil
	}

	changed := cfg.Name != generatedName
	cfg.Name = generatedName
	cfg.NameSource = thread.ThreadNameSourceGenerated
	if !changed {
		return cfg.Name, false, nil
	}
	if err := a.store.SaveConfig(threadID, cfg); err != nil {
		return "", false, err
	}
	return cfg.Name, true, nil
}

func (b *backgroundThreadName) flush(
	block bool,
	yield func(message.MessageChunk, error) bool,
) bool {
	if b == nil || b.resultCh == nil {
		return true
	}

	var result threadNameResult
	if block {
		result = <-b.resultCh
	} else {
		select {
		case result = <-b.resultCh:
		default:
			return true
		}
	}
	b.resultCh = nil

	_, changed, err := b.agent.saveGeneratedThreadName(b.threadID, result.name)
	if err != nil {
		return yield(nil, fmt.Errorf("save thread name: %w", err))
	}
	if !changed {
		return true
	}
	cfg, err := b.agent.store.LoadConfig(b.threadID)
	if err != nil {
		return yield(nil, fmt.Errorf("load updated thread config: %w", err))
	}
	return yield(thread.UpdateChunkFromConfig(b.threadID, cfg), nil)
}

func firstThreadNameText(parts []message.UIPart) string {
	for _, part := range parts {
		textPart, ok := part.(message.UITextPart)
		if !ok {
			continue
		}
		if trimmed := strings.TrimSpace(textPart.Text); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func stripLeadingSlashCommand(text string) (string, bool) {
	if !strings.HasPrefix(text, "/") {
		return text, false
	}

	fields := strings.Fields(text)
	if len(fields) < 2 {
		return "", true
	}

	command := fields[0]
	if strings.Contains(command[1:], "/") {
		return text, false
	}

	for _, r := range command[1:] {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == ':' || r == '.' {
			continue
		}
		return text, false
	}

	return strings.Join(fields[1:], " "), true
}

// resolveModelDisplayName returns the human-readable display name for a
// provider/model pair, falling back to the bare model ID if not found.
func resolveModelDisplayName(providerID, modelID string) string {
	if info := modelsdev.Lookup(providerID, modelID); info != nil && info.Name != "" {
		return info.Name
	}
	return modelID
}

func formatRuntimeEnvironmentReminder(cwd, modelName string) string {
	resolvedCWD := filepath.Clean(cwd)
	if abs, err := filepath.Abs(resolvedCWD); err == nil {
		resolvedCWD = abs
	}

	return sessionconfig.FormatRuntimeEnvironmentReminder(sessionconfig.RuntimeEnvironmentSnapshot{
		CurrentWorkingDirectory: resolvedCWD,
		CurrentModel:            modelName,
		CurrentDateTime:         time.Now(),
		GitState:                gitStateSnapshot(resolvedCWD),
	})
}

func gitStateSnapshot(cwd string) string {
	insideWorktree, err := gitCommandOutput(cwd, "rev-parse", "--is-inside-work-tree")
	if err != nil || insideWorktree != "true" {
		return "not a git repository"
	}

	branch, err := gitCommandOutput(cwd, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil || branch == "" {
		branch = "unknown"
	}

	statusOut, err := gitCommandOutput(cwd, "status", "--porcelain")
	if err != nil {
		return fmt.Sprintf("branch=%s, working_tree=unknown", branch)
	}

	workingTreeState := "clean"
	if statusOut != "" {
		workingTreeState = "dirty"
	}

	return fmt.Sprintf("branch=%s, working_tree=%s", branch, workingTreeState)
}

func gitCommandOutput(cwd string, args ...string) (string, error) {
	cmdCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	gitArgs := append([]string{"-C", cwd}, args...)
	out, err := exec.CommandContext(cmdCtx, "git", gitArgs...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// errorIter returns an iterator that yields a single error.
func errorIter(err error) iter.Seq2[message.MessageChunk, error] {
	return func(yield func(message.MessageChunk, error) bool) {
		yield(nil, err)
	}
}

func (a *DefaultAgent) handleCompactCommand(ctx context.Context, threadID string, req agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
	leafID, err := a.resolveCurrentLeaf(threadID)
	if err != nil {
		return errorIter(fmt.Errorf("resolve current leaf: %w", err))
	}
	if leafID == "" {
		return func(yield func(message.MessageChunk, error) bool) {
			yieldCompactStatusMessage(yield, "Nothing to compact yet.")
		}
	}

	threadCfg, err := a.store.LoadConfig(threadID)
	if err != nil {
		return errorIter(fmt.Errorf("load thread config: %w", err))
	}

	model := req.Model
	if model == "" && threadCfg.Model != "" {
		model = threadCfg.Model
	}

	currentProviderID := providers.CurrentProviderFromRef(threadCfg.Model)
	ref, err := a.registry.ResolveModelInProvider(currentProviderID, model, providers.ModelTaskChat)
	if err != nil {
		return errorIter(fmt.Errorf("resolve model for /compact: %w", err))
	}

	provider, err := a.registry.Get(ref.ProviderID)
	if err != nil {
		return errorIter(fmt.Errorf("resolve provider for /compact: %w", err))
	}

	turnCfg := &thread.TurnConfig{ProviderID: ref.ProviderID, Model: ref.ModelID}
	if modelInfo := modelsdev.Lookup(ref.ProviderID, ref.ModelID); modelInfo != nil {
		turnCfg.ContextWindow = modelInfo.ContextWindow
		turnCfg.MaxOutputTokens = modelInfo.MaxOutputTokens
		turnCfg.TokenPrices = message.TokenPrices{
			Input:  modelInfo.Cost.Input,
			Output: modelInfo.Cost.Output,
		}
	}
	if summaryRef, err := a.registry.ResolveSupportingModel(ref, req.SupportingModels, providers.SupportingModelThreadSummarization); err == nil {
		turnCfg.SupportingModels = compactSupportingModels(ref, map[providers.SupportingModelType]providers.ModelRef{
			providers.SupportingModelThreadSummarization: summaryRef,
		})
	}

	return func(yield func(message.MessageChunk, error) bool) {
		log.Printf("agent: compacting thread %s at leaf %s using %s/%s", threadID, leafID, ref.ProviderID, ref.ModelID)
		messageID := agent.GenerateID()
		textID := agent.GenerateID()
		if !yield(message.StartChunk{MessageID: messageID}, nil) {
			return
		}
		if !yield(message.TextStartChunk{ID: textID}, nil) {
			return
		}
		if !yield(message.TextDeltaChunk{ID: textID, Delta: "Compacting conversation...\n"}, nil) {
			return
		}
		compacted, compactErr := thread.ForceCompactThread(ctx, provider, a.registry, a.store, threadID, leafID, turnCfg)
		if compactErr != nil {
			yield(nil, fmt.Errorf("force compaction: %w", compactErr))
			return
		}

		if compacted {
			log.Printf("agent: compacted thread %s at leaf %s", threadID, leafID)
			if !yield(message.TextDeltaChunk{ID: textID, Delta: "Conversation compacted."}, nil) {
				return
			}
			if !yield(message.TextEndChunk{ID: textID}, nil) {
				return
			}
			yield(message.ResponseFinishChunk{FinishReason: "stop"}, nil)
			return
		}

		log.Printf("agent: compact skipped for thread %s at leaf %s: nothing to compact", threadID, leafID)
		if !yield(message.TextDeltaChunk{ID: textID, Delta: "Nothing to compact yet."}, nil) {
			return
		}
		if !yield(message.TextEndChunk{ID: textID}, nil) {
			return
		}
		yield(message.ResponseFinishChunk{FinishReason: "stop"}, nil)
	}
}

func yieldCompactStatusMessage(yield func(message.MessageChunk, error) bool, text string) bool {
	messageID := agent.GenerateID()
	textID := agent.GenerateID()
	if !yield(message.StartChunk{MessageID: messageID}, nil) {
		return false
	}
	if !yield(message.TextStartChunk{ID: textID}, nil) {
		return false
	}
	if !yield(message.TextDeltaChunk{ID: textID, Delta: text}, nil) {
		return false
	}
	if !yield(message.TextEndChunk{ID: textID}, nil) {
		return false
	}
	return yield(message.ResponseFinishChunk{FinishReason: "stop"}, nil)
}

// Cancel cancels the active prompt for a thread.
// If the turn is paused waiting for AskUserQuestion answers, it finalizes that
// turn as cancelled so the thread can accept new prompts again.
func (a *DefaultAgent) Cancel(threadID string) bool {
	a.mu.Lock()
	cancel, ok := a.cancels[threadID]
	a.mu.Unlock()
	if ok {
		cancel()
		return true
	}
	if a.store == nil {
		return false
	}
	cancelled, err := thread.CancelWaitingTurn(a.store, threadID, "cancelled by user")
	if err != nil {
		log.Printf("agent: cancel paused turn for %s: %v", threadID, err)
		return false
	}
	return cancelled
}

// Messages returns the conversation history as UI-projected messages.
func (a *DefaultAgent) Messages(threadID, leafID string) ([]message.UIMessage, error) {
	if leafID == "" {
		var err error
		leafID, err = a.resolveCurrentLeaf(threadID)
		if err != nil {
			return nil, fmt.Errorf("resolve current leaf: %w", err)
		}
		if leafID == "" {
			return nil, nil
		}
	}
	historyEntries, err := a.store.BuildHistoryWithIDs(threadID, leafID)
	if err != nil {
		return nil, err
	}
	turnIDsByMessageID, err := a.store.HistoryTurnIDs(threadID)
	if err != nil {
		return nil, err
	}
	history := make([]message.Message, 0, len(historyEntries))
	for _, entry := range historyEntries {
		msg := entry.Message
		if turnID := strings.TrimSpace(turnIDsByMessageID[entry.ID]); turnID != "" {
			msg.Metadata = mergeTurnIDIntoMessageMetadata(msg.Metadata, turnID)
		}
		history = append(history, msg)
	}
	return message.ProjectUIMessages(history)
}

func mergeTurnIDIntoMessageMetadata(metadata json.RawMessage, turnID string) json.RawMessage {
	if strings.TrimSpace(turnID) == "" {
		return metadata
	}
	payload := map[string]any{}
	if len(metadata) > 0 {
		if err := json.Unmarshal(metadata, &payload); err != nil {
			return metadata
		}
	}
	discobot := map[string]any{}
	if current, ok := payload["discobot"].(map[string]any); ok {
		maps.Copy(discobot, current)
	}
	discobot["turnId"] = turnID
	payload["discobot"] = discobot
	data, err := json.Marshal(payload)
	if err != nil {
		return metadata
	}
	return data
}

// FinalResponse returns the last assistant text from a completed thread turn.
// Returns empty string if the thread has no content or if a turn is in progress.
func (a *DefaultAgent) FinalResponse(threadID string) (string, error) {
	// If a turn is interrupted, the thread isn't done yet.
	state, err := a.store.LoadTurnState(threadID)
	if err != nil {
		return "", fmt.Errorf("load turn state: %w", err)
	}
	if state != nil {
		return "", nil
	}

	leafID, err := a.resolveCurrentLeaf(threadID)
	if err != nil {
		return "", fmt.Errorf("resolve current leaf: %w", err)
	}
	if leafID == "" {
		return "", nil
	}

	history, err := a.store.BuildHistory(threadID, leafID)
	if err != nil {
		return "", fmt.Errorf("build history: %w", err)
	}

	// Return the last assistant message's text content.
	for _, v := range slices.Backward(history) {
		if v.Role == "assistant" {
			var sb strings.Builder
			for _, p := range v.Parts {
				if tp, ok := p.(message.TextPart); ok {
					sb.WriteString(tp.Text)
				}
			}
			return sb.String(), nil
		}
	}
	return "", nil
}

func (a *DefaultAgent) resolveEffectiveLeafID(
	threadID string,
	startFresh bool,
	systemPrompt string,
	displayName string,
	sessionCfg *sessionconfig.SessionConfig,
	subAgentCfg *sessionconfig.SubAgentConfig,
	tools []providers.ToolDefinition,
) (string, error) {
	if hasStartupBootstrapContent(systemPrompt, displayName, sessionCfg, subAgentCfg, tools) {
		leaf, err := a.resolveExistingLeafForPrompt(threadID, startFresh)
		if err != nil {
			return "", fmt.Errorf("resolve current leaf: %w", err)
		}
		if leaf != "" {
			return leaf, nil
		}
		return a.bootstrapNewThreadMessages(threadID, systemPrompt, displayName, sessionCfg, subAgentCfg, tools)
	}

	if !startFresh {
		leaf, err := a.resolveCurrentLeaf(threadID)
		if err != nil {
			return "", fmt.Errorf("resolve current leaf: %w", err)
		}
		return leaf, nil
	}

	return "", nil
}

func hasStartupBootstrapContent(
	systemPrompt string,
	displayName string,
	sessionCfg *sessionconfig.SessionConfig,
	subAgentCfg *sessionconfig.SubAgentConfig,
	tools []providers.ToolDefinition,
) bool {
	if systemPrompt != "" {
		return true
	}
	if subAgentCfg != nil {
		return false
	}
	if sessionconfig.FormatUserInstructions(sessionCfg.UserInstructions) != "" {
		return true
	}
	if formatRuntimeEnvironmentReminder("", displayName) != "" {
		return true
	}
	if hasNamedTool(tools, "Task") && sessionconfig.FormatSubAgentReminder(sessionCfg.SubAgents) != "" {
		return true
	}
	if hasNamedTool(sessionCfg.Tools, "Skill") {
		if sessionconfig.FormatSkillLikeDiscoveryWarningsReminder(sessionCfg.SkillDiscoveryWarnings, sessionCfg.ScriptDiscoveryWarnings) != "" {
			return true
		}
		return sessionconfig.FormatSkillLikeReminder(sessionCfg.Skills, sessionCfg.Scripts) != ""
	}
	return false
}

func (a *DefaultAgent) resolveExistingLeafForPrompt(threadID string, startFresh bool) (string, error) {
	if startFresh {
		return "", nil
	}
	return a.resolveCurrentLeaf(threadID)
}

func (a *DefaultAgent) bootstrapNewThreadMessages(
	threadID string,
	systemPrompt string,
	displayName string,
	sessionCfg *sessionconfig.SessionConfig,
	subAgentCfg *sessionconfig.SubAgentConfig,
	tools []providers.ToolDefinition,
) (string, error) {
	effectiveLeafID := ""

	appendMessage := func(id, role, text string) error {
		if text == "" {
			return nil
		}
		msg := thread.StoredMessage{
			ID:       id,
			ParentID: effectiveLeafID,
			Message: message.Message{
				Role:      role,
				Synthetic: true,
				Parts:     []message.Part{message.TextPart{Text: text}},
			},
		}
		if err := a.store.SaveMessage(threadID, msg); err != nil {
			return err
		}
		effectiveLeafID = id
		return nil
	}

	if err := appendMessage("system-"+agent.GenerateID(), "system", systemPrompt); err != nil {
		return "", fmt.Errorf("save system prompt: %w", err)
	}

	if subAgentCfg != nil {
		return effectiveLeafID, nil
	}

	userInstr := sessionconfig.FormatUserInstructions(sessionCfg.UserInstructions)
	if err := appendMessage("instructions-"+agent.GenerateID(), "user", userInstr); err != nil {
		return "", fmt.Errorf("save user instructions: %w", err)
	}

	runtimeReminder := formatRuntimeEnvironmentReminder(a.cwd, displayName)
	if err := appendMessage("runtime-"+agent.GenerateID(), "user", runtimeReminder); err != nil {
		return "", fmt.Errorf("save runtime reminder: %w", err)
	}

	recentThreadsReminder := a.formatRecentThreadsReminder(threadID)
	if err := appendMessage("recent-threads-"+agent.GenerateID(), "user", recentThreadsReminder); err != nil {
		return "", fmt.Errorf("save recent threads reminder: %w", err)
	}

	servicesReminder := sessionconfig.FormatDiscobotServicesReminder(sessionCfg.DiscobotServicesConfigured)
	if err := appendMessage("discobot-services-"+agent.GenerateID(), "user", servicesReminder); err != nil {
		return "", fmt.Errorf("save Discobot services reminder: %w", err)
	}

	hooksReminder := sessionconfig.FormatDiscobotHooksReminder(sessionCfg.DiscobotHooksConfigured)
	if err := appendMessage("discobot-hooks-"+agent.GenerateID(), "user", hooksReminder); err != nil {
		return "", fmt.Errorf("save Discobot hooks reminder: %w", err)
	}

	if hasNamedTool(tools, "Task") {
		subAgentReminder := sessionconfig.FormatSubAgentReminder(sessionCfg.SubAgents)
		if err := appendMessage("sub-agents-"+agent.GenerateID(), "user", subAgentReminder); err != nil {
			return "", fmt.Errorf("save sub-agent reminder: %w", err)
		}
	}

	if hasNamedTool(sessionCfg.Tools, "Skill") {
		skillWarningsReminder := sessionconfig.FormatSkillLikeDiscoveryWarningsReminder(sessionCfg.SkillDiscoveryWarnings, sessionCfg.ScriptDiscoveryWarnings)
		if err := appendMessage("skill-warnings-"+agent.GenerateID(), "user", skillWarningsReminder); err != nil {
			return "", fmt.Errorf("save skill-like discovery warnings reminder: %w", err)
		}

		skillsReminder := sessionconfig.FormatSkillLikeReminder(sessionCfg.Skills, sessionCfg.Scripts)
		if err := appendMessage("skills-"+agent.GenerateID(), "user", skillsReminder); err != nil {
			return "", fmt.Errorf("save skill-like reminder: %w", err)
		}
	}

	return effectiveLeafID, nil
}

func hasNamedTool(tools []providers.ToolDefinition, name string) bool {
	for _, tool := range tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}

func (a *DefaultAgent) resolveResumeMessageID(threadID string, state *thread.TurnState) string {
	if state == nil || state.LeafMsgID == "" {
		return ""
	}
	history, err := a.store.BuildHistory(threadID, state.LeafMsgID)
	if err != nil {
		return ""
	}
	uiMessages, err := message.ProjectUIMessages(history)
	if err != nil {
		return ""
	}
	for _, v := range slices.Backward(uiMessages) {
		if v.Role == "assistant" && v.ID != "" {
			return v.ID
		}
	}
	return ""
}

func (a *DefaultAgent) resolveCurrentLeaf(threadID string) (string, error) {
	state, err := a.store.LoadTurnState(threadID)
	if err != nil {
		return "", fmt.Errorf("load turn state: %w", err)
	}
	if state != nil && state.LeafMsgID != "" {
		return state.LeafMsgID, nil
	}

	cfg, err := a.store.LoadConfig(threadID)
	if err != nil {
		return "", fmt.Errorf("load thread config: %w", err)
	}
	if cfg.ActiveLeafID != "" {
		return cfg.ActiveLeafID, nil
	}
	if cfg.ContextReset {
		return "", nil
	}

	leafID, err := a.store.FindLeaf(threadID)
	if err != nil {
		return "", fmt.Errorf("find leaf: %w", err)
	}
	return leafID, nil
}

func (a *DefaultAgent) persistActiveLeaf(threadID string) {
	leafID, err := a.store.FindLeaf(threadID)
	if err != nil || leafID == "" {
		return
	}
	cfg, err := a.store.LoadConfig(threadID)
	if err != nil {
		return
	}
	if cfg.ActiveLeafID == leafID {
		return
	}
	cfg.ActiveLeafID = leafID
	_ = a.store.SaveConfig(threadID, cfg)
}

func (a *DefaultAgent) persistLastTurnState(threadID string, state thread.State) {
	cfg, err := a.store.LoadConfig(threadID)
	if err != nil || cfg.LastTurnState == state {
		return
	}
	cfg.LastTurnState = state
	_ = a.store.SaveConfig(threadID, cfg)
}

func (a *DefaultAgent) clearActiveCommand(
	threadID string,
	yield func(message.MessageChunk, error) bool,
) bool {
	cfg, err := a.store.LoadConfig(threadID)
	if err != nil {
		return yield(nil, fmt.Errorf("load thread config: %w", err))
	}
	if strings.TrimSpace(cfg.ActiveCommand) == "" {
		return true
	}
	cfg.ActiveCommand = ""
	if err := a.store.SaveConfig(threadID, cfg); err != nil {
		return yield(nil, fmt.Errorf("save thread config: %w", err))
	}
	return yield(thread.UpdateChunkFromConfig(threadID, cfg), nil)
}

// builtinCommands are slash commands handled natively by the agent, independent
// of any user-defined skills or legacy commands.
var builtinCommands = []agent.Command{
	{Name: "compact", Description: "Force conversation compaction immediately.", Kind: agent.CommandKindBuiltin},
	{Name: "reset", Description: "Reset the active conversation context.", Kind: agent.CommandKindBuiltin},
}

// ListCommands returns all slash commands available to the user: user-defined
// skills, legacy commands discovered from the project and home directories,
// and built-in commands handled by the agent itself.
func (a *DefaultAgent) ListCommands() ([]agent.Command, error) {
	sessionCfg, err := sessionconfig.Load(a.cwd)
	if err != nil {
		// Non-fatal: return built-ins only.
		return builtinCommands, nil //nolint:nilerr
	}

	commands := make([]agent.Command, 0, len(sessionCfg.Skills)+len(sessionCfg.Scripts)+len(builtinCommands))
	for _, s := range sessionCfg.Skills {
		commands = append(commands, agent.Command{
			Name:        s.Name,
			Description: s.Description,
			Kind:        agent.CommandKind(s.Kind),
			Discobot:    discobotCommandMetadata(s.Discobot),
		})
	}
	for _, script := range sessionCfg.Scripts {
		if !script.Visible {
			continue
		}
		commands = append(commands, agent.Command{
			Name:        script.Name,
			Description: script.Description,
			Kind:        agent.CommandKindScript,
			Discobot:    discobotCommandMetadata(script.Discobot),
		})
	}
	commands = append(commands, builtinCommands...)
	return commands, nil
}

// resolveSlashCommand checks whether the user parts contain a single text
// message starting with "/command-name [args]".
//
// Skills (.claude/skills/ or .discobot/skills/) take precedence. When found,
// the original text is passed through unchanged so the model can decide whether
// to invoke the Skill tool, but discobot metadata is attached for UI/state.
//
// Legacy commands (.claude/commands/ or .discobot/commands/) are expanded
// programmatically to match Claude Code behavior. The returned original text
// preserves the raw slash command for message-level UI metadata.
func resolveSlashCommand(cwd string, parts []message.UIPart) ([]message.UIPart, string, string, *thread.UserSlashCommandMetadata) {
	first, parsed, ok := parseSlashCommand(parts)
	if !ok {
		return parts, "", "", nil
	}

	projectRoot := sessionconfig.FindProjectRoot(cwd)
	cfg, found, err := resolveSlashCommandMatch(projectRoot, parsed)
	if err != nil || !found {
		return parts, "", "", nil // not a known slash command — pass through unchanged
	}

	kind := skillLikeKindToCommandKind(cfg.Kind)
	if kind == "" {
		return parts, "", "", nil
	}

	replacement := first.Text
	if kind == agent.CommandKindCommand {
		expanded, err := cfg.Expand(parsed.args)
		if err != nil {
			return parts, "", "", nil
		}
		replacement = expanded
	}

	annotated := annotateSlashCommandParts(parts, first, parsed.original, kind, replacement)
	return annotated, parsed.original, parsed.name, slashCommandMetadata(parsed, kind, cfg)
}

func scriptCommandArgs(originalText string) string {
	_, parsed, ok := parseSlashCommand([]message.UIPart{message.UITextPart{Text: originalText}})
	if !ok {
		return ""
	}
	return parsed.args
}

func firstUserText(parts []message.UIPart) string {
	for _, part := range parts {
		if textPart, ok := part.(message.UITextPart); ok {
			return textPart.Text
		}
	}
	return ""
}

func buildStandaloneUserMessageMetadata(metadata json.RawMessage, originalText string, slashCommand *thread.UserSlashCommandMetadata) json.RawMessage {
	payload := map[string]any{}
	if len(metadata) > 0 {
		if err := json.Unmarshal(metadata, &payload); err != nil {
			return metadata
		}
	}
	if originalText != "" {
		payload["originalText"] = originalText
	}
	if slashCommand != nil {
		payload["slashCommand"] = slashCommand
	}
	if len(payload) == 0 {
		return nil
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	return data
}

// persistUserOnlyTurn handles the special case where a slash-command script
// completes without producing any LLM-visible prompt content. In the normal
// path, thread.RunTurn persists the user message and advances the thread state.
// Here we skip RunTurn entirely, so agentimpl must persist the user message,
// update the active leaf, and emit the user chunk directly.
func (a *DefaultAgent) persistUserOnlyTurn(
	threadID, parentID string,
	cfg thread.TurnConfig,
	cfgToSave thread.Config,
	yield func(message.MessageChunk, error) bool,
) bool {
	startedAt := time.Now().UTC()
	userMessage := message.Message{
		ID:        "user-" + agent.GenerateID(),
		Role:      "user",
		Parts:     cfg.UserParts,
		Metadata:  buildStandaloneUserMessageMetadata(cfg.Metadata, cfg.OriginalUserText, cfg.SlashCommand),
		CreatedAt: &startedAt,
	}
	if err := a.store.SaveMessage(threadID, thread.StoredMessage{
		ID:       userMessage.ID,
		ParentID: parentID,
		Message:  userMessage,
	}); err != nil {
		return yield(nil, fmt.Errorf("save user message: %w", err))
	}
	cfgToSave.ActiveLeafID = userMessage.ID
	if err := a.store.SaveConfig(threadID, cfgToSave); err != nil {
		return yield(nil, fmt.Errorf("save thread config: %w", err))
	}
	uiMessages, err := message.ProjectUIMessages([]message.Message{userMessage})
	if err != nil {
		return yield(nil, fmt.Errorf("project user message for stream: %w", err))
	}
	if len(uiMessages) != 1 {
		return yield(nil, fmt.Errorf("project user message for stream: expected 1 UI message, got %d", len(uiMessages)))
	}
	if !yield(message.UserMessageChunk{Data: message.UserMessageData{Message: uiMessages[0]}}, nil) {
		return false
	}
	a.persistActiveLeaf(threadID)
	return true
}

// ListThreads returns all thread IDs.
func (a *DefaultAgent) ListThreads() ([]string, error) {
	return a.store.ListThreads()
}

func (a *DefaultAgent) ListThreadInfos() ([]agent.ThreadInfo, error) {
	infos, err := a.store.ListThreadInfos()
	if err != nil {
		return nil, err
	}
	result := make([]agent.ThreadInfo, 0, len(infos))
	for _, info := range infos {
		result = append(result, threadInfoToAgent(info))
	}
	return result, nil
}

func (a *DefaultAgent) GetThreadInfo(threadID string) (agent.ThreadInfo, error) {
	info, err := a.store.GetThreadInfo(threadID)
	if err != nil {
		return agent.ThreadInfo{}, err
	}
	return threadInfoToAgent(info), nil
}

func (a *DefaultAgent) GetThreadTokenUsageDetails(threadID string) (agent.ThreadTokenUsageDetails, error) {
	info, err := a.GetThreadInfo(threadID)
	if err != nil {
		return agent.ThreadTokenUsageDetails{}, err
	}

	turnIDs, err := a.store.ListTurnIDs(threadID)
	if err != nil {
		return agent.ThreadTokenUsageDetails{}, err
	}

	turns := make([]agent.TokenUsageTurn, 0, len(turnIDs))
	for _, turnID := range turnIDs {
		record, err := a.store.LoadTurnRecord(threadID, turnID)
		if err != nil {
			return agent.ThreadTokenUsageDetails{}, err
		}
		if record == nil {
			continue
		}

		stepIndexes, err := a.store.ListStepResultIndexes(threadID, turnID)
		if err != nil {
			return agent.ThreadTokenUsageDetails{}, err
		}

		turnUsage := message.Usage{}
		steps := make([]agent.TokenUsageStep, 0, len(stepIndexes))
		for _, stepIndex := range stepIndexes {
			result, err := a.store.LoadStepResult(threadID, turnID, stepIndex)
			if err != nil {
				return agent.ThreadTokenUsageDetails{}, err
			}
			if result == nil {
				continue
			}

			toolCalls := make([]agent.TokenUsageToolCall, 0, len(result.ToolCalls))
			for _, toolCall := range result.ToolCalls {
				toolCalls = append(toolCalls, agent.TokenUsageToolCall{
					ID:   toolCall.ToolCallID,
					Name: toolCall.ToolName,
				})
			}
			turnUsage = addAgentUsage(turnUsage, result.Usage)
			steps = append(steps, agent.TokenUsageStep{
				Index:              stepIndex,
				AssistantMessageID: strings.TrimSpace(result.AssistantMessageID),
				ToolCalls:          toolCalls,
				Usage:              result.Usage,
			})
		}

		startedAt := ""
		if record.StartedAt != nil {
			startedAt = record.StartedAt.UTC().Format(time.RFC3339Nano)
		}
		finishedAt := ""
		if record.FinishedAt != nil {
			finishedAt = record.FinishedAt.UTC().Format(time.RFC3339Nano)
		}

		turns = append(turns, agent.TokenUsageTurn{
			ID:              record.ID,
			Model:           record.Config.Model,
			Reasoning:       string(record.Config.Reasoning),
			ServiceTier:     record.Config.ServiceTier,
			ModelMaxTokens:  record.Config.ContextWindow,
			MaxOutputTokens: record.Config.MaxOutputTokens,
			Prices:          record.Config.TokenPrices,
			Usage:           turnUsage,
			StartedAt:       startedAt,
			FinishedAt:      finishedAt,
			Steps:           steps,
		})
	}

	slices.SortStableFunc(turns, func(a, b agent.TokenUsageTurn) int {
		if a.StartedAt != b.StartedAt {
			return strings.Compare(a.StartedAt, b.StartedAt)
		}
		return strings.Compare(a.ID, b.ID)
	})

	return agent.ThreadTokenUsageDetails{
		ThreadID: threadID,
		Summary:  info.TokenUsage,
		Turns:    turns,
	}, nil
}

func (a *DefaultAgent) CreateThread(_ context.Context, req agent.CreateThreadRequest) (agent.ThreadInfo, error) {
	info, err := a.store.CreateThreadInfo(a.cwd, thread.CreateThreadRequest(req))
	if err != nil {
		return agent.ThreadInfo{}, err
	}
	return threadInfoToAgent(info), nil
}

func (a *DefaultAgent) UpdateThread(_ context.Context, threadID string, req agent.UpdateThreadRequest) (agent.ThreadInfo, error) {
	info, err := a.store.UpdateThreadInfo(threadID, thread.UpdateThreadRequest(req))
	if err != nil {
		return agent.ThreadInfo{}, err
	}
	return threadInfoToAgent(info), nil
}

func threadInfoToAgent(info thread.Info) agent.ThreadInfo {
	return agent.ThreadInfo{
		ID:           info.ID,
		Name:         info.Name,
		CWD:          info.CWD,
		LastMessage:  info.LastMessage,
		ErrorMessage: info.ErrorMessage,
		Model:        info.Model,
		Reasoning:    info.Reasoning,
		ServiceTier:  info.ServiceTier,
		State:        agent.ThreadState(info.State),
		TokenUsage: agent.TokenUsageInfo{
			Total:           info.TokenUsage.Total,
			LastStep:        info.TokenUsage.LastStep,
			LastTurn:        info.TokenUsage.LastTurn,
			ModelMaxTokens:  info.TokenUsage.ModelMaxTokens,
			MaxOutputTokens: info.TokenUsage.MaxOutputTokens,
			Prices:          info.TokenUsage.Prices,
		},
		PendingQuestion: info.PendingQuestion,
		ActiveCommand:   info.ActiveCommand,
		Metadata:        info.Metadata,
	}
}

func addAgentUsage(a, b message.Usage) message.Usage {
	a.InputTokens.Total += b.InputTokens.Total
	a.InputTokens.NoCache += b.InputTokens.NoCache
	a.InputTokens.CacheRead += b.InputTokens.CacheRead
	a.InputTokens.CacheWrite += b.InputTokens.CacheWrite
	a.OutputTokens.Total += b.OutputTokens.Total
	a.OutputTokens.Text += b.OutputTokens.Text
	a.OutputTokens.Reasoning += b.OutputTokens.Reasoning
	return a
}

func (a *DefaultAgent) DeleteThread(_ context.Context, threadID string) error {
	a.Cancel(threadID)
	return a.store.DeleteThreadInfo(threadID)
}

// HasInterruptedTurn reports whether threadID has an unfinished turn.
// Threads paused for AskUserQuestion (waiting_for_answer) are excluded.
func (a *DefaultAgent) HasInterruptedTurn(threadID string) (bool, error) {
	state, err := a.store.LoadTurnState(threadID)
	if err != nil {
		return false, err
	}
	return state != nil && state.Phase != thread.PhaseWaitingForAnswer, nil
}

// PendingQuestion returns the pending AskUserQuestion for a thread, or nil.
func (a *DefaultAgent) PendingQuestion(threadID string) (*agent.PendingQuestion, error) {
	state, err := a.store.LoadTurnState(threadID)
	if err != nil {
		return nil, err
	}
	if state == nil || state.Phase != thread.PhaseWaitingForAnswer {
		return nil, nil
	}
	q, err := a.store.LoadQuestion(threadID, state.ID, state.PendingApprovalID)
	if err != nil {
		return nil, err
	}
	if q == nil {
		return nil, nil
	}
	var questions []api.AskUserQuestion
	if len(q.Questions) > 0 {
		if err := json.Unmarshal(q.Questions, &questions); err != nil {
			return nil, fmt.Errorf("unmarshal questions: %w", err)
		}
	}
	var credentials []api.RequestedCredential
	if len(q.Credentials) > 0 {
		if err := json.Unmarshal(q.Credentials, &credentials); err != nil {
			return nil, fmt.Errorf("unmarshal credentials: %w", err)
		}
	}
	return &agent.PendingQuestion{
		ApprovalID:  q.ApprovalID,
		Questions:   questions,
		Credentials: credentials,
		Metadata:    q.Metadata,
		Context:     q.Context,
	}, nil
}

// SubmitAnswer persists the user's response for a pending approval.
func (a *DefaultAgent) SubmitAnswer(threadID, approvalID string, req api.AnswerQuestionRequest) error {
	state, err := a.store.LoadTurnState(threadID)
	if err != nil {
		return fmt.Errorf("load turn state: %w", err)
	}
	if state == nil || state.Phase != thread.PhaseWaitingForAnswer {
		return fmt.Errorf("no pending question for thread %s", threadID)
	}

	// Verify the toolCallID matches.
	q, err := a.store.LoadQuestion(threadID, state.ID, approvalID)
	if err != nil {
		return fmt.Errorf("load question: %w", err)
	}
	if q == nil || q.ApprovalID != approvalID {
		return fmt.Errorf("question %s not found for thread %s", approvalID, threadID)
	}

	return a.store.SaveAnswer(threadID, state.ID, thread.QuestionAnswer{
		ApprovalID:  approvalID,
		Answers:     req.Answers,
		Credentials: req.Credentials,
	})
}

// filterTools applies allowed/disallowed lists to a tool set.
// If allowedTools is non-empty, only listed tools are kept.
// disallowedTools always removes the named tools.
func filterTools(tools []providers.ToolDefinition, allowedTools, disallowedTools []string) []providers.ToolDefinition {
	if len(allowedTools) == 0 && len(disallowedTools) == 0 {
		return tools
	}

	allowed := make(map[string]bool, len(allowedTools))
	for _, t := range allowedTools {
		allowed[t] = true
	}

	disallowed := make(map[string]bool, len(disallowedTools))
	for _, t := range disallowedTools {
		disallowed[t] = true
	}

	filtered := make([]providers.ToolDefinition, 0, len(tools))
	for _, t := range tools {
		if len(allowedTools) > 0 && !allowed[t.Name] {
			continue
		}
		if disallowed[t.Name] {
			continue
		}
		filtered = append(filtered, t)
	}
	return filtered
}
