// Package store provides database operations using GORM.
package store

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/obot-platform/discobot/server/internal/model"
)

// Common errors
var (
	ErrNotFound = errors.New("record not found")
)

// Store wraps GORM DB for database operations.
// For SQLite, writeDB and readDB point to separate connection pools;
// for PostgreSQL (or in-memory tests), they point to the same instance.
type Store struct {
	writeDB *gorm.DB
	readDB  *gorm.DB
}

// New creates a new Store with separate write and read GORM DBs.
// If readDB is nil, the writeDB is used for both reads and writes.
func New(writeDB, readDB *gorm.DB) *Store {
	if readDB == nil {
		readDB = writeDB
	}
	return &Store{writeDB: writeDB, readDB: readDB}
}

// DB returns the underlying write GORM DB for advanced queries.
func (s *Store) DB() *gorm.DB {
	return s.writeDB
}

// --- Users ---

func (s *Store) GetUserByID(ctx context.Context, id string) (*model.User, error) {
	var user model.User
	if err := s.readDB.WithContext(ctx).First(&user, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &user, nil
}

func (s *Store) GetUserByProviderID(ctx context.Context, provider, providerID string) (*model.User, error) {
	var user model.User
	if err := s.readDB.WithContext(ctx).First(&user, "provider = ? AND provider_id = ?", provider, providerID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &user, nil
}

func (s *Store) CreateUser(ctx context.Context, user *model.User) error {
	return s.writeDB.WithContext(ctx).Create(user).Error
}

func (s *Store) UpdateUser(ctx context.Context, user *model.User) error {
	return s.writeDB.WithContext(ctx).Save(user).Error
}

// --- User Sessions ---

func (s *Store) CreateUserSession(ctx context.Context, session *model.UserSession) error {
	return s.writeDB.WithContext(ctx).Create(session).Error
}

func (s *Store) GetUserSessionByToken(ctx context.Context, tokenHash string) (*model.UserSession, error) {
	var session model.UserSession
	if err := s.readDB.WithContext(ctx).Preload("User").First(&session, "token_hash = ?", tokenHash).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &session, nil
}

func (s *Store) DeleteUserSession(ctx context.Context, tokenHash string) error {
	return s.writeDB.WithContext(ctx).Delete(&model.UserSession{}, "token_hash = ?", tokenHash).Error
}

func (s *Store) DeleteExpiredUserSessions(ctx context.Context) error {
	return s.writeDB.WithContext(ctx).Delete(&model.UserSession{}, "expires_at < ?", time.Now()).Error
}

// --- Projects ---

func (s *Store) GetProjectByID(ctx context.Context, id string) (*model.Project, error) {
	var project model.Project
	if err := s.readDB.WithContext(ctx).First(&project, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &project, nil
}

func (s *Store) ListProjectsByUser(ctx context.Context, userID string) ([]*model.Project, error) {
	var projects []*model.Project
	err := s.readDB.WithContext(ctx).
		Joins("JOIN project_members ON project_members.project_id = projects.id").
		Where("project_members.user_id = ?", userID).
		Find(&projects).Error
	return projects, err
}

func (s *Store) CreateProject(ctx context.Context, project *model.Project) error {
	return s.writeDB.WithContext(ctx).Create(project).Error
}

func (s *Store) UpdateProject(ctx context.Context, project *model.Project) error {
	return s.writeDB.WithContext(ctx).Save(project).Error
}

func (s *Store) DeleteProject(ctx context.Context, id string) error {
	return s.writeDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Delete all related records explicitly (no cascade in schema)
		// Order matters due to foreign key relationships

		// Get all workspaces to delete their sessions
		var workspaces []*model.Workspace
		if err := tx.Where("project_id = ?", id).Find(&workspaces).Error; err != nil {
			return err
		}
		for _, ws := range workspaces {
			// Delete messages and terminal history for each session
			if err := tx.Where("session_id IN (SELECT id FROM sessions WHERE workspace_id = ?)", ws.ID).Delete(&model.Message{}).Error; err != nil {
				return err
			}
			if err := tx.Where("session_id IN (SELECT id FROM sessions WHERE workspace_id = ?)", ws.ID).Delete(&model.TerminalHistory{}).Error; err != nil {
				return err
			}
			// Delete sessions
			if err := tx.Where("workspace_id = ?", ws.ID).Delete(&model.Session{}).Error; err != nil {
				return err
			}
		}

		// Delete workspaces
		if err := tx.Where("project_id = ?", id).Delete(&model.Workspace{}).Error; err != nil {
			return err
		}

		// Delete invitations
		if err := tx.Where("project_id = ?", id).Delete(&model.ProjectInvitation{}).Error; err != nil {
			return err
		}

		// Delete credentials
		if err := tx.Where("project_id = ?", id).Delete(&model.Credential{}).Error; err != nil {
			return err
		}

		// Delete members
		if err := tx.Where("project_id = ?", id).Delete(&model.ProjectMember{}).Error; err != nil {
			return err
		}

		// Finally delete the project
		return tx.Delete(&model.Project{}, "id = ?", id).Error
	})
}

// --- Project Members ---

func (s *Store) GetProjectMember(ctx context.Context, projectID, userID string) (*model.ProjectMember, error) {
	var member model.ProjectMember
	if err := s.readDB.WithContext(ctx).First(&member, "project_id = ? AND user_id = ?", projectID, userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &member, nil
}

func (s *Store) ListProjectMembers(ctx context.Context, projectID string) ([]*model.ProjectMember, error) {
	var members []*model.ProjectMember
	err := s.readDB.WithContext(ctx).Preload("User").Where("project_id = ?", projectID).Find(&members).Error
	return members, err
}

func (s *Store) CreateProjectMember(ctx context.Context, member *model.ProjectMember) error {
	return s.writeDB.WithContext(ctx).Create(member).Error
}

func (s *Store) DeleteProjectMember(ctx context.Context, projectID, userID string) error {
	return s.writeDB.WithContext(ctx).Delete(&model.ProjectMember{}, "project_id = ? AND user_id = ?", projectID, userID).Error
}

// --- Project Invitations ---

func (s *Store) GetInvitationByToken(ctx context.Context, token string) (*model.ProjectInvitation, error) {
	var invitation model.ProjectInvitation
	if err := s.readDB.WithContext(ctx).First(&invitation, "token = ?", token).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &invitation, nil
}

func (s *Store) CreateInvitation(ctx context.Context, invitation *model.ProjectInvitation) error {
	return s.writeDB.WithContext(ctx).Create(invitation).Error
}

func (s *Store) DeleteInvitation(ctx context.Context, id string) error {
	return s.writeDB.WithContext(ctx).Delete(&model.ProjectInvitation{}, "id = ?", id).Error
}

// --- Workspaces ---

func (s *Store) GetWorkspaceByID(ctx context.Context, id string) (*model.Workspace, error) {
	var workspace model.Workspace
	if err := s.readDB.WithContext(ctx).First(&workspace, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &workspace, nil
}

func (s *Store) ListWorkspacesByProject(ctx context.Context, projectID string) ([]*model.Workspace, error) {
	var workspaces []*model.Workspace
	err := s.readDB.WithContext(ctx).Where("project_id = ?", projectID).Find(&workspaces).Error
	return workspaces, err
}

func (s *Store) ListWorkspacesByProvider(ctx context.Context, provider string) ([]*model.Workspace, error) {
	var workspaces []*model.Workspace
	err := s.readDB.WithContext(ctx).Where("provider = ?", provider).Find(&workspaces).Error
	return workspaces, err
}

func (s *Store) CreateWorkspace(ctx context.Context, workspace *model.Workspace) error {
	return s.writeDB.WithContext(ctx).Create(workspace).Error
}

func (s *Store) UpdateWorkspace(ctx context.Context, workspace *model.Workspace) error {
	return s.writeDB.WithContext(ctx).Save(workspace).Error
}

func (s *Store) DeleteWorkspace(ctx context.Context, id string) error {
	return s.writeDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Delete messages and terminal history for all sessions in this workspace
		if err := tx.Where("session_id IN (SELECT id FROM sessions WHERE workspace_id = ?)", id).Delete(&model.Message{}).Error; err != nil {
			return err
		}
		if err := tx.Where("session_id IN (SELECT id FROM sessions WHERE workspace_id = ?)", id).Delete(&model.TerminalHistory{}).Error; err != nil {
			return err
		}

		// Delete sessions
		if err := tx.Where("workspace_id = ?", id).Delete(&model.Session{}).Error; err != nil {
			return err
		}

		// Delete the workspace
		return tx.Delete(&model.Workspace{}, "id = ?", id).Error
	})
}

// --- Sessions ---

func (s *Store) GetSessionByID(ctx context.Context, id string) (*model.Session, error) {
	var session model.Session
	if err := s.readDB.WithContext(ctx).First(&session, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &session, nil
}

// ListSessionsByProject returns all sessions for a project.
func (s *Store) ListSessionsByProject(ctx context.Context, projectID string) ([]*model.Session, error) {
	var sessions []*model.Session
	err := s.readDB.WithContext(ctx).Where("project_id = ?", projectID).Find(&sessions).Error
	return sessions, err
}

// ListSessionsByWorkspace returns all sessions for a workspace.
// Deprecated: prefer ListSessionsByProject for project-scoped session listing.
func (s *Store) ListSessionsByWorkspace(ctx context.Context, workspaceID string) ([]*model.Session, error) {
	var sessions []*model.Session
	err := s.readDB.WithContext(ctx).Where("workspace_id = ?", workspaceID).Find(&sessions).Error
	return sessions, err
}

// ListSessionsByStatuses returns all sessions with any of the given statuses.
func (s *Store) ListSessionsByStatuses(ctx context.Context, statuses []string) ([]*model.Session, error) {
	var sessions []*model.Session
	err := s.readDB.WithContext(ctx).Where("status IN ?", statuses).Find(&sessions).Error
	return sessions, err
}

// ListSessionsByCommitStatuses returns sessions with the given commit statuses.
func (s *Store) ListSessionsByCommitStatuses(ctx context.Context, commitStatuses []string) ([]*model.Session, error) {
	var sessions []*model.Session
	err := s.readDB.WithContext(ctx).Where("commit_status IN ?", commitStatuses).Find(&sessions).Error
	return sessions, err
}

// GetSessionsByStatus returns all sessions with the given status.
func (s *Store) GetSessionsByStatus(ctx context.Context, status string) ([]model.Session, error) {
	var sessions []model.Session
	err := s.readDB.WithContext(ctx).Where("status = ?", status).Find(&sessions).Error
	return sessions, err
}

// UpdateSessionStatus updates only the status and error message fields for a session.
func (s *Store) UpdateSessionStatus(ctx context.Context, id, status string, errorMessage *string) error {
	updates := map[string]interface{}{
		"status": status,
	}
	if errorMessage != nil {
		updates["error_message"] = *errorMessage
	} else {
		updates["error_message"] = nil
	}
	return s.writeDB.WithContext(ctx).Model(&model.Session{}).Where("id = ?", id).Updates(updates).Error
}

func (s *Store) CreateSession(ctx context.Context, session *model.Session) error {
	return s.writeDB.WithContext(ctx).Create(session).Error
}

func (s *Store) UpdateSession(ctx context.Context, session *model.Session) error {
	return s.writeDB.WithContext(ctx).Save(session).Error
}

func (s *Store) UpdateSessionSSHKey(ctx context.Context, id string, encryptedData []byte) error {
	return s.writeDB.WithContext(ctx).
		Model(&model.Session{}).
		Where("id = ?", id).
		Update("ssh_key_encrypted_data", encryptedData).Error
}

// UpdateSessionWorkspace updates the workspace path and commit for a session.
func (s *Store) UpdateSessionWorkspace(ctx context.Context, id, workspacePath, workspaceCommit string) error {
	updates := map[string]interface{}{
		"workspace_path": workspacePath,
	}
	if workspaceCommit != "" {
		updates["workspace_commit"] = workspaceCommit
	}
	return s.writeDB.WithContext(ctx).Model(&model.Session{}).Where("id = ?", id).Updates(updates).Error
}

func (s *Store) DeleteSession(ctx context.Context, id string) error {
	return s.writeDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Delete messages
		if err := tx.Where("session_id = ?", id).Delete(&model.Message{}).Error; err != nil {
			return err
		}

		// Delete terminal history
		if err := tx.Where("session_id = ?", id).Delete(&model.TerminalHistory{}).Error; err != nil {
			return err
		}

		// Delete the session
		return tx.Delete(&model.Session{}, "id = ?", id).Error
	})
}

func (s *Store) CreateMessage(ctx context.Context, message *model.Message) error {
	return s.writeDB.WithContext(ctx).Create(message).Error
}

// --- Credentials ---

func (s *Store) GetCredentialByProvider(ctx context.Context, projectID, provider string) (*model.Credential, error) {
	var credential model.Credential
	if err := s.readDB.WithContext(ctx).First(&credential, "project_id = ? AND provider = ?", projectID, provider).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &credential, nil
}

func (s *Store) ListCredentialsByProject(ctx context.Context, projectID string) ([]*model.Credential, error) {
	var credentials []*model.Credential
	err := s.readDB.WithContext(ctx).Where("project_id = ?", projectID).Find(&credentials).Error
	return credentials, err
}

func (s *Store) CreateCredential(ctx context.Context, credential *model.Credential) error {
	return s.writeDB.WithContext(ctx).Create(credential).Error
}

func (s *Store) UpdateCredential(ctx context.Context, credential *model.Credential) error {
	return s.writeDB.WithContext(ctx).Save(credential).Error
}

func (s *Store) DeleteCredential(ctx context.Context, projectID, provider string) error {
	return s.writeDB.WithContext(ctx).Delete(&model.Credential{}, "project_id = ? AND provider = ?", projectID, provider).Error
}

// --- Env Sets ---

func (s *Store) CreateEnvSet(ctx context.Context, envSet *model.EnvSet) error {
	return s.writeDB.WithContext(ctx).Create(envSet).Error
}

func (s *Store) GetEnvSetByID(ctx context.Context, id string) (*model.EnvSet, error) {
	var envSet model.EnvSet
	if err := s.readDB.WithContext(ctx).First(&envSet, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &envSet, nil
}

func (s *Store) ListEnvSetsByProject(ctx context.Context, projectID string) ([]*model.EnvSet, error) {
	var envSets []*model.EnvSet
	err := s.readDB.WithContext(ctx).Where("project_id = ?", projectID).Order("created_at ASC").Find(&envSets).Error
	return envSets, err
}

func (s *Store) UpdateEnvSet(ctx context.Context, envSet *model.EnvSet) error {
	return s.writeDB.WithContext(ctx).Save(envSet).Error
}

func (s *Store) DeleteEnvSet(ctx context.Context, id string) error {
	return s.writeDB.WithContext(ctx).Delete(&model.EnvSet{}, "id = ?", id).Error
}

func (s *Store) UpdateSessionActiveEnvSets(ctx context.Context, sessionID string, envSetIDs []string) error {
	if envSetIDs == nil {
		envSetIDs = []string{}
	}
	return s.writeDB.WithContext(ctx).Model(&model.Session{}).Where("id = ?", sessionID).Select("ActiveEnvSetIDs").Updates(&model.Session{ActiveEnvSetIDs: envSetIDs}).Error
}

// --- Terminal History ---

func (s *Store) ListTerminalHistory(ctx context.Context, sessionID string, limit int) ([]*model.TerminalHistory, error) {
	var history []*model.TerminalHistory
	query := s.readDB.WithContext(ctx).Where("session_id = ?", sessionID).Order("created_at DESC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	err := query.Find(&history).Error
	return history, err
}

func (s *Store) CreateTerminalHistory(ctx context.Context, entry *model.TerminalHistory) error {
	return s.writeDB.WithContext(ctx).Create(entry).Error
}

// --- Jobs ---

// CreateJob creates a new job in the queue.
func (s *Store) CreateJob(ctx context.Context, job *model.Job) error {
	return s.writeDB.WithContext(ctx).Create(job).Error
}

// GetJobByID retrieves a job by its ID.
func (s *Store) GetJobByID(ctx context.Context, id string) (*model.Job, error) {
	var job model.Job
	if err := s.readDB.WithContext(ctx).First(&job, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &job, nil
}

// GetJobByResourceID retrieves the most recent job for a specific resource.
// Returns ErrNotFound if no job exists for the resource.
func (s *Store) GetJobByResourceID(ctx context.Context, resourceType, resourceID string) (*model.Job, error) {
	var job model.Job
	err := s.readDB.WithContext(ctx).
		Where("resource_type = ? AND resource_id = ?", resourceType, resourceID).
		Order("created_at DESC").
		First(&job).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &job, nil
}

// HasActiveJobForResource checks if there's a pending or running job for the given resource.
// Returns true if a job exists that would block enqueueing a new one.
func (s *Store) HasActiveJobForResource(ctx context.Context, resourceType, resourceID string) (bool, error) {
	var count int64
	err := s.readDB.WithContext(ctx).Model(&model.Job{}).
		Where("resource_type = ? AND resource_id = ? AND status IN ?",
			resourceType, resourceID, []string{string(model.JobStatusPending), string(model.JobStatusRunning)}).
		Count(&count).Error
	return count > 0, err
}

// ClaimJob atomically claims a pending job of the given type.
// Returns nil, nil if no job is available.
func (s *Store) ClaimJob(ctx context.Context, jobType string, workerID string) (*model.Job, error) {
	return s.ClaimJobOfTypes(ctx, []string{jobType}, workerID)
}

// ClaimJobOfTypes atomically claims a pending job of any of the given types.
// Jobs are selected by priority (highest first), then by scheduled time (oldest first).
// If a job has resource_type/resource_id set, it will only be claimed if no other job
// for the same resource is currently running.
// Returns nil, nil if no job is available.
func (s *Store) ClaimJobOfTypes(ctx context.Context, jobTypes []string, workerID string) (*model.Job, error) {
	if len(jobTypes) == 0 {
		return nil, nil
	}

	var job model.Job
	var found bool

	err := s.writeDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Find pending jobs of any allowed type that are scheduled to run
		// Order: priority (highest first), scheduled_at (oldest first), created_at (tiebreaker)
		var candidates []model.Job
		query := tx.Where("type IN ? AND status = ? AND scheduled_at <= ?",
			jobTypes, model.JobStatusPending, time.Now()).
			Order("priority DESC, scheduled_at ASC, created_at ASC").
			Limit(10) // Check up to 10 candidates to find one without resource conflicts

		if err := query.Find(&candidates).Error; err != nil {
			return err
		}

		if len(candidates) == 0 {
			return nil // No jobs available
		}

		// Find first candidate without a resource conflict
		for _, candidate := range candidates {
			// If job has no resource tracking, claim it immediately
			if candidate.ResourceType == nil || candidate.ResourceID == nil {
				job = candidate
				found = true
				break
			}

			// Check if another job for this resource is already running
			var runningCount int64
			if err := tx.Model(&model.Job{}).
				Where("resource_type = ? AND resource_id = ? AND status = ? AND id != ?",
					*candidate.ResourceType, *candidate.ResourceID, model.JobStatusRunning, candidate.ID).
				Count(&runningCount).Error; err != nil {
				return err
			}

			if runningCount == 0 {
				// No conflict, claim this job
				job = candidate
				found = true
				break
			}
			// Resource is busy, try next candidate
		}

		if !found {
			return nil // All candidates have resource conflicts
		}

		// Claim the job
		now := time.Now()
		job.Status = string(model.JobStatusRunning)
		job.WorkerID = &workerID
		job.StartedAt = &now
		job.Attempts++

		return tx.Save(&job).Error
	})

	if err != nil {
		return nil, err
	}

	if !found {
		return nil, nil
	}

	return &job, nil
}

// CompleteJob marks a job as completed.
func (s *Store) CompleteJob(ctx context.Context, jobID string) error {
	now := time.Now()
	return s.writeDB.WithContext(ctx).Model(&model.Job{}).
		Where("id = ?", jobID).
		Updates(map[string]interface{}{
			"status":       model.JobStatusCompleted,
			"completed_at": now,
		}).Error
}

// FailJob marks a job as failed with an error message.
// If attempts < max_attempts, requeues as pending for retry with backoff.
// The baseBackoff is multiplied by the attempt number for exponential backoff.
func (s *Store) FailJob(ctx context.Context, jobID string, errMsg string, baseBackoff time.Duration) error {
	return s.writeDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var job model.Job
		if err := tx.First(&job, "id = ?", jobID).Error; err != nil {
			return err
		}

		if job.Attempts < job.MaxAttempts {
			// Retry: reset to pending with exponential backoff
			backoff := time.Duration(job.Attempts) * baseBackoff
			scheduledAt := time.Now().Add(backoff)

			return tx.Model(&job).Updates(map[string]interface{}{
				"status":       model.JobStatusPending,
				"worker_id":    nil,
				"started_at":   nil,
				"scheduled_at": scheduledAt,
				"error":        errMsg,
			}).Error
		}

		// Max attempts reached, mark as failed
		now := time.Now()
		return tx.Model(&job).Updates(map[string]interface{}{
			"status":       model.JobStatusFailed,
			"completed_at": now,
			"error":        errMsg,
		}).Error
	})
}

// CountRunningJobsByType returns the count of running jobs of a given type.
func (s *Store) CountRunningJobsByType(ctx context.Context, jobType string) (int64, error) {
	var count int64
	err := s.readDB.WithContext(ctx).Model(&model.Job{}).
		Where("type = ? AND status = ?", jobType, model.JobStatusRunning).
		Count(&count).Error
	return count, err
}

// CleanupStaleJobs resets jobs that have been running too long (worker died).
// Returns the number of jobs reset.
func (s *Store) CleanupStaleJobs(ctx context.Context, staleAfter time.Duration) (int64, error) {
	cutoff := time.Now().Add(-staleAfter)
	result := s.writeDB.WithContext(ctx).Model(&model.Job{}).
		Where("status = ? AND started_at < ?", model.JobStatusRunning, cutoff).
		Updates(map[string]interface{}{
			"status":     model.JobStatusPending,
			"worker_id":  nil,
			"started_at": nil,
		})
	return result.RowsAffected, result.Error
}

// ListPendingJobTypes returns the distinct types of pending jobs.
func (s *Store) ListPendingJobTypes(ctx context.Context) ([]string, error) {
	var types []string
	err := s.readDB.WithContext(ctx).Model(&model.Job{}).
		Where("status = ? AND scheduled_at <= ?", model.JobStatusPending, time.Now()).
		Distinct("type").
		Pluck("type", &types).Error
	return types, err
}

// --- Dispatcher Leader Election ---

// TryAcquireLeadership attempts to become the leader.
// Returns true if this server is now the leader.
func (s *Store) TryAcquireLeadership(ctx context.Context, serverID string, heartbeatTimeout time.Duration) (bool, error) {
	now := time.Now()
	cutoff := now.Add(-heartbeatTimeout)

	var acquired bool
	err := s.writeDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing model.DispatcherLeader
		err := tx.First(&existing, "id = ?", model.DispatcherLeaderSingletonID).Error

		if errors.Is(err, gorm.ErrRecordNotFound) {
			// No leader exists, try to become leader
			leader := model.DispatcherLeader{
				ID:          model.DispatcherLeaderSingletonID,
				ServerID:    serverID,
				HeartbeatAt: now,
				AcquiredAt:  now,
			}
			if err := tx.Create(&leader).Error; err != nil {
				// Another server might have won the race
				return nil
			}
			acquired = true
			return nil
		}

		if err != nil {
			return err
		}

		// Leader exists - check if it's us or if heartbeat has expired
		if existing.ServerID == serverID {
			// We are already the leader, update heartbeat
			existing.HeartbeatAt = now
			if err := tx.Save(&existing).Error; err != nil {
				return err
			}
			acquired = true
			return nil
		}

		if existing.HeartbeatAt.Before(cutoff) {
			// Previous leader's heartbeat expired, take over
			existing.ServerID = serverID
			existing.HeartbeatAt = now
			existing.AcquiredAt = now
			if err := tx.Save(&existing).Error; err != nil {
				return err
			}
			acquired = true
			return nil
		}

		// Another server is the active leader
		acquired = false
		return nil
	})

	return acquired, err
}

// ReleaseLeadership releases leadership on graceful shutdown.
func (s *Store) ReleaseLeadership(ctx context.Context, serverID string) error {
	return s.writeDB.WithContext(ctx).
		Where("id = ? AND server_id = ?", model.DispatcherLeaderSingletonID, serverID).
		Delete(&model.DispatcherLeader{}).Error
}

// --- Project Events ---

// CreateProjectEvent persists a new event for a project.
func (s *Store) CreateProjectEvent(ctx context.Context, event *model.ProjectEvent) error {
	return s.writeDB.WithContext(ctx).Create(event).Error
}

// ListProjectEventsSince returns all events for a project created after the given time.
// Events are returned in ascending order by creation time.
func (s *Store) ListProjectEventsSince(ctx context.Context, projectID string, since time.Time) ([]model.ProjectEvent, error) {
	var events []model.ProjectEvent
	err := s.readDB.WithContext(ctx).
		Where("project_id = ? AND created_at > ?", projectID, since).
		Order("created_at ASC").
		Find(&events).Error
	if err != nil {
		return nil, err
	}
	return events, nil
}

// ListProjectEventsAfterID returns all events for a project created after the event with the given ID.
// This is useful for resuming from a specific event ID.
func (s *Store) ListProjectEventsAfterID(ctx context.Context, projectID, afterID string) ([]model.ProjectEvent, error) {
	// First get the timestamp of the reference event
	var refEvent model.ProjectEvent
	if err := s.readDB.WithContext(ctx).First(&refEvent, "id = ?", afterID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// If reference event not found, return all events
			return s.ListProjectEventsSince(ctx, projectID, time.Time{})
		}
		return nil, err
	}

	var events []model.ProjectEvent
	err := s.readDB.WithContext(ctx).
		Where("project_id = ? AND created_at > ?", projectID, refEvent.CreatedAt).
		Order("created_at ASC").
		Find(&events).Error
	if err != nil {
		return nil, err
	}
	return events, nil
}

// ListEventsAfterSeq returns all events (across all projects) with seq > afterSeq.
// Events are returned in ascending order by sequence number.
// This is used by the event poller to fetch new events globally.
func (s *Store) ListEventsAfterSeq(ctx context.Context, afterSeq int64, limit int) ([]model.ProjectEvent, error) {
	var events []model.ProjectEvent
	query := s.readDB.WithContext(ctx).
		Where("seq > ?", afterSeq).
		Order("seq ASC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	if err := query.Find(&events).Error; err != nil {
		return nil, err
	}
	return events, nil
}

// GetMaxEventSeq returns the maximum sequence number of all events.
// Returns 0 if there are no events.
func (s *Store) GetMaxEventSeq(ctx context.Context) (int64, error) {
	var maxSeq int64
	err := s.readDB.WithContext(ctx).
		Model(&model.ProjectEvent{}).
		Select("COALESCE(MAX(seq), 0)").
		Scan(&maxSeq).Error
	return maxSeq, err
}

// DeleteOldProjectEvents deletes events older than the specified duration.
// This can be called periodically to clean up old events.
func (s *Store) DeleteOldProjectEvents(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan)
	result := s.writeDB.WithContext(ctx).
		Where("created_at < ?", cutoff).
		Delete(&model.ProjectEvent{})
	return result.RowsAffected, result.Error
}

// --- User Preferences ---

// GetUserPreference returns a single preference by user ID and key.
func (s *Store) GetUserPreference(ctx context.Context, userID, key string) (*model.UserPreference, error) {
	var pref model.UserPreference
	if err := s.readDB.WithContext(ctx).First(&pref, "user_id = ? AND key = ?", userID, key).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &pref, nil
}

// ListUserPreferences returns all preferences for a user.
func (s *Store) ListUserPreferences(ctx context.Context, userID string) ([]*model.UserPreference, error) {
	var prefs []*model.UserPreference
	err := s.readDB.WithContext(ctx).Where("user_id = ?", userID).Order("key ASC").Find(&prefs).Error
	return prefs, err
}

// SetUserPreference creates or updates a user preference (upsert).
func (s *Store) SetUserPreference(ctx context.Context, pref *model.UserPreference) error {
	return s.writeDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing model.UserPreference
		err := tx.First(&existing, "user_id = ? AND key = ?", pref.UserID, pref.Key).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		if errors.Is(err, gorm.ErrRecordNotFound) {
			// Create new preference
			return tx.Create(pref).Error
		}

		// Update existing preference
		existing.Value = pref.Value
		pref.ID = existing.ID
		pref.CreatedAt = existing.CreatedAt
		return tx.Save(&existing).Error
	})
}

// DeleteUserPreference deletes a user preference by key.
func (s *Store) DeleteUserPreference(ctx context.Context, userID, key string) error {
	result := s.writeDB.WithContext(ctx).Delete(&model.UserPreference{}, "user_id = ? AND key = ?", userID, key)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
