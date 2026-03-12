package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/obot-platform/discobot/agent-go/message"
)

// wsMaxAge is the maximum age of a WebSocket connection before it is proactively
// replaced. The OpenAI Responses API closes connections after 60 minutes; we
// reconnect at 55 minutes to stay well clear of the limit.
const wsMaxAge = 55 * time.Minute

// wsIdleTTL is the maximum time a pooled connection may sit idle before it is
// evicted and closed. This prevents abandoned response chains from keeping
// sockets open indefinitely.
const wsIdleTTL = 10 * time.Minute

// wsReadLimit raises coder/websocket's default 32 KiB per-message cap so large
// OpenAI response events (for example tool arguments, reasoning payloads, or
// terminal response summaries) can be read without tripping ErrMessageTooBig.
const wsReadLimit = 16 << 20

// wsPool manages a pool of persistent WebSocket connections to the Responses API.
// Each connection is keyed by the last response ID it produced. A subsequent
// request that cites that response as its previous_response_id reuses the same
// connection (and its server-side cached state), giving ~40% faster completions
// for agentic tool-call loops.
//
// Connections are exclusively owned while in use: checkout atomically removes a
// connection from the pool, and checkin returns it under the new response ID.
// Multiple parallel sessions therefore each own their own connection with no
// global lock — each session's chain of response IDs maps to its own connection.
// Idle pooled connections are evicted opportunistically on pool operations so
// abandoned response chains do not keep sockets open indefinitely.
type wsPool struct {
	mu     sync.Mutex
	byPrev map[string]*pooledConn // prevRespID → conn primed for the next request on this chain
	apiKey string
	wsURL  string
}

// pooledConn holds a WebSocket connection together with its creation time for
// max-age checks and its last-idle time for opportunistic idle eviction.
type pooledConn struct {
	conn       *websocket.Conn
	createdAt  time.Time
	lastUsedAt time.Time
}

type webSocketAttemptResult struct {
	responseID string
	clean      bool
	emitted    bool
	err        error
}

type openAIStreamError struct {
	message string
	code    string
}

func (e *openAIStreamError) Error() string {
	if e.code != "" {
		return fmt.Sprintf("openai: stream error: %s (code: %s)", e.message, e.code)
	}
	return fmt.Sprintf("openai: stream error: %s", e.message)
}

func isPreviousResponseNotFoundError(err error) bool {
	var streamErr *openAIStreamError
	return errors.As(err, &streamErr) && streamErr.code == "previous_response_not_found"
}

// newWSPool derives a wss:// WebSocket URL from the HTTP base URL and returns
// an initialised wsPool. Connections are created lazily on first use.
func newWSPool(apiKey, httpBaseURL string) *wsPool {
	wsURL := strings.Replace(httpBaseURL, "https://", "wss://", 1)
	wsURL = strings.Replace(wsURL, "http://", "ws://", 1)
	wsURL += "/responses"
	return &wsPool{
		byPrev: make(map[string]*pooledConn),
		apiKey: apiKey,
		wsURL:  wsURL,
	}
}

// checkout atomically removes and returns the connection primed for prevRespID.
// Returns nil when prevRespID is empty (always dial fresh for new chains) or
// when no matching connection is found in the pool. Before lookup it evicts any
// other pooled connections that have been idle for longer than wsIdleTTL.
func (pool *wsPool) checkout(prevRespID string) *pooledConn {
	if prevRespID == "" {
		return nil
	}
	now := time.Now()
	pool.mu.Lock()
	defer pool.mu.Unlock()
	pool.evictIdleLocked(now)
	pc := pool.byPrev[prevRespID]
	delete(pool.byPrev, prevRespID)
	return pc
}

// checkin returns a connection to the pool, keyed by the response ID it just
// produced. If newRespID is empty (should not normally happen) the connection is
// closed, since there is no key under which to store it. Before storing it
// evicts any other pooled connections that have been idle for longer than
// wsIdleTTL.
func (pool *wsPool) checkin(newRespID string, pc *pooledConn) {
	if newRespID == "" {
		closePooledConnNow(pc)
		return
	}
	now := time.Now()
	pc.lastUsedAt = now
	pool.mu.Lock()
	defer pool.mu.Unlock()
	pool.evictIdleLocked(now)
	if replaced := pool.byPrev[newRespID]; replaced != nil {
		closePooledConnNow(replaced)
	}
	pool.byPrev[newRespID] = pc
}

// evictIdle evicts pooled connections that have been idle longer than wsIdleTTL.
func (pool *wsPool) evictIdle(now time.Time) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	pool.evictIdleLocked(now)
}

func (pool *wsPool) evictIdleLocked(now time.Time) {
	for respID, pc := range pool.byPrev {
		if now.Sub(pc.lastUsedAt) > wsIdleTTL {
			delete(pool.byPrev, respID)
			closePooledConnNow(pc)
		}
	}
}

func closePooledConnNow(pc *pooledConn) {
	if pc == nil || pc.conn == nil {
		return
	}
	_ = pc.conn.CloseNow()
}

func cloneWebSocketBody(body map[string]any) map[string]any {
	cloned := make(map[string]any, len(body)+2)
	for k, v := range body {
		cloned[k] = v
	}
	return cloned
}

// dial opens a new authenticated WebSocket connection to the Responses API.
func (pool *wsPool) dial(ctx context.Context) (*pooledConn, error) {
	conn, _, err := websocket.Dial(ctx, pool.wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{
			"Authorization": {"Bearer " + pool.apiKey},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("openai: websocket dial: %w", err)
	}
	now := time.Now()
	conn.SetReadLimit(wsReadLimit)
	return &pooledConn{conn: conn, createdAt: now, lastUsedAt: now}, nil
}

func openAIResponseID(msg message.Message) string {
	if strings.HasPrefix(msg.ProviderResponseID, "resp") {
		return msg.ProviderResponseID
	}
	if strings.HasPrefix(msg.ID, "resp") {
		return msg.ID
	}
	return ""
}

// lastAssistantID returns the OpenAI response ID of the last assistant message
// in msgs, or "" if there is none. It prefers the preserved provider-native
// response ID and only falls back to Message.ID when it already looks like a
// real OpenAI response ID. This avoids sending client-generated UI message IDs
// as previous_response_id.
func lastAssistantID(msgs []message.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" {
			return openAIResponseID(msgs[i])
		}
	}
	return ""
}

// completeViaWebSocket sends a response.create event on a pooled (or fresh)
// WebSocket connection and streams the response events through yield.
//
// prevRespID is the ID of the last assistant message in the request history.
// When non-empty the request carries previous_response_id to reuse server-side
// cached state, and the exact connection that produced that response is reused
// from the pool. Parallel sessions are fully independent: each follows its own
// chain of response IDs and therefore its own connection.
func (p *Provider) completeViaWebSocket(ctx context.Context, body map[string]any, prevRespID string, yield func(message.ProviderMessageChunk, error) bool) {
	// Retrieve the pooled connection for this response chain (if any).
	pc := p.ws.checkout(prevRespID)
	droppedPrevRespID := false

	for {
		usedFreshConn := false

		// If the connection is too old or missing, dial a fresh one.
		if pc == nil || time.Since(pc.createdAt) >= wsMaxAge {
			if pc != nil {
				// Proactively replace an ageing connection before the 60-minute limit.
				_ = pc.conn.Close(websocket.StatusNormalClosure, "reconnecting")
			}
			var err error
			pc, err = p.ws.dial(ctx)
			if err != nil {
				yield(nil, err)
				return
			}
			usedFreshConn = true
		}

		result := p.completeViaWebSocketAttempt(ctx, body, prevRespID, pc, yield)
		if result.clean {
			// Return the connection to the pool keyed by the new response ID so the
			// next request in this chain can reuse it.
			p.ws.checkin(result.responseID, pc)
			return
		}

		// Invalidate on errors or premature consumer exit: the connection may be
		// in an inconsistent state (server may still be sending events).
		closePooledConnNow(pc)
		pc = nil

		if result.err == nil {
			return
		}

		if !result.emitted && prevRespID != "" && !usedFreshConn {
			// A reused pooled connection failed before streaming anything. Retry
			// once on a fresh socket for the same response chain.
			continue
		}

		if !result.emitted && prevRespID != "" && !droppedPrevRespID && isPreviousResponseNotFoundError(result.err) {
			// The server-side cache for this response chain is gone. Fall back to a
			// fresh connection without previous_response_id so the full input
			// history can rebuild state.
			prevRespID = ""
			droppedPrevRespID = true
			continue
		}

		if !result.emitted {
			yield(nil, result.err)
		}
		return
	}
}

func (p *Provider) completeViaWebSocketAttempt(ctx context.Context, body map[string]any, prevRespID string, pc *pooledConn, yield func(message.ProviderMessageChunk, error) bool) webSocketAttemptResult {
	reqBody := cloneWebSocketBody(body)
	reqBody["type"] = "response.create"
	if prevRespID != "" {
		reqBody["previous_response_id"] = prevRespID
	}

	msgBytes, err := json.Marshal(reqBody)
	if err != nil {
		return webSocketAttemptResult{err: fmt.Errorf("openai: websocket marshal: %w", err)}
	}

	if err := pc.conn.Write(ctx, websocket.MessageText, msgBytes); err != nil {
		return webSocketAttemptResult{err: fmt.Errorf("openai: websocket write: %w", err)}
	}

	result := webSocketAttemptResult{}
	respID, clean := parseWebSocketStream(ctx, pc.conn, func(chunk message.ProviderMessageChunk, err error) bool {
		if err != nil {
			result.err = err
			if result.emitted {
				return yield(nil, err)
			}
			return false
		}
		if chunk != nil {
			result.emitted = true
		}
		return yield(chunk, nil)
	})
	result.responseID = respID
	result.clean = clean
	return result
}

// parseWebSocketStream reads JSON events from conn until a terminal event or
// an error. It delegates to the shared SSE event handlers because the Responses
// API emits identical event shapes over both transports; the only difference is
// that WebSocket messages are standalone JSON objects carrying a "type" field
// rather than separate SSE event/data lines.
//
// Returns the response ID captured from the response.created event (empty if
// not received) and clean=true when the stream ended with a terminal event.
func parseWebSocketStream(ctx context.Context, conn *websocket.Conn, yield func(message.ProviderMessageChunk, error) bool) (responseID string, clean bool) {
	state := &streamState{itemCallIDs: make(map[string]string)}

	for {
		_, msgBytes, err := conn.Read(ctx)
		if err != nil {
			yield(nil, fmt.Errorf("openai: websocket read: %w", err))
			return responseID, false
		}

		var header struct {
			Type     string `json:"type"`
			Response struct {
				ID string `json:"id"`
			} `json:"response"`
		}
		if err := json.Unmarshal(msgBytes, &header); err != nil {
			if !yield(nil, fmt.Errorf("openai: parse websocket event: %w", err)) {
				return responseID, false
			}
			continue
		}

		// Capture the response ID from the first response.created event.
		if header.Type == "response.created" && responseID == "" {
			responseID = header.Response.ID
		}

		// Delegate to the shared SSE event handlers. The full JSON message
		// (including the "type" field) is passed as data; all handlers use
		// json.Unmarshal to extract only the fields they care about and
		// silently ignore the extra "type" key present in WebSocket messages.
		if !state.handleSSEEvent(header.Type, msgBytes, yield) {
			return responseID, false
		}

		// Terminal events mark the end of a single response on this connection.
		// Only response.completed is considered clean (connection + cached state
		// are preserved). response.failed and response.incomplete both result in
		// clean=false so the connection is closed and a fresh one dialled next turn.
		switch header.Type {
		case "response.completed":
			return responseID, true
		case "response.failed", "response.incomplete":
			return responseID, false
		}
	}
}
