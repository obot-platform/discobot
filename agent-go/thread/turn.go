package thread

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"log"
	"time"

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
	UserParts             []message.Part             `json:"-"`
	UserMessage           message.Message            `json:"userMessage"` // serializable form of UserParts
	Tools                 []providers.ToolDefinition `json:"tools,omitempty"`
	MaxTokens             *int                       `json:"maxTokens,omitempty"`
	Temperature           *float64                   `json:"temperature,omitempty"`
	TopP                  *float64                   `json:"topP,omitempty"`
	Reasoning             providers.Reasoning        `json:"reasoning,omitempty"`
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
		cfg.UserMessage = message.Message{Role: "user", Parts: cfg.UserParts}

		turnID := generateID()
		turnState := TurnState{
			ID:          turnID,
			ThreadID:    threadID,
			LeafID:      leafID,
			Config:      cfg,
			CurrentStep: 0,
			Phase:       PhaseStreaming,
			LeafMsgID:   leafID,
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
			MessageMetadata: buildMessageMetadata(cfg),
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
		yield(message.ResponseFinishChunk{FinishReason: "stop"}, nil) //nolint:errcheck
		_ = store.DeleteTurnState(threadID)
	}
}

// ResumeTurn recovers an interrupted turn from persisted state.
// Before re-entering the execution loop it replays all previously streamed
// chunks so that the consumer can rebuild its in-memory state (tool calls,
// partial output, etc.) from the interrupted turn.
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

		replayPrefix := turnState.Phase != PhaseWaitingForAnswer

		if replayPrefix {
			// Re-emit the user message before the start envelope on resume.
			if !yield(message.UserMessageChunk{
				Data: message.UserMessageData{
					Message:               turnState.Config.UserMessage,
					InsertBeforeMessageID: turnState.AssistantMsgID,
				},
			}, nil) {
				return
			}

			// Re-emit the outer start envelope so the AI SDK can bind the resumed
			// stream to the same message ID as the original run.
			if !yield(message.StartChunk{
				MessageID:       turnState.AssistantMsgID,
				MessageMetadata: buildMessageMetadata(turnState.Config),
			}, nil) {
				return
			}

			// If the turn was interrupted mid-stream, try to recover a complete
			// tool call message from the persisted step file. If the already-streamed
			// chunks contain tool calls we can proceed directly to tool execution
			// without re-calling the provider.
			if turnState.Phase == PhaseStreaming {
				recoverStreamingStep(store, threadID, turnID, turnState)
			}

			// Replay all chunks from previously completed steps and the current
			// step (if streaming already finished) so the consumer can reconstruct
			// its in-memory state before the loop continues execution.
			if !replayCompletedSteps(store, threadID, turnID, turnState, yield) {
				return
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

		yield(message.ResponseFinishChunk{FinishReason: "stop"}, nil) //nolint:errcheck
		_ = store.DeleteTurnState(threadID)
	}
}

// pendingAsyncEntry tracks an in-flight async task handle during the step loop.
type pendingAsyncEntry struct {
	toolCallID string
	toolName   string
	handle     *AsyncTaskHandle
}

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
	var history []message.Message
	var asyncHandles []pendingAsyncEntry
	var lastUsage *message.Usage // usage from the most recent successful LLM step

	for {
		switch turnState.Phase {
		case PhaseStreaming:
			stepIndex := turnState.CurrentStep

			// Stop if the caller imposed a step limit.
			if cfg.MaxSteps > 0 && stepIndex >= cfg.MaxSteps {
				return true
			}

			// Load history for the LLM call.
			historyEntries, err := store.BuildHistoryWithIDs(threadID, turnState.LeafMsgID)
			if err != nil {
				yield(nil, fmt.Errorf("build history: %w", err))
				return false
			}

			// Apply compaction if context window info is available (from cfg or models.dev).
			compacted, compactErr := maybeCompact(ctx, provider, store, threadID, turnState, cfg, historyEntries, lastUsage)
			if compactErr != nil {
				log.Printf("compaction: %v (using full history)", compactErr)
				history = entriesToMessages(historyEntries)
			} else {
				history = compacted
			}

			// Check for existing step result (crash recovery: LLM completed
			// but crashed before transitioning to the tools phase).
			existingResult, _ := store.LoadStepResult(threadID, turnID, stepIndex)

			var assistantMsg message.Message
			var toolCalls []message.ToolCallPart

			if existingResult != nil {
				assistantMsg = existingResult.AssistantMessage
				toolCalls = extractToolCalls(assistantMsg)
			} else {
				var stepUsage message.Usage
				var completionErr error
				var ok bool
				idOverride := ""
				if stepIndex == 0 {
					idOverride = turnState.AssistantMsgID
				}
				assistantMsg, toolCalls, stepUsage, ok, completionErr = runCompletion(ctx, provider, store, threadID, turnID, stepIndex, cfg, history, idOverride, yield)
				if !ok {
					// If the provider rejected the input due to context length,
					// attempt a one-shot emergency compaction and retry.
					if isContextLengthExceeded(completionErr) {
						log.Printf("compaction: context_length_exceeded — forcing emergency compaction for thread %s", threadID)
						forceCompacted, forceErr := forceCompact(ctx, provider, store, threadID, cfg, historyEntries)
						if forceErr != nil {
							log.Printf("compaction: emergency compaction failed: %v", forceErr)
							if !yield(nil, completionErr) {
								return false
							}
							return false
						}
						assistantMsg, toolCalls, stepUsage, ok, completionErr = runCompletion(ctx, provider, store, threadID, turnID, stepIndex, cfg, forceCompacted, idOverride, yield)
						if !ok {
							if completionErr != nil && !yield(nil, completionErr) {
								return false
							}
							return false
						}
					} else {
						return false
					}
				}
				// Update lastUsage so the next step's maybeCompact can use the
				// actual token counts from the model instead of calling CountTokens.
				if stepUsage.InputTokens.Total > 0 || stepUsage.OutputTokens.Total > 0 {
					lastUsage = &stepUsage
				}
			}

			if len(toolCalls) == 0 {
				// No tool calls — save assistant message and finish turn.
				assistantMsgID := resolveMessageID(assistantMsg)
				if err := store.SaveMessage(threadID, StoredMessage{
					ID:       assistantMsgID,
					ParentID: turnState.LeafMsgID,
					Message:  assistantMsg,
				}); err != nil {
					yield(nil, fmt.Errorf("save assistant message: %w", err))
					return false
				}
				turnState.LeafMsgID = assistantMsgID
				return yieldFinishStep(stepIndex, yield)
			}

			// Has tool calls — transition to tools phase.
			turnState.Phase = PhaseTools
			turnState.CurrentStep = stepIndex
			if err := store.SaveTurnState(threadID, *turnState); err != nil {
				yield(nil, fmt.Errorf("save turn state (tools): %w", err))
				return false
			}
			continue

		case PhaseTools:
			stepIndex := turnState.CurrentStep

			stepResult, err := store.LoadStepResult(threadID, turnID, stepIndex)
			if err != nil || stepResult == nil {
				yield(nil, fmt.Errorf("load step result for tools phase: %w", err))
				return false
			}

			assistantMsg := stepResult.AssistantMessage
			toolCalls := extractToolCalls(assistantMsg)

			// Load existing tool results and async tasks (crash recovery state).
			existingToolResults, _ := store.LoadToolResults(threadID, turnID, stepIndex)
			completedTools := make(map[string]message.ToolResultPart)
			for _, r := range existingToolResults.Results {
				completedTools[r.ToolCallID] = r
			}

			existingAsyncTasks, _ := store.LoadAsyncTasks(threadID, turnID, stepIndex)
			existingAsyncByID := make(map[string]AsyncTaskInfo)
			for _, at := range existingAsyncTasks.Tasks {
				existingAsyncByID[at.ToolCallID] = at
			}

			// Track tool results for incremental persistence.
			var toolResults StepToolResults
			toolResults.Results = append(toolResults.Results, existingToolResults.Results...)

			// Track async tasks — both newly launched and resumed from crash.
			asyncHandles = nil
			asyncTasksState := existingAsyncTasks

			// allResults collects every tool result keyed by ID.
			// We build the final tool message from this in original order.
			allResults := make(map[string]message.ToolResultPart)
			for id, r := range completedTools {
				allResults[id] = r
			}

			paused := false
			interruptedOne := false

			// --- Execute tools ---
			for _, tc := range toolCalls {
				// Stop executing tools if the context was cancelled.
				if ctx.Err() != nil {
					break
				}

				// Skip tools that already completed (from before a crash).
				if _, ok := allResults[tc.ToolCallID]; ok {
					continue
				}

				// Re-attach existing async task from crash recovery.
				if at, ok := existingAsyncByID[tc.ToolCallID]; ok {
					call := message.ToolCallPart{
						ToolCallID: tc.ToolCallID,
						ToolName:   tc.ToolName,
						Input:      string(at.Input),
					}
					resumeResult, resumeErr := executor.ResumeAsync(ctx, toolCtx, call, at.TaskID)
					if resumeErr != nil {
						result := message.ToolResultPart{
							ToolCallID: tc.ToolCallID,
							ToolName:   tc.ToolName,
							Output:     message.ErrorTextOutput{Value: fmt.Sprintf("async task lost: %v", resumeErr)},
						}
						allResults[tc.ToolCallID] = result
						toolResults.Results = append(toolResults.Results, result)
						if err := store.SaveToolResults(threadID, turnID, stepIndex, toolResults); err != nil {
							yield(nil, fmt.Errorf("save tool results: %w", err))
							return false
						}
						for _, mc := range message.ToolResultToChunks(result) {
							if !yield(mc, nil) {
								return false
							}
						}
						continue
					}
					if resumeResult.Async != nil {
						asyncHandles = append(asyncHandles, pendingAsyncEntry{
							toolCallID: tc.ToolCallID,
							toolName:   tc.ToolName,
							handle:     resumeResult.Async,
						})
					} else {
						allResults[tc.ToolCallID] = resumeResult.Result
						toolResults.Results = append(toolResults.Results, resumeResult.Result)
						if err := store.SaveToolResults(threadID, turnID, stepIndex, toolResults); err != nil {
							yield(nil, fmt.Errorf("save tool results: %w", err))
							return false
						}
						for _, mc := range message.ToolResultToChunks(resumeResult.Result) {
							if !yield(mc, nil) {
								return false
							}
						}
					}
					continue
				}

				// If some tools completed but this one didn't, it was likely
				// in-progress when the crash happened. Mark the FIRST uncompleted
				// tool as interrupted (it may have partially executed).
				if len(completedTools) > 0 && !interruptedOne {
					interruptedOne = true
					result := message.ToolResultPart{
						ToolCallID: tc.ToolCallID,
						ToolName:   tc.ToolName,
						Output:     message.ErrorTextOutput{Value: "interrupted by transient system failure"},
					}
					allResults[tc.ToolCallID] = result
					toolResults.Results = append(toolResults.Results, result)
					if err := store.SaveToolResults(threadID, turnID, stepIndex, toolResults); err != nil {
						yield(nil, fmt.Errorf("save tool results: %w", err))
						return false
					}
					continue
				}

				// Normal execution.
				execResult, execErr := executor.Execute(ctx, toolCtx, tc)
				if execErr != nil {
					execResult = ToolExecuteResult{
						Result: message.ToolResultPart{
							ToolCallID: tc.ToolCallID,
							ToolName:   tc.ToolName,
							Output:     message.ErrorTextOutput{Value: execErr.Error()},
						},
					}
				}

				// Handle async tool — record handle, persist metadata.
				if execResult.Async != nil {
					asyncHandles = append(asyncHandles, pendingAsyncEntry{
						toolCallID: tc.ToolCallID,
						toolName:   tc.ToolName,
						handle:     execResult.Async,
					})
					asyncTasksState.Tasks = append(asyncTasksState.Tasks, AsyncTaskInfo{
						ToolCallID: tc.ToolCallID,
						ToolName:   tc.ToolName,
						TaskID:     execResult.Async.TaskID,
						Input:      string(tc.Input),
					})
					if err := store.SaveAsyncTasks(threadID, turnID, stepIndex, asyncTasksState); err != nil {
						yield(nil, fmt.Errorf("save async tasks: %w", err))
						return false
					}
					continue
				}

				// Handle approval — wait for any in-flight async first, then pause.
				if execResult.Approval != nil {
					if len(asyncHandles) > 0 {
						if !waitForAsyncTasks(ctx, store, threadID, turnID, stepIndex,
							asyncHandles, allResults, &toolResults, yield) {
							return false
						}
						asyncHandles = nil
					}

					// Save question to disk.
					if err := store.SaveQuestion(threadID, turnID, PendingQuestionState{
						ToolCallID: tc.ToolCallID,
						StepIndex:  stepIndex,
						Questions:  execResult.Approval.Questions,
					}); err != nil {
						yield(nil, fmt.Errorf("save question: %w", err))
						return false
					}

					// Save partial tool results.
					if len(toolResults.Results) > 0 {
						if err := store.SaveToolResults(threadID, turnID, stepIndex, toolResults); err != nil {
							yield(nil, fmt.Errorf("save tool results: %w", err))
							return false
						}
					}

					// Add approval request part to assistant message for UI state.
					approvalPart := message.ToolApprovalRequest{
						ApprovalID: tc.ToolCallID,
						ToolCallID: tc.ToolCallID,
					}
					msgWithApproval := assistantMsg
					msgWithApproval.Parts = append(append([]message.Part{}, assistantMsg.Parts...), approvalPart)

					assistantMsgID := resolveMessageID(assistantMsg)
					if err := store.SaveMessage(threadID, StoredMessage{
						ID:       assistantMsgID,
						ParentID: turnState.LeafMsgID,
						Message:  msgWithApproval,
					}); err != nil {
						yield(nil, fmt.Errorf("save assistant message (waiting): %w", err))
						return false
					}
					turnState.LeafMsgID = assistantMsgID

					// Emit the approval request chunk to the SSE stream.
					if !yield(message.ToolApprovalRequestChunk{
						ApprovalID: tc.ToolCallID,
						ToolCallID: tc.ToolCallID,
					}, nil) {
						return false
					}

					turnState.Phase = PhaseWaitingForAnswer
					turnState.CurrentStep = stepIndex
					turnState.PendingApprovalID = tc.ToolCallID
					if err := store.SaveTurnState(threadID, *turnState); err != nil {
						yield(nil, fmt.Errorf("save turn state (waiting): %w", err))
						return false
					}

					paused = true
					break
				}

				// Sync tool result.
				result := execResult.Result
				allResults[tc.ToolCallID] = result
				toolResults.Results = append(toolResults.Results, result)

				if err := store.SaveToolResults(threadID, turnID, stepIndex, toolResults); err != nil {
					yield(nil, fmt.Errorf("save tool results: %w", err))
					return false
				}

				for _, mc := range message.ToolResultToChunks(result) {
					if !yield(mc, nil) {
						return false
					}
				}

				// If the tool signaled a mode change during normal execution,
				// emit it immediately so consumers can keep session mode in sync.
				if toolCtx.ModeChange != nil {
					modeChunk := message.ModeChangeChunk{
						Data: message.ModeChangeData{Mode: *toolCtx.ModeChange},
					}
					toolCtx.ModeChange = nil
					if !yield(modeChunk, nil) {
						return false
					}
				}
			}

			if paused {
				return true
			}

			// Context cancelled — save what we have and end the turn.
			if ctx.Err() != nil {
				var orderedResults []message.ToolResultPart
				for _, tc := range toolCalls {
					if r, ok := allResults[tc.ToolCallID]; ok {
						orderedResults = append(orderedResults, r)
					} else {
						orderedResults = append(orderedResults, message.ToolResultPart{
							ToolCallID: tc.ToolCallID,
							ToolName:   tc.ToolName,
							Output:     message.ErrorTextOutput{Value: "cancelled"},
						})
					}
				}

				assistantMsgID := resolveMessageID(assistantMsg)
				_ = store.SaveMessage(threadID, StoredMessage{
					ID:       assistantMsgID,
					ParentID: turnState.LeafMsgID,
					Message:  assistantMsg,
				})

				toolMsg := message.Message{Role: "tool"}
				for _, r := range orderedResults {
					toolMsg.Parts = append(toolMsg.Parts, r)
				}
				toolMsgID := generateID()
				_ = store.SaveMessage(threadID, StoredMessage{
					ID:       toolMsgID,
					ParentID: assistantMsgID,
					Message:  toolMsg,
				})
				turnState.LeafMsgID = toolMsgID
				return true
			}

			// Transition to async wait if there are pending handles.
			if len(asyncHandles) > 0 {
				turnState.Phase = PhaseWaitingForAsync
				turnState.CurrentStep = stepIndex
				if err := store.SaveTurnState(threadID, *turnState); err != nil {
					yield(nil, fmt.Errorf("save turn state (waiting_for_async): %w", err))
					return false
				}
				continue
			}

			// All tools done — save messages and advance step.
			var orderedResults []message.ToolResultPart
			for _, tc := range toolCalls {
				if r, ok := allResults[tc.ToolCallID]; ok {
					orderedResults = append(orderedResults, r)
				}
			}

			toolMsg, saveErr := saveStepMessages(store, threadID, turnState, assistantMsg, orderedResults)
			if saveErr != nil {
				yield(nil, saveErr)
				return false
			}
			if !yieldFinishStep(stepIndex, yield) {
				return false
			}
			history = append(history, assistantMsg, toolMsg)
			continue

		case PhaseWaitingForAsync:
			stepIndex := turnState.CurrentStep

			stepResult, err := store.LoadStepResult(threadID, turnID, stepIndex)
			if err != nil || stepResult == nil {
				yield(nil, fmt.Errorf("load step result for async phase: %w", err))
				return false
			}

			assistantMsg := stepResult.AssistantMessage
			toolCalls := extractToolCalls(assistantMsg)

			// Load existing tool results.
			existingToolResults, _ := store.LoadToolResults(threadID, turnID, stepIndex)
			allResults := make(map[string]message.ToolResultPart)
			for _, r := range existingToolResults.Results {
				allResults[r.ToolCallID] = r
			}

			var toolResults StepToolResults
			toolResults.Results = append(toolResults.Results, existingToolResults.Results...)

			if len(asyncHandles) == 0 {
				// Crash recovery — no in-memory handles, resume from disk.
				asyncTasks, _ := store.LoadAsyncTasks(threadID, turnID, stepIndex)

				for _, task := range asyncTasks.Tasks {
					if _, done := allResults[task.ToolCallID]; done {
						continue
					}

					call := message.ToolCallPart{
						ToolCallID: task.ToolCallID,
						ToolName:   task.ToolName,
						Input:      string(task.Input),
					}

					resumeResult, resumeErr := executor.ResumeAsync(ctx, toolCtx, call, task.TaskID)
					if resumeErr != nil {
						result := message.ToolResultPart{
							ToolCallID: task.ToolCallID,
							ToolName:   task.ToolName,
							Output:     message.ErrorTextOutput{Value: fmt.Sprintf("async task lost after restart: %v", resumeErr)},
						}
						allResults[task.ToolCallID] = result
						toolResults.Results = append(toolResults.Results, result)
						continue
					}

					if resumeResult.Async != nil {
						asyncHandles = append(asyncHandles, pendingAsyncEntry{
							toolCallID: task.ToolCallID,
							toolName:   task.ToolName,
							handle:     resumeResult.Async,
						})
					} else {
						allResults[task.ToolCallID] = resumeResult.Result
						toolResults.Results = append(toolResults.Results, resumeResult.Result)
					}
				}
			}

			// Wait for any remaining async handles.
			if len(asyncHandles) > 0 {
				if !waitForAsyncTasks(ctx, store, threadID, turnID, stepIndex,
					asyncHandles, allResults, &toolResults, yield) {
					return false
				}
				asyncHandles = nil
			}

			// Build ordered results and save.
			var orderedResults []message.ToolResultPart
			for _, tc := range toolCalls {
				if r, ok := allResults[tc.ToolCallID]; ok {
					orderedResults = append(orderedResults, r)
				} else {
					orderedResults = append(orderedResults, message.ToolResultPart{
						ToolCallID: tc.ToolCallID,
						ToolName:   tc.ToolName,
						Output:     message.ErrorTextOutput{Value: "interrupted by transient system failure"},
					})
				}
			}

			toolMsg, saveErr := saveStepMessages(store, threadID, turnState, assistantMsg, orderedResults)
			if saveErr != nil {
				yield(nil, saveErr)
				return false
			}
			if !yieldFinishStep(stepIndex, yield) {
				return false
			}
			history = append(history, assistantMsg, toolMsg)
			continue

		case PhaseWaitingForAnswer:
			stepIndex := turnState.CurrentStep

			answer, err := store.LoadAnswer(threadID, turnID, turnState.PendingApprovalID)
			if err != nil {
				yield(nil, fmt.Errorf("load answer: %w", err))
				return false
			}
			if answer == nil {
				// Still waiting for user input.
				return true
			}

			log.Printf("turn: answer received for tool %s, resuming turn %s", answer.ToolCallID, turnID)

			stepResult, err := store.LoadStepResult(threadID, turnID, stepIndex)
			if err != nil || stepResult == nil {
				yield(nil, fmt.Errorf("load step result for answer phase: %w", err))
				return false
			}

			assistantMsg := stepResult.AssistantMessage

			// Load existing tool results.
			existingToolResults, _ := store.LoadToolResults(threadID, turnID, stepIndex)
			completedTools := make(map[string]message.ToolResultPart)
			for _, r := range existingToolResults.Results {
				completedTools[r.ToolCallID] = r
			}

			// Find the original tool call and resolve the answer.
			var answerCall message.ToolCallPart
			for _, tc := range stepResult.ToolCalls {
				if tc.ToolCallID == answer.ToolCallID {
					answerCall = message.ToolCallPart{
						ToolCallID: tc.ToolCallID,
						ToolName:   tc.ToolName,
						Input:      string(tc.Input),
					}
					break
				}
			}
			resolved, resolveErr := executor.ResolveApproval(toolCtx, answerCall, answer.Answers)
			if resolveErr != nil {
				yield(nil, fmt.Errorf("resolve approval: %w", resolveErr))
				return false
			}
			completedTools[answer.ToolCallID] = resolved

			// Yield the resolved tool result so consumers (e.g. the CLI) can
			// observe the approval outcome — for example to switch plan mode off
			// when ExitPlanMode is approved.
			for _, mc := range message.ToolResultToChunks(resolved) {
				if !yield(mc, nil) {
					return false
				}
			}

			// If the tool signaled a mode change (e.g. ExitPlanMode approved),
			// emit a ModeChangeChunk so the server can update the session mode.
			if toolCtx.ModeChange != nil {
				modeChunk := message.ModeChangeChunk{
					Data: message.ModeChangeData{Mode: *toolCtx.ModeChange},
				}
				toolCtx.ModeChange = nil
				if !yield(modeChunk, nil) {
					return false
				}
			}

			// Build complete tool results in original order.
			var orderedResults []message.ToolResultPart
			for _, tc := range stepResult.ToolCalls {
				if result, ok := completedTools[tc.ToolCallID]; ok {
					orderedResults = append(orderedResults, result)
				} else {
					orderedResults = append(orderedResults, message.ToolResultPart{
						ToolCallID: tc.ToolCallID,
						ToolName:   tc.ToolName,
						Output:     message.ErrorTextOutput{Value: "interrupted by transient system failure"},
					})
				}
			}

			toolMsg, saveErr := saveStepMessages(store, threadID, turnState, assistantMsg, orderedResults)
			if saveErr != nil {
				yield(nil, saveErr)
				return false
			}
			if !yieldFinishStep(stepIndex, yield) {
				return false
			}
			history = append(history, assistantMsg, toolMsg)
			continue

		default:
			yield(nil, fmt.Errorf("unknown turn phase: %s", turnState.Phase))
			return false
		}
	}
}

// saveStepMessages saves the assistant and tool messages to the thread and
// advances the turn state to the next step. This is the shared save logic
// used by PhaseTools, PhaseWaitingForAsync, and PhaseWaitingForAnswer.
func saveStepMessages(
	store *Store,
	threadID string,
	turnState *TurnState,
	assistantMsg message.Message,
	toolResults []message.ToolResultPart,
) (message.Message, error) {
	assistantMsgID := resolveMessageID(assistantMsg)
	// Skip re-saving the assistant message if it is already the current leaf.
	// This happens in PhaseWaitingForAnswer: the approval pause already saved the
	// assistant message with the correct parentId and then advanced LeafMsgID to
	// assistantMsgID. Re-saving here would overwrite that correct parentId with a
	// self-referential one (parentId == id), causing an infinite history traversal.
	if assistantMsgID != turnState.LeafMsgID {
		if err := store.SaveMessage(threadID, StoredMessage{
			ID:       assistantMsgID,
			ParentID: turnState.LeafMsgID,
			Message:  assistantMsg,
		}); err != nil {
			return message.Message{}, fmt.Errorf("save assistant message: %w", err)
		}
	}

	toolMsg := message.Message{Role: "tool"}
	for _, r := range toolResults {
		toolMsg.Parts = append(toolMsg.Parts, r)
	}

	toolMsgID := generateID()
	if err := store.SaveMessage(threadID, StoredMessage{
		ID:       toolMsgID,
		ParentID: assistantMsgID,
		Message:  toolMsg,
	}); err != nil {
		return message.Message{}, fmt.Errorf("save tool message: %w", err)
	}

	// Advance to next step.
	turnState.LeafMsgID = toolMsgID
	turnState.CurrentStep++
	turnState.Phase = PhaseStreaming
	if err := store.SaveTurnState(threadID, *turnState); err != nil {
		return message.Message{}, fmt.Errorf("save turn state (next step): %w", err)
	}

	return toolMsg, nil
}

func yieldFinishStep(stepIndex int, yield func(message.MessageChunk, error) bool) bool {
	if stepIndex == 0 {
		return true
	}
	return yield(message.FinishStepChunk{}, nil)
}

// waitForAsyncTasks waits for all in-flight async task handles concurrently.
// Results are persisted incrementally and yielded to the SSE stream as they arrive.
// Returns false if the caller should return early (yield returned false or error).
func waitForAsyncTasks(
	ctx context.Context,
	store *Store,
	threadID, turnID string,
	stepIndex int,
	pending []pendingAsyncEntry,
	allResults map[string]message.ToolResultPart,
	toolResults *StepToolResults,
	yield func(message.MessageChunk, error) bool,
) bool {
	type asyncResult struct {
		toolCallID string
		result     message.ToolResultPart
	}

	ch := make(chan asyncResult, len(pending))
	for _, pa := range pending {
		go func(pa pendingAsyncEntry) {
			result, err := pa.handle.Wait(ctx)
			if err != nil {
				result = message.ToolResultPart{
					ToolCallID: pa.toolCallID,
					ToolName:   pa.toolName,
					Output:     message.ErrorTextOutput{Value: err.Error()},
				}
			}
			ch <- asyncResult{toolCallID: pa.toolCallID, result: result}
		}(pa)
	}

	for range pending {
		ar := <-ch
		allResults[ar.toolCallID] = ar.result
		toolResults.Results = append(toolResults.Results, ar.result)

		if err := store.SaveToolResults(threadID, turnID, stepIndex, *toolResults); err != nil {
			yield(nil, fmt.Errorf("save async tool results: %w", err))
			return false
		}

		for _, mc := range message.ToolResultToChunks(ar.result) {
			if !yield(mc, nil) {
				return false
			}
		}
	}

	return true
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
// the model identifier in "providerID/modelID" format and the effective reasoning
// setting, as expected by the server when it intercepts "start" SSE events.
//
// The reasoning field reflects what will actually be used: "auto" when cfg.Reasoning
// is "auto" or when it's unset and the model supports reasoning
// (matching the auto-detection logic in the providers). "none" otherwise.
func buildMessageMetadata(cfg TurnConfig) json.RawMessage {
	if cfg.ProviderID == "" || cfg.Model == "" {
		return nil
	}
	reasoning := effectiveReasoning(cfg)
	data, err := json.Marshal(map[string]string{
		"model":     cfg.ProviderID + "/" + cfg.Model,
		"reasoning": string(reasoning),
	})
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

// replayCompletedSteps replays persisted step chunks to the consumer.
// Called at the start of ResumeTurn so the caller can rebuild its in-memory
// state from chunks that were streamed in previous steps of the interrupted turn.
//
// For each fully completed step (0..currentStep-1):
//   - Replays LLM output chunks via ChunkExpander
//   - Replays completed tool result chunks
//   - Emits FinishStepChunk after the tool outputs for that step
//
// For the current step if streaming is already done (phase != PhaseStreaming):
//   - Replays LLM output chunks via ChunkExpander
//   - Replays any persisted tool result chunks
//   - Does not emit FinishStepChunk yet because the step may still be in progress
//
// For PhaseWaitingForAnswer, also emits a ToolApprovalRequestChunk so the
// consumer can re-surface the pending approval in its state.
//
// Returns false if yield returned false (consumer cancelled).
func replayCompletedSteps(
	store *Store,
	threadID, turnID string,
	turnState *TurnState,
	yield func(message.MessageChunk, error) bool,
) bool {
	// Replay all fully completed steps.
	for step := range turnState.CurrentStep {
		if !replayStepLLMChunks(store, threadID, turnID, step, yield) {
			return false
		}
		if !replayStepToolResults(store, threadID, turnID, step, yield) {
			return false
		}
		if !yieldFinishStep(step, yield) {
			return false
		}
	}

	// Always replay LLM chunks for the current step — replayStepLLMChunks is a
	// no-op when the file doesn't exist, so this is safe when the crash happened
	// before any streaming started. When Phase == PhaseStreaming this also covers
	// the recovery case where recoverStreamingStep found completed chunks and saved
	// a StepResult: executeLoop will use existingResult without re-streaming, so the
	// client needs the chunks replayed here to see the message content.
	if !replayStepLLMChunks(store, threadID, turnID, turnState.CurrentStep, yield) {
		return false
	}
	if !replayStepToolResults(store, threadID, turnID, turnState.CurrentStep, yield) {
		return false
	}

	// If paused for user approval, re-emit the approval request chunk so the
	// consumer knows a question is pending.
	if turnState.Phase == PhaseWaitingForAnswer {
		q, _ := store.LoadQuestion(threadID, turnID, turnState.PendingApprovalID)
		if q != nil {
			if !yield(message.ToolApprovalRequestChunk{
				ApprovalID: q.ToolCallID,
				ToolCallID: q.ToolCallID,
			}, nil) {
				return false
			}
		}
	}

	return true
}

// replayStepLLMChunks reads step-NNN.jsonl and yields the expanded MessageChunks.
func replayStepLLMChunks(
	store *Store,
	threadID, turnID string,
	step int,
	yield func(message.MessageChunk, error) bool,
) bool {
	chunks, err := store.LoadStepChunks(threadID, turnID, step)
	if err != nil || len(chunks) == 0 {
		return true // tolerate missing file
	}
	exp := message.NewChunkExpander(step == 0)
	for _, chunk := range chunks {
		for _, mc := range exp.Expand(chunk) {
			if !yield(mc, nil) {
				return false
			}
		}
	}
	return true
}

// replayStepToolResults reads step-NNN-tools.json and yields ToolOutput chunks.
func replayStepToolResults(
	store *Store,
	threadID, turnID string,
	step int,
	yield func(message.MessageChunk, error) bool,
) bool {
	toolResults, _ := store.LoadToolResults(threadID, turnID, step)
	for _, result := range toolResults.Results {
		for _, mc := range message.ToolResultToChunks(result) {
			if !yield(mc, nil) {
				return false
			}
		}
	}
	return true
}

// recoverStreamingStep attempts to recover a turn that was interrupted while
// the provider was streaming. It loads the persisted step chunks, accumulates
// them into a message, and saves a StepResult so that executeLoop's existing
// existingResult path handles the rest — transitioning to PhaseTools if there
// are tool calls, or finishing the turn if not.
//
// Returns true if a StepResult was saved.
func recoverStreamingStep(store *Store, threadID, turnID string, turnState *TurnState) bool {
	stepIndex := turnState.CurrentStep

	chunks, err := store.LoadStepChunks(threadID, turnID, stepIndex)
	if err != nil || len(chunks) == 0 {
		return false
	}

	acc := message.NewChunkAccumulator()
	for _, chunk := range chunks {
		acc.Push(chunk)
	}
	acc.Close()

	partialMsg := acc.Message()

	// For step 0 the StartChunk already bound the stream to AssistantMsgID,
	// so we must use the same ID here (mirroring the msgIDOverride in runCompletion).
	if stepIndex == 0 {
		partialMsg.ID = turnState.AssistantMsgID
	}

	if err := store.SaveStepResult(threadID, turnID, stepIndex, StepResult{AssistantMessage: partialMsg}); err != nil {
		log.Printf("turn: recover streaming step %d: save step result: %v", stepIndex, err)
		return false
	}

	return true
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
