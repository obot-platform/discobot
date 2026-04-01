package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"

	"github.com/obot-platform/discobot/server/internal/sandbox"
	"github.com/obot-platform/discobot/server/internal/terminal"
)

// Minimum terminal dimensions to prevent zero-size PTY
const (
	minTermRows = 20
	minTermCols = 80
)

// upgrader configures the WebSocket upgrader.
// Origin checking is handled by the CORS middleware in the router,
// so we allow all origins here to avoid duplicate validation.
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(_ *http.Request) bool {
		return true // CORS middleware handles origin validation
	},
}

// TerminalMessage represents a message sent over the WebSocket
type TerminalMessage struct {
	Type string          `json:"type"` // "input", "output", "resize", "error"
	Data json.RawMessage `json:"data,omitempty"`
}

// ResizeData contains terminal resize dimensions
type ResizeData struct {
	Rows int `json:"rows"`
	Cols int `json:"cols"`
}

// TerminalWebSocket handles WebSocket terminal connections.
//
// Each (sandboxSession, user) pair has one persistent PTY managed by
// h.terminalManager. Navigating away and returning reconnects to the same
// shell — the output buffer is replayed so the client sees recent history.
func (h *Handler) TerminalWebSocket(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionId")
	if sessionID == "" {
		h.Error(w, http.StatusBadRequest, "session ID is required")
		return
	}

	if h.sandboxService == nil {
		h.Error(w, http.StatusServiceUnavailable, "sandbox provider not available")
		return
	}

	// Get terminal dimensions from query params, enforcing minimum size
	rows, _ := strconv.Atoi(r.URL.Query().Get("rows"))
	cols, _ := strconv.Atoi(r.URL.Query().Get("cols"))
	if rows < minTermRows {
		rows = minTermRows
	}
	if cols < minTermCols {
		cols = minTermCols
	}

	// Check if root access is requested
	runAsRoot := r.URL.Query().Get("root") == "true"

	ctx := r.Context()

	// Get sandbox client (ensures sandbox is ready and container is running)
	client, err := h.sandboxService.GetClient(ctx, sessionID)
	if err != nil {
		log.Printf("failed to ensure sandbox ready for session %s: %v", sessionID, err)
		h.Error(w, http.StatusInternalServerError, "failed to start sandbox")
		return
	}

	// Determine user for terminal session
	var user string
	if runAsRoot {
		user = "root"
	} else {
		// Get default user from sandbox (uses UID:GID format for compatibility)
		userInfo, err := client.GetUserInfo(ctx)
		if err != nil {
			log.Printf("failed to get user info, falling back to root: %v", err)
			user = "root"
		} else {
			user = strconv.Itoa(userInfo.UID) + ":" + strconv.Itoa(userInfo.GID)
		}
	}

	// Fetch environment variables for the terminal session.
	// Visible credentials are exposed to the terminal. Failures are non-fatal;
	// the terminal still opens without them.
	envVars := map[string]string{}
	if h.credentialService != nil {
		credentialVars, err := h.credentialService.GetVisibleEnvVarsForSession(ctx, sessionID)
		if err != nil {
			log.Printf("failed to get visible credential env vars for session %s: %v", sessionID, err)
		} else {
			for k, v := range credentialVars {
				envVars[k] = v
			}
		}
	}

	// Get or create the persistent terminal session for this (sandbox, user) pair.
	// If one already exists (from a previous WebSocket connection) it is reused —
	// the caller never sees the PTY directly, only a subscriber channel.
	termKey := sessionID + ":" + user
	termSession, err := h.terminalManager.GetOrCreate(ctx, termKey, func(ctx context.Context) (sandbox.PTY, error) {
		return h.sandboxService.Attach(ctx, sessionID, rows, cols, user, envVars)
	})
	if err != nil {
		log.Printf("failed to attach to sandbox PTY: %v", err)
		h.Error(w, http.StatusInternalServerError, "failed to attach to terminal")
		return
	}

	// Resize to the connecting client's viewport. On first connect this is a
	// no-op (PTY was just created with these dimensions). On reconnect it
	// ensures the shell matches the current browser window size.
	if err := termSession.Resize(ctx, rows, cols); err != nil {
		log.Printf("PTY resize on connect: %v", err)
	}

	// Upgrade to WebSocket (after all validation so we don't upgrade then fail)
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("failed to upgrade websocket: %v", err)
		return
	}
	defer func() { _ = conn.Close() }()

	// Subscribe to the session. This returns a channel pre-loaded with the
	// output buffer (recent history) followed by live PTY output.
	sub := termSession.Subscribe()
	defer termSession.Unsubscribe(sub)

	handlePersistentTerminalSession(ctx, termSession, sub, conn)
}

// handlePersistentTerminalSession relays data between a persistent terminal
// session and a WebSocket connection.
//
// The function returns (and the WebSocket is closed) when:
//   - The WebSocket client disconnects (input goroutine exits first).
//   - The PTY process exits (output goroutine exits first, sets a read deadline
//     so the input goroutine unblocks within one second).
//
// The PTY itself is NOT closed when the WebSocket disconnects; it keeps
// running so the next WebSocket connection can reattach to the same shell.
func handlePersistentTerminalSession(ctx context.Context, sess *terminal.Session, sub terminal.Subscriber, conn *websocket.Conn) {
	wsWriteDone := make(chan struct{})

	// Session output → WebSocket.
	// Sends the buffered history first, then streams live output until the
	// subscriber channel is closed (PTY exited) or a WebSocket write fails.
	go func() {
		defer close(wsWriteDone)
		for chunk := range sub {
			data, err := json.Marshal(string(chunk))
			if err != nil {
				log.Printf("terminal: JSON marshal error: %v", err)
				return
			}
			msg := TerminalMessage{Type: "output", Data: json.RawMessage(data)}
			if err := conn.WriteJSON(msg); err != nil {
				// WebSocket write failed (client disconnected); stop sending.
				return
			}
		}
		// sub channel closed → PTY exited; send a clean close frame.
		closeMsg := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "shell exited")
		_ = conn.WriteControl(websocket.CloseMessage, closeMsg, time.Now().Add(time.Second))
	}()

	// WebSocket → session input.
	// Reads input and resize messages from the client. Exits when the client
	// closes the WebSocket or a network error occurs (including a read deadline).
	// Does NOT close the PTY — only the subscriber is cleaned up (via defer).
	inputDone := make(chan struct{})
	go func() {
		defer close(inputDone)
		for {
			var msg TerminalMessage
			if err := conn.ReadJSON(&msg); err != nil {
				if websocket.IsUnexpectedCloseError(err,
					websocket.CloseNormalClosure,
					websocket.CloseGoingAway,
					websocket.CloseAbnormalClosure) {
					log.Printf("terminal: WebSocket read error: %v", err)
				}
				return
			}

			switch msg.Type {
			case "input":
				var input string
				if err := json.Unmarshal(msg.Data, &input); err != nil {
					log.Printf("terminal: failed to unmarshal input: %v", err)
					continue
				}
				if err := sess.Write([]byte(input)); err != nil {
					log.Printf("terminal: PTY write error: %v", err)
					return
				}

			case "resize":
				var resize ResizeData
				if err := json.Unmarshal(msg.Data, &resize); err != nil {
					log.Printf("terminal: failed to unmarshal resize: %v", err)
					continue
				}
				if resize.Cols < minTermCols {
					resize.Cols = minTermCols
				}
				if resize.Rows < minTermRows {
					resize.Rows = minTermRows
				}
				if err := sess.Resize(ctx, resize.Rows, resize.Cols); err != nil {
					log.Printf("terminal: PTY resize error: %v", err)
				}
			}
		}
	}()

	// Wait for either side to finish.
	select {
	case <-wsWriteDone:
		// PTY exited: the close frame has been sent. Set a short read deadline so
		// the input goroutine unblocks (either after the client sends a close ack
		// or after the deadline expires) and then wait for it to finish.
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		<-inputDone
	case <-inputDone:
		// Client disconnected: wait for the output goroutine to finish.
		<-wsWriteDone
	}
}

// GetTerminalHistory returns terminal history for a session
func (h *Handler) GetTerminalHistory(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionId")
	if sessionID == "" {
		h.Error(w, http.StatusBadRequest, "session ID is required")
		return
	}

	// Get limit from query params, default to 100
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 100
	}

	ctx := r.Context()
	history, err := h.store.ListTerminalHistory(ctx, sessionID, limit)
	if err != nil {
		h.Error(w, http.StatusInternalServerError, "failed to get terminal history")
		return
	}

	h.JSON(w, http.StatusOK, map[string]any{"history": history})
}

// GetTerminalStatus returns the sandbox status
func (h *Handler) GetTerminalStatus(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionId")
	if sessionID == "" {
		h.Error(w, http.StatusBadRequest, "session ID is required")
		return
	}

	if h.sandboxService == nil {
		h.JSON(w, http.StatusOK, map[string]string{
			"status": "unavailable",
			"error":  "sandbox provider not configured",
		})
		return
	}

	ctx := r.Context()
	sb, err := h.sandboxService.GetForSession(ctx, sessionID)
	if err != nil {
		if errors.Is(err, sandbox.ErrNotFound) {
			h.JSON(w, http.StatusOK, map[string]string{"status": "not_created"})
			return
		}
		h.Error(w, http.StatusInternalServerError, "failed to get sandbox status")
		return
	}

	response := map[string]any{
		"status":    string(sb.Status),
		"image":     sb.Image,
		"createdAt": sb.CreatedAt.Format(time.RFC3339),
	}
	if sb.StartedAt != nil {
		response["startedAt"] = sb.StartedAt.Format(time.RFC3339)
	}
	if sb.StoppedAt != nil {
		response["stoppedAt"] = sb.StoppedAt.Format(time.RFC3339)
	}
	if sb.Error != "" {
		response["error"] = sb.Error
	}

	h.JSON(w, http.StatusOK, response)
}
