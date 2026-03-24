package dispatcher

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/obot-platform/discobot/server/internal/config"
	"github.com/obot-platform/discobot/server/internal/database"
	"github.com/obot-platform/discobot/server/internal/jobs"
	"github.com/obot-platform/discobot/server/internal/model"
	"github.com/obot-platform/discobot/server/internal/store"
)

type dispatcherTestEnv struct {
	cfg   *config.Config
	store *store.Store
	close func()
}

func newDispatcherTestEnv(t *testing.T) *dispatcherTestEnv {
	t.Helper()

	cfg := &config.Config{
		DatabaseDSN:                  fmt.Sprintf("sqlite3://%s/test.db", t.TempDir()),
		DatabaseDriver:               "sqlite",
		DispatcherPollInterval:       10 * time.Millisecond,
		DispatcherHeartbeatInterval:  10 * time.Millisecond,
		DispatcherHeartbeatTimeout:   100 * time.Millisecond,
		DispatcherJobTimeout:         5 * time.Second,
		DispatcherStaleJobTimeout:    1 * time.Minute,
		DispatcherImmediateExecution: true,
		JobRetryBackoff:              5 * time.Millisecond,
		JobMaxAttempts:               1,
	}

	db, err := database.New(cfg)
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	return &dispatcherTestEnv{
		cfg:   cfg,
		store: store.New(db.DB, db.ReadDB),
		close: func() {
			_ = db.Close()
		},
	}
}

type blockingExecutor struct {
	jobType jobs.JobType
	started chan string
	release chan struct{}
}

func (e *blockingExecutor) Type() jobs.JobType {
	return e.jobType
}

func (e *blockingExecutor) Execute(ctx context.Context, job *model.Job) error {
	select {
	case e.started <- job.ID:
	case <-ctx.Done():
		return ctx.Err()
	}

	select {
	case <-e.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestDrainAndStopWaitsForRunningJobsWithoutStartingNewOnes(t *testing.T) {
	env := newDispatcherTestEnv(t)
	defer env.close()

	disp := NewService(env.store, env.cfg, nil)
	executor := &blockingExecutor{
		jobType: jobs.JobType("test_job"),
		started: make(chan string, 2),
		release: make(chan struct{}),
	}
	disp.RegisterExecutor(executor)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	disp.Start(ctx)

	firstJob := &model.Job{
		Type:        string(executor.jobType),
		Payload:     []byte(`{}`),
		Status:      string(model.JobStatusPending),
		Priority:    10,
		MaxAttempts: 1,
	}
	if err := env.store.CreateJob(context.Background(), firstJob); err != nil {
		t.Fatalf("failed to create first job: %v", err)
	}

	disp.processAvailableJobs()

	select {
	case startedJobID := <-executor.started:
		if startedJobID != firstJob.ID {
			t.Fatalf("expected first job %s to start, got %s", firstJob.ID, startedJobID)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first job to start")
	}

	disp.BeginDrain()

	secondJob := &model.Job{
		Type:        string(executor.jobType),
		Payload:     []byte(`{}`),
		Status:      string(model.JobStatusPending),
		Priority:    10,
		MaxAttempts: 1,
	}
	if err := env.store.CreateJob(context.Background(), secondJob); err != nil {
		t.Fatalf("failed to create second job: %v", err)
	}

	disp.processAvailableJobs()

	select {
	case startedJobID := <-executor.started:
		t.Fatalf("expected drain mode to block new jobs, but job %s started", startedJobID)
	case <-time.After(100 * time.Millisecond):
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer shutdownCancel()
	shutdownDone := make(chan error, 1)
	go func() {
		shutdownDone <- disp.DrainAndStop(shutdownCtx)
	}()

	select {
	case err := <-shutdownDone:
		t.Fatalf("expected shutdown to wait for running job, returned early: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	close(executor.release)

	select {
	case err := <-shutdownDone:
		if err != nil {
			t.Fatalf("DrainAndStop returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for shutdown to finish")
	}

	storedSecondJob, err := env.store.GetJobByID(context.Background(), secondJob.ID)
	if err != nil {
		t.Fatalf("failed to reload second job: %v", err)
	}
	if storedSecondJob.Status != string(model.JobStatusPending) {
		t.Fatalf("expected second job to remain pending during drain, got %s", storedSecondJob.Status)
	}
}
