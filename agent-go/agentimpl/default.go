// Package agentimpl provides the default Agent implementation.
// The Agent interface and PromptRequest type live in the agent package;
// this package contains the concrete DefaultAgent that implements them.
package agentimpl

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"log"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/obot-platform/discobot/agent-go/agent"
	"github.com/obot-platform/discobot/agent-go/internal/api"
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

	mcpMu      sync.Mutex
	mcpMgr     *mcp.Manager                    // nil until first Prompt with MCP servers
	mcpServers []sessionconfig.MCPServerConfig // config the manager was initialized with
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
	threadCfg             thread.Config
	useThreadConfig       bool
	sessionCfg            *sessionconfig.SessionConfig
	subAgentCfg           *sessionconfig.SubAgentConfig
	tools                 []providers.ToolDefinition
	modelRef              providers.ModelRef
	threadSummaryRef      providers.ModelRef
	displayName           string
	systemPrompt          string
	mcpMgr                *mcp.Manager
	executor              thread.ToolExecutor
	planMode              bool
	modeChangedByPrompt   bool
	promptRequestPlanMode bool
	currentDepth          int
	maxSteps              int
}

// Store returns the underlying thread store.
func (a *DefaultAgent) Store() *thread.Store {
	return a.store
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
	return &DefaultAgent{
		store:    store,
		registry: registry,
		executor: executor,
		cwd:      cwd,
		mcpCfg:   mcpCfg,
		cancels:  make(map[string]context.CancelFunc),
	}
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
//
// If the user message is exactly "/compact", compaction is forced immediately
// without running a normal LLM turn.
func (a *DefaultAgent) Prompt(ctx context.Context, threadID string, req agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
	if isCompactCommand(req.UserParts) {
		return a.handleCompactCommand(ctx, threadID, req)
	}

	env, err := a.resolvePromptEnvironment(ctx, threadID, req)
	if err != nil {
		return errorIter(err)
	}

	toolCtx := &thread.ToolContext{
		ThreadID:              threadID,
		PlanMode:              env.planMode,
		PromptRequestPlanMode: env.promptRequestPlanMode,
		SubagentDepth:         env.currentDepth,
		MaxSubagentDepth:      env.sessionCfg.MaxSubagentDepth,
		CurrentTaskID:         req.ParentTaskID,
		ProviderResolver:      a.registry,
		Agent:                 a,
		ProviderID:            env.modelRef.ProviderID,
		ModelID:               env.modelRef.ModelID,
	}

	cfg := thread.TurnConfig{
		ProviderID:            env.modelRef.ProviderID,
		Model:                 env.modelRef.ModelID,
		SupportingModels:      compactSupportingModels(env.modelRef, map[providers.SupportingModelType]providers.ModelRef{providers.SupportingModelThreadSummarization: env.threadSummaryRef}),
		Reasoning:             providers.Reasoning(req.Reasoning),
		PlanMode:              env.planMode,
		PromptRequestPlanMode: env.promptRequestPlanMode,
		UserParts:             message.UIPartsToParts(expandLegacyCommand(a.cwd, req.UserParts)),
		Tools:                 env.tools,
		MaxSteps:              env.maxSteps,
	}

	return func(yield func(message.MessageChunk, error) bool) {
		effectiveLeafID, err := a.resolveEffectiveLeafID(threadID, req.LeafID, req.FreshContext, env.systemPrompt, env.displayName, env.sessionCfg, env.subAgentCfg)
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
			yield(nil, agent.ErrInterruptedTurnRequiresResume)
			return
		}

		threadNameBg := a.startBackgroundThreadName(promptCtx, threadID, env.threadCfg, req.UserParts, env.threadSummaryRef)

		if env.modeChangedByPrompt {
			modeReminderID := "mode-" + agent.GenerateID()
			if err := a.store.SaveMessage(threadID, thread.StoredMessage{
				ID:       modeReminderID,
				ParentID: effectiveLeafID,
				Message: message.Message{
					Role:      "user",
					Synthetic: true,
					Parts: []message.Part{message.TextPart{
						Text: formatModeChangeReminder(env.planMode),
						ProviderMetadata: message.MarshalProviderMetadata(message.DiscobotPartMetadata{
							ReminderKind: "mode",
							Mode:         map[bool]string{true: "plan", false: "build"}[env.planMode],
						}),
					}},
				},
			}); err != nil {
				yield(nil, fmt.Errorf("save mode reminder: %w", err))
				return
			}
			effectiveLeafID = modeReminderID

			if !threadNameBg.flush(false, yield) {
				return
			}
		}

		// Resolve provider for new turn.
		provider, resolveErr := a.registry.Get(cfg.ProviderID)
		if resolveErr != nil {
			yield(nil, fmt.Errorf("resolve provider: %w", resolveErr))
			return
		}

		// Persist the resolved model and cwd so new sessions can resume by directory.
		cwd := filepath.Clean(a.cwd)
		if abs, err := filepath.Abs(cwd); err == nil {
			cwd = abs
		}
		if env.useThreadConfig && strings.TrimSpace(env.threadCfg.CWD) != "" {
			cwd = env.threadCfg.CWD
		}
		cfgToSave := thread.Config{
			Name:         env.threadCfg.Name,
			NameSource:   env.threadCfg.NameSource,
			LastMessage:  lastUserPromptFromUIParts(req.UserParts),
			Model:        cfg.ProviderID + "/" + cfg.Model,
			Reasoning:    cfg.Reasoning,
			CWD:          cwd,
			PlanMode:     env.planMode,
			ActiveLeafID: effectiveLeafID,
		}
		if err := a.store.SaveConfig(threadID, cfgToSave); err != nil {
			yield(nil, fmt.Errorf("save thread config: %w", err))
			return
		}
		if env.modeChangedByPrompt {
			if !yield(thread.UpdateChunkFromConfig(threadID, cfgToSave), nil) {
				return
			}
		}

		// Start new turn.
		for chunk, chunkErr := range thread.RunTurn(promptCtx, provider, env.executor, a.store, threadID, effectiveLeafID, cfg, toolCtx) {
			if !threadNameBg.flush(false, yield) {
				return
			}
			if !yield(chunk, chunkErr) {
				return
			}
		}
		a.persistActiveLeaf(threadID)
		if !threadNameBg.flush(true, yield) {
			return
		}
	}
}

// Resume continues or finalizes an interrupted turn from persisted disk state.
func (a *DefaultAgent) Resume(ctx context.Context, threadID string) iter.Seq2[message.MessageChunk, error] {
	return func(yield func(message.MessageChunk, error) bool) {
		state, err := a.store.LoadTurnState(threadID)
		if err != nil {
			yield(nil, err)
			return
		}
		if state == nil {
			yield(nil, agent.ErrInterruptedTurnRequiresResume)
			return
		}

		log.Printf("agent: resuming interrupted turn %s for thread %s (step %d, phase %s)",
			state.ID, threadID, state.CurrentStep, state.Phase)

		if ref, err := providers.ParseModelRef(state.Config.Model); err == nil {
			state.Config.ProviderID = ref.ProviderID
			state.Config.Model = ref.ModelID
		}

		provider, resolveErr := a.registry.Get(state.Config.ProviderID)
		if resolveErr != nil {
			yield(nil, fmt.Errorf("resolve provider for resume: %w", resolveErr))
			return
		}

		sessionCfg, err := sessionconfig.Load(a.cwd)
		if err != nil {
			log.Printf("agent: warning: session config: %v", err)
			sessionCfg = &sessionconfig.SessionConfig{MaxSubagentDepth: sessionconfig.DefaultMaxSubagentDepth}
		}

		toolCtx := &thread.ToolContext{
			ThreadID:              threadID,
			PlanMode:              state.Config.PlanMode,
			PromptRequestPlanMode: state.Config.PromptRequestPlanMode,
			MaxSubagentDepth:      sessionCfg.MaxSubagentDepth,
			ProviderResolver:      a.registry,
			Agent:                 a,
			ProviderID:            state.Config.ProviderID,
			ModelID:               state.Config.Model,
		}
		resumeMessageID := a.resolveResumeMessageID(threadID, state)

		executor := thread.ToolExecutor(a.executor)
		if mcpMgr := a.resolveMCPManager(ctx, sessionCfg); mcpMgr != nil {
			executor = mcp.NewExecutor(a.executor, mcpMgr)
		}

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

		if resumeMessageID != "" {
			if !yield(message.ThreadResumeChunk{
				Data: message.ThreadResumeData{ThreadID: threadID, MessageID: resumeMessageID},
			}, nil) {
				return
			}
		}

		for chunk, chunkErr := range thread.ResumeTurn(promptCtx, provider, executor, a.store, state, toolCtx) {
			if !yield(chunk, chunkErr) {
				return
			}
		}
		a.persistActiveLeaf(threadID)
	}
}

func (a *DefaultAgent) resolvePromptEnvironment(ctx context.Context, threadID string, req agent.PromptRequest) (*promptEnvironment, error) {
	threadCfg, threadCfgErr := a.store.LoadConfig(threadID)
	if threadCfgErr != nil {
		log.Printf("agent: warning: thread config: %v", threadCfgErr)
		threadCfg = thread.Config{}
	}
	useThreadConfig := threadCfgErr == nil
	planMode, modeChangedByPrompt := resolvePlanMode(req.Mode, threadCfg, useThreadConfig)
	promptRequestPlanMode := req.Mode == "plan"
	currentDepth := req.SubagentDepth
	if currentDepth < 0 {
		currentDepth = 0
	}

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

	mcpMgr := a.resolveMCPManager(ctx, sessionCfg)
	if mcpMgr != nil {
		tools = append(tools, mcpMgr.Tools()...)
	}
	executor := thread.ToolExecutor(a.executor)
	if mcpMgr != nil {
		executor = mcp.NewExecutor(a.executor, mcpMgr)
	}

	model := req.Model
	if model == "" && useThreadConfig && threadCfg.Model != "" {
		model = threadCfg.Model
	}

	currentProviderID := ""
	if useThreadConfig {
		currentProviderID = providers.CurrentProviderFromRef(threadCfg.Model)
	}

	ref, err := a.registry.ResolveModelInProvider(currentProviderID, model, providers.ModelTaskChat)
	if err != nil {
		return nil, fmt.Errorf("invalid model: %w", err)
	}
	if subAgentCfg != nil && subAgentCfg.Model != "" {
		ref, err = a.registry.ResolveModelInProvider(ref.ProviderID, subAgentCfg.Model, providers.ModelTaskChat)
		if err != nil {
			return nil, fmt.Errorf("invalid sub-agent model: %w", err)
		}
	}
	threadSummaryRef, err := a.registry.ResolveSupportingModel(ref, supportingModels, providers.SupportingModelThreadSummarization)
	if err != nil {
		return nil, fmt.Errorf("invalid thread summarization model: %w", err)
	}

	displayName := resolveModelDisplayName(ref.ProviderID, ref.ModelID)
	if req.Tools == nil && req.SubagentType == "" {
		tools = sessionconfig.BuiltinTools(displayName)
		if mcpMgr != nil {
			tools = append(tools, mcpMgr.Tools()...)
		}
	}

	maxSteps := req.MaxTurns
	if subAgentCfg != nil && subAgentCfg.MaxTurns > 0 {
		if maxSteps == 0 || subAgentCfg.MaxTurns < maxSteps {
			maxSteps = subAgentCfg.MaxTurns
		}
	}

	return &promptEnvironment{
		threadCfg:             threadCfg,
		useThreadConfig:       useThreadConfig,
		sessionCfg:            sessionCfg,
		subAgentCfg:           subAgentCfg,
		tools:                 tools,
		modelRef:              ref,
		threadSummaryRef:      threadSummaryRef,
		displayName:           displayName,
		systemPrompt:          systemPrompt,
		mcpMgr:                mcpMgr,
		executor:              executor,
		planMode:              planMode,
		modeChangedByPrompt:   modeChangedByPrompt,
		promptRequestPlanMode: promptRequestPlanMode,
		currentDepth:          currentDepth,
		maxSteps:              maxSteps,
	}, nil
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
	return tools
}

func (a *DefaultAgent) resolveMCPManager(ctx context.Context, sessionCfg *sessionconfig.SessionConfig) *mcp.Manager {
	a.mcpMu.Lock()
	defer a.mcpMu.Unlock()
	if !mcpServersEqual(a.mcpServers, sessionCfg.MCPServers) {
		if a.mcpMgr != nil {
			log.Printf("agent: .mcp.json changed, reloading MCP manager")
			a.mcpMgr.Close()
			a.mcpMgr = nil
		}
		if len(sessionCfg.MCPServers) > 0 {
			callback := mcp.MakeTokenCallback(a.mcpCfg.discobotServerURL, a.mcpCfg.projectID)
			a.mcpMgr = mcp.NewManager(callback)
			a.mcpMgr.Connect(ctx, sessionCfg.MCPServers, a.mcpCfg.redirectBase, a.mcpCfg.sessionID)
		}
		a.mcpServers = sessionCfg.MCPServers
	}
	return a.mcpMgr
}

// mcpServersEqual reports whether two MCP server config slices are identical.
// Uses JSON marshaling so that any field change (URL, auth, args, etc.) triggers a reload.
func mcpServersEqual(a, b []sessionconfig.MCPServerConfig) bool {
	if len(a) != len(b) {
		return false
	}
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	return string(aj) == string(bj)
}

func resolvePlanMode(reqMode string, cfg thread.Config, hasConfig bool) (planMode bool, changedByPrompt bool) {
	previousPlanMode := false
	if hasConfig {
		previousPlanMode = cfg.PlanMode
		planMode = cfg.PlanMode
	}
	if reqMode == "" {
		return planMode, false
	}
	planMode = reqMode == "plan"
	return planMode, planMode != previousPlanMode
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

func formatModeChangeReminder(planMode bool) string {
	if planMode {
		return "<system-reminder>\nMode update: the current mode is now plan. This change was triggered by the current prompt request.\n</system-reminder>"
	}
	return "<system-reminder>\nMode update: the current mode is now build. Plan mode has been exited. This change was triggered by the current prompt request.\n</system-reminder>"
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
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range override {
		merged[k] = v
	}
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

	gitState := gitStateSnapshot(resolvedCWD)

	var b strings.Builder
	b.WriteString("<system-reminder>\n")
	b.WriteString("Runtime environment snapshot:\n")
	fmt.Fprintf(&b, "- Current working directory: %s\n", resolvedCWD)
	fmt.Fprintf(&b, "- OS/platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(&b, "- Current date/time: %s\n", time.Now().Format(time.RFC3339))
	if modelName != "" {
		fmt.Fprintf(&b, "- Current model: %s\n", modelName)
	}
	fmt.Fprintf(&b, "- Git state (captured at the current time of this reminder; this may change throughout the conversation): %s\n", gitState)
	b.WriteString("</system-reminder>")
	return b.String()
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
	cmdCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
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
			yield(message.TextDeltaChunk{Delta: "Nothing to compact yet."}, nil)
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
	if summaryRef, err := a.registry.ResolveSupportingModel(ref, req.SupportingModels, providers.SupportingModelThreadSummarization); err == nil {
		turnCfg.SupportingModels = compactSupportingModels(ref, map[providers.SupportingModelType]providers.ModelRef{
			providers.SupportingModelThreadSummarization: summaryRef,
		})
	}

	return func(yield func(message.MessageChunk, error) bool) {
		compacted, compactErr := thread.ForceCompactThread(ctx, provider, a.registry, a.store, threadID, leafID, turnCfg)
		if compactErr != nil {
			yield(nil, fmt.Errorf("force compaction: %w", compactErr))
			return
		}

		if compacted {
			yield(message.TextDeltaChunk{Delta: "Conversation compacted."}, nil)
			return
		}

		yield(message.TextDeltaChunk{Delta: "Nothing to compact yet."}, nil)
	}
}

// Cancel cancels the active prompt for a thread.
func (a *DefaultAgent) Cancel(threadID string) bool {
	a.mu.Lock()
	cancel, ok := a.cancels[threadID]
	a.mu.Unlock()
	if ok {
		cancel()
		return true
	}
	return false
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
	history, err := a.store.BuildHistory(threadID, leafID)
	if err != nil {
		return nil, err
	}
	return message.ProjectUIMessages(history)
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
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == "assistant" {
			var sb strings.Builder
			for _, p := range history[i].Parts {
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
	requestedLeafID string,
	startFresh bool,
	systemPrompt string,
	displayName string,
	sessionCfg *sessionconfig.SessionConfig,
	subAgentCfg *sessionconfig.SubAgentConfig,
) (string, error) {
	effectiveLeafID := requestedLeafID
	if effectiveLeafID != "" {
		valid, err := a.store.IsLeaf(threadID, effectiveLeafID)
		if err != nil {
			return "", fmt.Errorf("validate requested leaf: %w", err)
		}
		if !valid {
			return "", fmt.Errorf("message %q is not a valid leaf in this thread; the thread may have diverged", effectiveLeafID)
		}
	}

	if effectiveLeafID == "" && hasStartupBootstrapContent(systemPrompt, displayName, sessionCfg, subAgentCfg) {
		leaf, err := a.resolveExistingLeafForPrompt(threadID, startFresh)
		if err != nil {
			return "", fmt.Errorf("resolve current leaf: %w", err)
		}
		if leaf != "" {
			return leaf, nil
		}
		return a.bootstrapNewThreadMessages(threadID, systemPrompt, displayName, sessionCfg, subAgentCfg)
	}

	if effectiveLeafID == "" && !startFresh {
		leaf, err := a.resolveCurrentLeaf(threadID)
		if err != nil {
			return "", fmt.Errorf("resolve current leaf: %w", err)
		}
		effectiveLeafID = leaf
	}

	return effectiveLeafID, nil
}

func hasStartupBootstrapContent(
	systemPrompt string,
	displayName string,
	sessionCfg *sessionconfig.SessionConfig,
	subAgentCfg *sessionconfig.SubAgentConfig,
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
	return sessionconfig.FormatSkillsReminder(sessionCfg.Skills) != ""
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

	skillsReminder := sessionconfig.FormatSkillsReminder(sessionCfg.Skills)
	if err := appendMessage("skills-"+agent.GenerateID(), "user", skillsReminder); err != nil {
		return "", fmt.Errorf("save skills reminder: %w", err)
	}

	return effectiveLeafID, nil
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
	for index := len(uiMessages) - 1; index >= 0; index-- {
		if uiMessages[index].Role == "assistant" && uiMessages[index].ID != "" {
			return uiMessages[index].ID
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

// ListModels returns available models from all registered providers.
// Model IDs are prefixed with "providerId/".
func (a *DefaultAgent) ListModels(ctx context.Context) ([]providers.ModelInfo, error) {
	return a.registry.ListModels(ctx)
}

// builtinCommands are slash commands handled natively by the agent, independent
// of any user-defined skills or legacy commands.
var builtinCommands = []agent.Command{
	{Name: "compact", Description: "Force conversation compaction immediately.", Kind: agent.CommandKindBuiltin},
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

	commands := make([]agent.Command, 0, len(sessionCfg.Skills)+len(builtinCommands))
	for _, s := range sessionCfg.Skills {
		commands = append(commands, agent.Command{Name: s.Name, Description: s.Description, Kind: agent.CommandKind(s.Kind)})
	}
	commands = append(commands, builtinCommands...)
	return commands, nil
}

// expandLegacyCommand checks whether the user parts contain a single text
// message starting with "/command-name [args]" that maps to a legacy command
// (i.e. a file in .claude/commands/ or .discobot/commands/).
//
// If found, the text part is replaced with the expanded command body so that
// the LLM receives the instructions directly — matching how the real Claude
// CLI handles slash commands programmatically rather than via the Skill tool.
//
// Skills (.claude/skills/) are intentionally excluded: they are invoked by the
// LLM through the Skill tool, not expanded here.
func expandLegacyCommand(cwd string, parts []message.UIPart) []message.UIPart {
	if len(parts) == 0 {
		return parts
	}
	first, ok := parts[0].(message.UITextPart)
	if !ok {
		return parts
	}
	text := strings.TrimLeft(first.Text, " \t")
	if !strings.HasPrefix(text, "/") {
		return parts
	}

	// Parse "/command-name [args...]"
	rest := text[1:]
	var cmdName, args string
	if idx := strings.IndexAny(rest, " \t\n"); idx >= 0 {
		cmdName = rest[:idx]
		args = strings.TrimLeft(rest[idx:], " \t")
	} else {
		cmdName = rest
	}
	if cmdName == "" {
		return parts
	}

	projectRoot := sessionconfig.FindProjectRoot(cwd)
	cmd, found, err := sessionconfig.LookupCommand(projectRoot, cmdName)
	if err != nil || !found {
		return parts // not a known command — pass through unchanged
	}

	// Encode the original slash command into ProviderMetadata so the UI can
	// display "/commit fix the bug" while the LLM receives the expanded body.
	meta := message.MarshalProviderMetadata(message.DiscobotPartMetadata{
		OriginalCommand: text,
	})

	expanded := make([]message.UIPart, len(parts))
	copy(expanded, parts)
	expanded[0] = message.UITextPart{Text: cmd.Expand(args), State: first.State, ProviderMetadata: meta}
	return expanded
}

// ListThreads returns all thread IDs.
func (a *DefaultAgent) ListThreads() ([]string, error) {
	return a.store.ListThreads()
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
	return &agent.PendingQuestion{
		ApprovalID: q.ApprovalID,
		Questions:  questions,
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
		ApprovalID: approvalID,
		Answers:    req.Answers,
	})
}

// isCompactCommand reports whether the user parts contain exactly the /compact command.
func isCompactCommand(parts []message.UIPart) bool {
	if len(parts) != 1 {
		return false
	}
	tp, ok := parts[0].(message.UITextPart)
	return ok && strings.TrimSpace(tp.Text) == "/compact"
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
