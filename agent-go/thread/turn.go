package thread

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"log"
	"time"

	"github.com/obot-platform/discobot/agent-go/internal/api"
	"github.com/obot-platform/discobot/agent-go/message"
	"github.com/obot-platform/discobot/agent-go/providers"
	"github.com/obot-platform/discobot/agent-go/providers/transport"
	"github.com/obot-platform/discobot/modelsdev"
)

// TurnConfig holds the parameters for a single turn of the agent loop.
// It is persisted to disk as part of TurnState for crash recovery.
type TurnConfig struct {
	ProviderID            string                     `json:"providerId"`
	Model                 string                     `json:"model"`
	SupportingModels      providers.SupportingModels `json:"supportingModels,omitempty"` // supporting model type -> full "providerId/modelId" ref
	UserParts             []message.Part             `json:"-"`
	UserMessage           message.Message            `json:"userMessage"` // serializable form of UserParts
	Tools                 []providers.ToolDefinition `json:"tools,omitempty"`
	MaxTokens             *int                       `json:"maxTokens,omitempty"`
	Temperature           *float64                   `json:"temperature,omitempty"`
	TopP                  *float64                   `json:"topP,omitempty"`
	Reasoning             providers.Reasoning        `json:"reasoning,omitempty"`
	PlanMode              bool                       `json:"planMode,omitempty"`
	PromptRequestPlanMode bool                       `json:"promptRequestPlanMode,omitempty"`
	ProviderOptions       json.RawMessage            `json:"providerOptions,omitempty"`
	ContextWindow         int                        `json:"contextWindow,omitempty"`   // model context window in tokens
	MaxOutputTokens       int                        `json:"maxOutputTokens,omitempty"` // model max output tokens
	MaxSteps              int                        `json:"maxSteps,omitempty"`        // max LLM calls; 0 = unlimited
}

// RunTurn executes a multi-step agent turn with crash-resilient persistence.
//
// State transitions are persisted to disk before being acted on:
//   - turn.json tracks current step, phase, and config
//   - Messages are saved to the thread incrementally after each step
//   - Step results and tool results are persisted to individual files
//
// On crash, the turn can be recovered by reading turn.json and resuming
// from the last known-good state via ResumeTurn.
func RunTurn(
	ctx context.Context,
	provider providers.Provider,
	executor ToolExecutor,
	store *Store,
	threadID string,
	leafID string,
	cfg TurnConfig,
	toolCtx ...*ToolContext,
) iter.Seq2[message.MessageChunk, error] {
	return func(yield func(message.MessageChunk, error) bool) {
		// 1. Build user message and persist config.
		startedAt := time.Now().UTC()
		cfg.UserMessage = message.Message{Role: "user", Parts: cfg.UserParts, CreatedAt: &startedAt}

		turnID := generateID()
		turnState := TurnState{
			ID:          turnID,
			ThreadID:    threadID,
			LeafID:      leafID,
			Config:      cfg,
			CurrentStep: 0,
			Phase:       PhaseStreaming,
			LeafMsgID:   leafID,
			StartedAt:   &startedAt,
		}

		// 2. Save user message to thread immediately.
		userMsgID := generateID()
		cfg.UserMessage.ID = userMsgID
		if err := store.SaveMessage(threadID, StoredMessage{
			ID:       userMsgID,
			ParentID: leafID,
			Message:  cfg.UserMessage,
		}); err != nil {
			yield(nil, fmt.Errorf("save user message: %w", err))
			return
		}
		turnState.LeafMsgID = userMsgID

		// Pre-generate the first assistant message ID so the frontend knows what
		// message ID to associate with the streaming content.
		turnState.AssistantMsgID = generateID()

		// 3. Persist turn state before starting.
		if err := store.SaveTurnState(threadID, turnState); err != nil {
			yield(nil, fmt.Errorf("save turn state: %w", err))
			return
		}

		// Emit the user message that initiated this turn before the start envelope,
		// so consumers know which message triggered this response stream.
		if !yield(message.UserMessageChunk{
			Data: message.UserMessageData{
				Message:               cfg.UserMessage,
				InsertBeforeMessageID: turnState.AssistantMsgID,
			},
		}, nil) {
			return
		}

		// Emit the outer start envelope so the AI SDK can bind the stream to a message ID.
		// Include the model in messageMetadata so the server can record which model was used.
		if !yield(message.StartChunk{
			MessageID:       turnState.AssistantMsgID,
			MessageMetadata: buildMessageMetadata(cfg, turnState.StartedAt, nil),
		}, nil) {
			return
		}

		var execToolCtx *ToolContext
		if len(toolCtx) > 0 {
			execToolCtx = toolCtx[0]
		}

		// 4. Execute the turn loop.
		if !executeLoop(ctx, provider, executor, execToolCtx, store, threadID, turnID, &turnState, yield) {
			// If context was cancelled (e.g. Ctrl+C), clean up turn state
			// so the turn is not resumed on the next prompt or restart.
			if ctx.Err() != nil {
				_ = store.DeleteTurnState(threadID)
			}
			return
		}

		if turnState.Phase == PhaseWaitingForAnswer {
			return // keep turn state on disk
		}

		// 5. Turn complete — emit finish envelope and delete turn state.
		if err := finalizeTurnState(store, threadID, &turnState); err != nil {
			yield(nil, fmt.Errorf("save finished turn state: %w", err))
			return
		}
		yield(message.ResponseFinishChunk{
			FinishReason:    "stop",
			MessageMetadata: buildMessageMetadata(turnState.Config, turnState.StartedAt, turnState.FinishedAt),
		}, nil) //nolint:errcheck
		_ = store.DeleteTurnState(threadID)
	}
}

// ResumeTurn recovers an interrupted turn from persisted disk state.
// Recovery does not replay prior chunks to rebuild consumer state; callers
// should rely on persisted message history for already-saved content.
func ResumeTurn(
	ctx context.Context,
	provider providers.Provider,
	executor ToolExecutor,
	store *Store,
	turnState *TurnState,
	toolCtx ...*ToolContext,
) iter.Seq2[message.MessageChunk, error] {
	return func(yield func(message.MessageChunk, error) bool) {
		threadID := turnState.ThreadID
		turnID := turnState.ID

		if turnState.Phase == PhaseStreaming {
			existingResult, _ := store.LoadStepResult(threadID, turnID, turnState.CurrentStep)
			if existingResult == nil {
				streamComplete, recovered := recoverStreamingStep(store, threadID, turnID, turnState)
				if recovered && !streamComplete {
					if err := finalizeTurnState(store, threadID, turnState); err != nil {
						yield(nil, fmt.Errorf("save finished turn state: %w", err))
						return
					}
					yield(message.ResponseFinishChunk{ //nolint:errcheck
						FinishReason:    "stop",
						MessageMetadata: buildMessageMetadata(turnState.Config, turnState.StartedAt, turnState.FinishedAt),
					}, nil)
					_ = store.DeleteTurnState(threadID)
					return
				}
			}
		}

		var execToolCtx *ToolContext
		if len(toolCtx) > 0 {
			execToolCtx = toolCtx[0]
		}
		if !executeLoop(ctx, provider, executor, execToolCtx, store, threadID, turnID, turnState, yield) {
			if ctx.Err() != nil {
				_ = store.DeleteTurnState(threadID)
			}
			return
		}

		if turnState.Phase == PhaseWaitingForAnswer {
			return // keep turn state on disk
		}

		if err := finalizeTurnState(store, threadID, turnState); err != nil {
			yield(nil, fmt.Errorf("save finished turn state: %w", err))
			return
		}
		yield(message.ResponseFinishChunk{ //nolint:errcheck
			FinishReason:    "stop",
			MessageMetadata: buildMessageMetadata(turnState.Config, turnState.StartedAt, turnState.FinishedAt),
		}, nil)
		_ = store.DeleteTurnState(threadID)
	}
}

// pendingAsyncEntry tracks an in-flight async continuation handle during the
// step loop.
type pendingAsyncEntry struct {
	toolCallID string
	toolName   string
	handle     *AsyncContinuationHandle
}

type pendingApprovalPause struct {
	toolCallID   string
	resumePhase  TurnPhase
	continuation json.RawMessage
	approval     *ApprovalRequest
}

type loopContext struct {
	ctx       context.Context
	provider  providers.Provider
	executor  ToolExecutor
	toolCtx   *ToolContext
	store     *Store
	threadID  string
	turnID    string
	turnState *TurnState
	yield     func(message.MessageChunk, error) bool

	asyncHandles []pendingAsyncEntry
	lastUsage    *message.Usage
}

type toolsPhaseState struct {
	stepIndex               int
	toolCalls               []message.ToolCallPart
	completedTools          map[string]message.ToolResultPart
	existingAsyncByID       map[string]AsyncContinuationInfo
	asyncContinuationsState StepAsyncContinuations
	allResults              map[string]message.ToolResultPart
}

type waitingForAsyncPhaseState struct {
	stepIndex          int
	toolCalls          []message.ToolCallPart
	asyncContinuations StepAsyncContinuations
	allResults         map[string]message.ToolResultPart
}

type loopStepResult int

const (
	loopStepStop loopStepResult = iota
	loopStepContinue
	loopStepDone
)

// executeLoop is the continuation-style state machine that drives the turn.
// Both RunTurn and ResumeTurn call this function. Each phase loads its state
// from disk, so crash recovery is handled naturally — the loop simply resumes
// from whatever phase was persisted in turn.json.
//
// Returns true on normal completion (turn done or paused for user input).
// Returns false if yield returned false or a fatal error occurred.
func executeLoop(
	ctx context.Context,
	provider providers.Provider,
	executor ToolExecutor,
	toolCtx *ToolContext,
	store *Store,
	threadID string,
	turnID string,
	turnState *TurnState,
	yield func(message.MessageChunk, error) bool,
) bool {
	cfg := &turnState.Config
	if toolCtx == nil {
		toolCtx = &ToolContext{}
	}
	if toolCtx.ThreadID == "" {
		toolCtx.ThreadID = threadID
	}
	toolCtx.PromptRequestPlanMode = cfg.PromptRequestPlanMode
	lc := &loopContext{
		ctx:       ctx,
		provider:  provider,
		executor:  executor,
		toolCtx:   toolCtx,
		store:     store,
		threadID:  threadID,
		turnID:    turnID,
		turnState: turnState,
		yield:     yield,
	}

	for {
		var result loopStepResult
		switch turnState.Phase {
		case PhaseStreaming:
			result = lc.runStreamingPhase(cfg)
		case PhaseTools:
			result = lc.runToolsPhase()
		case PhaseWaitingForAsync:
			result = lc.runWaitingForAsyncPhase()
		case PhaseWaitingForAnswer:
			result = lc.runWaitingForAnswerPhase()
		default:
			yield(nil, fmt.Errorf("unknown turn phase: %s", turnState.Phase))
			return false
		}
		switch result {
		case loopStepContinue:
			continue
		case loopStepDone:
			return true
		default:
			return false
		}
	}
}

// runStreamingPhase drives the model-completion part of a step. It rebuilds
// the conversation history from persisted messages, restores any already-saved
// step output when resuming, and only invokes the provider when this step has
// not yet produced a persisted StepResult. If the assistant emits tool calls,
// the turn advances into PhaseTools; otherwise the turn finishes.
func (lc *loopContext) runStreamingPhase(cfg *TurnConfig) loopStepResult {
	stepIndex := lc.turnState.CurrentStep
	if cfg.MaxSteps > 0 && stepIndex >= cfg.MaxSteps {
		return loopStepDone
	}

	// Always rebuild history from storage so resumed turns use the same source of
	// truth as fresh turns.
	historyEntries, err := lc.store.BuildHistoryWithIDs(lc.threadID, lc.turnState.LeafMsgID)
	if err != nil {
		lc.yield(nil, fmt.Errorf("build history: %w", err))
		return loopStepStop
	}

	// Best-effort compaction keeps normal turns within the provider context
	// window. If compaction fails, continue with the unmodified history and let
	// the provider report a context-length error if necessary.
	compacted, compactErr := maybeCompact(lc.ctx, lc.provider, lc.toolCtx.ProviderResolver, lc.store, lc.threadID, lc.turnState, cfg, historyEntries, lc.lastUsage)
	history := compacted
	if compactErr != nil {
		log.Printf("compaction: %v (using full history)", compactErr)
		history = entriesToMessages(historyEntries)
	}

	stepResult, _ := lc.store.LoadStepResult(lc.threadID, lc.turnID, stepIndex)
	var assistantMsg message.Message
	var toolCalls []message.ToolCallPart
	if stepResult != nil {
		// Resumes land here: the completion already ran earlier, so just reload the
		// persisted assistant output and continue from there.
		assistantMsg, err = loadStepAssistantMessage(lc.store, lc.threadID, stepResult)
		if err != nil {
			lc.yield(nil, fmt.Errorf("load assistant message for step: %w", err))
			return loopStepStop
		}
		toolCalls = extractToolCalls(assistantMsg)
	} else {
		// Fresh execution path: run the model, then load the just-persisted step
		// result back from disk so the rest of the loop uses the same state shape as
		// resume.
		idOverride := ""
		if stepIndex == 0 {
			idOverride = lc.turnState.AssistantMsgID
		}
		stepUsage, ok := lc.runCompletionWithMaybeCompaction(cfg, history, historyEntries, stepIndex, idOverride)
		if !ok {
			return loopStepStop
		}
		if stepUsage.InputTokens.Total > 0 || stepUsage.OutputTokens.Total > 0 {
			lc.lastUsage = &stepUsage
		}
		stepResult, err = lc.store.LoadStepResult(lc.threadID, lc.turnID, stepIndex)
		if err != nil || stepResult == nil {
			lc.yield(nil, fmt.Errorf("load step result after completion: %w", err))
			return loopStepStop
		}
		assistantMsg, err = saveAssistantStepMessage(lc.store, lc.threadID, lc.turnID, lc.turnState, *cfg, stepIndex, stepResult)
		if err != nil {
			lc.yield(nil, err)
			return loopStepStop
		}
		toolCalls = extractToolCalls(assistantMsg)
	}

	if stepResult != nil && stepResult.AssistantMessageID == "" {
		// Older or partially-written step state may have the assistant payload
		// without the saved thread message; materialize it before moving on.
		assistantMsg, err = saveAssistantStepMessage(lc.store, lc.threadID, lc.turnID, lc.turnState, *cfg, stepIndex, stepResult)
		if err != nil {
			lc.yield(nil, err)
			return loopStepStop
		}
		toolCalls = extractToolCalls(assistantMsg)
	}

	if len(toolCalls) == 0 {
		// No tool calls means this assistant response is the last output for the
		// turn, so emit the step-finished marker and stop the loop.
		if !yieldFinishStep(stepIndex, lc.yield) {
			return loopStepStop
		}
		return loopStepDone
	}

	// Persist the phase transition before doing any tool work so resume can pick
	// up exactly at the tools phase after a crash or restart.
	lc.turnState.Phase = PhaseTools
	lc.turnState.CurrentStep = stepIndex
	if err := lc.store.SaveTurnState(lc.threadID, *lc.turnState); err != nil {
		lc.yield(nil, fmt.Errorf("save turn state (tools): %w", err))
		return loopStepStop
	}
	return loopStepContinue
}

// runCompletionWithMaybeCompaction runs a provider completion once and retries
// exactly once after emergency compaction when the provider reports that the
// context window was exceeded. This keeps the streaming phase linear while
// isolating the "context too large" recovery path.
func (lc *loopContext) runCompletionWithMaybeCompaction(
	cfg *TurnConfig,
	history []message.Message,
	historyEntries []HistoryEntry,
	stepIndex int,
	idOverride string,
) (message.Usage, bool) {
	_, _, stepUsage, ok, completionErr := runCompletion(lc.ctx, lc.provider, lc.store, lc.threadID, lc.turnID, stepIndex, cfg, history, idOverride, lc.yield)
	if ok {
		return stepUsage, true
	}
	if !isContextLengthExceeded(completionErr) {
		// Non-context errors are already surfaced by runCompletion, so just stop.
		return message.Usage{}, false
	}

	// A context-length failure means normal compaction was insufficient or could
	// not run. Force a more aggressive compaction pass, then retry once.
	log.Printf("compaction: context_length_exceeded — forcing emergency compaction for thread %s", lc.threadID)
	forceCompacted, forceErr := forceCompact(lc.ctx, lc.provider, lc.toolCtx.ProviderResolver, lc.store, lc.threadID, cfg, historyEntries)
	if forceErr != nil {
		log.Printf("compaction: emergency compaction failed: %v", forceErr)
		if !lc.yield(nil, completionErr) {
			return message.Usage{}, false
		}
		return message.Usage{}, false
	}

	_, _, stepUsage, ok, completionErr = runCompletion(lc.ctx, lc.provider, lc.store, lc.threadID, lc.turnID, stepIndex, cfg, forceCompacted, idOverride, lc.yield)
	if ok {
		return stepUsage, true
	}
	// If the retry still fails, propagate that final failure and let the turn
	// stop in place for inspection or resume.
	if completionErr != nil && !lc.yield(nil, completionErr) {
		return message.Usage{}, false
	}
	return message.Usage{}, false
}

// runToolsPhase walks the assistant's tool calls for the current step and makes
// sure each one reaches a persisted terminal state, an async wait state, or an
// approval pause. It treats fresh execution and crash recovery uniformly by
// consulting the persisted per-step tool state before acting.
func (lc *loopContext) runToolsPhase() loopStepResult {
	state, ok := lc.loadToolsPhaseState()
	if !ok {
		return loopStepStop
	}

	paused := false
	interruptedOne := false
	for _, tc := range state.toolCalls {
		if lc.ctx.Err() != nil {
			break
		}
		// Skip tools that already have a persisted result. This makes the phase
		// idempotent across resumes and partial writes.
		if _, ok := state.allResults[tc.ToolCallID]; ok {
			continue
		}
		if at, ok := state.existingAsyncByID[tc.ToolCallID]; ok {
			// This tool already launched async work earlier, so resume that
			// continuation instead of re-executing the tool from scratch.
			phaseResult := lc.handleRecoveredAsyncTool(state.stepIndex, tc, at, state.allResults)
			if phaseResult == loopStepContinue {
				continue
			}
			if phaseResult == loopStepDone {
				paused = true
				break
			}
			return phaseResult
		}

		if len(state.completedTools) > 0 && !interruptedOne {
			// If we resumed after some tools completed but before another one started,
			// mark exactly one missing tool as interrupted. This preserves the
			// original one-tool-at-a-time execution model without replaying the call.
			interruptedOne = true
			result := message.ToolResultPart{ToolCallID: tc.ToolCallID, ToolName: tc.ToolName, Output: message.ErrorTextOutput{Value: "interrupted by transient system failure"}}
			state.allResults[tc.ToolCallID] = result
			if _, err := appendStepToolEventMessage(lc.store, lc.threadID, lc.turnID, lc.turnState, state.stepIndex, result); err != nil {
				lc.yield(nil, fmt.Errorf("append tool result message: %w", err))
				return loopStepStop
			}
			continue
		}

		phaseResult := lc.handleFreshToolExecution(state.stepIndex, tc, state.asyncContinuationsState, state.allResults)
		if phaseResult == loopStepContinue {
			continue
		}
		if phaseResult == loopStepDone {
			paused = true
			break
		}
		return phaseResult
	}

	if paused {
		return loopStepDone
	}
	return lc.finalizeToolsPhase(state)
}

// loadToolsPhaseState reconstructs the persisted state needed to process tool
// calls for the current step. The returned structure intentionally merges the
// assistant-declared tool calls, any already-written tool results, and any
// async continuation metadata so the rest of the tools phase can stay mostly
// linear.
func (lc *loopContext) loadToolsPhaseState() (*toolsPhaseState, bool) {
	stepIndex := lc.turnState.CurrentStep
	stepResult, err := lc.store.LoadStepResult(lc.threadID, lc.turnID, stepIndex)
	if err != nil || stepResult == nil {
		lc.yield(nil, fmt.Errorf("load step result for tools phase: %w", err))
		return nil, false
	}
	assistantMsg, err := loadStepAssistantMessage(lc.store, lc.threadID, stepResult)
	if err != nil {
		lc.yield(nil, fmt.Errorf("load assistant message for tools phase: %w", err))
		return nil, false
	}
	toolCalls := extractToolCalls(assistantMsg)
	if err := ensureLegacyStepToolResultsMaterialized(lc.store, lc.threadID, lc.turnID, lc.turnState, stepIndex, toolCalls); err != nil {
		lc.yield(nil, fmt.Errorf("materialize legacy tool results: %w", err))
		return nil, false
	}
	// Tool results are loaded from step event messages so the phase can recover
	// from interruptions without trusting in-memory execution state.
	completedTools, err := loadCompletedToolResultsFromStepEvents(lc.store, lc.threadID, lc.turnID, stepIndex)
	if err != nil {
		lc.yield(nil, fmt.Errorf("load completed tool results: %w", err))
		return nil, false
	}
	existingAsyncContinuations, _ := lc.store.LoadAsyncContinuations(lc.threadID, lc.turnID, stepIndex)
	existingAsyncByID := make(map[string]AsyncContinuationInfo)
	for _, at := range existingAsyncContinuations.Continuations {
		existingAsyncByID[at.ToolCallID] = at
	}
	lc.asyncHandles = nil
	allResults := make(map[string]message.ToolResultPart)
	for id, r := range completedTools {
		allResults[id] = r
	}
	return &toolsPhaseState{
		stepIndex:               stepIndex,
		toolCalls:               toolCalls,
		completedTools:          completedTools,
		existingAsyncByID:       existingAsyncByID,
		asyncContinuationsState: existingAsyncContinuations,
		allResults:              allResults,
	}, true
}

// handleRecoveredAsyncTool resumes a tool call that already persisted async
// continuation metadata in a previous process. Depending on what the executor reports,
// the tool either stays async, pauses for approval, or resolves into a final
// tool result that is appended to the step events.
func (lc *loopContext) handleRecoveredAsyncTool(stepIndex int, tc message.ToolCallPart, at AsyncContinuationInfo, allResults map[string]message.ToolResultPart) loopStepResult {
	call := message.ToolCallPart{ToolCallID: tc.ToolCallID, ToolName: tc.ToolName, Input: string(at.Input)}
	resumeResult, resumeErr := lc.executor.Continue(lc.ctx, lc.toolCtx, call, at.Continuation, nil)
	if resumeErr != nil {
		// The async continuation is gone or unrecoverable. Convert that into a
		// persisted tool error so the step can keep moving.
		result := message.ToolResultPart{ToolCallID: tc.ToolCallID, ToolName: tc.ToolName, Output: message.ErrorTextOutput{Value: fmt.Sprintf("async continuation lost: %v", resumeErr)}}
		allResults[tc.ToolCallID] = result
		if _, err := appendStepToolEventMessage(lc.store, lc.threadID, lc.turnID, lc.turnState, stepIndex, result); err != nil {
			lc.yield(nil, fmt.Errorf("append tool result message: %w", err))
			return loopStepStop
		}
		for _, mc := range message.ToolResultToChunks(result) {
			if !lc.yield(mc, nil) {
				return loopStepStop
			}
		}
		return loopStepContinue
	}
	if resumeResult.Async != nil {
		lc.asyncHandles = append(lc.asyncHandles, pendingAsyncEntry{toolCallID: tc.ToolCallID, toolName: tc.ToolName, handle: resumeResult.Async})
		return loopStepContinue
	}
	if resumeResult.Approval != nil {
		// Some async tools transition into approval instead of a terminal result.
		continuation := resumeResult.Approval.Continuation
		if len(continuation) == 0 {
			continuation = at.Continuation
		}
		if !pauseForApproval(lc.store, lc.threadID, lc.turnState, lc.turnID, stepIndex, &pendingApprovalPause{toolCallID: tc.ToolCallID, resumePhase: PhaseTools, continuation: continuation, approval: resumeResult.Approval}, lc.yield) {
			return loopStepStop
		}
		return loopStepDone
	}
	allResults[tc.ToolCallID] = resumeResult.Result
	if _, err := appendStepToolEventMessage(lc.store, lc.threadID, lc.turnID, lc.turnState, stepIndex, resumeResult.Result); err != nil {
		lc.yield(nil, fmt.Errorf("append tool result message: %w", err))
		return loopStepStop
	}
	for _, mc := range message.ToolResultToChunks(resumeResult.Result) {
		if !lc.yield(mc, nil) {
			return loopStepStop
		}
	}
	return loopStepContinue
}

// handleFreshToolExecution runs a tool that has not been persisted before. It
// normalizes executor outcomes into the three states the loop understands:
// asynchronous work, approval pauses, or a final tool result event.
func (lc *loopContext) handleFreshToolExecution(stepIndex int, tc message.ToolCallPart, asyncContinuationsState StepAsyncContinuations, allResults map[string]message.ToolResultPart) loopStepResult {
	execResult, execErr := lc.executor.Execute(lc.ctx, lc.toolCtx, tc)
	if execErr != nil {
		// Synchronous tool execution failures are recorded as normal tool results so
		// the model can react to them on the next step.
		execResult = ToolExecuteResult{Result: message.ToolResultPart{ToolCallID: tc.ToolCallID, ToolName: tc.ToolName, Output: message.ErrorTextOutput{Value: execErr.Error()}}}
	}
	if execResult.Async != nil {
		lc.asyncHandles = append(lc.asyncHandles, pendingAsyncEntry{toolCallID: tc.ToolCallID, toolName: tc.ToolName, handle: execResult.Async})
		asyncContinuationsState.Continuations = append(asyncContinuationsState.Continuations, AsyncContinuationInfo{ToolCallID: tc.ToolCallID, ToolName: tc.ToolName, Continuation: cloneRawMessage(execResult.Async.Continuation), Input: string(tc.Input)})
		if err := lc.store.SaveAsyncContinuations(lc.threadID, lc.turnID, stepIndex, asyncContinuationsState); err != nil {
			lc.yield(nil, fmt.Errorf("save async continuations: %w", err))
			return loopStepStop
		}
		return loopStepContinue
	}
	if execResult.Approval != nil {
		if len(lc.asyncHandles) > 0 {
			// Flush any already-started async work before pausing for approval so the
			// turn state does not forget about it.
			pause, ok := waitForAsyncHandles(lc.ctx, lc.store, lc.threadID, lc.turnID, lc.turnState, stepIndex, lc.asyncHandles, allResults, lc.yield)
			if !ok {
				return loopStepStop
			}
			if pause != nil {
				pause.resumePhase = PhaseTools
				if !pauseForApproval(lc.store, lc.threadID, lc.turnState, lc.turnID, stepIndex, pause, lc.yield) {
					return loopStepStop
				}
				return loopStepDone
			}
			lc.asyncHandles = nil
		}
		if !pauseForApproval(lc.store, lc.threadID, lc.turnState, lc.turnID, stepIndex, &pendingApprovalPause{toolCallID: tc.ToolCallID, resumePhase: PhaseTools, continuation: execResult.Approval.Continuation, approval: execResult.Approval}, lc.yield) {
			return loopStepStop
		}
		return loopStepDone
	}

	result := execResult.Result
	allResults[tc.ToolCallID] = result
	if _, err := appendStepToolEventMessage(lc.store, lc.threadID, lc.turnID, lc.turnState, stepIndex, result); err != nil {
		lc.yield(nil, fmt.Errorf("append tool result message: %w", err))
		return loopStepStop
	}
	for _, mc := range message.ToolResultToChunks(result) {
		if !lc.yield(mc, nil) {
			return loopStepStop
		}
	}
	if lc.toolCtx.ModeChange != nil {
		// Tools can request a mode transition; emit the full thread update after the
		// result so clients observe the same ordering as the persisted events.
		lc.toolCtx.ModeChange = nil
		if !YieldThreadUpdate(lc.yield, lc.store, lc.threadID) {
			return loopStepStop
		}
	}
	return loopStepContinue
}

// finalizeToolsPhase closes out PhaseTools once every tool call has either
// produced a result, moved into async waiting, or the turn has been cancelled.
// It is the only place that decides whether the loop transitions to
// PhaseWaitingForAsync or advances to the next model step.
func (lc *loopContext) finalizeToolsPhase(state *toolsPhaseState) loopStepResult {
	if lc.ctx.Err() != nil {
		// Cancellation is persisted as synthetic "cancelled" tool results for any
		// tool call that never reached a terminal event.
		for _, tc := range state.toolCalls {
			if _, ok := state.allResults[tc.ToolCallID]; ok {
				continue
			}
			result := message.ToolResultPart{ToolCallID: tc.ToolCallID, ToolName: tc.ToolName, Output: message.ErrorTextOutput{Value: "cancelled"}}
			state.allResults[tc.ToolCallID] = result
			_, _ = appendStepToolEventMessage(lc.store, lc.threadID, lc.turnID, lc.turnState, state.stepIndex, result)
		}
		return loopStepDone
	}
	if len(lc.asyncHandles) > 0 {
		lc.turnState.Phase = PhaseWaitingForAsync
		lc.turnState.CurrentStep = state.stepIndex
		if err := lc.store.SaveTurnState(lc.threadID, *lc.turnState); err != nil {
			lc.yield(nil, fmt.Errorf("save turn state (waiting_for_async): %w", err))
			return loopStepStop
		}
		return loopStepContinue
	}
	if err := advanceToNextStep(lc.store, lc.threadID, lc.turnState); err != nil {
		lc.yield(nil, err)
		return loopStepStop
	}
	// Once all tool output for the step is persisted, signal to the client that
	// the current step is complete before looping back into streaming.
	if !yieldFinishStep(state.stepIndex, lc.yield) {
		return loopStepStop
	}
	return loopStepContinue
}

// loadWaitingForAsyncPhaseState reconstructs the persisted state needed to
// continue a step that is currently in PhaseWaitingForAsync. It loads the
// assistant-declared tool calls, any already-materialized tool results, and the
// persisted async continuation metadata so the waiting phase can run without
// relying on
// process-local state.
func (lc *loopContext) loadWaitingForAsyncPhaseState() (*waitingForAsyncPhaseState, bool) {
	stepIndex := lc.turnState.CurrentStep
	stepResult, err := lc.store.LoadStepResult(lc.threadID, lc.turnID, stepIndex)
	if err != nil || stepResult == nil {
		lc.yield(nil, fmt.Errorf("load step result for async phase: %w", err))
		return nil, false
	}
	assistantMsg, err := loadStepAssistantMessage(lc.store, lc.threadID, stepResult)
	if err != nil {
		lc.yield(nil, fmt.Errorf("load assistant message for async phase: %w", err))
		return nil, false
	}
	toolCalls := extractToolCalls(assistantMsg)
	if err := ensureLegacyStepToolResultsMaterialized(lc.store, lc.threadID, lc.turnID, lc.turnState, stepIndex, toolCalls); err != nil {
		lc.yield(nil, fmt.Errorf("materialize legacy async tool results: %w", err))
		return nil, false
	}
	allResults, err := loadCompletedToolResultsFromStepEvents(lc.store, lc.threadID, lc.turnID, stepIndex)
	if err != nil {
		lc.yield(nil, fmt.Errorf("load async step tool results: %w", err))
		return nil, false
	}
	asyncContinuations, _ := lc.store.LoadAsyncContinuations(lc.threadID, lc.turnID, stepIndex)
	return &waitingForAsyncPhaseState{
		stepIndex:          stepIndex,
		toolCalls:          toolCalls,
		asyncContinuations: asyncContinuations,
		allResults:         allResults,
	}, true
}

// handleRecoveredWaitingAsyncContinuation restores one persisted async
// continuation while the step is in PhaseWaitingForAsync. The continuation may
// still be running, may now need approval, or may have finished with a result
// that must be appended to the step's event stream.
func (lc *loopContext) handleRecoveredWaitingAsyncContinuation(stepIndex int, continuation AsyncContinuationInfo, allResults map[string]message.ToolResultPart) loopStepResult {
	call := message.ToolCallPart{ToolCallID: continuation.ToolCallID, ToolName: continuation.ToolName, Input: string(continuation.Input)}
	resumeResult, resumeErr := lc.executor.Continue(lc.ctx, lc.toolCtx, call, continuation.Continuation, nil)
	if resumeErr != nil {
		result := message.ToolResultPart{ToolCallID: continuation.ToolCallID, ToolName: continuation.ToolName, Output: message.ErrorTextOutput{Value: fmt.Sprintf("async continuation lost after restart: %v", resumeErr)}}
		allResults[continuation.ToolCallID] = result
		if _, err := appendStepToolEventMessage(lc.store, lc.threadID, lc.turnID, lc.turnState, stepIndex, result); err != nil {
			lc.yield(nil, fmt.Errorf("append async recovery result message: %w", err))
			return loopStepStop
		}
		return loopStepContinue
	}
	if resumeResult.Async != nil {
		lc.asyncHandles = append(lc.asyncHandles, pendingAsyncEntry{toolCallID: continuation.ToolCallID, toolName: continuation.ToolName, handle: resumeResult.Async})
		return loopStepContinue
	}
	if resumeResult.Approval != nil {
		approvalContinuation := resumeResult.Approval.Continuation
		if len(approvalContinuation) == 0 {
			approvalContinuation = continuation.Continuation
		}
		if !pauseForApproval(lc.store, lc.threadID, lc.turnState, lc.turnID, stepIndex, &pendingApprovalPause{toolCallID: continuation.ToolCallID, resumePhase: PhaseWaitingForAsync, continuation: approvalContinuation, approval: resumeResult.Approval}, lc.yield) {
			return loopStepStop
		}
		return loopStepDone
	}
	allResults[continuation.ToolCallID] = resumeResult.Result
	if _, err := appendStepToolEventMessage(lc.store, lc.threadID, lc.turnID, lc.turnState, stepIndex, resumeResult.Result); err != nil {
		lc.yield(nil, fmt.Errorf("append async resumed result message: %w", err))
		return loopStepStop
	}
	return loopStepContinue
}

// finalizeWaitingForAsyncPhase closes out PhaseWaitingForAsync once there are
// no more in-memory async handles to wait on. Any tool call that still lacks a
// result at this point is recorded as an interrupted failure so the turn can
// advance instead of remaining stuck forever.
func (lc *loopContext) finalizeWaitingForAsyncPhase(state *waitingForAsyncPhaseState) loopStepResult {
	for _, tc := range state.toolCalls {
		if _, ok := state.allResults[tc.ToolCallID]; ok {
			continue
		}
		result := message.ToolResultPart{ToolCallID: tc.ToolCallID, ToolName: tc.ToolName, Output: message.ErrorTextOutput{Value: "interrupted by transient system failure"}}
		state.allResults[tc.ToolCallID] = result
		if _, err := appendStepToolEventMessage(lc.store, lc.threadID, lc.turnID, lc.turnState, state.stepIndex, result); err != nil {
			lc.yield(nil, fmt.Errorf("append missing async result message: %w", err))
			return loopStepStop
		}
	}
	if err := advanceToNextStep(lc.store, lc.threadID, lc.turnState); err != nil {
		lc.yield(nil, err)
		return loopStepStop
	}
	if !yieldFinishStep(state.stepIndex, lc.yield) {
		return loopStepStop
	}
	return loopStepContinue
}

// runWaitingForAsyncPhase waits for tool calls that previously entered async
// execution. On resume it recreates in-memory async handles from persisted
// continuation metadata, then waits until those continuations either resolve
// into results or pause
// again for approval. Once all async work is accounted for, the step advances.
func (lc *loopContext) runWaitingForAsyncPhase() loopStepResult {
	state, ok := lc.loadWaitingForAsyncPhaseState()
	if !ok {
		return loopStepStop
	}
	if len(lc.asyncHandles) == 0 {
		// Resumed turns rebuild async handles from disk because the original
		// in-memory handles were lost with the previous process.
		for _, continuation := range state.asyncContinuations.Continuations {
			if _, done := state.allResults[continuation.ToolCallID]; done {
				continue
			}
			phaseResult := lc.handleRecoveredWaitingAsyncContinuation(state.stepIndex, continuation, state.allResults)
			if phaseResult == loopStepContinue {
				continue
			}
			return phaseResult
		}
	}
	if len(lc.asyncHandles) > 0 {
		pause, ok := waitForAsyncHandles(lc.ctx, lc.store, lc.threadID, lc.turnID, lc.turnState, state.stepIndex, lc.asyncHandles, state.allResults, lc.yield)
		if !ok {
			return loopStepStop
		}
		if pause != nil {
			pause.resumePhase = PhaseWaitingForAsync
			if !pauseForApproval(lc.store, lc.threadID, lc.turnState, lc.turnID, state.stepIndex, pause, lc.yield) {
				return loopStepStop
			}
			return loopStepDone
		}
		lc.asyncHandles = nil
	}
	return lc.finalizeWaitingForAsyncPhase(state)
}

func answerResumePhase(q *PendingQuestionState) TurnPhase {
	if q == nil {
		return PhaseTools
	}
	if q.ResumePhase != "" {
		return q.ResumePhase
	}
	return PhaseTools
}

// runWaitingForAnswerPhase resumes a turn that previously paused for tool
// approval or tool-provided questions. Once an answer is available, it records
// the approval response, resolves the answered tool into a normal tool outcome,
// and routes execution back into the interrupted phase.
func (lc *loopContext) runWaitingForAnswerPhase() loopStepResult {
	stepIndex := lc.turnState.CurrentStep
	answer, err := lc.store.LoadAnswer(lc.threadID, lc.turnID, lc.turnState.PendingApprovalID)
	if err != nil {
		lc.yield(nil, fmt.Errorf("load answer: %w", err))
		return loopStepStop
	}
	if answer == nil {
		// No answer has been submitted yet, so keep the turn paused in place.
		return loopStepDone
	}
	pendingQuestion, err := lc.store.LoadQuestion(lc.threadID, lc.turnID, lc.turnState.PendingApprovalID)
	if err != nil {
		lc.yield(nil, fmt.Errorf("load pending approval: %w", err))
		return loopStepStop
	}
	if pendingQuestion == nil {
		lc.yield(nil, fmt.Errorf("pending approval %s not found", lc.turnState.PendingApprovalID))
		return loopStepStop
	}
	log.Printf("turn: answer received for tool %s, resuming turn %s", pendingQuestion.ToolCallID, lc.turnID)
	stepResult, err := lc.store.LoadStepResult(lc.threadID, lc.turnID, stepIndex)
	if err != nil || stepResult == nil {
		lc.yield(nil, fmt.Errorf("load step result for answer phase: %w", err))
		return loopStepStop
	}
	var answerCall message.ToolCallPart
	// Recover the original tool call payload so the executor can continue from
	// the same input that produced the approval request.
	for _, tc := range stepResult.ToolCalls {
		if tc.ToolCallID == pendingQuestion.ToolCallID {
			answerCall = message.ToolCallPart{ToolCallID: tc.ToolCallID, ToolName: tc.ToolName, Input: string(tc.Input)}
			break
		}
	}
	if err := ensureLegacyStepToolResultsMaterialized(lc.store, lc.threadID, lc.turnID, lc.turnState, stepIndex, extractToolCalls(stepResult.AssistantMessage)); err != nil {
		lc.yield(nil, fmt.Errorf("materialize legacy approval tool results: %w", err))
		return loopStepStop
	}
	approvalResponse := message.ToolApprovalResponse{ToolCallID: pendingQuestion.ToolCallID, ApprovalID: pendingQuestion.ApprovalID, Approved: true}
	if _, err := appendStepToolEventMessage(lc.store, lc.threadID, lc.turnID, lc.turnState, stepIndex, approvalResponse); err != nil {
		lc.yield(nil, fmt.Errorf("append approval response message: %w", err))
		return loopStepStop
	}
	if !lc.yield(message.ToolApprovalResponseDataChunk{Data: message.ToolApprovalResponseData{ApprovalID: pendingQuestion.ApprovalID, ToolCallID: pendingQuestion.ToolCallID, Approved: true}}, nil) {
		return loopStepStop
	}

	resumePhase := answerResumePhase(pendingQuestion)
	answerReq := api.AnswerQuestionRequest{Answers: answer.Answers}
	next, err := lc.executor.Continue(lc.ctx, lc.toolCtx, answerCall, pendingQuestion.Continuation, &answerReq)
	if err != nil {
		lc.yield(nil, fmt.Errorf("continue answered tool: %w", err))
		return loopStepStop
	}

	if next.Approval != nil {
		continuation := next.Approval.Continuation
		if len(continuation) == 0 {
			continuation = pendingQuestion.Continuation
		}
		pause := &pendingApprovalPause{
			toolCallID:   answerCall.ToolCallID,
			resumePhase:  resumePhase,
			continuation: continuation,
			approval:     next.Approval,
		}
		if !pauseForApproval(lc.store, lc.threadID, lc.turnState, lc.turnID, stepIndex, pause, lc.yield) {
			return loopStepStop
		}
		return loopStepDone
	}

	if next.Async != nil {
		lc.asyncHandles = []pendingAsyncEntry{{toolCallID: answerCall.ToolCallID, toolName: answerCall.ToolName, handle: next.Async}}
		asyncContinuationsState, _ := lc.store.LoadAsyncContinuations(lc.threadID, lc.turnID, stepIndex)
		upserted := false
		for i := range asyncContinuationsState.Continuations {
			if asyncContinuationsState.Continuations[i].ToolCallID != answerCall.ToolCallID {
				continue
			}
			asyncContinuationsState.Continuations[i].Continuation = cloneRawMessage(next.Async.Continuation)
			asyncContinuationsState.Continuations[i].Input = answerCall.Input
			upserted = true
			break
		}
		if !upserted {
			asyncContinuationsState.Continuations = append(asyncContinuationsState.Continuations, AsyncContinuationInfo{
				ToolCallID:   answerCall.ToolCallID,
				ToolName:     answerCall.ToolName,
				Continuation: cloneRawMessage(next.Async.Continuation),
				Input:        answerCall.Input,
			})
		}
		if err := lc.store.SaveAsyncContinuations(lc.threadID, lc.turnID, stepIndex, asyncContinuationsState); err != nil {
			lc.yield(nil, fmt.Errorf("save async continuations: %w", err))
			return loopStepStop
		}
		lc.turnState.Phase = PhaseWaitingForAsync
		lc.turnState.CurrentStep = stepIndex
		lc.turnState.PendingApprovalID = ""
		if err := lc.store.SaveTurnState(lc.threadID, *lc.turnState); err != nil {
			lc.yield(nil, fmt.Errorf("save turn state (waiting_for_async): %w", err))
			return loopStepStop
		}
		return loopStepContinue
	}

	resolved := next.Result
	if _, err := appendStepToolEventMessage(lc.store, lc.threadID, lc.turnID, lc.turnState, stepIndex, resolved); err != nil {
		lc.yield(nil, fmt.Errorf("append resolved tool result message: %w", err))
		return loopStepStop
	}
	for _, mc := range message.ToolResultToChunks(resolved) {
		if !lc.yield(mc, nil) {
			return loopStepStop
		}
	}
	if lc.toolCtx.ModeChange != nil {
		lc.toolCtx.ModeChange = nil
		if !YieldThreadUpdate(lc.yield, lc.store, lc.threadID) {
			return loopStepStop
		}
	}
	lc.turnState.Phase = resumePhase
	lc.turnState.CurrentStep = stepIndex
	lc.turnState.PendingApprovalID = ""
	if err := lc.store.SaveTurnState(lc.threadID, *lc.turnState); err != nil {
		lc.yield(nil, fmt.Errorf("save turn state (%s): %w", resumePhase, err))
		return loopStepStop
	}
	return loopStepContinue
}

func cloneRawMessage(data json.RawMessage) json.RawMessage {
	if len(data) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), data...)
}

func saveAssistantStepMessage(
	store *Store,
	threadID, turnID string,
	turnState *TurnState,
	cfg TurnConfig,
	stepIndex int,
	stepResult *StepResult,
) (message.Message, error) {
	if stepResult == nil {
		return message.Message{}, fmt.Errorf("step result is required")
	}
	if stepResult.AssistantMessageID != "" {
		stored, err := store.LoadMessage(threadID, stepResult.AssistantMessageID)
		if err != nil {
			return message.Message{}, fmt.Errorf("load assistant message: %w", err)
		}
		turnState.LeafMsgID = stepResult.AssistantMessageID
		return stored.Message, nil
	}

	assistantMsg := stepResult.AssistantMessage
	assistantMsg.Metadata = buildMessageMetadata(cfg, turnState.StartedAt, nil)
	assistantMsgID := resolveMessageID(assistantMsg)
	if err := store.SaveMessage(threadID, StoredMessage{
		ID:       assistantMsgID,
		ParentID: turnState.LeafMsgID,
		Message:  assistantMsg,
	}); err != nil {
		return message.Message{}, fmt.Errorf("save assistant message: %w", err)
	}
	turnState.LeafMsgID = assistantMsgID
	stepResult.AssistantMessageID = assistantMsgID
	if err := store.SaveStepResult(threadID, turnID, stepIndex, *stepResult); err != nil {
		return message.Message{}, fmt.Errorf("save step result assistant message id: %w", err)
	}
	return assistantMsg, nil
}

func loadStepAssistantMessage(store *Store, threadID string, stepResult *StepResult) (message.Message, error) {
	if stepResult == nil {
		return message.Message{}, fmt.Errorf("step result is required")
	}
	if stepResult.AssistantMessageID != "" {
		stored, err := store.LoadMessage(threadID, stepResult.AssistantMessageID)
		if err != nil {
			return message.Message{}, fmt.Errorf("load assistant message: %w", err)
		}
		return stored.Message, nil
	}
	return stepResult.AssistantMessage, nil
}

func appendMessage(
	store *Store,
	threadID string,
	turnState *TurnState,
	msg message.Message,
) (StoredMessage, error) {
	msgID := resolveMessageID(msg)
	stored := StoredMessage{
		ID:       msgID,
		ParentID: turnState.LeafMsgID,
		Message:  msg,
	}
	if err := store.SaveMessage(threadID, stored); err != nil {
		return StoredMessage{}, err
	}
	turnState.LeafMsgID = msgID
	return stored, nil
}

func appendStepEventMessageID(store *Store, threadID, turnID string, stepIndex int, msgID string) error {
	events, err := store.LoadStepEventMessages(threadID, turnID, stepIndex)
	if err != nil {
		return err
	}
	events.MessageIDs = append(events.MessageIDs, msgID)
	return store.SaveStepEventMessages(threadID, turnID, stepIndex, events)
}

func appendStepToolEventMessage(
	store *Store,
	threadID, turnID string,
	turnState *TurnState,
	stepIndex int,
	parts ...message.Part,
) (message.Message, error) {
	createdAt := time.Now().UTC()
	msg := message.Message{
		Role:      "tool",
		Parts:     append([]message.Part{}, parts...),
		CreatedAt: &createdAt,
	}
	stored, err := appendMessage(store, threadID, turnState, msg)
	if err != nil {
		return message.Message{}, fmt.Errorf("save tool event message: %w", err)
	}
	if err := appendStepEventMessageID(store, threadID, turnID, stepIndex, stored.ID); err != nil {
		return message.Message{}, fmt.Errorf("save step event message id: %w", err)
	}
	var toolResults []message.ToolResultPart
	for _, part := range parts {
		if result, ok := part.(message.ToolResultPart); ok {
			toolResults = append(toolResults, result)
		}
	}
	if len(toolResults) > 0 {
		existing, err := store.LoadToolResults(threadID, turnID, stepIndex)
		if err != nil {
			return message.Message{}, fmt.Errorf("load tool results: %w", err)
		}
		existing.Results = append(existing.Results, toolResults...)
		if err := store.SaveToolResults(threadID, turnID, stepIndex, existing); err != nil {
			return message.Message{}, fmt.Errorf("save tool results: %w", err)
		}
	}
	return stored.Message, nil
}

func advanceToNextStep(store *Store, threadID string, turnState *TurnState) error {
	turnState.CurrentStep++
	turnState.Phase = PhaseStreaming
	turnState.PendingApprovalID = ""
	if err := store.SaveTurnState(threadID, *turnState); err != nil {
		return fmt.Errorf("save turn state (next step): %w", err)
	}
	return nil
}

func ensureLegacyStepToolResultsMaterialized(
	store *Store,
	threadID, turnID string,
	turnState *TurnState,
	stepIndex int,
	toolCalls []message.ToolCallPart,
) error {
	events, err := store.LoadStepEventMessages(threadID, turnID, stepIndex)
	if err != nil {
		return fmt.Errorf("load step event messages: %w", err)
	}
	if len(events.MessageIDs) > 0 {
		return nil
	}
	legacy, err := store.LoadToolResults(threadID, turnID, stepIndex)
	if err != nil {
		return fmt.Errorf("load legacy tool results: %w", err)
	}
	if len(legacy.Results) == 0 {
		return nil
	}
	byID := make(map[string]message.ToolResultPart, len(legacy.Results))
	for _, result := range legacy.Results {
		byID[result.ToolCallID] = result
	}
	for _, tc := range toolCalls {
		result, ok := byID[tc.ToolCallID]
		if !ok {
			continue
		}
		if _, err := appendStepToolEventMessage(store, threadID, turnID, turnState, stepIndex, result); err != nil {
			return fmt.Errorf("materialize legacy tool result %s: %w", tc.ToolCallID, err)
		}
	}
	return nil
}

func pauseForApproval(
	store *Store,
	threadID string,
	turnState *TurnState,
	turnID string,
	stepIndex int,
	pause *pendingApprovalPause,
	yield func(message.MessageChunk, error) bool,
) bool {
	if pause == nil || pause.approval == nil {
		return false
	}

	approvalID := generateID()
	continuation := pause.continuation
	if len(continuation) == 0 {
		continuation = pause.approval.Continuation
	}

	if err := store.SaveQuestion(threadID, turnState.ID, PendingQuestionState{
		ApprovalID:   approvalID,
		ToolCallID:   pause.toolCallID,
		StepIndex:    stepIndex,
		ResumePhase:  pause.resumePhase,
		Continuation: cloneRawMessage(continuation),
		Questions:    pause.approval.Questions,
	}); err != nil {
		yield(nil, fmt.Errorf("save question: %w", err))
		return false
	}

	approvalPart := message.ToolApprovalRequest{
		ApprovalID: approvalID,
		ToolCallID: pause.toolCallID,
	}
	if _, err := appendStepToolEventMessage(store, threadID, turnID, turnState, stepIndex, approvalPart); err != nil {
		yield(nil, fmt.Errorf("append approval request message: %w", err))
		return false
	}

	if !yield(message.ToolApprovalRequestChunk{
		ApprovalID: approvalID,
		ToolCallID: pause.toolCallID,
	}, nil) {
		return false
	}

	turnState.Phase = PhaseWaitingForAnswer
	turnState.CurrentStep = stepIndex
	turnState.PendingApprovalID = approvalID
	if err := store.SaveTurnState(threadID, *turnState); err != nil {
		yield(nil, fmt.Errorf("save turn state (waiting): %w", err))
		return false
	}

	return true
}

func yieldFinishStep(stepIndex int, yield func(message.MessageChunk, error) bool) bool {
	if stepIndex == 0 {
		return true
	}
	return yield(message.FinishStepChunk{}, nil)
}

// waitForAsyncHandles waits for all in-flight async continuation handles
// concurrently.
// Results are persisted incrementally and yielded to the SSE stream as they arrive.
// Returns false if the caller should return early (yield returned false or error).
func waitForAsyncHandles(
	ctx context.Context,
	store *Store,
	threadID, turnID string,
	turnState *TurnState,
	stepIndex int,
	pending []pendingAsyncEntry,
	allResults map[string]message.ToolResultPart,
	yield func(message.MessageChunk, error) bool,
) (*pendingApprovalPause, bool) {
	type asyncResult struct {
		toolCallID   string
		continuation json.RawMessage
		result       AsyncWaitResult
		err          error
	}

	ch := make(chan asyncResult, len(pending))
	for _, pa := range pending {
		go func(pa pendingAsyncEntry) {
			result, err := pa.handle.Wait(ctx)
			ch <- asyncResult{
				toolCallID:   pa.toolCallID,
				continuation: cloneRawMessage(pa.handle.Continuation),
				result:       result,
				err:          err,
			}
		}(pa)
	}

	var pause *pendingApprovalPause
	for range pending {
		ar := <-ch
		if ar.err != nil {
			ar.result.Result = message.ToolResultPart{
				ToolCallID: ar.toolCallID,
				ToolName:   lookupPendingToolName(pending, ar.toolCallID),
				Output:     message.ErrorTextOutput{Value: ar.err.Error()},
			}
		}
		if ar.result.Approval != nil {
			if pause == nil {
				pause = &pendingApprovalPause{
					toolCallID:   ar.toolCallID,
					continuation: cloneRawMessage(ar.result.Approval.Continuation),
					approval:     ar.result.Approval,
				}
				if len(pause.continuation) == 0 {
					pause.continuation = ar.continuation
				}
			}
			continue
		}
		allResults[ar.toolCallID] = ar.result.Result
		if _, err := appendStepToolEventMessage(store, threadID, turnID, turnState, stepIndex, ar.result.Result); err != nil {
			yield(nil, fmt.Errorf("append async tool result message: %w", err))
			return nil, false
		}

		for _, mc := range message.ToolResultToChunks(ar.result.Result) {
			if !yield(mc, nil) {
				return nil, false
			}
		}
	}

	return pause, true
}

func lookupPendingToolName(pending []pendingAsyncEntry, toolCallID string) string {
	for _, entry := range pending {
		if entry.toolCallID == toolCallID {
			return entry.toolName
		}
	}
	return ""
}

// runCompletion calls the LLM provider and persists the result.
// Returns the assistant message, extracted tool calls, token usage from the
// response, whether to continue, and the stream error (if any).
// When the stream error is a context_length_exceeded error, it is returned
// without being yielded so the caller can retry with compaction.
func runCompletion(
	ctx context.Context,
	provider providers.Provider,
	store *Store,
	threadID string,
	turnID string,
	stepIndex int,
	cfg *TurnConfig,
	history []message.Message,
	msgIDOverride string,
	yield func(message.MessageChunk, error) bool,
) (message.Message, []message.ToolCallPart, message.Usage, bool, error) {
	req := providers.CompleteRequest{
		Model:           providers.ModelRef{ProviderID: cfg.ProviderID, ModelID: cfg.Model},
		Messages:        history,
		Tools:           cfg.Tools,
		MaxTokens:       cfg.MaxTokens,
		Temperature:     cfg.Temperature,
		TopP:            cfg.TopP,
		Reasoning:       cfg.Reasoning,
		ProviderOptions: cfg.ProviderOptions,
	}

	// Open step file for persistence. This also creates the turn directory,
	// which must exist before the transport logging goroutines fire.
	stepFile, err := store.CreateStepFile(threadID, turnID, stepIndex)
	if err != nil {
		yield(nil, fmt.Errorf("create step file: %w", err))
		return message.Message{}, nil, message.Usage{}, false, nil
	}

	// Inject per-step log file paths so the transport writes raw request/
	// response bytes alongside the other step-NNN-* files.
	ctx = transport.WithLogFiles(ctx,
		store.StepLogReqPath(threadID, turnID, stepIndex),
		store.StepLogRespPath(threadID, turnID, stepIndex),
	)

	acc := message.NewChunkAccumulator()
	exp := message.NewChunkExpander(stepIndex == 0)
	var streamErr error

	emitRetryChunk := func(event transport.RetryEvent) {
		retryChunk := message.ErrorChunk{ErrorText: formatRetryMessage(event)}
		if err := store.AppendChunk(stepFile, retryChunk); err != nil {
			return
		}
		acc.Push(retryChunk)
		for _, mc := range exp.Expand(retryChunk) {
			yield(mc, nil) //nolint:errcheck // best-effort retry visibility
		}
	}
	ctx = transport.WithRetryObserver(ctx, emitRetryChunk)

	// Stream from provider.
	for chunk, chunkErr := range provider.Complete(ctx, req) {
		if chunkErr != nil {
			streamErr = chunkErr
			if ctx.Err() == nil && !isContextLengthExceeded(chunkErr) {
				// Non-cancellation, non-retryable error — yield to consumer.
				if !yield(nil, chunkErr) {
					stepFile.Close()
					return message.Message{}, nil, message.Usage{}, false, nil
				}
			}
			break
		}

		if writeErr := store.AppendChunk(stepFile, chunk); writeErr != nil {
			streamErr = writeErr
			if !yield(nil, fmt.Errorf("write chunk: %w", writeErr)) {
				stepFile.Close()
				return message.Message{}, nil, message.Usage{}, false, nil
			}
			break
		}

		acc.Push(chunk)

		for _, mc := range exp.Expand(chunk) {
			if !yield(mc, nil) {
				stepFile.Close()
				return message.Message{}, nil, message.Usage{}, false, nil
			}
		}
	}

	stepFile.Close()

	// Handle context cancellation — save any partial result.
	if streamErr != nil && ctx.Err() != nil {
		acc.Close()
		partialMsg := acc.Message()
		partialMsg.Parts = filterContentParts(partialMsg.Parts)
		if len(partialMsg.Parts) > 0 {
			stepResult := StepResult{AssistantMessage: partialMsg}
			_ = store.SaveStepResult(threadID, turnID, stepIndex, stepResult)
			return partialMsg, nil, message.Usage{}, true, nil
		}
		return message.Message{}, nil, message.Usage{}, false, nil
	}

	if streamErr != nil {
		// Return the error to the caller. context_length_exceeded errors are
		// returned without having been yielded so the caller can retry with
		// compaction; all other errors have already been yielded above.
		return message.Message{}, nil, message.Usage{}, false, streamErr
	}

	acc.Close()
	assistantMsg := acc.Message()

	// Extract usage reported by the provider for use in compaction heuristics.
	var usage message.Usage
	if finish := acc.FinishResult(); finish != nil {
		usage = finish.Usage
	}

	// Override the public/UI message ID so it matches the StartChunk we already
	// emitted. Provider-native response IDs remain on assistantMsg.ProviderResponseID.
	if msgIDOverride != "" {
		assistantMsg.ID = msgIDOverride
	}

	// Extract non-provider-executed tool calls.
	toolCalls := extractToolCalls(assistantMsg)

	// Persist step result (assistant message + tool call list).
	stepResult := StepResult{AssistantMessage: assistantMsg}
	for _, tc := range toolCalls {
		stepResult.ToolCalls = append(stepResult.ToolCalls, ToolCallInfo{
			ToolCallID: tc.ToolCallID,
			ToolName:   tc.ToolName,
			Input:      string(tc.Input),
		})
	}
	if err := store.SaveStepResult(threadID, turnID, stepIndex, stepResult); err != nil {
		yield(nil, fmt.Errorf("save step result: %w", err))
		return message.Message{}, nil, message.Usage{}, false, nil
	}

	return assistantMsg, toolCalls, usage, true, nil
}

func formatRetryMessage(event transport.RetryEvent) string {
	delay := event.Delay
	if delay < 0 {
		delay = 0
	}
	delayText := delay.Round(100 * time.Millisecond).String()

	switch {
	case event.StatusCode == 429:
		return fmt.Sprintf("provider rate limited (HTTP 429); retrying in %s (attempt %d/%d)", delayText, event.Attempt, event.MaxRetries)
	case event.StatusCode > 0:
		return fmt.Sprintf("provider request failed (HTTP %d); retrying in %s (attempt %d/%d)", event.StatusCode, delayText, event.Attempt, event.MaxRetries)
	case event.Err != nil:
		return fmt.Sprintf("provider request failed: %v; retrying in %s (attempt %d/%d)", event.Err, delayText, event.Attempt, event.MaxRetries)
	default:
		return fmt.Sprintf("provider request failed; retrying in %s (attempt %d/%d)", delayText, event.Attempt, event.MaxRetries)
	}
}

// buildMessageMetadata returns a JSON-encoded messageMetadata object containing
// the model identifier in "providerID/modelID" format, the effective reasoning
// setting, and optional response timing fields.
func buildMessageMetadata(cfg TurnConfig, startedAt, finishedAt *time.Time) json.RawMessage {
	payload := map[string]any{}
	if cfg.ProviderID != "" && cfg.Model != "" {
		payload["model"] = cfg.ProviderID + "/" + cfg.Model
		payload["reasoning"] = string(effectiveReasoning(cfg))
	}
	if startedAt != nil {
		payload["startedAt"] = startedAt.UTC().Format(time.RFC3339Nano)
	}
	if finishedAt != nil {
		payload["finishedAt"] = finishedAt.UTC().Format(time.RFC3339Nano)
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

// effectiveReasoning returns the reasoning setting that the provider will use
// for this turn, matching the auto-detection logic inside the providers.
func effectiveReasoning(cfg TurnConfig) providers.Reasoning {
	switch cfg.Reasoning {
	case providers.ReasoningDisabled, providers.ReasoningNone:
		return providers.ReasoningNone
	case providers.ReasoningEmpty, providers.ReasoningDefault:
		// Auto-detect from models.dev metadata.
		if md := modelsdev.Lookup(cfg.ProviderID, cfg.Model); md != nil {
			if md.DefaultReasonLevel != "" {
				return providers.Reasoning(md.DefaultReasonLevel)
			}
			if md.Reasoning {
				return providers.ReasoningAuto
			}
		}
		return providers.ReasoningNone
	default:
		return cfg.Reasoning
	}
}

// resolveMessageID returns the message's ID if set by the provider, otherwise
// generates a new random ID. This allows providers to supply message IDs
// (e.g., via ResponseMetadataChunk) while ensuring every message has an ID.
func resolveMessageID(msg message.Message) string {
	if msg.ID != "" {
		return msg.ID
	}
	return generateID()
}

// extractToolCalls returns all non-provider-executed ToolCallParts from an assistant message.
func extractToolCalls(msg message.Message) []message.ToolCallPart {
	var calls []message.ToolCallPart
	for _, part := range msg.Parts {
		if tc, ok := part.(message.ToolCallPart); ok {
			if tc.ProviderExecuted != nil && *tc.ProviderExecuted {
				continue
			}
			calls = append(calls, tc)
		}
	}
	return calls
}

func loadCompletedToolResultsFromStepEvents(store *Store, threadID, turnID string, step int) (map[string]message.ToolResultPart, error) {
	events, err := store.LoadStepEventMessages(threadID, turnID, step)
	if err != nil {
		return nil, fmt.Errorf("load step event messages: %w", err)
	}
	results := make(map[string]message.ToolResultPart)
	if len(events.MessageIDs) == 0 {
		legacy, err := store.LoadToolResults(threadID, turnID, step)
		if err != nil {
			return nil, fmt.Errorf("load legacy tool results: %w", err)
		}
		for _, result := range legacy.Results {
			results[result.ToolCallID] = result
		}
		return results, nil
	}
	for _, msgID := range events.MessageIDs {
		stored, err := store.LoadMessage(threadID, msgID)
		if err != nil {
			return nil, fmt.Errorf("load step event message %s: %w", msgID, err)
		}
		for _, part := range stored.Message.Parts {
			if result, ok := part.(message.ToolResultPart); ok {
				results[result.ToolCallID] = result
			}
		}
	}
	return results, nil
}

// recoverStreamingStep attempts to recover a turn that was interrupted while
// the provider was streaming.
//
// If the persisted step file contains a terminal FinishChunk, the streamed step
// was complete and we reconstruct a StepResult so execution can continue.
//
// If the persisted step file is incomplete, we reconstruct a partial assistant
// message, drop any incomplete tool calls, persist it as the step result, and
// let the caller finalize the interrupted turn without executing tools.
//
// Returns (streamComplete, recovered).
func recoverStreamingStep(store *Store, threadID, turnID string, turnState *TurnState) (bool, bool) {
	stepIndex := turnState.CurrentStep

	chunks, err := store.LoadStepChunks(threadID, turnID, stepIndex)
	if err != nil || len(chunks) == 0 {
		return false, false
	}

	acc := message.NewChunkAccumulator()
	streamComplete := false
	for _, chunk := range chunks {
		acc.Push(chunk)
		if _, ok := chunk.(message.FinishChunk); ok {
			streamComplete = true
		}
	}
	acc.Close()

	partialMsg := acc.Message()
	if stepIndex == 0 {
		partialMsg.ID = turnState.AssistantMsgID
	}
	if !streamComplete {
		partialMsg.Parts = filterContentParts(partialMsg.Parts)
	}

	if err := store.SaveStepResult(threadID, turnID, stepIndex, StepResult{AssistantMessage: partialMsg}); err != nil {
		log.Printf("turn: recover streaming step %d: save step result: %v", stepIndex, err)
		return false, false
	}

	return streamComplete, true
}

func finalizeTurnState(store *Store, threadID string, turnState *TurnState) error {
	if turnState.FinishedAt == nil {
		finishedAt := time.Now().UTC()
		turnState.FinishedAt = &finishedAt
	}
	return store.SaveTurnState(threadID, *turnState)
}

// filterContentParts returns only text and reasoning parts from a message,
// dropping incomplete tool calls. Used when saving a partial assistant message
// after context cancellation.
func filterContentParts(parts []message.Part) []message.Part {
	var filtered []message.Part
	for _, p := range parts {
		switch p.(type) {
		case message.TextPart, message.ReasoningPart:
			filtered = append(filtered, p)
		}
	}
	return filtered
}
