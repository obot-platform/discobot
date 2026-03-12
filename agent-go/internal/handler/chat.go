package handler

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/obot-platform/discobot/agent-go/agent"
	"github.com/obot-platform/discobot/agent-go/internal/api"
	"github.com/obot-platform/discobot/agent-go/message"
)

// ListMessages handles GET /threads/{id}/messages — returns all messages for the session.
func (h *Handler) ListMessages(w http.ResponseWriter, r *http.Request) {
	threadID := chi.URLParam(r, "id")
	leafID := r.URL.Query().Get("leafId")

	msgs, err := h.completions.Messages(threadID, leafID)
	if err != nil {
		h.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if msgs == nil {
		msgs = []json.RawMessage{}
	}
	data, err := json.Marshal(msgs)
	if err != nil {
		h.Error(w, http.StatusInternalServerError, "failed to marshal messages: "+err.Error())
		return
	}

	h.JSON(w, http.StatusOK, api.GetMessagesResponse{
		Messages: data,
	})
}

// PostChat handles POST /threads/{id}/chat — starts a completion and streams the response via SSE.
func (h *Handler) PostChat(w http.ResponseWriter, r *http.Request) {
	threadID := chi.URLParam(r, "id")

	var req api.ChatRequest
	if err := h.DecodeJSON(r, &req); err != nil {
		h.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Check for active completion.
	if activeID := h.completions.ActiveCompletionID(threadID); activeID != "" {
		h.JSON(w, http.StatusConflict, api.ChatConflictResponse{
			Error:        "completion_in_progress",
			CompletionID: activeID,
		})
		return
	}

	// Reset hook state for user-initiated completions.
	h.resetHookState()

	promptReq := agent.PromptRequest{
		Model:     req.Model,
		Reasoning: req.Reasoning,
		UserParts: extractUserParts(req.Messages),
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
		h.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.JSON(w, http.StatusAccepted, api.ChatStartedResponse{
		CompletionID: completionID,
		Status:       "started",
	})
}

// ChatStream handles GET /threads/{id}/chat/stream — streams SSE events for a completion.
// Fresh requests (no Last-Event-ID, or an invalid one) replay the full persisted
// UI message history first using named SSE events, then continue with any live
// in-memory deltas. Valid Last-Event-ID reconnects keep the existing resume-only
// behavior. The SSE protocol is explicit:
//   - history-start / history-message / history-end for replayed UIMessage values
//   - chunk for UIMessageChunk deltas
//   - done for stream completion
func (h *Handler) ChatStream(w http.ResponseWriter, r *http.Request) {
	threadID := chi.URLParam(r, "id")

	snapshot := h.completions.PollChunks(threadID, 0)
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

	var historyMessages []json.RawMessage
	if freshRequest {
		var err error
		historyMessages, err = h.completions.Messages(threadID, "")
		if err != nil {
			h.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		if historyMessages == nil {
			historyMessages = []json.RawMessage{}
		}
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		h.Error(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	isActive := snapshot != nil && !snapshot.Done
	w.Header().Set("X-Discobot-Completion-Active", strconv.FormatBool(isActive))
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	if freshRequest {
		writeSSEEvent(w, "", "history-start", json.RawMessage(`{}`))
		flusher.Flush()
		for _, msg := range historyMessages {
			writeSSEEvent(w, "", "history-message", msg)
			flusher.Flush()
		}

		if snapshot != nil && !snapshot.Done {
			for index, chunk := range snapshot.Chunks {
				data, err := message.MarshalChunk(chunk)
				if err != nil {
					continue
				}
				writeSSEEvent(w, fmt.Sprintf("%s:%d", snapshot.CompletionID, index), "chunk", data)
				flusher.Flush()
				offset = index + 1
			}
		}

		writeSSEEvent(w, "", "history-end", json.RawMessage(`{}`))
		flusher.Flush()

		if snapshot == nil || snapshot.Done {
			writeSSEEvent(w, "", "done", json.RawMessage(`{}`))
			flusher.Flush()
			return
		}
	}

	for {
		result := h.completions.WaitChunks(r.Context(), threadID, offset)
		if result == nil {
			writeSSEEvent(w, "", "done", json.RawMessage(`{}`))
			flusher.Flush()
			return
		}

		for _, chunk := range result.Chunks {
			data, err := message.MarshalChunk(chunk)
			if err != nil {
				offset++
				continue
			}
			writeSSEEvent(w, fmt.Sprintf("%s:%d", result.CompletionID, offset), "chunk", data)
			flusher.Flush()
			offset++
		}

		if result.Done {
			writeSSEEvent(w, "", "done", json.RawMessage(`{}`))
			flusher.Flush()
			return
		}

		if r.Context().Err() != nil {
			return
		}
	}
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

	if pending != nil && pending.ToolCallID == questionID {
		// Question is pending — parse and return it.
		var questions []api.AskUserQuestion
		if err := json.Unmarshal(pending.Questions, &questions); err != nil {
			h.Error(w, http.StatusInternalServerError, "failed to parse questions: "+err.Error())
			return
		}

		h.JSON(w, http.StatusOK, api.PendingQuestionResponse{
			Status: "pending",
			Question: &api.PendingQuestion{
				ToolUseID: pending.ToolCallID,
				Questions: questions,
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
	if err := h.completions.SubmitAnswer(threadID, questionID, req.Answers); err != nil {
		h.Error(w, http.StatusNotFound, err.Error())
		return
	}

	// Track as answered for status polling.
	h.answeredMu.Lock()
	h.answeredQuestions[questionID] = true
	h.answeredMu.Unlock()

	// Resume the turn. Prompt() will detect the waiting_for_answer state
	// with the answer file on disk and continue the turn.
	completionID, chatErr := h.completions.Chat(threadID, agent.PromptRequest{})
	if chatErr != nil {
		// Answer was saved but resume failed — log but still return success.
		log.Printf("question: answer saved but failed to resume turn: %v", chatErr)
	}
	_ = completionID

	h.JSON(w, http.StatusOK, api.AnswerQuestionResponse{Success: true})
}

// extractUserParts parses the last user message from the AI SDK UIMessages JSON array
// and returns its parts as message.Part values. This converts the frontend wire format
// to the agent's internal representation using UnmarshalUIPart for type dispatch.
func extractUserParts(msgs json.RawMessage) []message.Part {
	if len(msgs) == 0 {
		return nil
	}
	var rawMsgs []struct {
		Role  string            `json:"role"`
		Parts []json.RawMessage `json:"parts"`
	}
	if err := json.Unmarshal(msgs, &rawMsgs); err != nil {
		// Fallback: treat the whole blob as plain text (CLI compat).
		return []message.Part{message.TextPart{Text: string(msgs)}}
	}
	for i := len(rawMsgs) - 1; i >= 0; i-- {
		if rawMsgs[i].Role != "user" {
			continue
		}
		var parts []message.Part
		for _, partData := range rawMsgs[i].Parts {
			p, err := message.UnmarshalUIPart(partData)
			if err != nil {
				continue
			}
			switch v := p.(type) {
			case message.UITextPart:
				parts = append(parts, message.TextPart{Text: v.Text})
			case message.UIFilePart:
				parts = append(parts, message.FilePart{Data: v.URL, MediaType: v.MediaType, Filename: v.Filename})
			}
		}
		if len(parts) > 0 {
			return parts
		}
	}
	return nil
}
