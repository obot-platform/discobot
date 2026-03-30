package cli

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"os"
	"strconv"
	"strings"

	"golang.org/x/term"

	"github.com/obot-platform/discobot/agent-go/agent"
	"github.com/obot-platform/discobot/agent-go/agentimpl"
	"github.com/obot-platform/discobot/agent-go/internal/api"
	"github.com/obot-platform/discobot/agent-go/message"
)

// sectionKind tracks which type of content is currently being printed,
// so the turn loop can insert blank-line separators at the right boundaries.
type sectionKind int

const (
	skNone      sectionKind = iota // nothing printed yet this turn
	skTool                         // tool input / output lines
	skReasoning                    // thinking / reasoning content
	skText                         // assistant text
)

// chunkSection returns the section kind a chunk belongs to, or skNone for
// chunks that produce no visible output (mode changes, approval requests, …).
func chunkSection(chunk message.MessageChunk) sectionKind {
	switch chunk.(type) {
	case message.TextDeltaChunk:
		return skText
	case message.ReasoningStartChunk, message.ReasoningDeltaChunk, message.ReasoningEndChunk:
		return skReasoning
	case message.ToolInputAvailableChunk, message.ToolOutputAvailableChunk,
		message.ToolOutputErrorChunk, message.ErrorChunk, message.AbortChunk:
		return skTool
	}
	return skNone
}

// sectionNeedsGap reports whether a blank line should be printed before
// transitioning from section `from` to section `to`.
//
// Rules:
//   - tool → tool: no gap (consecutive tool lines stay together)
//   - same → same: no gap (continuation)
//   - anything else involving text or reasoning: one blank line
func sectionNeedsGap(from, to sectionKind) bool {
	if from == to {
		return false
	}
	if from == skTool && to == skTool {
		return false
	}
	// At the very start of a turn only separate text/reasoning from the prompt.
	if from == skNone {
		return to == skText || to == skReasoning
	}
	return from == skReasoning || from == skText || to == skReasoning || to == skText
}

// runTurnLoop drives an agent turn to completion, looping to handle
// intermediate AskUserQuestion / ExitPlanMode approval requests.
//
// req is the initial PromptRequest. When startWithResume is true, the loop
// starts by resuming an interrupted turn instead of prompting a new one.
// On each approval loop iteration, the agent is resumed explicitly so the
// interrupted turn continues from persisted disk state after the user's answer
// is saved.
func runTurnLoop(ctx context.Context, cancel context.CancelFunc, a *agentimpl.DefaultAgent, threadID string, req agent.PromptRequest, startWithResume bool, onPlanModeChange func(bool)) {
	toolState := newToolRenderState()
	pendingPlanToolCalls := map[string]string{}
	activePlanMode := req.Mode == "plan"
	resumeOnly := startWithResume
	setPlanMode := func(enabled bool) {
		activePlanMode = enabled
		if onPlanModeChange != nil {
			onPlanModeChange(enabled)
		}
	}
	for {
		md := newMarkdownRenderer(os.Stdout, term.IsTerminal(int(os.Stdout.Fd())), !noColor)
		currentSection := skNone

		// Show a spinner while waiting for the first response chunk.
		spin := newSpinner()
		spin.Start()

		// Stream the turn, printing chunks as they arrive.
		watchCtx, stopEscWatch := context.WithCancel(ctx)
		watcher := startEscWatch(watchCtx, cancel)
		var seq iter.Seq2[message.MessageChunk, error]
		if resumeOnly {
			seq = a.Resume(ctx, threadID)
		} else {
			seq = a.Prompt(ctx, threadID, req)
		}
		for chunk, err := range seq {
			if err != nil {
				stopEscWatch()
				watcher.Wait()
				md.Finish()
				spin.Stop()
				if errors.Is(err, context.Canceled) || ctx.Err() != nil {
					return
				}
				fmt.Fprintf(os.Stderr, "\nError: %v\n", err)
				return
			}
			if chunk != nil {
				switch c := chunk.(type) {
				case message.ThreadUpdateChunk:
					setPlanMode(strings.EqualFold(c.Data.Thread.Mode, "planning") || strings.EqualFold(c.Data.Thread.Mode, "plan"))
				case message.ToolInputAvailableChunk:
					if isPlanToolName(c.ToolName) {
						pendingPlanToolCalls[c.ToolCallID] = c.ToolName
					}
				case message.ToolOutputAvailableChunk:
					if toolName, ok := pendingPlanToolCalls[c.ToolCallID]; ok {
						switch toolName {
						case "EnterPlanMode":
							setPlanMode(true)
						case "ExitPlanMode":
							if isExitPlanApproved(extractOutputText(c.Output)) {
								setPlanMode(false)
							}
						}
						delete(pendingPlanToolCalls, c.ToolCallID)
					}
				case message.ToolOutputErrorChunk:
					delete(pendingPlanToolCalls, c.ToolCallID)
				case message.ToolOutputDeniedChunk:
					delete(pendingPlanToolCalls, c.ToolCallID)
				}

				switch chunk.(type) {
				case message.TextDeltaChunk,
					message.ReasoningStartChunk,
					message.ReasoningDeltaChunk,
					message.ReasoningEndChunk,
					message.ToolInputAvailableChunk,
					message.ToolOutputAvailableChunk,
					message.ToolOutputErrorChunk,
					message.ErrorChunk,
					message.AbortChunk:
					spin.Stop()
				}

				// Flush buffered text before non-text chunks, then apply
				// section-boundary spacing rules.
				target := chunkSection(chunk)
				if target != skNone {
					if _, isText := chunk.(message.TextDeltaChunk); !isText {
						md.FlushForBoundary()
					}
					if target != currentSection {
						// If text was streaming without a trailing newline, end
						// the line on stderr so tool/reasoning output starts clean.
						if currentSection == skText && !md.AtLineStart() {
							fmt.Fprintln(os.Stderr)
						}
						if sectionNeedsGap(currentSection, target) {
							fmt.Fprintln(os.Stderr)
						}
						currentSection = target
					}
				}

				renderChunk(chunk, md, toolState)
				// After tool output, restart the spinner: the model is about
				// to process the result and stream its next response.
				switch chunk.(type) {
				case message.ToolOutputAvailableChunk, message.ToolOutputErrorChunk:
					spin = newSpinner()
					spin.Start()
				}
			}
		}
		stopEscWatch()
		watcher.Wait()
		md.Finish()
		spin.Stop()

		if ctx.Err() != nil {
			return // cancelled by Ctrl+C or ESC
		}

		// Check whether the turn paused waiting for user approval.
		pending, err := a.PendingQuestion(threadID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\nError checking for pending question: %v\n", err)
			return
		}
		if pending == nil {
			// Turn complete: blank line after text, plain newline otherwise.
			if currentSection == skText {
				if !md.AtLineStart() {
					fmt.Fprint(os.Stdout, "\n")
				}
				fmt.Fprintln(os.Stderr)
			} else {
				fmt.Println()
			}
			return
		}

		// Handle the approval interactively and resume the turn.
		if !handlePendingQuestion(ctx, a, threadID, pending) {
			return
		}
		resumeOnly = true
		req = agent.PromptRequest{Mode: planModeStr(activePlanMode)}
	}
}

// handlePendingQuestion presents a pending AskUserQuestion / ExitPlanMode
// approval to the user, collects answers, and submits them.
// Returns false if stdin was closed or an error occurred.
func handlePendingQuestion(ctx context.Context, a *agentimpl.DefaultAgent, threadID string, pending *agent.PendingQuestion) bool {
	answers := collectAnswers(ctx, pending.Questions)
	if answers == nil {
		return false // EOF or cancellation
	}

	if err := a.SubmitAnswer(threadID, pending.ApprovalID, api.AnswerQuestionRequest{
		Answers: answers,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "\nError submitting answer: %v\n", err)
		return false
	}
	return true
}

// collectAnswers presents each question to the user on stderr and reads
// answers from stdin. Returns nil if stdin closes or ctx is done.
func collectAnswers(ctx context.Context, questions []api.AskUserQuestion) map[string]string {
	answers := make(map[string]string)
	fmt.Fprintln(os.Stderr)

	for _, q := range questions {
		if ctx.Err() != nil {
			return nil
		}

		// Print any context notes (e.g. the plan file content) before the question.
		if q.Notes != "" {
			md := newMarkdownRenderer(os.Stderr, term.IsTerminal(int(os.Stderr.Fd())), !noColor)
			md.WriteText(strings.TrimRight(q.Notes, "\n"))
			md.Finish()
			fmt.Fprintln(os.Stderr)
		}

		fmt.Fprintf(os.Stderr, "%s\n", q.Question)

		if len(q.Options) > 0 {
			for i, opt := range q.Options {
				if opt.Description != "" {
					fmt.Fprintf(os.Stderr, "  %d. %s — %s\n", i+1, opt.Label, opt.Description)
				} else {
					fmt.Fprintf(os.Stderr, "  %d. %s\n", i+1, opt.Label)
				}
			}
			otherNum := len(q.Options) + 1
			fmt.Fprintf(os.Stderr, "  %d. Other — Enter a custom response\n", otherNum)

			for {
				input, err := readLine(fmt.Sprintf("Choice (1-%d or label): ", otherNum), nil)
				if err != nil {
					return nil
				}
				input = strings.TrimSpace(input)

				// Try as 1-based index.
				if n, err := strconv.Atoi(input); err == nil {
					if n >= 1 && n <= len(q.Options) {
						answers[q.Question] = q.Options[n-1].Label
						break
					}
					if n == otherNum {
						custom, err := readLine("Custom response: ", nil)
						if err != nil {
							return nil
						}
						answers[q.Question] = strings.TrimSpace(custom)
						break
					}
				}

				// Try as label (case-insensitive).
				matched := false
				for _, opt := range q.Options {
					if strings.EqualFold(input, opt.Label) {
						answers[q.Question] = opt.Label
						matched = true
						break
					}
				}
				if matched {
					break
				}

				fmt.Fprintf(os.Stderr, "Please enter a number (1-%d) or a matching label.\n", otherNum)
			}
		} else {
			// Free-text answer.
			input, err := readLine("Answer: ", nil)
			if err != nil {
				return nil
			}
			answers[q.Question] = strings.TrimSpace(input)
		}
	}

	return answers
}
