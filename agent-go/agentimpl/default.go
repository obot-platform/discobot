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

	mu        sync.Mutex
	cancels   map[string]context.CancelFunc
	clearNext sync.Map // threadID → struct{}: next Prompt should start fresh

	mcpMu      sync.Mutex
	mcpMgr     *mcp.Manager                    // nil until first Prompt with MCP servers
	mcpServers []sessionconfig.MCPServerConfig // config the manager was initialized with
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
// If the thread has an interrupted turn, it resumes that instead.
//
// If req.SubagentType is set, the named SubAgentConfig from session config is
// used to restrict tools, override the model, and set the system prompt.
//
// The req.Model field should be in "providerId/modelId" format for new turns.
// For resume (empty req), the provider is resolved from the persisted turn state.
//
// If the user message is exactly "/clear", the thread is marked to start a fresh
// branch on the next Prompt call and a confirmation is streamed back without
// contacting the LLM. If the user message is exactly "/compact", compaction is
// forced immediately without running a normal LLM turn.
func (a *DefaultAgent) Prompt(ctx context.Context, threadID string, req agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
	// Handle /clear internally: mark the thread for a fresh start next turn
	// and return a confirmation without making any LLM call.
	if isClearCommand(req.UserParts) {
		a.clearNext.Store(threadID, struct{}{})
		return func(yield func(message.MessageChunk, error) bool) {
			yield(message.TextDeltaChunk{Delta: "Thread cleared. Next message starts a fresh conversation (history preserved on disk)."}, nil)
		}
	}

	if isCompactCommand(req.UserParts) {
		return a.handleCompactCommand(ctx, threadID, req)
	}

	threadCfg, threadCfgErr := a.store.LoadConfig(threadID)
	planMode, modeChangedByPrompt := resolvePlanMode(req.Mode, threadCfg, threadCfgErr == nil)
	promptRequestPlanMode := req.Mode == "plan"
	currentDepth := req.SubagentDepth
	if currentDepth < 0 {
		currentDepth = 0
	}
	toolCtx := &thread.ToolContext{
		ThreadID:              threadID,
		PlanMode:              planMode,
		PromptRequestPlanMode: promptRequestPlanMode,
		SubagentDepth:         currentDepth,
		CurrentTaskID:         req.ParentTaskID,
		Agent:                 a,
	}

	// Load session config from the working directory.
	sessionCfg, err := sessionconfig.Load(a.cwd)
	if err != nil {
		log.Printf("agent: warning: session config: %v", err)
		sessionCfg = &sessionconfig.SessionConfig{MaxSubagentDepth: sessionconfig.DefaultMaxSubagentDepth}
	}
	toolCtx.MaxSubagentDepth = sessionCfg.MaxSubagentDepth

	// Look up sub-agent config if a subagent type is specified.
	var subAgentCfg *sessionconfig.SubAgentConfig
	if req.SubagentType != "" {
		for i := range sessionCfg.SubAgents {
			if sessionCfg.SubAgents[i].Name == req.SubagentType {
				subAgentCfg = &sessionCfg.SubAgents[i]
				break
			}
		}
		if subAgentCfg == nil {
			return func(yield func(message.MessageChunk, error) bool) {
				yield(nil, fmt.Errorf("sub-agent type %q not found in session config", req.SubagentType))
			}
		}
	}

	// Determine tool set: sub-agent restrictions take priority over request override.
	tools := req.Tools
	if tools == nil {
		tools = sessionCfg.Tools
	}
	if toolCtx.MaxSubagentDepth > 0 && toolCtx.SubagentDepth >= toolCtx.MaxSubagentDepth {
		tools = filterTools(tools, nil, []string{"Task", "Agent"})
	}
	if subAgentCfg != nil {
		tools = filterTools(tools, subAgentCfg.AllowedTools, subAgentCfg.DisallowedTools)
	}

	// Determine model: sub-agent model overrides request model.
	model := req.Model
	if subAgentCfg != nil && subAgentCfg.Model != "" {
		model = subAgentCfg.Model
	}

	// Determine system prompt: sub-agent prompt overrides session default.
	systemPrompt := sessionCfg.SystemPrompt
	if subAgentCfg != nil && subAgentCfg.Prompt != "" {
		systemPrompt = subAgentCfg.Prompt
	}

	// Init or reload MCP manager whenever the server list changes.
	var mcpMgr *mcp.Manager
	a.mcpMu.Lock()
	if !mcpServersEqual(a.mcpServers, sessionCfg.MCPServers) {
		if a.mcpMgr != nil {
			log.Printf("agent: .mcp.json changed, reloading MCP manager")
			a.mcpMgr.Close()
			a.mcpMgr = nil
		}
		if len(sessionCfg.MCPServers) > 0 {
			callback := mcp.MakeTokenCallback(a.mcpCfg.discobotServerURL, a.mcpCfg.projectID)
			a.mcpMgr = mcp.NewManager(callback)
			a.mcpMgr.Connect(ctx, sessionCfg.MCPServers,
				a.mcpCfg.redirectBase, a.mcpCfg.sessionID)
		}
		a.mcpServers = sessionCfg.MCPServers
	}
	mcpMgr = a.mcpMgr
	a.mcpMu.Unlock()

	if mcpMgr != nil {
		// Augment tool list with all currently-connected MCP tools.
		tools = append(tools, mcpMgr.Tools()...)
	}

	// If no model is explicitly requested, fall back to the model last used for
	// this thread (persisted in its config.json). This lets new sessions continue
	// with the same provider/model without the user needing to re-select.
	if model == "" && threadCfgErr == nil && threadCfg.Model != "" {
		model = threadCfg.Model
	}

	// Resolve the model reference: "" → provider default, "providerID" → provider default,
	// "provider/model" → explicit. Always resolves to a concrete provider/model pair.
	ref, err := a.registry.ResolveModel(model, providers.ModelTaskChat)
	if err != nil {
		return errorIter(fmt.Errorf("invalid model: %w", err))
	}
	providerID, modelID := ref.ProviderID, ref.ModelID
	toolCtx.ProviderID = providerID
	toolCtx.ModelID = modelID

	// Resolve a human-readable model name for use in system reminders and
	// commit co-author attribution. Falls back to the bare model ID.
	displayName := resolveModelDisplayName(providerID, modelID)

	// Rebuild default tools with the resolved model display name so the commit
	// co-author line includes the actual model name. Only applies when tools
	// come from session defaults (not custom req.Tools or sub-agent overrides).
	if req.Tools == nil && req.SubagentType == "" {
		tools = sessionconfig.BuiltinTools(displayName)
		if mcpMgr != nil {
			tools = append(tools, mcpMgr.Tools()...)
		}
	}

	// MaxSteps: take the stricter of the request value and the sub-agent config value.
	maxSteps := req.MaxTurns
	if subAgentCfg != nil && subAgentCfg.MaxTurns > 0 {
		if maxSteps == 0 || subAgentCfg.MaxTurns < maxSteps {
			maxSteps = subAgentCfg.MaxTurns
		}
	}

	cfg := thread.TurnConfig{
		ProviderID:            providerID,
		Model:                 modelID,
		Reasoning:             providers.Reasoning(req.Reasoning),
		PromptRequestPlanMode: promptRequestPlanMode,
		UserParts:             message.UIPartsToParts(expandLegacyCommand(a.cwd, req.UserParts)),
		Tools:                 tools,
		MaxSteps:              maxSteps,
	}

	return func(yield func(message.MessageChunk, error) bool) {
		// Consume the clear flag atomically: if set, this Prompt starts a fresh
		// branch with no parent, ignoring any existing thread history.
		_, startFresh := a.clearNext.LoadAndDelete(threadID)

		// Inject system prompt and user instructions as root messages on new threads.
		if req.LeafID == "" && systemPrompt != "" {
			var leaf string
			if !startFresh {
				leaf, _ = a.resolveCurrentLeaf(threadID)
			}
			if leaf != "" {
				// Thread already has messages — continue from the current leaf.
				req.LeafID = leaf
			} else {
				// 1. System prompt as role: "system".
				sysID := "system-" + agent.GenerateID()
				if err := a.store.SaveMessage(threadID, thread.StoredMessage{
					ID: sysID,
					Message: message.Message{
						Role:      "system",
						Synthetic: true,
						Parts:     []message.Part{message.TextPart{Text: systemPrompt}},
					},
				}); err != nil {
					yield(nil, fmt.Errorf("save system prompt: %w", err))
					return
				}
				req.LeafID = sysID

				// 2. User instructions as role: "user" with <system-reminder> tags.
				// Only inject when using the default session config (not a sub-agent prompt).
				if subAgentCfg == nil {
					userInstr := sessionconfig.FormatUserInstructions(sessionCfg.UserInstructions)
					if userInstr != "" {
						instrID := "instructions-" + agent.GenerateID()
						if err := a.store.SaveMessage(threadID, thread.StoredMessage{
							ID:       instrID,
							ParentID: sysID,
							Message: message.Message{
								Role:      "user",
								Synthetic: true,
								Parts:     []message.Part{message.TextPart{Text: userInstr}},
							},
						}); err != nil {
							yield(nil, fmt.Errorf("save user instructions: %w", err))
							return
						}
						req.LeafID = instrID
					}

					// 3. Runtime environment reminder as role: "user".
					runtimeReminder := formatRuntimeEnvironmentReminder(a.cwd, displayName)
					if runtimeReminder != "" {
						runtimeID := "runtime-" + agent.GenerateID()
						parentID := req.LeafID
						if parentID == "" {
							parentID = sysID
						}
						if err := a.store.SaveMessage(threadID, thread.StoredMessage{
							ID:       runtimeID,
							ParentID: parentID,
							Message: message.Message{
								Role:      "user",
								Synthetic: true,
								Parts:     []message.Part{message.TextPart{Text: runtimeReminder}},
							},
						}); err != nil {
							yield(nil, fmt.Errorf("save runtime reminder: %w", err))
							return
						}
						req.LeafID = runtimeID
					}

					// 4. Skills reminder as role: "user" listing available skills.
					skillsReminder := sessionconfig.FormatSkillsReminder(sessionCfg.Skills)
					if skillsReminder != "" {
						skillsID := "skills-" + agent.GenerateID()
						parentID := req.LeafID
						if parentID == "" {
							parentID = sysID
						}
						if err := a.store.SaveMessage(threadID, thread.StoredMessage{
							ID:       skillsID,
							ParentID: parentID,
							Message: message.Message{
								Role:      "user",
								Synthetic: true,
								Parts:     []message.Part{message.TextPart{Text: skillsReminder}},
							},
						}); err != nil {
							yield(nil, fmt.Errorf("save skills reminder: %w", err))
							return
						}
						req.LeafID = skillsID
					}
				}
			}
		}

		// If req.LeafID is still unset (no system prompt, or the injection block
		// was skipped), resolve it from the current thread leaf so that new turns
		// continue from where the conversation left off rather than starting fresh.
		// Skip this when startFresh is set — the caller explicitly wants a new branch.
		if req.LeafID == "" && !startFresh {
			if leaf, err := a.resolveCurrentLeaf(threadID); err == nil {
				req.LeafID = leaf
			}
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

		// Wrap the executor with MCP routing if the MCP manager is active.
		executor := thread.ToolExecutor(a.executor)
		if mcpMgr != nil {
			executor = mcp.NewExecutor(a.executor, mcpMgr)
		}

		// Check for interrupted turn first.
		state, err := a.store.LoadTurnState(threadID)
		if err != nil {
			yield(nil, err)
			return
		}

		if state != nil {
			log.Printf("agent: resuming interrupted turn %s for thread %s (step %d, phase %s)",
				state.ID, threadID, state.CurrentStep, state.Phase)

			// Normalize persisted model: old versions stored Model as "providerID/modelID"
			// instead of the bare model ID. Strip the provider prefix if present.
			if ref, err := providers.ParseModelRef(state.Config.Model); err == nil {
				state.Config.ProviderID = ref.ProviderID
				state.Config.Model = ref.ModelID
			}

			// If the current request specifies a model, override all user-configurable
			// fields in the persisted turn config (model, reasoning, limits, context
			// window metadata). The user message and tools are kept from the persisted
			// state since they belong to the original interrupted turn.
			if model != "" {
				state.Config.ProviderID = providerID
				state.Config.Model = modelID
				state.Config.Reasoning = cfg.Reasoning
				state.Config.MaxSteps = cfg.MaxSteps
			}

			// Resolve provider from (possibly updated) turn state config.
			state.ReplayTurn = req.ReplayTurn
			provider, resolveErr := a.registry.Get(state.Config.ProviderID)
			if resolveErr != nil {
				yield(nil, fmt.Errorf("resolve provider for resume: %w", resolveErr))
				return
			}

			for chunk, chunkErr := range thread.ResumeTurn(promptCtx, provider, executor, a.store, state, toolCtx) {
				if !yield(chunk, chunkErr) {
					return
				}
			}
			a.persistActiveLeaf(threadID)
			return
		}

		if generatedName := generatedThreadName(req.UserParts); shouldGenerateThreadName(threadCfg) && generatedName != "" {
			threadCfg.Name = generatedName
			if err := a.store.SaveConfig(threadID, threadCfg); err != nil {
				yield(nil, fmt.Errorf("save thread name: %w", err))
				return
			}
			if !yield(message.ThreadNameChunk{
				Data: message.ThreadNameData{
					Name: generatedName,
				},
			}, nil) {
				return
			}
		}

		if modeChangedByPrompt {
			modeReminderID := "mode-" + agent.GenerateID()
			if err := a.store.SaveMessage(threadID, thread.StoredMessage{
				ID:       modeReminderID,
				ParentID: req.LeafID,
				Message: message.Message{
					Role:  "user",
					Parts: []message.Part{message.TextPart{Text: formatModeChangeReminder(planMode)}},
				},
			}); err != nil {
				yield(nil, fmt.Errorf("save mode reminder: %w", err))
				return
			}
			req.LeafID = modeReminderID

			// Notify the server of the mode change so it can update the session.
			newMode := "build"
			if planMode {
				newMode = "plan"
			}
			if !yield(message.ModeChangeChunk{Data: message.ModeChangeData{Mode: newMode}}, nil) {
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
		if threadCfgErr == nil && strings.TrimSpace(threadCfg.CWD) != "" {
			cwd = threadCfg.CWD
		}
		_ = a.store.SaveConfig(threadID, thread.Config{
			Name:         threadCfg.Name,
			Model:        cfg.ProviderID + "/" + cfg.Model,
			Reasoning:    cfg.Reasoning,
			CWD:          cwd,
			PlanMode:     planMode,
			ActiveLeafID: req.LeafID,
		})

		// Start new turn.
		for chunk, chunkErr := range thread.RunTurn(promptCtx, provider, executor, a.store, threadID, req.LeafID, cfg, toolCtx) {
			if !yield(chunk, chunkErr) {
				return
			}
		}
		a.persistActiveLeaf(threadID)
	}
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
	if hasConfig {
		planMode = cfg.PlanMode
	}
	if reqMode == "" {
		return planMode, false
	}
	planMode = reqMode == "plan"
	if !hasConfig {
		return planMode, false
	}
	return planMode, planMode != cfg.PlanMode
}

func formatModeChangeReminder(planMode bool) string {
	mode := "build"
	if planMode {
		mode = "plan"
	}
	return fmt.Sprintf("<system-reminder>\nMode update: the current mode is now %s. This change was triggered by the current prompt request.\n</system-reminder>", mode)
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

	ref, err := a.registry.ResolveModel(model, providers.ModelTaskChat)
	if err != nil {
		return errorIter(fmt.Errorf("resolve model for /compact: %w", err))
	}

	provider, err := a.registry.Get(ref.ProviderID)
	if err != nil {
		return errorIter(fmt.Errorf("resolve provider for /compact: %w", err))
	}

	turnCfg := &thread.TurnConfig{ProviderID: ref.ProviderID, Model: ref.ModelID}

	return func(yield func(message.MessageChunk, error) bool) {
		compacted, compactErr := thread.ForceCompactThread(ctx, provider, a.store, threadID, leafID, turnCfg)
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
	{Name: "clear", Description: "Clear the current thread and start a fresh conversation (history is preserved on disk).", Kind: agent.CommandKindBuiltin},
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

// IsLeaf reports whether msgID is a valid leaf in the thread's message tree.
// Fast path: compares against the thread's persisted ActiveLeafID to avoid a
// full store scan when the client is simply continuing the current branch.
func (a *DefaultAgent) IsLeaf(threadID, msgID string) (bool, error) {
	cfg, err := a.store.LoadConfig(threadID)
	if err == nil && cfg.ActiveLeafID == msgID {
		return true, nil
	}
	return a.store.IsLeaf(threadID, msgID)
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

// isClearCommand reports whether the user parts contain exactly the /clear command.
func isClearCommand(parts []message.UIPart) bool {
	if len(parts) != 1 {
		return false
	}
	tp, ok := parts[0].(message.UITextPart)
	return ok && strings.TrimSpace(tp.Text) == "/clear"
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
