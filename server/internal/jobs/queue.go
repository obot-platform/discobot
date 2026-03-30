package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/obot-platform/discobot/server/internal/config"
	"github.com/obot-platform/discobot/server/internal/model"
	"github.com/obot-platform/discobot/server/internal/store"
)

// Queue provides helper methods for enqueueing jobs.
type Queue struct {
	store      *store.Store
	cfg        *config.Config
	notifyFunc func() // Called after job creation to notify dispatcher
}

// NewQueue creates a new job queue helper.
func NewQueue(s *store.Store, cfg *config.Config) *Queue {
	return &Queue{store: s, cfg: cfg}
}

// SetNotifyFunc sets the function to call after job creation.
// This is typically dispatcher.NotifyNewJob.
func (q *Queue) SetNotifyFunc(f func()) {
	q.notifyFunc = f
}

// notify calls the notify function if set.
func (q *Queue) notify() {
	if q.notifyFunc != nil {
		q.notifyFunc()
	}
}

// Resource type constants for job deduplication.
const (
	ResourceTypeSession         = "session"
	ResourceTypeRetainedSandbox = "retained_sandbox"
	ResourceTypeWorkspace       = "workspace"
)

// Schedulable is an optional interface payloads can implement to request
// execution at a specific time in the future.
type Schedulable interface {
	ScheduledAt() time.Time
}

// ErrJobAlreadyExists is returned when a job for the resource already exists.
var ErrJobAlreadyExists = errors.New("job already exists for resource")

// Enqueue enqueues a job from the given payload.
// The payload determines the job type, resource key for deduplication,
// and optionally the priority and max attempts.
// Returns ErrJobAlreadyExists if a pending/running job for this resource already exists,
// unless the payload implements DuplicateAllower and returns true.
func (q *Queue) Enqueue(ctx context.Context, payload JobPayload) error {
	resType, resID := payload.ResourceKey()

	// Check for duplicate jobs unless the payload explicitly allows them
	allowDuplicates := false
	if d, ok := payload.(DuplicateAllower); ok {
		allowDuplicates = d.AllowDuplicates()
	}
	if !allowDuplicates {
		exists, err := q.store.HasActiveJobForResource(ctx, resType, resID)
		if err != nil {
			return err
		}
		if exists {
			return ErrJobAlreadyExists
		}
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	priority := 10 // default
	if p, ok := payload.(Prioritized); ok {
		priority = p.Priority()
	}

	maxAttempts := q.cfg.JobMaxAttempts
	if m, ok := payload.(MaxAttempter); ok {
		maxAttempts = m.MaxAttempts()
	}

	scheduledAt := time.Now()
	if s, ok := payload.(Schedulable); ok {
		if requested := s.ScheduledAt(); !requested.IsZero() {
			scheduledAt = requested
		}
	}

	job := &model.Job{
		Type:         string(payload.JobType()),
		Payload:      data,
		Status:       string(model.JobStatusPending),
		MaxAttempts:  maxAttempts,
		Priority:     priority,
		ScheduledAt:  scheduledAt,
		ResourceType: &resType,
		ResourceID:   &resID,
	}

	if err := q.store.CreateJob(ctx, job); err != nil {
		return err
	}
	q.notify()
	return nil
}
