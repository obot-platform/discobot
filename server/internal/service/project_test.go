package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/obot-platform/discobot/server/internal/model"
	"github.com/obot-platform/discobot/server/internal/sandbox"
	mocksandbox "github.com/obot-platform/discobot/server/internal/sandbox/mock"
)

// TestGenerateSlug validates slug generation from project names.
func TestGenerateSlug(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		input          string
		wantPrefix     string
		wantNotContain string
	}{
		{
			name:       "simple lowercase name",
			input:      "my project",
			wantPrefix: "my-project-",
		},
		{
			name:       "uppercase name is lowercased",
			input:      "My Project",
			wantPrefix: "my-project-",
		},
		{
			name:       "special characters replaced with hyphens",
			input:      "hello world!@#$%",
			wantPrefix: "hello-world-",
		},
		{
			name:       "leading/trailing spaces trimmed",
			input:      "  my project  ",
			wantPrefix: "my-project-",
		},
		{
			name:       "multiple consecutive special chars become single hyphen",
			input:      "hello   world",
			wantPrefix: "hello-world-",
		},
		{
			name:           "no leading hyphens",
			input:          "---test",
			wantNotContain: "-test--",
		},
		{
			name:       "numbers preserved",
			input:      "project 123",
			wantPrefix: "project-123-",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			slug := generateSlug(tt.input)
			if slug == "" {
				t.Fatal("generateSlug returned empty string")
			}
			// Slug must end with a hex suffix (8 chars) after the last hyphen
			parts := strings.Split(slug, "-")
			suffix := parts[len(parts)-1]
			if len(suffix) != 8 {
				t.Errorf("expected 8-char hex suffix, got %q in slug %q", suffix, slug)
			}
			if tt.wantPrefix != "" && !strings.HasPrefix(slug, tt.wantPrefix) {
				t.Errorf("expected slug to start with %q, got %q", tt.wantPrefix, slug)
			}
			if tt.wantNotContain != "" && strings.Contains(slug, tt.wantNotContain) {
				t.Errorf("expected slug not to contain %q, got %q", tt.wantNotContain, slug)
			}
			// Slug should only contain valid characters
			for _, ch := range slug {
				if (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') && ch != '-' {
					t.Errorf("slug %q contains invalid character %q", slug, ch)
				}
			}
		})
	}
}

// TestGenerateSlug_Uniqueness verifies that two calls produce different slugs.
func TestGenerateSlug_Uniqueness(t *testing.T) {
	t.Parallel()
	seen := make(map[string]bool)
	for iteration := range 20 {
		slug := generateSlug("my project")
		if seen[slug] {
			t.Errorf("duplicate slug %q on iteration %d", slug, iteration)
		}
		seen[slug] = true
	}
}

// TestProjectVMResourcesFromInfo verifies the mapping from sandbox info to ProjectVMResources.
func TestProjectVMResourcesFromInfo(t *testing.T) {
	t.Parallel()

	t.Run("nil info returns zero value", func(t *testing.T) {
		t.Parallel()
		result := projectVMResourcesFromInfo(nil)
		if result.CPUCount != 0 || result.MemoryMB != 0 || result.DataDiskGB != 0 {
			t.Errorf("expected zero values for nil info, got %+v", result)
		}
	})

	t.Run("maps fields correctly", func(t *testing.T) {
		t.Parallel()
		info := &sandbox.ProjectResourceInfo{
			Provider:   "test-provider",
			CPUCount:   4,
			MemoryMB:   8192,
			DataDiskGB: 50,
		}
		result := projectVMResourcesFromInfo(info)
		if result.CPUCount != 4 {
			t.Errorf("CPUCount: expected 4, got %d", result.CPUCount)
		}
		if result.MemoryMB != 8192 {
			t.Errorf("MemoryMB: expected 8192, got %d", result.MemoryMB)
		}
		if result.DataDiskGB != 50 {
			t.Errorf("DataDiskGB: expected 50, got %d", result.DataDiskGB)
		}
	})

	t.Run("capability flags are always set correctly", func(t *testing.T) {
		t.Parallel()
		info := &sandbox.ProjectResourceInfo{}
		result := projectVMResourcesFromInfo(info)
		if !result.CanIncreaseDisk {
			t.Error("CanIncreaseDisk should always be true")
		}
		if result.CanDecreaseDisk {
			t.Error("CanDecreaseDisk should always be false")
		}
		if !result.CanChangeMemory {
			t.Error("CanChangeMemory should always be true")
		}
		if !result.RestartRequiredForDisk {
			t.Error("RestartRequiredForDisk should always be true")
		}
		if !result.RestartRequiredForMemory {
			t.Error("RestartRequiredForMemory should always be true")
		}
	})
}

// TestRequestValidationError verifies the error type.
func TestRequestValidationError(t *testing.T) {
	t.Parallel()

	err := newValidationError("test error message")
	if err.Error() != "test error message" {
		t.Errorf("expected 'test error message', got %q", err.Error())
	}

	validErr, ok := err.(*RequestValidationError)
	if !ok {
		t.Fatalf("expected *RequestValidationError, got %T", err)
	}
	if validErr.message != "test error message" {
		t.Errorf("expected message 'test error message', got %q", validErr.message)
	}
}

// TestProjectService_UpdateProjectResources_Validation checks validation rules.
func TestProjectService_UpdateProjectResources_Validation(t *testing.T) {
	t.Parallel()

	st := setupTestStore(t)
	ctx := context.Background()

	provider := mocksandbox.NewProvider()
	svc := NewProjectService(st, provider)

	// Test: no fields provided is rejected before any DB lookup.
	t.Run("no fields provided", func(t *testing.T) {
		t.Parallel()
		_, err := svc.UpdateProjectResources(ctx, "any-project", UpdateProjectResourcesRequest{})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "at least one resource must be provided") {
			t.Errorf("unexpected error: %q", err.Error())
		}
	})

	// Tests that require valid memoryMB/dataDiskGB values are against a real project.
	// Create it once for the remaining subtests.
	project := &model.Project{
		ID:   "test-project-resources",
		Name: "Test Project",
		Slug: "test-project",
	}
	if err := st.CreateProject(ctx, project); err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	tests := []struct {
		name       string
		req        UpdateProjectResourcesRequest
		wantErrMsg string
	}{
		{
			name: "negative memoryMB",
			req: UpdateProjectResourcesRequest{
				MemoryMB: new(-1024),
			},
			wantErrMsg: "memoryMB must be greater than 0",
		},
		{
			name: "zero memoryMB",
			req: UpdateProjectResourcesRequest{
				MemoryMB: new(0),
			},
			wantErrMsg: "memoryMB must be greater than 0",
		},
		{
			name: "memoryMB not aligned to GiB",
			req: UpdateProjectResourcesRequest{
				MemoryMB: new(1500),
			},
			wantErrMsg: "memoryMB must be a whole GiB multiple",
		},
		{
			name: "negative dataDiskGB",
			req: UpdateProjectResourcesRequest{
				DataDiskGB: new(-10),
			},
			wantErrMsg: "dataDiskGB must be greater than 0",
		},
		{
			name: "zero dataDiskGB",
			req: UpdateProjectResourcesRequest{
				DataDiskGB: new(0),
			},
			wantErrMsg: "dataDiskGB must be greater than 0",
		},
	}

	// Run these subtests sequentially (no t.Parallel) to share the same DB connection.
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.UpdateProjectResources(ctx, project.ID, tt.req)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErrMsg) {
				t.Errorf("expected error containing %q, got %q", tt.wantErrMsg, err.Error())
			}
		})
	}
}

// TestProjectService_UpdateProjectResources_DiskDecreaseRejected checks the disk
// decrease restriction (current disk is 100GB by default in the mock).
func TestProjectService_UpdateProjectResources_DiskDecreaseRejected(t *testing.T) {
	t.Parallel()

	st := setupTestStore(t)
	ctx := context.Background()

	project := &model.Project{
		ID:   "test-project-disk-decrease",
		Name: "Test Project",
		Slug: "test-proj-disk",
	}
	if err := st.CreateProject(ctx, project); err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	// Mock provider defaults to 100GB disk; trying to set 10GB should be rejected.
	provider := mocksandbox.NewProvider()
	svc := NewProjectService(st, provider)

	_, err := svc.UpdateProjectResources(ctx, project.ID, UpdateProjectResourcesRequest{
		DataDiskGB: new(10),
	})
	if err == nil {
		t.Fatal("expected error when decreasing disk size")
	}
	if !strings.Contains(err.Error(), "data disk size can only increase") {
		t.Errorf("expected 'data disk size can only increase' error, got %q", err.Error())
	}
}

// TestProjectService_AcceptInvitation_Expired verifies expired invitations are rejected.
func TestProjectService_AcceptInvitation_Expired(t *testing.T) {
	t.Parallel()

	st := setupTestStore(t)
	ctx := context.Background()

	project := &model.Project{
		ID:   "test-project-inv",
		Name: "Test Project",
		Slug: "test-proj-inv",
	}
	if err := st.CreateProject(ctx, project); err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	inviterID := "inviter-user"
	inv := &model.ProjectInvitation{
		ProjectID: project.ID,
		Email:     "test@example.com",
		Role:      "member",
		InvitedBy: &inviterID,
		Token:     "expired-token-123",
		ExpiresAt: time.Now().Add(-24 * time.Hour), // already expired
	}
	if err := st.CreateInvitation(ctx, inv); err != nil {
		t.Fatalf("failed to create invitation: %v", err)
	}

	provider := mocksandbox.NewProvider()
	svc := NewProjectService(st, provider)

	err := svc.AcceptInvitation(ctx, "expired-token-123", "new-user")
	if err == nil {
		t.Fatal("expected error for expired invitation")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Errorf("expected 'expired' in error, got %q", err.Error())
	}
}

// TestProjectService_AcceptInvitation_NotFound verifies missing tokens are rejected.
func TestProjectService_AcceptInvitation_NotFound(t *testing.T) {
	t.Parallel()

	st := setupTestStore(t)
	ctx := context.Background()
	provider := mocksandbox.NewProvider()
	svc := NewProjectService(st, provider)

	err := svc.AcceptInvitation(ctx, "nonexistent-token", "user-123")
	if err == nil {
		t.Fatal("expected error for nonexistent invitation token")
	}
}

// TestProjectService_CreateProject verifies project creation and slug generation.
func TestProjectService_CreateProject(t *testing.T) {
	t.Parallel()

	st := setupTestStore(t)
	ctx := context.Background()
	provider := mocksandbox.NewProvider()
	svc := NewProjectService(st, provider)

	project, err := svc.CreateProject(ctx, "user-123", "My Test Project")
	if err != nil {
		t.Fatalf("CreateProject failed: %v", err)
	}
	if project.ID == "" {
		t.Error("expected non-empty project ID")
	}
	if project.Name != "My Test Project" {
		t.Errorf("expected name 'My Test Project', got %q", project.Name)
	}
	if !strings.HasPrefix(project.Slug, "my-test-project-") {
		t.Errorf("expected slug to start with 'my-test-project-', got %q", project.Slug)
	}
	if project.CreatedAt.IsZero() {
		t.Error("expected non-zero CreatedAt")
	}
}

// TestProjectService_GetMemberRole verifies role lookup.
func TestProjectService_GetMemberRole(t *testing.T) {
	t.Parallel()

	st := setupTestStore(t)
	ctx := context.Background()
	provider := mocksandbox.NewProvider()
	svc := NewProjectService(st, provider)

	// Create project (which also creates the owner membership)
	project, err := svc.CreateProject(ctx, "owner-user", "Role Test")
	if err != nil {
		t.Fatalf("CreateProject failed: %v", err)
	}

	role, err := svc.GetMemberRole(ctx, project.ID, "owner-user")
	if err != nil {
		t.Fatalf("GetMemberRole failed: %v", err)
	}
	if role != "owner" {
		t.Errorf("expected role 'owner', got %q", role)
	}
}

// TestProjectService_ListMembers verifies listing project members.
func TestProjectService_ListMembers(t *testing.T) {
	t.Parallel()

	st := setupTestStore(t)
	ctx := context.Background()
	provider := mocksandbox.NewProvider()
	svc := NewProjectService(st, provider)

	project, err := svc.CreateProject(ctx, "owner-user", "Members Test")
	if err != nil {
		t.Fatalf("CreateProject failed: %v", err)
	}

	members, err := svc.ListMembers(ctx, project.ID)
	if err != nil {
		t.Fatalf("ListMembers failed: %v", err)
	}
	if len(members) != 1 {
		t.Fatalf("expected 1 member (owner), got %d", len(members))
	}
	if members[0].Role != "owner" {
		t.Errorf("expected member role 'owner', got %q", members[0].Role)
	}
	if members[0].UserID != "owner-user" {
		t.Errorf("expected user ID 'owner-user', got %q", members[0].UserID)
	}
}

// TestProjectService_UpdateProject verifies project name updates.
func TestProjectService_UpdateProject(t *testing.T) {
	t.Parallel()

	st := setupTestStore(t)
	ctx := context.Background()
	provider := mocksandbox.NewProvider()
	svc := NewProjectService(st, provider)

	project, err := svc.CreateProject(ctx, "user-123", "Original Name")
	if err != nil {
		t.Fatalf("CreateProject failed: %v", err)
	}

	updated, err := svc.UpdateProject(ctx, project.ID, "Updated Name")
	if err != nil {
		t.Fatalf("UpdateProject failed: %v", err)
	}
	if updated.Name != "Updated Name" {
		t.Errorf("expected name 'Updated Name', got %q", updated.Name)
	}
	if updated.ID != project.ID {
		t.Errorf("expected same project ID, got %q", updated.ID)
	}
	// Slug should remain unchanged
	if updated.Slug != project.Slug {
		t.Errorf("expected slug unchanged %q, got %q", project.Slug, updated.Slug)
	}
}

// TestProjectService_DeleteProject verifies project deletion.
func TestProjectService_DeleteProject(t *testing.T) {
	t.Parallel()

	st := setupTestStore(t)
	ctx := context.Background()
	provider := mocksandbox.NewProvider()
	svc := NewProjectService(st, provider)

	project, err := svc.CreateProject(ctx, "user-123", "Delete Me")
	if err != nil {
		t.Fatalf("CreateProject failed: %v", err)
	}

	if err := svc.DeleteProject(ctx, project.ID); err != nil {
		t.Fatalf("DeleteProject failed: %v", err)
	}

	// Should no longer be retrievable
	_, err = svc.GetProject(ctx, project.ID)
	if err == nil {
		t.Error("expected error when getting deleted project")
	}
}

// TestProjectService_ListProjects verifies listing projects for a user.
func TestProjectService_ListProjects(t *testing.T) {
	t.Parallel()

	st := setupTestStore(t)
	ctx := context.Background()
	provider := mocksandbox.NewProvider()
	svc := NewProjectService(st, provider)

	userID := "list-user"
	// Create multiple projects
	for _, name := range []string{"Project A", "Project B", "Project C"} {
		_, err := svc.CreateProject(ctx, userID, name)
		if err != nil {
			t.Fatalf("CreateProject(%s) failed: %v", name, err)
		}
	}

	projects, err := svc.ListProjects(ctx, userID)
	if err != nil {
		t.Fatalf("ListProjects failed: %v", err)
	}
	if len(projects) != 3 {
		t.Errorf("expected 3 projects, got %d", len(projects))
	}
}
