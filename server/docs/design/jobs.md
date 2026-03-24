# Jobs Module

This module provides background job processing for async operations like workspace initialization.

## Files

| File | Description |
|------|-------------|
| `internal/jobs/types.go` | Job type definitions |
| `internal/jobs/queue.go` | Job queue operations |
| `internal/jobs/workspace_init.go` | Workspace init executor |
| `internal/jobs/session_init.go` | Session init executor |
| `internal/jobs/session_commit.go` | Session commit executor |
| `internal/dispatcher/dispatcher.go` | Job dispatcher |
| `internal/dispatcher/executor.go` | Executor interface |
| `internal/dispatcher/limits.go` | Concurrency limits |

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                        Job System                                │
│                                                                  │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────────┐  │
│  │   Handler    │───▶│    Queue     │───▶│    Dispatcher    │  │
│  │  (enqueue)   │    │  (database)  │    │    (process)     │  │
│  └──────────────┘    └──────────────┘    └──────────────────┘  │
│                                                │                 │
│                                                ▼                 │
│                                     ┌──────────────────┐        │
│                                     │    Executors     │        │
│                                     │  - WorkspaceInit │        │
│                                     │  - SessionInit   │        │
│                                     │  - SessionCommit │        │
│                                     └──────────────────┘        │
└─────────────────────────────────────────────────────────────────┘
```

## Job Types

```go
const (
    JobTypeWorkspaceInit = "workspace_init"
    JobTypeSessionInit   = "session_init"
    JobTypeSessionCommit = "session_commit"
)

type WorkspaceInitPayload struct {
    WorkspaceID string `json:"workspaceId"`
}

type SessionInitPayload struct {
    SessionID string `json:"sessionId"`
}

type SessionCommitPayload struct {
    SessionID string `json:"sessionId"`
    ProjectID string `json:"projectId"`
}
```

## Job Queue

### Structure

```go
type Queue struct {
    store      *store.Store
    notifyFunc func()  // Called when job enqueued
}

func NewQueue(store *store.Store) *Queue {
    return &Queue{store: store}
}
```

### SetNotifyFunc

```go
func (q *Queue) SetNotifyFunc(fn func()) {
    q.notifyFunc = fn
}
```

### Enqueue

```go
func (q *Queue) Enqueue(ctx context.Context, jobType string, payload any) (*model.Job, error) {
    job, err := q.store.EnqueueJob(ctx, jobType, payload)
    if err != nil {
        return nil, err
    }

    // Notify dispatcher
    if q.notifyFunc != nil {
        go q.notifyFunc()
    }

    return job, nil
}

func (q *Queue) EnqueueWorkspaceInit(ctx context.Context, workspaceID string) (*model.Job, error) {
    return q.Enqueue(ctx, JobTypeWorkspaceInit, WorkspaceInitPayload{
        WorkspaceID: workspaceID,
    })
}

func (q *Queue) EnqueueSessionInit(ctx context.Context, sessionID string) (*model.Job, error) {
    return q.Enqueue(ctx, JobTypeSessionInit, SessionInitPayload{
        SessionID: sessionID,
    })
}
```

## Dispatcher

### Structure

```go
type Dispatcher struct {
    config     *config.Config
    store      *store.Store
    queue      *Queue
    executors  map[string]Executor
    limits     *Limits
    running    map[string]int  // Count per job type
    mu         sync.Mutex
    stopCh     chan struct{}
    notifyCh   chan struct{}
    isLeader   bool
    leaderMu   sync.Mutex
}
```

### Executor Interface

```go
type Executor interface {
    Execute(ctx context.Context, job *model.Job) error
}
```

### Initialize

```go
func NewDispatcher(cfg *config.Config, store *store.Store, queue *Queue) *Dispatcher {
    d := &Dispatcher{
        config:    cfg,
        store:     store,
        queue:     queue,
        executors: make(map[string]Executor),
        limits:    NewLimits(),
        running:   make(map[string]int),
        stopCh:    make(chan struct{}),
        notifyCh:  make(chan struct{}, 1),
    }

    // Wire up notification
    queue.SetNotifyFunc(func() {
        select {
        case d.notifyCh <- struct{}{}:
        default:
        }
    })

    return d
}
```

### Register Executor

```go
func (d *Dispatcher) RegisterExecutor(jobType string, executor Executor) {
    d.executors[jobType] = executor
}
```

### Start

```go
func (d *Dispatcher) Start(ctx context.Context) {
    // Start leader election
    go d.leaderElection(ctx)

    // Main processing loop
    ticker := time.NewTicker(d.config.JobPollInterval)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-d.stopCh:
            return
        case <-ticker.C:
            d.process(ctx)
        case <-d.notifyCh:
            // Immediate processing on notification
            d.process(ctx)
        }
    }
}
```

### Graceful Shutdown

The dispatcher now shuts down in two phases:

1. **Drain mode**: `BeginDrain` / `DrainAndStop` stops claiming new jobs but does **not** cancel the contexts of jobs that are already running.
2. **Final stop**: once the in-flight jobs finish (or the shutdown deadline expires), the dispatcher cancels its background loops, releases leadership, and lets the rest of the server tear down.

This allows long-running commit/rebase jobs to finish cleanly during a dev-server restart while preventing any new background work from starting during shutdown.

### Leader Election

```go
func (d *Dispatcher) leaderElection(ctx context.Context) {
    ticker := time.NewTicker(d.config.HeartbeatInterval)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            d.tryAcquireLeadership(ctx)
        }
    }
}

func (d *Dispatcher) tryAcquireLeadership(ctx context.Context) {
    d.leaderMu.Lock()
    defer d.leaderMu.Unlock()

    // Try to become leader or renew lease
    acquired, err := d.store.TryAcquireLeader(ctx, d.config.LeaderLeaseDuration)
    if err != nil {
        log.Printf("Leader election error: %v", err)
        return
    }

    d.isLeader = acquired
}
```

### Process Jobs

```go
func (d *Dispatcher) process(ctx context.Context) {
    if !d.isLeader {
        return
    }

    // Get available job types based on limits
    jobTypes := d.getAvailableJobTypes()
    if len(jobTypes) == 0 {
        return
    }

    // Dequeue job
    job, err := d.store.DequeueJob(ctx, jobTypes)
    if err != nil {
        return
    }

    // Execute in goroutine
    go d.execute(ctx, job)
}

func (d *Dispatcher) getAvailableJobTypes() []string {
    d.mu.Lock()
    defer d.mu.Unlock()

    var types []string
    for jobType, limit := range d.limits.Limits {
        if d.running[jobType] < limit {
            types = append(types, jobType)
        }
    }
    return types
}
```

### Execute Job

```go
func (d *Dispatcher) execute(ctx context.Context, job *model.Job) {
    d.mu.Lock()
    d.running[job.Type]++
    d.mu.Unlock()

    defer func() {
        d.mu.Lock()
        d.running[job.Type]--
        d.mu.Unlock()
    }()

    executor, ok := d.executors[job.Type]
    if !ok {
        d.store.CompleteJob(ctx, job.ID, fmt.Errorf("unknown job type: %s", job.Type))
        return
    }

    err := executor.Execute(ctx, job)
    d.store.CompleteJob(ctx, job.ID, err)
}
```

## Concurrency Limits

```go
type Limits struct {
    Limits map[string]int
}

func NewLimits() *Limits {
    return &Limits{
        Limits: map[string]int{
            JobTypeWorkspaceInit: 5,
            JobTypeSessionInit:   10,
        },
    }
}
```

## Job Executors

### WorkspaceInitExecutor

```go
type WorkspaceInitExecutor struct {
    workspaceService *service.WorkspaceService
}

func (e *WorkspaceInitExecutor) Execute(ctx context.Context, job *model.Job) error {
    var payload WorkspaceInitPayload
    if err := json.Unmarshal(job.Payload, &payload); err != nil {
        return err
    }

    return e.workspaceService.Initialize(ctx, payload.WorkspaceID)
}
```

### SessionInitExecutor

```go
type SessionInitExecutor struct {
    sessionService *service.SessionService
}

func (e *SessionInitExecutor) Execute(ctx context.Context, job *model.Job) error {
    var payload SessionInitPayload
    if err := json.Unmarshal(job.Payload, &payload); err != nil {
        return err
    }

    return e.sessionService.Initialize(ctx, payload.SessionID)
}
```

### SessionCommitExecutor

```go
type SessionCommitExecutor struct {
    sessionService *service.SessionService
}

func (e *SessionCommitExecutor) Execute(ctx context.Context, job *model.Job) error {
    var payload SessionCommitPayload
    if err := json.Unmarshal(job.Payload, &payload); err != nil {
        return err
    }

    return e.sessionService.PerformCommit(ctx, payload.ProjectID, payload.SessionID)
}
```

**Sandbox Reconciliation**: The commit executor automatically handles sandbox unavailability. If the sandbox is not running when commit operations are attempted (checking for patches, sending commit prompt, fetching patches), the system will:
1. Detect the unavailability error
2. Update session status to `reinitializing`
3. Start the sandbox via `Initialize()`
4. Retry the operation

This ensures commits succeed even if the sandbox was stopped or deleted between sessions. Only if the sandbox fails to start will the commit job fail.

## Job Lifecycle

```
┌───────────┐
│  pending  │ ← Job created
└───────────┘
      │
      │ Dequeued
      ▼
┌───────────┐
│ processing│ ← Being executed
└───────────┘
      │
      │ Complete/Fail
      ▼
┌───────────────────┐
│ completed / failed│
└───────────────────┘
```

## Stale Job Handling

Jobs stuck in processing are marked as failed:

```go
func (d *Dispatcher) cleanupStaleJobs(ctx context.Context) {
    cutoff := time.Now().Add(-d.config.JobTimeout)
    d.store.FailStaleJobs(ctx, cutoff)
}
```

## Configuration

```go
type Config struct {
    JobPollInterval     time.Duration // How often to poll for jobs
    HeartbeatInterval   time.Duration // Leader heartbeat interval
    LeaderLeaseDuration time.Duration // How long leader lease lasts
    JobTimeout          time.Duration // Max job execution time
}
```

## Testing

```go
func TestDispatcher_Process(t *testing.T) {
    store := store.NewMock()
    queue := NewQueue(store)
    dispatcher := NewDispatcher(&config.Config{}, store, queue)

    // Register mock executor
    executed := make(chan string, 1)
    dispatcher.RegisterExecutor(JobTypeWorkspaceInit, &mockExecutor{
        fn: func(job *model.Job) error {
            executed <- job.ID
            return nil
        },
    })

    // Force leader status
    dispatcher.isLeader = true

    // Enqueue job
    job, _ := queue.EnqueueWorkspaceInit(context.Background(), "workspace-1")

    // Process
    ctx, cancel := context.WithTimeout(context.Background(), time.Second)
    defer cancel()

    go dispatcher.Start(ctx)

    select {
    case id := <-executed:
        assert.Equal(t, job.ID, id)
    case <-ctx.Done():
        t.Fatal("Job not executed")
    }
}
```
