// Package model defines the database models used throughout the application.
// These models work with both PostgreSQL and SQLite via GORM.
package model

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// User represents an authenticated user.
type User struct {
	ID         string    `gorm:"primaryKey;type:text" json:"id"`
	Email      string    `gorm:"uniqueIndex;not null;type:text" json:"email"`
	Name       *string   `gorm:"type:text" json:"name,omitempty"`
	AvatarURL  *string   `gorm:"column:avatar_url;type:text" json:"avatar_url,omitempty"`
	Provider   string    `gorm:"not null;type:text" json:"provider"`
	ProviderID string    `gorm:"column:provider_id;not null;type:text" json:"provider_id"`
	CreatedAt  time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt  time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

func (User) TableName() string { return "users" }

func (u *User) BeforeCreate(_ *gorm.DB) error {
	if u.ID == "" {
		u.ID = uuid.New().String()
	}
	return nil
}

// UserSession represents an authentication session (cookie-based).
type UserSession struct {
	ID        string    `gorm:"primaryKey;type:text" json:"id"`
	UserID    string    `gorm:"column:user_id;not null;type:text;index" json:"user_id"`
	TokenHash string    `gorm:"column:token_hash;uniqueIndex;not null;type:text" json:"token_hash"`
	ExpiresAt time.Time `gorm:"column:expires_at;not null;index" json:"expires_at"`
	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`

	User *User `gorm:"foreignKey:UserID" json:"-"`
}

func (UserSession) TableName() string { return "user_sessions" }

func (s *UserSession) BeforeCreate(_ *gorm.DB) error {
	if s.ID == "" {
		s.ID = uuid.New().String()
	}
	return nil
}

// Project represents a multi-tenant container.
type Project struct {
	ID        string    `gorm:"primaryKey;type:text" json:"id"`
	Name      string    `gorm:"not null;type:text" json:"name"`
	Slug      string    `gorm:"uniqueIndex;not null;type:text" json:"slug"`
	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updated_at"`

	Members    []ProjectMember `gorm:"foreignKey:ProjectID" json:"-"`
	Workspaces []Workspace     `gorm:"foreignKey:ProjectID" json:"-"`
}

func (Project) TableName() string { return "projects" }

func (p *Project) BeforeCreate(_ *gorm.DB) error {
	if p.ID == "" {
		p.ID = uuid.New().String()
	}
	return nil
}

// ProjectMember represents a user's membership in a project.
type ProjectMember struct {
	ID         string     `gorm:"primaryKey;type:text" json:"id"`
	ProjectID  string     `gorm:"column:project_id;not null;type:text;uniqueIndex:idx_project_user" json:"project_id"`
	UserID     string     `gorm:"column:user_id;not null;type:text;uniqueIndex:idx_project_user;index" json:"user_id"`
	Role       string     `gorm:"not null;type:text;default:member" json:"role"`
	InvitedBy  *string    `gorm:"column:invited_by;type:text" json:"invited_by,omitempty"`
	InvitedAt  *time.Time `gorm:"column:invited_at" json:"invited_at,omitempty"`
	AcceptedAt *time.Time `gorm:"column:accepted_at" json:"accepted_at,omitempty"`

	Project *Project `gorm:"foreignKey:ProjectID" json:"-"`
	User    *User    `gorm:"foreignKey:UserID" json:"-"`
}

func (ProjectMember) TableName() string { return "project_members" }

func (m *ProjectMember) BeforeCreate(_ *gorm.DB) error {
	if m.ID == "" {
		m.ID = uuid.New().String()
	}
	return nil
}

// ProjectInvitation represents a pending invitation to join a project.
type ProjectInvitation struct {
	ID        string    `gorm:"primaryKey;type:text" json:"id"`
	ProjectID string    `gorm:"column:project_id;not null;type:text;uniqueIndex:idx_project_email" json:"project_id"`
	Email     string    `gorm:"not null;type:text;uniqueIndex:idx_project_email" json:"email"`
	Role      string    `gorm:"not null;type:text;default:member" json:"role"`
	InvitedBy *string   `gorm:"column:invited_by;type:text" json:"invited_by,omitempty"`
	Token     string    `gorm:"uniqueIndex;not null;type:text" json:"token"`
	ExpiresAt time.Time `gorm:"column:expires_at;not null" json:"expires_at"`
	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`

	Project *Project `gorm:"foreignKey:ProjectID" json:"-"`
}

func (ProjectInvitation) TableName() string { return "project_invitations" }

func (i *ProjectInvitation) BeforeCreate(_ *gorm.DB) error {
	if i.ID == "" {
		i.ID = uuid.New().String()
	}
	return nil
}

// Agent represents an AI agent configuration.
// Deprecated: Agent concept has been removed. This struct is kept only for DB migration compatibility.
type Agent struct {
	ID        string    `gorm:"primaryKey;type:text" json:"id"`
	ProjectID string    `gorm:"column:project_id;not null;type:text;index" json:"project_id"`
	AgentType string    `gorm:"column:agent_type;not null;type:text" json:"agent_type"`
	IsDefault bool      `gorm:"column:is_default;default:false" json:"is_default"`
	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updated_at"`

	Project *Project `gorm:"foreignKey:ProjectID" json:"-"`
}

func (Agent) TableName() string { return "agents" }

func (a *Agent) BeforeCreate(_ *gorm.DB) error {
	if a.ID == "" {
		a.ID = uuid.New().String()
	}
	return nil
}

// Workspace status constants representing the lifecycle of a workspace
const (
	WorkspaceStatusInitializing = "initializing" // Workspace just created, starting setup
	WorkspaceStatusCloning      = "cloning"      // Cloning git repository
	WorkspaceStatusReady        = "ready"        // Workspace is ready for use
	WorkspaceStatusError        = "error"        // Something failed during setup
)

// Workspace provider constants representing how sessions are executed.
// When a workspace has no provider set (empty string), the platform default is used
// at runtime: "vz" on macOS, "docker" on other platforms.
const (
	WorkspaceProviderVZ     = "vz"     // Run in Virtualization.framework VMs (macOS only)
	WorkspaceProviderDocker = "docker" // Run in Docker containers
	WorkspaceProviderLocal  = "local"  // Run in local directory without isolation
)

// Workspace represents a working directory (local folder or git repo).
type Workspace struct {
	ID            string    `gorm:"primaryKey;type:text" json:"id"`
	ProjectID     string    `gorm:"column:project_id;not null;type:text;index" json:"projectId"`
	Path          string    `gorm:"not null;type:text" json:"path"`
	DisplayName   *string   `gorm:"column:display_name;type:text" json:"displayName,omitempty"`
	SourceType    string    `gorm:"column:source_type;not null;type:text" json:"sourceType"`
	Provider      string    `gorm:"type:text;default:''" json:"provider,omitempty"`
	AutoGenerated bool      `gorm:"column:auto_generated;not null;default:false" json:"autoGenerated"`
	Status        string    `gorm:"not null;type:text;default:initializing" json:"status"`
	ErrorMessage  *string   `gorm:"column:error_message;type:text" json:"errorMessage,omitempty"`
	CreatedAt     time.Time `gorm:"autoCreateTime" json:"createdAt"`
	UpdatedAt     time.Time `gorm:"autoUpdateTime" json:"updatedAt"`

	Project  *Project  `gorm:"foreignKey:ProjectID" json:"-"`
	Sessions []Session `gorm:"foreignKey:WorkspaceID" json:"-"`
}

func (Workspace) TableName() string { return "workspaces" }

func (w *Workspace) BeforeCreate(_ *gorm.DB) error {
	if w.ID == "" {
		w.ID = uuid.New().String()
	}
	return nil
}

// Session status constants representing the lifecycle of a session
const (
	SessionStatusInitializing    = "initializing"     // Session just created, starting setup
	SessionStatusReinitializing  = "reinitializing"   // Recreating sandbox after it was deleted
	SessionStatusCloning         = "cloning"          // Cloning git repository
	SessionStatusPullingImage    = "pulling_image"    // Pulling runtime image
	SessionStatusCreatingSandbox = "creating_sandbox" // Creating sandbox environment
	SessionStatusReady           = "ready"            // Session is ready for use
	SessionStatusStopped         = "stopped"          // Sandbox is stopped, will restart on demand
	SessionStatusError           = "error"            // Something failed during setup
	SessionStatusRemoving        = "removing"         // Session is being deleted
	SessionStatusRemoved         = "removed"          // Session has been deleted
)

// Commit status constants representing the commit state of a session (orthogonal to session status)
const (
	CommitStatusNone       = ""           // No commit in progress (default)
	CommitStatusPending    = "pending"    // Commit requested, waiting to start
	CommitStatusCommitting = "committing" // Commit in progress
	CommitStatusCompleted  = "completed"  // Commit completed successfully
	CommitStatusFailed     = "failed"     // Commit failed
)

// Commit operation constants representing the active operation using commit status fields.
const (
	CommitOperationCommit = "commit"
	CommitOperationRebase = "rebase"
)

// Session represents a chat thread within a workspace.
type Session struct {
	ID                  string         `gorm:"primaryKey;type:text" json:"id"`
	ProjectID           string         `gorm:"column:project_id;not null;type:text;index" json:"projectId"`
	WorkspaceID         string         `gorm:"column:workspace_id;not null;type:text;index" json:"workspaceId"`
	Name                string         `gorm:"not null;type:text" json:"name"`
	DisplayName         *string        `gorm:"column:display_name;type:text" json:"displayName,omitempty"`
	Description         *string        `gorm:"type:text" json:"description,omitempty"`
	Status              string         `gorm:"not null;type:text;default:initializing" json:"status"`
	CommitStatus        string         `gorm:"column:commit_status;type:text;default:''" json:"commitStatus"`
	CommitOperation     *string        `gorm:"column:commit_operation;type:text" json:"commitOperation,omitempty"`
	CommitError         *string        `gorm:"column:commit_error;type:text" json:"commitError,omitempty"`
	BaseCommit          *string        `gorm:"column:base_commit;type:text" json:"baseCommit,omitempty"`
	AppliedCommit       *string        `gorm:"column:applied_commit;type:text" json:"appliedCommit,omitempty"`
	ErrorMessage        *string        `gorm:"column:error_message;type:text" json:"errorMessage,omitempty"`
	WorkspacePath       *string        `gorm:"column:workspace_path;type:text" json:"workspacePath,omitempty"`
	WorkspaceCommit     *string        `gorm:"column:workspace_commit;type:text" json:"workspaceCommit,omitempty"`
	SSHKeyEncryptedData []byte         `gorm:"column:ssh_key_encrypted_data" json:"-"`
	CreatedAt           time.Time      `gorm:"autoCreateTime" json:"createdAt"`
	UpdatedAt           time.Time      `gorm:"autoUpdateTime" json:"updatedAt"`
	DeletedAt           gorm.DeletedAt `gorm:"index" json:"-"`

	Project   *Project   `gorm:"foreignKey:ProjectID" json:"-"`
	Workspace *Workspace `gorm:"foreignKey:WorkspaceID" json:"-"`
	Messages  []Message  `gorm:"foreignKey:SessionID" json:"-"`
}

func (Session) TableName() string { return "sessions" }

func (s *Session) BeforeCreate(_ *gorm.DB) error {
	if s.ID == "" {
		s.ID = uuid.New().String()
	}
	return nil
}

// Message represents a chat message in a session.
// Stored in UIMessage format compatible with AI SDK.
type Message struct {
	ID        string          `gorm:"primaryKey;type:text" json:"id"`
	SessionID string          `gorm:"column:session_id;not null;type:text;index" json:"sessionId"`
	Role      string          `gorm:"not null;type:text" json:"role"`
	Parts     json.RawMessage `gorm:"type:text;not null" json:"parts"`
	CreatedAt time.Time       `gorm:"autoCreateTime" json:"createdAt"`

	Session *Session `gorm:"foreignKey:SessionID" json:"-"`
}

func (Message) TableName() string { return "messages" }

func (m *Message) BeforeCreate(_ *gorm.DB) error {
	if m.ID == "" {
		m.ID = uuid.New().String()
	}
	return nil
}

// TextPart represents a text part in a UIMessage.
type TextPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// NewTextParts creates a JSON parts array with a single text part.
func NewTextParts(text string) json.RawMessage {
	parts := []TextPart{{Type: "text", Text: text}}
	data, _ := json.Marshal(parts)
	return data
}

// Credential represents stored credentials for AI providers and custom env bundles.
type Credential struct {
	ID            string    `gorm:"primaryKey;type:text" json:"id"`
	ProjectID     string    `gorm:"column:project_id;not null;type:text;uniqueIndex:idx_project_provider" json:"project_id"`
	Provider      string    `gorm:"not null;type:text;uniqueIndex:idx_project_provider" json:"provider"`
	Name          string    `gorm:"not null;type:text" json:"name"`
	Description   *string   `gorm:"type:text" json:"description,omitempty"`
	AuthType      string    `gorm:"column:auth_type;not null;type:text" json:"auth_type"`
	EncryptedData []byte    `gorm:"column:encrypted_data" json:"-"`
	IsConfigured  bool      `gorm:"column:is_configured;default:false" json:"is_configured"`
	AgentVisible  bool      `gorm:"column:agent_visible;not null;default:false" json:"agent_visible"`
	CreatedAt     time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt     time.Time `gorm:"autoUpdateTime" json:"updated_at"`

	Project *Project `gorm:"foreignKey:ProjectID" json:"-"`
}

func (Credential) TableName() string { return "credentials" }

func (c *Credential) BeforeCreate(_ *gorm.DB) error {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	return nil
}

// SessionCredentialAssignment stores which credentials are assigned to a session
// and whether each assignment is visible to the agent/LLM environment.
type SessionCredentialAssignment struct {
	ID           string    `gorm:"primaryKey;type:text" json:"id"`
	SessionID    string    `gorm:"column:session_id;not null;type:text;index;uniqueIndex:idx_session_credential_assignment" json:"sessionId"`
	CredentialID string    `gorm:"column:credential_id;not null;type:text;index;uniqueIndex:idx_session_credential_assignment" json:"credentialId"`
	AgentVisible bool      `gorm:"column:agent_visible;not null;default:false" json:"agentVisible"`
	CreatedAt    time.Time `gorm:"autoCreateTime" json:"createdAt"`
	UpdatedAt    time.Time `gorm:"autoUpdateTime" json:"updatedAt"`

	Session    *Session    `gorm:"foreignKey:SessionID" json:"-"`
	Credential *Credential `gorm:"foreignKey:CredentialID" json:"-"`
}

func (SessionCredentialAssignment) TableName() string { return "session_credential_assignments" }

func (a *SessionCredentialAssignment) BeforeCreate(_ *gorm.DB) error {
	if a.ID == "" {
		a.ID = uuid.New().String()
	}
	return nil
}

// TerminalHistory represents a terminal command/output entry.
type TerminalHistory struct {
	ID        string    `gorm:"primaryKey;type:text" json:"id"`
	SessionID string    `gorm:"column:session_id;not null;type:text;index" json:"session_id"`
	EntryType string    `gorm:"column:entry_type;not null;type:text" json:"entry_type"`
	Content   string    `gorm:"not null;type:text" json:"content"`
	ExitCode  *int      `gorm:"column:exit_code" json:"exit_code,omitempty"`
	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`

	Session *Session `gorm:"foreignKey:SessionID" json:"-"`
}

func (TerminalHistory) TableName() string { return "terminal_history" }

func (t *TerminalHistory) BeforeCreate(_ *gorm.DB) error {
	if t.ID == "" {
		t.ID = uuid.New().String()
	}
	return nil
}

// Event type constants
const (
	EventTypeSessionUpdated = "session_updated"
)

// ProjectEvent represents a persisted event for a project.
// Events are used for SSE streaming to clients.
type ProjectEvent struct {
	ID        string          `gorm:"primaryKey;type:text" json:"id"`
	Seq       int64           `gorm:"column:seq;autoIncrement;uniqueIndex" json:"seq"`
	ProjectID string          `gorm:"column:project_id;not null;type:text;index:idx_project_seq,priority:1" json:"projectId"`
	Type      string          `gorm:"not null;type:text" json:"type"`
	Data      json.RawMessage `gorm:"type:text;not null" json:"data"`
	CreatedAt time.Time       `gorm:"autoCreateTime;index:idx_project_seq,priority:2" json:"createdAt"`

	Project *Project `gorm:"foreignKey:ProjectID" json:"-"`
}

func (ProjectEvent) TableName() string { return "project_events" }

func (e *ProjectEvent) BeforeCreate(_ *gorm.DB) error {
	if e.ID == "" {
		e.ID = uuid.New().String()
	}
	return nil
}

// UserPreference represents a user preference (key/value store scoped to user).
type UserPreference struct {
	ID        string    `gorm:"primaryKey;type:text" json:"id"`
	UserID    string    `gorm:"column:user_id;not null;type:text;uniqueIndex:idx_user_key" json:"user_id"`
	Key       string    `gorm:"not null;type:text;uniqueIndex:idx_user_key" json:"key"`
	Value     string    `gorm:"not null;type:text" json:"value"`
	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updated_at"`

	User *User `gorm:"foreignKey:UserID" json:"-"`
}

func (UserPreference) TableName() string { return "user_preferences" }

func (p *UserPreference) BeforeCreate(_ *gorm.DB) error {
	if p.ID == "" {
		p.ID = uuid.New().String()
	}
	return nil
}

// AllModels returns all model types for migration.
func AllModels() []interface{} {
	return []interface{}{
		&User{},
		&UserSession{},
		&Project{},
		&ProjectMember{},
		&ProjectInvitation{},
		&Agent{},
		&Workspace{},
		&Session{},
		&Message{},
		&Credential{},
		&SessionCredentialAssignment{},
		&TerminalHistory{},
		&ProjectEvent{},
		&Job{},
		&DispatcherLeader{},
		&UserPreference{},
	}
}
