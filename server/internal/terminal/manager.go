// Package terminal manages persistent PTY sessions that survive WebSocket disconnects.
// Each (sandboxSessionID, user) pair gets one long-lived PTY that clients can
// attach to and detach from without terminating the underlying shell process.
package terminal

import (
	"context"
	"io"
	"log"
	"strings"
	"sync"

	"github.com/obot-platform/discobot/server/internal/sandbox"
)

// outputBufferSize is the size of the per-session output ring buffer (512 KB).
// This is replayed to new WebSocket clients on reconnect, giving them recent history.
const outputBufferSize = 512 * 1024

// AttachFunc creates a new PTY session. Called only when no live session exists for the key.
type AttachFunc func(ctx context.Context) (sandbox.PTY, error)

// Subscriber is a channel that receives chunks of raw PTY output.
// It is buffered so that slow consumers do not block the broadcast loop.
type Subscriber chan []byte

// Manager maintains persistent PTY sessions keyed by an opaque string
// (typically "<sandboxSessionID>:<user>").
//
// Lifecycle:
//   - GetOrCreate returns an existing live session or spawns a new one.
//   - The session's internal goroutine removes itself from the map when the PTY exits.
//   - Remove forcefully tears down all sessions for a given sandbox session ID,
//     e.g. when the container is stopped from the outside.
type Manager struct {
	mu       sync.Mutex
	sessions map[string]*Session
}

// NewManager creates a new Manager.
func NewManager() *Manager {
	return &Manager{sessions: make(map[string]*Session)}
}

// GetOrCreate returns the live Session for key, creating one via attachFn if needed.
// If the previous session for key has already exited, a fresh one is created.
func (m *Manager) GetOrCreate(ctx context.Context, key string, attachFn AttachFunc) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if s, ok := m.sessions[key]; ok {
		select {
		case <-s.done:
			// Exited — fall through to create a new one.
			delete(m.sessions, key)
		default:
			return s, nil
		}
	}

	pty, err := attachFn(ctx)
	if err != nil {
		return nil, err
	}

	s := &Session{
		pty:  pty,
		buf:  newRingBuffer(outputBufferSize),
		subs: make(map[Subscriber]struct{}),
		done: make(chan struct{}),
	}
	m.sessions[key] = s

	go s.readLoop(func() {
		m.mu.Lock()
		delete(m.sessions, key)
		m.mu.Unlock()
	})

	return s, nil
}

// Remove forcefully closes all persistent sessions whose key starts with
// sandboxSessionID+":". Call this when the container for that session is
// stopped or deleted so resources are reclaimed immediately.
func (m *Manager) Remove(sandboxSessionID string) {
	prefix := sandboxSessionID + ":"
	m.mu.Lock()
	toClose := make([]*Session, 0)
	for key, s := range m.sessions {
		if strings.HasPrefix(key, prefix) {
			toClose = append(toClose, s)
			delete(m.sessions, key)
		}
	}
	m.mu.Unlock()

	for _, s := range toClose {
		s.forceClose()
	}
}

// Shutdown forcefully closes all persistent terminal sessions.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	toClose := make([]*Session, 0, len(m.sessions))
	for key, s := range m.sessions {
		toClose = append(toClose, s)
		delete(m.sessions, key)
	}
	m.mu.Unlock()

	for _, s := range toClose {
		s.forceClose()
	}
}

// Session is a long-lived PTY with output buffering and fan-out to subscribers.
//
// Concurrency invariant: s.subs is nil if and only if the session has exited.
// Both Subscribe() and closeOnce.Do() hold s.mu when reading/writing s.subs,
// so there is no race between a late Subscribe() and session teardown.
type Session struct {
	pty       sandbox.PTY
	buf       *ringBuffer
	mu        sync.Mutex
	subs      map[Subscriber]struct{} // nil after session exits
	done      chan struct{}           // closed once the PTY's read loop exits
	exitCode  int
	closeOnce sync.Once
}

// Subscribe registers a new WebSocket client.
//
// It immediately enqueues the entire output buffer so the client can replay
// history, then adds the channel to the live broadcast set.
//
// If the session has already exited the returned channel is pre-populated with
// the output snapshot and immediately closed, so callers can always use
// `for chunk := range sub { ... }` without blocking forever.
//
// Call Unsubscribe when the WebSocket closes.
func (s *Session) Subscribe() Subscriber {
	ch := make(Subscriber, 512)

	s.mu.Lock()
	defer s.mu.Unlock()

	// Replay recent history to the new subscriber.
	if snapshot := s.buf.snapshot(); len(snapshot) > 0 {
		ch <- snapshot
	}

	if s.subs == nil {
		// Session has already exited; close the channel immediately so callers
		// don't block on it indefinitely.
		close(ch)
		return ch
	}

	s.subs[ch] = struct{}{}
	return ch
}

// Unsubscribe removes ch from the live broadcast set and closes it.
// Safe to call after the session has exited.
func (s *Session) Unsubscribe(ch Subscriber) {
	s.mu.Lock()
	_, ok := s.subs[ch]
	if ok {
		delete(s.subs, ch)
	}
	s.mu.Unlock()

	if ok {
		close(ch)
	}
}

// Write sends input bytes to the PTY (keyboard input from the WebSocket client).
func (s *Session) Write(p []byte) error {
	_, err := s.pty.Write(p)
	return err
}

// Resize updates the PTY window dimensions. Called whenever the browser window
// resizes or a new client attaches with different dimensions.
func (s *Session) Resize(ctx context.Context, rows, cols int) error {
	return s.pty.Resize(ctx, rows, cols)
}

// Done returns a channel that is closed once the PTY process has exited.
func (s *Session) Done() <-chan struct{} {
	return s.done
}

// ExitCode returns the PTY process exit code.
// Only meaningful after Done() is closed.
func (s *Session) ExitCode() int {
	return s.exitCode
}

// readLoop drains the PTY and broadcasts each chunk to the ring buffer and all
// subscribers. It runs in a dedicated goroutine for each session and calls
// onExit when the PTY closes so the Manager can remove the map entry.
func (s *Session) readLoop(onExit func()) {
	defer onExit()

	buf := make([]byte, 4096)
	for {
		n, err := s.pty.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])

			s.mu.Lock()
			s.buf.write(chunk)
			for ch := range s.subs {
				select {
				case ch <- chunk:
				default:
					// Subscriber channel full; drop chunk.
					// The subscriber will see the full buffer on reconnect.
				}
			}
			s.mu.Unlock()
		}
		if err != nil {
			if err != io.EOF {
				log.Printf("terminal: PTY read error: %v", err)
			}
			break
		}
	}

	exitCode, _ := s.pty.Wait(context.Background())
	s.closeSubscribers(exitCode)
}

// forceClose terminates the PTY and notifies all subscribers.
// Called by Manager.Remove when the container is stopped externally.
func (s *Session) forceClose() {
	_ = s.pty.Close()
	s.closeSubscribers(0)
}

// closeSubscribers runs exactly once: it sets exitCode, closes all subscriber
// channels, marks subs as nil (session-done sentinel), and closes s.done.
// All of this happens under s.mu so that Subscribe() cannot race with teardown.
func (s *Session) closeSubscribers(exitCode int) {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.exitCode = exitCode
		for ch := range s.subs {
			close(ch)
		}
		s.subs = nil  // sentinel: session has exited
		close(s.done) // safe to close inside the mutex
		s.mu.Unlock()
	})
}

// ringBuffer is a fixed-capacity circular byte buffer. Writes overwrite the
// oldest data once capacity is reached. snapshot returns a contiguous copy of
// the current contents in chronological order.
type ringBuffer struct {
	mu   sync.Mutex
	data []byte
	cap  int
	used int
	head int // next write position
}

func newRingBuffer(capacity int) *ringBuffer {
	return &ringBuffer{data: make([]byte, capacity), cap: capacity}
}

func (r *ringBuffer) write(p []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, b := range p {
		r.data[r.head] = b
		r.head = (r.head + 1) % r.cap
		if r.used < r.cap {
			r.used++
		}
	}
}

func (r *ringBuffer) snapshot() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.used == 0 {
		return nil
	}
	out := make([]byte, r.used)
	if r.used < r.cap {
		// Buffer not yet full; data lives at [0, used).
		copy(out, r.data[:r.used])
	} else {
		// Buffer is full; oldest byte is at head.
		n := copy(out, r.data[r.head:])
		copy(out[n:], r.data[:r.head])
	}
	return out
}
