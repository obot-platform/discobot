package service

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/obot-platform/discobot/server/internal/config"
	"github.com/obot-platform/discobot/server/internal/model"
	"github.com/obot-platform/discobot/server/internal/sandbox"
	mocksandbox "github.com/obot-platform/discobot/server/internal/sandbox/mock"
	"github.com/obot-platform/discobot/server/internal/sandbox/sandboxapi"
)

func TestValidateSessionID(t *testing.T) {
	tests := []struct {
		name      string
		sessionID string
		wantErr   bool
		errMsg    string
	}{
		{
			name:      "valid alphanumeric",
			sessionID: "abc123",
			wantErr:   false,
		},
		{
			name:      "valid with hyphens",
			sessionID: "session-123-abc",
			wantErr:   false,
		},
		{
			name:      "valid UUID format",
			sessionID: "550e8400-e29b-41d4-a716-446655440000",
			wantErr:   false,
		},
		{
			name:      "valid at max length (65 chars)",
			sessionID: strings.Repeat("a", 65),
			wantErr:   false,
		},
		{
			name:      "empty string",
			sessionID: "",
			wantErr:   true,
			errMsg:    "session ID is required",
		},
		{
			name:      "exceeds max length (66 chars)",
			sessionID: strings.Repeat("a", 66),
			wantErr:   true,
			errMsg:    "exceeds maximum length",
		},
		{
			name:      "contains underscore",
			sessionID: "session_123",
			wantErr:   true,
			errMsg:    "must contain only alphanumeric characters and hyphens",
		},
		{
			name:      "contains space",
			sessionID: "session 123",
			wantErr:   true,
			errMsg:    "must contain only alphanumeric characters and hyphens",
		},
		{
			name:      "contains special characters",
			sessionID: "session@123!",
			wantErr:   true,
			errMsg:    "must contain only alphanumeric characters and hyphens",
		},
		{
			name:      "contains dot",
			sessionID: "session.123",
			wantErr:   true,
			errMsg:    "must contain only alphanumeric characters and hyphens",
		},
		{
			name:      "contains slash",
			sessionID: "session/123",
			wantErr:   true,
			errMsg:    "must contain only alphanumeric characters and hyphens",
		},
		{
			name:      "only hyphens",
			sessionID: "---",
			wantErr:   false,
		},
		{
			name:      "single character",
			sessionID: "a",
			wantErr:   false,
		},
		{
			name:      "contains newline",
			sessionID: "session\n123",
			wantErr:   true,
			errMsg:    "must contain only alphanumeric characters and hyphens",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSessionID(tt.sessionID)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ValidateSessionID(%q) expected error, got nil", tt.sessionID)
					return
				}
				if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("ValidateSessionID(%q) error = %v, expected to contain %q", tt.sessionID, err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("ValidateSessionID(%q) unexpected error: %v", tt.sessionID, err)
				}
			}
		})
	}
}

func TestSessionIDMaxLength(t *testing.T) {
	// Verify the constant is set to 65
	if SessionIDMaxLength != 65 {
		t.Errorf("SessionIDMaxLength = %d, want 65", SessionIDMaxLength)
	}
}

func TestSessionServiceGetSessionSyncsNameFromPrimaryThread(t *testing.T) {
	ctx := context.Background()
	testStore := setupTestStore(t)
	provider := mocksandbox.NewProvider()
	sandboxSvc := NewSandboxService(testStore, provider, &config.Config{}, nil, nil, nil, nil)
	sessionSvc := NewSessionService(testStore, nil, provider, sandboxSvc, nil, nil)

	workspace := &model.Workspace{
		ID:         "workspace-1",
		ProjectID:  "project-1",
		Path:       "/workspace",
		SourceType: "local",
		Status:     model.WorkspaceStatusReady,
	}
	if err := testStore.CreateWorkspace(ctx, workspace); err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}

	dbSession := &model.Session{
		ID:          "session-1",
		ProjectID:   workspace.ProjectID,
		WorkspaceID: workspace.ID,
		Status:      model.SessionStatusReady,
	}
	if err := testStore.CreateSession(ctx, dbSession); err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	if _, err := provider.Create(ctx, dbSession.ID, sandbox.CreateOptions{SharedSecret: "test-secret", WorkspacePath: workspace.Path}); err != nil {
		t.Fatalf("failed to create sandbox: %v", err)
	}
	if err := provider.Start(ctx, dbSession.ID); err != nil {
		t.Fatalf("failed to start sandbox: %v", err)
	}

	client, err := sandboxSvc.GetClient(ctx, dbSession.ID)
	if err != nil {
		t.Fatalf("failed to get sandbox client: %v", err)
	}
	if _, err := client.CreateThread(ctx, &sandboxapi.CreateThreadRequest{ID: dbSession.ID, Name: "Primary thread name"}); err != nil {
		t.Fatalf("failed to create thread: %v", err)
	}

	session, err := sessionSvc.GetSession(ctx, dbSession.ID)
	if err != nil {
		t.Fatalf("failed to get session: %v", err)
	}
	if session.Name != "Primary thread name" {
		t.Fatalf("expected synced session name, got %q", session.Name)
	}

	stored, err := testStore.GetSessionByID(ctx, dbSession.ID)
	if err != nil {
		t.Fatalf("failed to reload session: %v", err)
	}
	if stored.Name != "Primary thread name" {
		t.Fatalf("expected stored session name to be updated, got %q", stored.Name)
	}
}

func TestSessionServiceListSessionsByProjectSyncsNameFromPrimaryThread(t *testing.T) {
	ctx := context.Background()
	testStore := setupTestStore(t)
	provider := mocksandbox.NewProvider()
	sandboxSvc := NewSandboxService(testStore, provider, &config.Config{}, nil, nil, nil, nil)
	sessionSvc := NewSessionService(testStore, nil, provider, sandboxSvc, nil, nil)

	workspace := &model.Workspace{
		ID:         "workspace-2",
		ProjectID:  "project-2",
		Path:       "/workspace-2",
		SourceType: "local",
		Status:     model.WorkspaceStatusReady,
	}
	if err := testStore.CreateWorkspace(ctx, workspace); err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}

	dbSession := &model.Session{
		ID:          "session-2",
		ProjectID:   workspace.ProjectID,
		WorkspaceID: workspace.ID,
		Status:      model.SessionStatusReady,
	}
	if err := testStore.CreateSession(ctx, dbSession); err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	if _, err := provider.Create(ctx, dbSession.ID, sandbox.CreateOptions{SharedSecret: "test-secret", WorkspacePath: workspace.Path}); err != nil {
		t.Fatalf("failed to create sandbox: %v", err)
	}
	if err := provider.Start(ctx, dbSession.ID); err != nil {
		t.Fatalf("failed to start sandbox: %v", err)
	}

	client, err := sandboxSvc.GetClient(ctx, dbSession.ID)
	if err != nil {
		t.Fatalf("failed to get sandbox client: %v", err)
	}
	if _, err := client.CreateThread(ctx, &sandboxapi.CreateThreadRequest{ID: dbSession.ID, Name: "Listed thread name"}); err != nil {
		t.Fatalf("failed to create thread: %v", err)
	}

	sessions, err := sessionSvc.ListSessionsByProject(ctx, workspace.ProjectID)
	if err != nil {
		t.Fatalf("failed to list sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].Name != "Listed thread name" {
		t.Fatalf("expected synced session name from list, got %q", sessions[0].Name)
	}
}

// TestMapSessionFieldCoverage ensures all model.Session fields are properly mapped to service.Session.
// This test uses reflection to verify complete field mapping and will fail if:
// 1. A field exists in model.Session but not in service.Session
// 2. A field exists but is not populated in mapSession
// This prevents bugs where new fields are added to the model but forgotten in the mapping.
func TestMapSessionFieldCoverage(t *testing.T) {
	// Create a fully populated model.Session with non-nil values
	strPtr := func(s string) *string { return &s }

	createdAt := time.Date(2026, time.March, 20, 8, 30, 0, 0, time.UTC)
	updatedAt := time.Date(2026, time.March, 21, 9, 45, 0, 0, time.UTC)

	modelSession := &model.Session{
		ID:              "test-id",
		ProjectID:       "test-project",
		WorkspaceID:     "test-workspace",
		Name:            "test-name",
		DisplayName:     strPtr("Test Display"),
		Description:     strPtr("Test Description"),
		Status:          "ready",
		CommitStatus:    "committed",
		CommitOperation: strPtr("rebase"),
		CommitError:     strPtr("commit error"),
		BaseCommit:      strPtr("base123"),
		AppliedCommit:   strPtr("applied456"),
		ErrorMessage:    strPtr("error message"),
		WorkspacePath:   strPtr("/path/to/workspace"),
		WorkspaceCommit: strPtr("commit789"),
		ActiveEnvSetIDs: []string{"test-env-set-id"},
		CreatedAt:       createdAt,
		UpdatedAt:       updatedAt,
	}

	// Create a mock SessionService (nil is fine since mapSession doesn't use it)
	svc := &SessionService{}

	// Map the session
	result := svc.mapSession(modelSession)

	// Define field mappings: model field -> service field
	// This map documents the expected mapping between model and service layer
	fieldMappings := map[string]string{
		"ID":              "ID",
		"ProjectID":       "ProjectID",
		"WorkspaceID":     "WorkspaceID",
		"Name":            "Name",
		"DisplayName":     "DisplayName",
		"Description":     "Description",
		"Status":          "Status",
		"CommitStatus":    "CommitStatus",
		"CommitOperation": "CommitOperation",
		"CommitError":     "CommitError",
		"BaseCommit":      "BaseCommit",
		"AppliedCommit":   "AppliedCommit",
		"ErrorMessage":    "ErrorMessage",
		"WorkspacePath":   "WorkspacePath",
		"WorkspaceCommit": "WorkspaceCommit",
		"ActiveEnvSetIDs": "ActiveEnvSetIDs",
		"CreatedAt":       "CreatedAt",
		// Excluded fields (not part of API response):
		// - SSHKeyEncryptedData: encrypted secret material, never exposed
		// - UpdatedAt: mapped to Timestamp
		// - Project, Workspace, Messages: relationships, not serialized
		// - Files: always initialized as empty array in mapSession
	}

	// Use reflection to verify all documented fields are mapped
	modelType := reflect.TypeOf(*modelSession)
	serviceType := reflect.TypeOf(*result)

	// Check all model fields
	for i := 0; i < modelType.NumField(); i++ {
		modelField := modelType.Field(i)
		modelFieldName := modelField.Name

		// Skip GORM metadata fields and relationship fields
		if modelFieldName == "SSHKeyEncryptedData" || modelFieldName == "UpdatedAt" ||
			modelFieldName == "Project" || modelFieldName == "Workspace" ||
			modelFieldName == "Messages" {
			continue
		}

		// Check if field is documented in fieldMappings
		serviceFieldName, mapped := fieldMappings[modelFieldName]
		if !mapped {
			t.Errorf("Model field %s is not documented in fieldMappings - add it or document why it's excluded", modelFieldName)
			continue
		}

		// Verify service type has the corresponding field
		serviceField, found := serviceType.FieldByName(serviceFieldName)
		if !found {
			t.Errorf("Service.Session missing field %s (maps from model.Session.%s)", serviceFieldName, modelFieldName)
			continue
		}

		// Verify the field was actually populated (not zero value)
		resultValue := reflect.ValueOf(*result)
		serviceValue := resultValue.FieldByName(serviceFieldName)

		// Special case for Files which is always initialized as empty array
		if serviceFieldName == "Files" {
			continue
		}

		// For string fields that come from pointers, verify they're not empty
		// (since we populated all pointers with non-empty values)
		if serviceField.Type.Kind() == reflect.String {
			if modelField.Type.Kind() == reflect.Ptr {
				// This field comes from a pointer, should be populated
				if serviceValue.String() == "" {
					t.Errorf("Field %s is empty but model.%s was set to a non-nil pointer", serviceFieldName, modelFieldName)
				}
			}
		}
	}

	// Verify specific field values to ensure mapping is correct
	if result.ID != "test-id" {
		t.Errorf("ID = %q, want %q", result.ID, "test-id")
	}
	if result.DisplayName != "Test Display" {
		t.Errorf("DisplayName = %q, want %q", result.DisplayName, "Test Display")
	}
	if result.CreatedAt != createdAt.Format(time.RFC3339) {
		t.Errorf("CreatedAt = %q, want %q", result.CreatedAt, createdAt.Format(time.RFC3339))
	}
	if result.Timestamp != updatedAt.Format(time.RFC3339) {
		t.Errorf("Timestamp = %q, want %q", result.Timestamp, updatedAt.Format(time.RFC3339))
	}

	// Verify Files is initialized (not nil)
	if result.Files == nil {
		t.Error("Files should be initialized to empty array, got nil")
	}
}
