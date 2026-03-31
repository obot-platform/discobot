package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/obot-platform/discobot/agent-go/agent"
	"github.com/obot-platform/discobot/agent-go/internal/api"
	"github.com/obot-platform/discobot/agent-go/message"
	"github.com/obot-platform/discobot/agent-go/thread"
)

const defaultChatStreamPingInterval = 15 * time.Second

func completionIDFromInProgressError(err error) string {
	if err == nil {
		return ""
	}
	parts := strings.SplitN(err.Error(), ":", 2)
	if len(parts) == 2 && parts[0] == "completion_in_progress" {
		return parts[1]
	}
	return ""
}

// PostChat handles POST /threads/{id}/chat — starts a completion and streams the response via SSE.
func (h *Handler) PostChat(w http.ResponseWriter, r *http.Request) {
	threadID := chi.URLParam(r, "id")

	var req api.ChatRequest
	if err := h.DecodeJSON(r, &req); err != nil {
		h.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	leafID, userMessage, err := resolveLeafAndUserMessage(req.Messages)
	if err != nil {
		h.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	// Check for active completion.
	if activeID := h.completions.ActiveCompletionID(threadID); activeID != "" {
		if h.defaultAgent == nil || h.defaultAgent.Store() == nil {
			h.JSON(w, http.StatusConflict, api.ChatConflictResponse{
				Error:        "completion_in_progress",
				CompletionID: activeID,
			})
			return
		}

		h.queueMu.Lock()
		cfg, queuedPrompt, queueErr := h.defaultAgent.Store().AppendQueuedPrompt(threadID, thread.QueuedPrompt{
			Message:   userMessage,
			Model:     req.Model,
			Reasoning: req.Reasoning,
			Mode:      req.Mode,
		})
		if queueErr == nil {
			h.completions.EmitChunkIfActive(threadID, thread.UpdateChunkFromConfig(threadID, cfg))
		}
		h.queueMu.Unlock()
		if queueErr != nil {
			h.Error(w, http.StatusInternalServerError, queueErr.Error())
			return
		}

		h.JSON(w, http.StatusAccepted, api.ChatStartedResponse{
			Status:         "queued",
			QueuedPromptID: queuedPrompt.ID,
		})
		return
	}

	// Check for persisted turn state that blocks new prompts.
	pendingQuestion, err := h.completions.PendingQuestion(threadID)
	if err != nil {
		h.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if pendingQuestion != nil {
		h.JSON(w, http.StatusConflict, api.ChatTurnStateConflictResponse{
			Error:      "pending_question_requires_answer",
			Message:    "This thread is waiting for an answer to an earlier question before sending a new message.",
			QuestionID: pendingQuestion.ApprovalID,
		})
		return
	}
	interrupted, err := h.completions.HasInterruptedTurn(threadID)
	if err != nil {
		h.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if interrupted {
		completionID, resumeErr := h.completions.Resume(threadID)
		if resumeErr != nil {
			if existingID := completionIDFromInProgressError(resumeErr); existingID != "" {
				h.JSON(w, http.StatusConflict, api.ChatConflictResponse{
					Error:        "completion_in_progress",
					CompletionID: existingID,
				})
				return
			}
			h.Error(w, http.StatusInternalServerError, resumeErr.Error())
			return
		}
		h.JSON(w, http.StatusConflict, api.ChatTurnStateConflictResponse{
			Error:        "interrupted_turn_requires_resume",
			Message:      "This thread has an interrupted turn that must resume before sending a new message.",
			CompletionID: completionID,
		})
		return
	}

	promptReq := agent.PromptRequest{
		LeafID:    leafID,
		Model:     req.Model,
		Reasoning: req.Reasoning,
		Mode:      req.Mode,
		UserParts: userMessage.Parts,
		Metadata:  userMessage.Metadata,
	}

	completionID, err := h.completions.Chat(threadID, promptReq)
	if err != nil {
		if strings.Contains(err.Error(), "completion_in_progress") {
			parts := strings.SplitN(err.Error(), ":", 2)
			existingID := ""
			if len(parts) == 2 {
				existingID = parts[1]
			}
			h.JSON(w, http.StatusConflict, api.ChatConflictResponse{
				Error:        "completion_in_progress",
				CompletionID: existingID,
			})
			return
		}
		if errors.Is(err, agent.ErrInterruptedTurnRequiresResume) {
			completionID, resumeErr := h.completions.Resume(threadID)
			if resumeErr != nil {
				if existingID := completionIDFromInProgressError(resumeErr); existingID != "" {
					h.JSON(w, http.StatusConflict, api.ChatConflictResponse{
						Error:        "completion_in_progress",
						CompletionID: existingID,
					})
					return
				}
				h.Error(w, http.StatusInternalServerError, resumeErr.Error())
				return
			}
			h.JSON(w, http.StatusConflict, api.ChatTurnStateConflictResponse{
				Error:        "interrupted_turn_requires_resume",
				Message:      "This thread has an interrupted turn that must resume before sending a new message.",
				CompletionID: completionID,
			})
			return
		}
		if errors.Is(err, agent.ErrPendingQuestionRequiresAnswer) {
			questionID := ""
			pending, pendingErr := h.completions.PendingQuestion(threadID)
			if pendingErr != nil {
				h.Error(w, http.StatusInternalServerError, pendingErr.Error())
				return
			}
			if pending != nil {
				questionID = pending.ApprovalID
			}
			h.JSON(w, http.StatusConflict, api.ChatTurnStateConflictResponse{
				Error:      "pending_question_requires_answer",
				Message:    "This thread is waiting for an answer to an earlier question before sending a new message.",
				QuestionID: questionID,
			})
			return
		}
		h.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.JSON(w, http.StatusAccepted, api.ChatStartedResponse{
		CompletionID: completionID,
		Status:       "started",
	})
}

// ChatStream handles GET /threads/{id}/chat/stream — streams SSE events for a thread.
// Fresh requests (no Last-Event-ID, or an invalid one) replay the full persisted
// UI message history first using named SSE events, then send the current
// in-memory chunk snapshot before history-end, and finally continue with live
// deltas. Valid Last-Event-ID reconnects keep the existing resume-only
// behavior. The SSE protocol is explicit:
//   - history-start / history-message / history-end for replayed UIMessage values
//   - chunk for UIMessageChunk deltas
//   - ping while the stream is otherwise idle
//
// Unlike the previous one-completion model, this endpoint stays connected until
// the client disconnects so later completions on the same thread can arrive on
// the same SSE connection. There is no terminal "done" event; completion
// boundaries are internal and the connection remains reusable for later turns.
func (h *Handler) ChatStream(w http.ResponseWriter, r *http.Request) {
	threadID := chi.URLParam(r, "id")

	snapshot := h.completions.PollChunks(threadID, 0)
	if snapshot == nil {
		interrupted, err := h.completions.HasInterruptedTurn(threadID)
		if err != nil {
			h.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		if interrupted {
			if _, err := h.completions.Resume(threadID); err != nil && !strings.Contains(err.Error(), "completion_in_progress") {
				h.Error(w, http.StatusInternalServerError, err.Error())
				return
			}
			snapshot = h.completions.PollChunks(threadID, 0)
		}
	}
	freshRequest := false
	offset := 0
	if lastEventID := r.Header.Get("Last-Event-ID"); lastEventID != "" {
		if id, n, ok := parseSSEEventID(lastEventID); ok && snapshot != nil && id == snapshot.CompletionID {
			freshRequest = false
			offset = n + 1
		} else {
			freshRequest = true
			offset = 0
		}
	} else {
		freshRequest = true
		offset = 0
	}

	var historyMessages []message.UIMessage
	if freshRequest {
		var err error
		historyMessages, err = h.completions.Messages(threadID, "")
		if err != nil {
			h.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		if historyMessages == nil {
			historyMessages = []message.UIMessage{}
		}
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		h.Error(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	var writeMu sync.Mutex
	writeEvent := func(id, event string, data []byte) {
		writeMu.Lock()
		defer writeMu.Unlock()
		writeSSEEvent(w, id, event, data)
		flusher.Flush()
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ephemeralCh, unsubscribeEphemeral := h.completions.SubscribeEphemeral()
	defer unsubscribeEphemeral()

	currentCompletionID := ""
	lastSeenCompletionID := ""
	if snapshot != nil {
		lastSeenCompletionID = snapshot.CompletionID
		if !snapshot.Done {
			currentCompletionID = snapshot.CompletionID
		}
	}

	if freshRequest {
		writeEvent("", "history-start", json.RawMessage(`{}`))
		for _, msg := range historyMessages {
			data, err := json.Marshal(msg)
			if err != nil {
				continue
			}
			writeEvent("", "history-message", data)
		}

		writeEvent("", "history-end", json.RawMessage(`{}`))
		if currentCompletionID != "" {
			emitCompletionStatusEvent(writeEvent, threadID, currentCompletionID, true)
			offset = 0
		}
	} else if snapshot != nil && !snapshot.Done {
		currentCompletionID = snapshot.CompletionID
		lastSeenCompletionID = snapshot.CompletionID
	}

	go func() {
		for {
			select {
			case <-r.Context().Done():
				return
			case chunk := <-ephemeralCh:
				if chunk == nil {
					continue
				}
				data, err := message.MarshalChunk(chunk)
				if err != nil {
					log.Printf("chat stream: failed to marshal ephemeral chunk: %v", err)
					continue
				}
				writeEvent("", "chunk", data)
			}
		}
	}()

	for {
		if currentCompletionID == "" {
			waitCtx, cancel := context.WithTimeout(r.Context(), h.chatPingEvery)
			result := h.completions.WaitNextCompletion(waitCtx, threadID, lastSeenCompletionID)
			timedOut := errors.Is(waitCtx.Err(), context.DeadlineExceeded)
			cancel()

			if r.Context().Err() != nil {
				return
			}
			if result == nil {
				if timedOut {
					writeEvent("", "ping", json.RawMessage(`{}`))
				}
				continue
			}

			currentCompletionID = result.CompletionID
			lastSeenCompletionID = result.CompletionID
			if !result.Done {
				emitCompletionStatusEvent(writeEvent, threadID, result.CompletionID, true)
			}
			for i, chunk := range result.Chunks {
				data, err := message.MarshalChunk(chunk)
				if err != nil {
					continue
				}
				writeEvent(fmt.Sprintf("%s:%d", result.CompletionID, result.ChunkOffsets[i]), "chunk", data)
			}
			offset = result.NextOffset
			if result.Done {
				currentCompletionID = ""
				offset = 0
			}
			continue
		}

		waitCtx, cancel := context.WithTimeout(r.Context(), h.chatPingEvery)
		result := h.completions.WaitChunks(waitCtx, threadID, currentCompletionID, offset)
		timedOut := errors.Is(waitCtx.Err(), context.DeadlineExceeded)
		cancel()

		if r.Context().Err() != nil {
			return
		}
		if result == nil {
			emitCompletionStatusEvent(writeEvent, threadID, currentCompletionID, false)
			currentCompletionID = ""
			offset = 0
			if timedOut {
				writeEvent("", "ping", json.RawMessage(`{}`))
			}
			continue
		}
		if result.CompletionID != currentCompletionID {
			currentCompletionID = result.CompletionID
			lastSeenCompletionID = result.CompletionID
		}

		for i, chunk := range result.Chunks {
			data, err := message.MarshalChunk(chunk)
			if err != nil {
				continue
			}
			writeEvent(fmt.Sprintf("%s:%d", result.CompletionID, result.ChunkOffsets[i]), "chunk", data)
		}
		offset = result.NextOffset

		if result.Done {
			lastSeenCompletionID = result.CompletionID
			emitCompletionStatusEvent(writeEvent, threadID, result.CompletionID, false)
			currentCompletionID = ""
			offset = 0
			continue
		}

		if timedOut && len(result.Chunks) == 0 {
			writeEvent("", "ping", json.RawMessage(`{}`))
		}
	}
}

func emitCompletionStatusEvent(
	writeEvent func(id, event string, data []byte),
	threadID, completionID string,
	isRunning bool,
) {
	data, err := message.MarshalChunk(message.CompletionStatusChunk{
		Data: message.CompletionStatusData{
			ThreadID:     threadID,
			CompletionID: completionID,
			IsRunning:    isRunning,
		},
	})
	if err != nil {
		return
	}
	writeEvent("", "chunk", data)
}

func writeSSEEvent(w http.ResponseWriter, id, event string, data []byte) {
	if id != "" {
		fmt.Fprintf(w, "id: %s\n", id)
	}
	if event != "" {
		fmt.Fprintf(w, "event: %s\n", event)
	}
	fmt.Fprintf(w, "data: %s\n\n", data)
}

// resolveLeafAndUserMessage extracts the leaf ID and the queued/submitted user
// message from the message history the client sends on every chat request.
//
//   - leafID: the ID of the last assistant message in msgs. This is the branch
//     point the new user turn will extend. Empty when there are no assistant
//     messages yet (first turn of a new thread).
//   - userMessage: the last user message that follows the leaf (i.e. the new
//     user input). Returns an error when no user message is found after the
//     last assistant, or when the last assistant message has no ID.
func resolveLeafAndUserMessage(msgs []message.UIMessage) (leafID string, userMessage message.UIMessage, err error) {
	// Find the last assistant message.
	lastAssistantIdx := -1
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" {
			lastAssistantIdx = i
			break
		}
	}

	startIdx := 0
	if lastAssistantIdx >= 0 {
		if msgs[lastAssistantIdx].ID == "" {
			return "", message.UIMessage{}, fmt.Errorf("last assistant message has no ID")
		}
		leafID = msgs[lastAssistantIdx].ID
		startIdx = lastAssistantIdx + 1
	}

	for i := len(msgs) - 1; i >= startIdx; i-- {
		if msgs[i].Role == "user" && len(msgs[i].Parts) > 0 {
			return leafID, msgs[i], nil
		}
	}
	return "", message.UIMessage{}, fmt.Errorf("no user message found after the last assistant message")
}

// parseSSEEventID parses a "{completionID}:{offset}" SSE event ID.
func parseSSEEventID(id string) (completionID string, offset int, ok bool) {
	idx := strings.LastIndex(id, ":")
	if idx < 0 {
		return "", 0, false
	}
	n, err := strconv.Atoi(id[idx+1:])
	if err != nil {
		return "", 0, false
	}
	return id[:idx], n, true
}

// ChatStatus handles GET /threads/{id}/chat/status — returns whether a completion is active.
// Unlike ChatStream, this never opens an SSE connection; it is safe to poll frequently.
func (h *Handler) ChatStatus(w http.ResponseWriter, r *http.Request) {
	threadID := chi.URLParam(r, "id")
	isRunning := h.completions.ActiveCompletionID(threadID) != ""
	h.JSON(w, http.StatusOK, api.ChatStatusResponse{IsRunning: isRunning})
}

// CancelChat handles POST /threads/{id}/chat/cancel — cancels in-progress completion.
func (h *Handler) CancelChat(w http.ResponseWriter, r *http.Request) {
	threadID := chi.URLParam(r, "id")

	completionID, ok := h.completions.Cancel(threadID)
	if !ok {
		h.JSON(w, http.StatusConflict, api.NoActiveCompletionResponse{
			Error: "no_active_completion",
		})
		return
	}

	h.JSON(w, http.StatusOK, api.CancelCompletionResponse{
		Success:      true,
		CompletionID: completionID,
		Status:       "cancelled",
	})
}

// GetQuestion handles GET /threads/{id}/chat/question/{questionId} — returns a pending user question.
func (h *Handler) GetQuestion(w http.ResponseWriter, r *http.Request) {
	threadID := chi.URLParam(r, "id")
	questionID := chi.URLParam(r, "questionId")

	pending, err := h.completions.PendingQuestion(threadID)
	if err != nil {
		h.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	if pending != nil && pending.ApprovalID == questionID {
		// Question is pending — return it directly.
		h.JSON(w, http.StatusOK, api.PendingQuestionResponse{
			Status: "pending",
			Question: &api.PendingQuestion{
				ToolUseID: pending.ApprovalID,
				Questions: pending.Questions,
			},
		})
		return
	}

	// No pending question — check if it was already answered.
	h.answeredMu.Lock()
	wasAnswered := h.answeredQuestions[questionID]
	h.answeredMu.Unlock()

	status := "expired"
	if wasAnswered {
		status = "answered"
	}

	h.JSON(w, http.StatusOK, api.PendingQuestionResponse{
		Status:   status,
		Question: nil,
	})
}

// PostAnswer handles POST /threads/{id}/chat/answer/{questionId} — submits answers to a pending question.
func (h *Handler) PostAnswer(w http.ResponseWriter, r *http.Request) {
	threadID := chi.URLParam(r, "id")
	questionID := chi.URLParam(r, "questionId")

	var req api.AnswerQuestionRequest
	if err := h.DecodeJSON(r, &req); err != nil {
		h.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Answers == nil {
		h.Error(w, http.StatusBadRequest, "answers is required")
		return
	}

	// Persist the answer.
	if err := h.completions.SubmitAnswer(threadID, questionID, req); err != nil {
		h.Error(w, http.StatusNotFound, err.Error())
		return
	}

	// Track as answered for status polling.
	h.answeredMu.Lock()
	h.answeredQuestions[questionID] = true
	h.answeredMu.Unlock()

	// Resume the interrupted turn.
	completionID, chatErr := h.completions.Resume(threadID)
	if chatErr != nil {
		// Answer was saved but resume failed — log but still return success.
		log.Printf("question: answer saved but failed to resume turn: %v", chatErr)
	}
	_ = completionID

	h.JSON(w, http.StatusOK, api.AnswerQuestionResponse{Success: true})
}
