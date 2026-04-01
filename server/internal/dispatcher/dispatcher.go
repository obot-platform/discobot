package dispatcher

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/obot-platform/discobot/server/internal/config"
	"github.com/obot-platform/discobot/server/internal/events"
	"github.com/obot-platform/discobot/server/internal/jobs"
	"github.com/obot-platform/discobot/server/internal/model"
	"github.com/obot-platform/discobot/server/internal/store"
)

// Service manages job processing with leader election.
type Service struct {
	store       *store.Store
	cfg         *config.Config
	serverID    string
	eventBroker *events.Broker
	singleNode  bool // true when using SQLite (single-host mode)

	// Registered executors by job type
	executors map[jobs.JobType]JobExecutor

	// Concurrency tracking per job type
	runningJobs   map[jobs.JobType]int
	runningJobsMu sync.Mutex

	// Leadership state
	isLeader   bool
	isLeaderMu sync.RWMutex

	// Notification channel for immediate job execution
	// When a job is enqueued, send to this channel to wake up the processor
	notifyCh chan struct{}

	// Drain state
	draining   bool
	drainingMu sync.RWMutex

	// Lifecycle management
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	jobWg  sync.WaitGroup
}

// NewService creates a new dispatcher service.
func NewService(s *store.Store, cfg *config.Config, eventBroker *events.Broker) *Service {
	return &Service{
		store:       s,
		cfg:         cfg,
		serverID:    uuid.New().String(),
		eventBroker: eventBroker,
		singleNode:  cfg.DatabaseDriver == "sqlite",
		executors:   make(map[jobs.JobType]JobExecutor),
		runningJobs: make(map[jobs.JobType]int),
		notifyCh:    make(chan struct{}, 100), // Buffered to avoid blocking enqueuers
	}
}

// RegisterExecutor registers an executor for a job type.
func (d *Service) RegisterExecutor(executor JobExecutor) {
	d.executors[executor.Type()] = executor
}

// ServerID returns this server's unique ID.
func (d *Service) ServerID() string {
	return d.serverID
}

// IsLeader returns whether this server is currently the leader.
func (d *Service) IsLeader() bool {
	d.isLeaderMu.RLock()
	defer d.isLeaderMu.RUnlock()
	return d.isLeader
}

// NotifyNewJob notifies the dispatcher that a new job was enqueued.
// This triggers immediate processing if enabled in config.
func (d *Service) NotifyNewJob() {
	if !d.cfg.DispatcherImmediateExecution {
		return
	}
	// Non-blocking send - if channel is full, poll will pick it up
	select {
	case d.notifyCh <- struct{}{}:
	default:
	}
}

// IsDraining returns whether the dispatcher is draining and refusing to start
// new jobs.
func (d *Service) IsDraining() bool {
	d.drainingMu.RLock()
	defer d.drainingMu.RUnlock()
	return d.draining
}

// BeginDrain stops the dispatcher from claiming any new jobs while allowing
// in-flight jobs to keep running.
func (d *Service) BeginDrain() {
	d.drainingMu.Lock()
	defer d.drainingMu.Unlock()
	if d.draining {
		return
	}
	d.draining = true
	log.Println("Dispatcher entering drain mode")
}

// Start begins the dispatcher service.
func (d *Service) Start(parentCtx context.Context) {
	d.ctx, d.cancel = context.WithCancel(parentCtx)

	log.Printf("Dispatcher starting with server ID: %s", d.serverID)
	if d.cfg.DisableSessionSandboxDeleteJobs {
		log.Println("DISABLE_SESSION_SANDBOX_DELETE_JOBS is set: session_sandbox_delete jobs will not be executed")
	}

	if d.singleNode {
		// SQLite is single-host: become leader immediately and clean up
		// any stale jobs left over from a previous process.
		d.isLeaderMu.Lock()
		d.isLeader = true
		d.isLeaderMu.Unlock()
		log.Printf("Single-node mode (SQLite): immediately became leader (server: %s)", d.serverID)

		count, err := d.store.CleanupStaleJobs(d.ctx, 0)
		if err != nil {
			log.Printf("Stale job cleanup on startup error: %v", err)
		} else if count > 0 {
			log.Printf("Reset %d stale jobs on startup", count)
		}
	} else {
		// Multi-node: start leader election loop
		d.wg.Add(1)
		go d.leaderElectionLoop()
	}

	// Start job processing loop
	d.wg.Add(1)
	go d.jobProcessingLoop()

	// Start stale job cleanup loop
	d.wg.Add(1)
	go d.staleJobCleanupLoop()
}

// Stop gracefully stops the dispatcher.
func (d *Service) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := d.DrainAndStop(ctx); err != nil {
		log.Printf("Dispatcher stop warning: %v", err)
	}
}

// DrainAndStop drains in-flight jobs, then stops dispatcher background loops.
func (d *Service) DrainAndStop(ctx context.Context) error {
	log.Println("Dispatcher stopping...")
	d.BeginDrain()

	var retErr error
	if err := d.waitForInFlightJobs(ctx); err != nil {
		retErr = err
	}

	if d.cancel != nil {
		d.cancel()
	}

	if err := d.waitForLoops(ctx); err != nil && retErr == nil {
		retErr = err
	}

	// Release leadership (skip for single-node since we never wrote a DB row)
	if !d.singleNode && d.IsLeader() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := d.store.ReleaseLeadership(releaseCtx, d.serverID); err != nil {
			if retErr == nil {
				retErr = err
			}
			log.Printf("Failed to release leadership: %v", err)
		} else {
			log.Println("Leadership released")
		}
	}

	return retErr
}

func (d *Service) waitForInFlightJobs(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		d.jobWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Println("All in-flight dispatcher jobs completed")
		return nil
	case <-ctx.Done():
		log.Println("Timeout waiting for in-flight dispatcher jobs")
		return ctx.Err()
	}
}

func (d *Service) waitForLoops(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		d.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Println("All dispatcher goroutines stopped")
		return nil
	case <-ctx.Done():
		log.Println("Timeout waiting for dispatcher goroutines")
		return ctx.Err()
	}
}

// leaderElectionLoop continuously tries to acquire/maintain leadership.
func (d *Service) leaderElectionLoop() {
	defer d.wg.Done()

	// Try to acquire leadership immediately on start
	d.tryAcquireLeadership()

	ticker := time.NewTicker(d.cfg.DispatcherHeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			d.tryAcquireLeadership()
		}
	}
}

// tryAcquireLeadership attempts to acquire or maintain leadership.
func (d *Service) tryAcquireLeadership() {
	acquired, err := d.store.TryAcquireLeadership(
		d.ctx,
		d.serverID,
		d.cfg.DispatcherHeartbeatTimeout,
	)
	if err != nil {
		log.Printf("Leader election error: %v", err)
		// On error, we can't confirm we own the lock, so stop acting as leader
		d.isLeaderMu.Lock()
		wasLeader := d.isLeader
		d.isLeader = false
		d.isLeaderMu.Unlock()
		if wasLeader {
			log.Printf("Relinquished leadership due to error (server: %s)", d.serverID)
		}
		return
	}

	d.isLeaderMu.Lock()
	wasLeader := d.isLeader
	d.isLeader = acquired
	d.isLeaderMu.Unlock()

	if acquired && !wasLeader {
		log.Printf("Became leader (server: %s)", d.serverID)
	} else if !acquired && wasLeader {
		log.Printf("Lost leadership (server: %s)", d.serverID)
	}
}

// jobProcessingLoop polls for and processes jobs.
func (d *Service) jobProcessingLoop() {
	defer d.wg.Done()

	ticker := time.NewTicker(d.cfg.DispatcherPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			d.processAvailableJobs()
		case <-d.notifyCh:
			// Immediate execution notification - try to process right away
			d.processAvailableJobs()
		}
	}
}

// processAvailableJobs attempts to claim and process jobs.
// Uses a single query to fetch any available job from types with capacity.
func (d *Service) processAvailableJobs() {
	if !d.IsLeader() {
		return
	}

	// Keep processing while there are jobs and capacity
	for {
		if d.IsDraining() {
			return
		}

		// Get job types that have available capacity
		availableTypes := d.getAvailableJobTypes()
		if len(availableTypes) == 0 {
			return // No capacity for any job type
		}

		// Try to claim any job of the available types (single query)
		job, err := d.store.ClaimJobOfTypes(d.ctx, availableTypes, d.serverID)
		if err != nil {
			log.Printf("Failed to claim job: %v", err)
			return
		}

		if job == nil {
			return // No jobs available
		}

		jobType := jobs.JobType(job.Type)

		// Increment running count for this job type
		d.runningJobsMu.Lock()
		d.runningJobs[jobType]++
		d.runningJobsMu.Unlock()

		// Process job in goroutine
		d.wg.Add(1)
		d.jobWg.Add(1)
		go func(j *model.Job, jt jobs.JobType) {
			defer d.wg.Done()
			defer d.jobWg.Done()
			defer d.decrementRunning(jt)
			d.executeJob(j)
		}(job, jobType)
	}
}

// getAvailableJobTypes returns job types that have capacity for more jobs.
func (d *Service) getAvailableJobTypes() []string {
	d.runningJobsMu.Lock()
	defer d.runningJobsMu.Unlock()

	var available []string
	for jobType := range d.executors {
		if d.cfg.DisableSessionSandboxDeleteJobs && jobType == jobs.JobTypeSessionSandboxDelete {
			continue
		}
		running := d.runningJobs[jobType]
		limit := GetConcurrencyLimit(jobType)
		if running < limit {
			available = append(available, string(jobType))
		}
	}
	return available
}

// executeJob processes a single job.
func (d *Service) executeJob(job *model.Job) {
	log.Printf("Processing job %s (type: %s)", job.ID, job.Type)

	executor, ok := d.executors[jobs.JobType(job.Type)]
	if !ok {
		errMsg := "no executor registered for job type"
		log.Printf("Job %s failed: %s", job.ID, errMsg)
		if err := d.store.FailJob(d.ctx, job.ID, errMsg, d.cfg.JobRetryBackoff); err != nil {
			log.Printf("Failed to mark job %s as failed: %v", job.ID, err)
		}
		return
	}

	// Execute with timeout
	ctx, cancel := context.WithTimeout(d.ctx, d.cfg.DispatcherJobTimeout)
	defer cancel()

	err := executor.Execute(ctx, job)
	if err != nil {
		log.Printf("Job %s failed: %v", job.ID, err)
		if err := d.store.FailJob(d.ctx, job.ID, err.Error(), d.cfg.JobRetryBackoff); err != nil {
			log.Printf("Failed to mark job %s as failed: %v", job.ID, err)
		}
		// Publish job completion event (failure)
		d.publishJobCompletionEvent(job, "failed", err.Error())
		return
	}

	log.Printf("Job %s completed successfully", job.ID)
	if err := d.store.CompleteJob(d.ctx, job.ID); err != nil {
		log.Printf("Failed to mark job %s as completed: %v", job.ID, err)
	}
	// Publish job completion event (success)
	d.publishJobCompletionEvent(job, "completed", "")
}

// decrementRunning decrements the running job count for a type.
func (d *Service) decrementRunning(jobType jobs.JobType) {
	d.runningJobsMu.Lock()
	d.runningJobs[jobType]--
	d.runningJobsMu.Unlock()
}

// staleJobCleanupLoop periodically cleans up stale running jobs.
func (d *Service) staleJobCleanupLoop() {
	defer d.wg.Done()

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			if !d.IsLeader() {
				continue
			}

			count, err := d.store.CleanupStaleJobs(d.ctx, d.cfg.DispatcherStaleJobTimeout)
			if err != nil {
				log.Printf("Stale job cleanup error: %v", err)
			} else if count > 0 {
				log.Printf("Reset %d stale jobs", count)
			}
		}
	}
}

// publishJobCompletionEvent publishes a job completion event to the event broker.
func (d *Service) publishJobCompletionEvent(job *model.Job, status, errorMsg string) {
	if d.eventBroker == nil {
		return
	}

	// Extract resource info
	resourceType := ""
	resourceID := ""
	if job.ResourceType != nil {
		resourceType = *job.ResourceType
	}
	if job.ResourceID != nil {
		resourceID = *job.ResourceID
	}

	// Extract project ID from job payload
	projectID := d.extractProjectIDFromJob(job)
	if projectID == "" {
		log.Printf("Warning: could not extract projectId from job %s, skipping event publish", job.ID)
		return
	}

	if err := d.eventBroker.PublishJobCompleted(
		d.ctx,
		projectID,
		job.ID,
		job.Type,
		resourceType,
		resourceID,
		status,
		errorMsg,
	); err != nil {
		log.Printf("Failed to publish job completion event for job %s: %v", job.ID, err)
	}
}

// extractProjectIDFromJob extracts the projectId from the job payload.
// Returns empty string if projectId cannot be found.
func (d *Service) extractProjectIDFromJob(job *model.Job) string {
	var payload map[string]interface{}
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return ""
	}

	if projectID, ok := payload["projectId"].(string); ok {
		return projectID
	}

	return ""
}
