package service

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/obot-platform/discobot/server/internal/conntrack"
	"github.com/obot-platform/discobot/server/internal/model"
	"github.com/obot-platform/discobot/server/internal/store"
)

// SandboxIdleMonitor monitors sessions for inactivity and automatically stops
// idle sandboxes after a configured timeout period.
type SandboxIdleMonitor struct {
	store         *store.Store
	sandboxSvc    *SandboxService
	sessionSvc    *SessionService
	connTracker   *conntrack.Tracker
	logger        *slog.Logger
	idleTimeout   time.Duration
	checkInterval time.Duration

	mu           sync.Mutex
	running      bool
	stopChan     chan struct{}
	wg           sync.WaitGroup
	shutdownOnce sync.Once
}

// NewSandboxIdleMonitor creates a new sandbox idle monitor.
// connTracker, if non-nil, is consulted before stopping a session: a session
// with active SSH or service-proxy connections will not be stopped even if it
// has been idle longer than idleTimeout.
func NewSandboxIdleMonitor(
	store *store.Store,
	sandboxSvc *SandboxService,
	sessionSvc *SessionService,
	connTracker *conntrack.Tracker,
	logger *slog.Logger,
	idleTimeout time.Duration,
	checkInterval time.Duration,
) *SandboxIdleMonitor {
	return &SandboxIdleMonitor{
		store:         store,
		sandboxSvc:    sandboxSvc,
		sessionSvc:    sessionSvc,
		connTracker:   connTracker,
		logger:        logger.With("component", "sandbox_idle_monitor"),
		idleTimeout:   idleTimeout,
		checkInterval: checkInterval,
		stopChan:      make(chan struct{}),
	}
}

// Start begins the idle monitoring loop.
func (m *SandboxIdleMonitor) Start(ctx context.Context) {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return
	}
	m.running = true
	m.mu.Unlock()

	m.wg.Add(1)
	go m.monitorLoop(ctx)

	m.logger.Info("sandbox idle monitor started",
		"idle_timeout", m.idleTimeout,
		"check_interval", m.checkInterval)
}

// Shutdown gracefully stops the idle monitor.
func (m *SandboxIdleMonitor) Shutdown(ctx context.Context) error {
	var err error
	m.shutdownOnce.Do(func() {
		m.logger.Info("shutting down sandbox idle monitor")
		close(m.stopChan)

		// Wait for goroutine to finish with timeout
		done := make(chan struct{})
		go func() {
			m.wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			m.logger.Info("sandbox idle monitor shutdown complete")
		case <-ctx.Done():
			err = fmt.Errorf("shutdown timeout exceeded")
			m.logger.Error("sandbox idle monitor shutdown timeout")
		}
	})
	return err
}

// monitorLoop is the main loop that periodically checks for idle sessions.
func (m *SandboxIdleMonitor) monitorLoop(ctx context.Context) {
	defer m.wg.Done()

	ticker := time.NewTicker(m.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.logger.Info("monitor loop stopped: context cancelled")
			return
		case <-m.stopChan:
			m.logger.Info("monitor loop stopped: shutdown signal")
			return
		case <-ticker.C:
			if err := m.checkIdleSessions(ctx); err != nil {
				m.logger.Error("error checking idle sessions", "error", err)
			}
		}
	}
}

// checkIdleSessions checks all active sessions and stops those that are idle.
func (m *SandboxIdleMonitor) checkIdleSessions(ctx context.Context) error {
	// Get all ready sessions that could potentially be idle.
	statuses := []string{model.SessionStatusReady, legacySessionStatusRunning}
	sessions, err := m.store.ListSessionsByStatuses(ctx, statuses)
	if err != nil {
		return fmt.Errorf("failed to list active sessions: %w", err)
	}

	if len(sessions) == 0 {
		return nil
	}

	m.logger.Debug("checking sessions for idle timeout", "count", len(sessions))

	stoppedCount := 0
	for _, session := range sessions {
		// Get last activity time from in-memory tracking
		lastActivity := m.sandboxSvc.GetLastActivity(session.ID)

		// If no activity recorded, use session's updated_at as fallback
		if lastActivity.IsZero() {
			lastActivity = session.UpdatedAt
		}

		// Check if session has been idle too long
		idleDuration := time.Since(lastActivity)
		if idleDuration > m.idleTimeout {
			if m.shouldStopSession(ctx, session, lastActivity) {
				stoppedCount++
			}
		}
	}

	if stoppedCount > 0 {
		m.logger.Info("stopped idle sessions", "count", stoppedCount)
	}

	return nil
}

// shouldStopSession determines if a session should be stopped and stops it if so.
// Returns true if the session was stopped, false otherwise.
func (m *SandboxIdleMonitor) shouldStopSession(ctx context.Context, session *model.Session, lastActivity time.Time) bool {
	logger := m.logger.With("session_id", session.ID, "project_id", session.ProjectID)

	// Don't stop if there are active SSH or service-proxy connections.
	if m.connTracker != nil && m.connTracker.HasActiveConnections(session.ID) {
		logger.Debug("session idle but has active connections, skipping stop",
			"idle_duration", time.Since(lastActivity),
			"active_connections", m.connTracker.ActiveCount(session.ID))
		return false
	}

	// Check if a completion is currently running - don't stop if so.
	client, err := m.sandboxSvc.GetClient(ctx, session.ID)
	if err != nil {
		logger.Warn("failed to get sandbox client for idle check", "error", err)
		return false
	}

	chatStatus, err := client.GetChatStatus(ctx)
	if err != nil {
		logger.Warn("failed to get chat status for idle check", "error", err)
		return false
	}

	if chatStatus.IsRunning {
		logger.Debug("session idle but completion in progress, skipping stop",
			"completion_id", chatStatus.CompletionID,
			"idle_duration", time.Since(lastActivity))
		return false
	}

	// Stop the sandbox
	logger.Info("stopping idle session",
		"last_activity", lastActivity,
		"idle_duration", time.Since(lastActivity))

	if err := m.sandboxSvc.StopForSession(ctx, session.ID); err != nil {
		logger.Error("failed to stop idle sandbox", "error", err)
		return false
	}

	if _, err := m.sessionSvc.UpdateStatus(ctx, session.ProjectID, session.ID, model.SessionStatusStopped, nil); err != nil {
		logger.Error("failed to update session status", "error", err)
		return false
	}

	return true
}
