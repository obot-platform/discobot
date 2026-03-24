package handler

import (
	"testing"

	"github.com/obot-platform/discobot/server/internal/model"
	"github.com/obot-platform/discobot/server/internal/service"
)

func TestDeriveSessionStatusAndError_MapsCompletedCommitOperation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		session    *service.Session
		wantStatus string
	}{
		{
			name: "commit completion maps to committed",
			session: &service.Session{
				Status:          "ready",
				CommitStatus:    "completed",
				CommitOperation: service.CommitOperationCommit,
			},
			wantStatus: "committed",
		},
		{
			name: "rebase completion maps to rebased",
			session: &service.Session{
				Status:          "ready",
				CommitStatus:    "completed",
				CommitOperation: service.CommitOperationRebase,
			},
			wantStatus: "rebased",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotStatus, gotError := deriveSessionStatusAndError(tt.session)
			if gotStatus != tt.wantStatus {
				t.Fatalf("deriveSessionStatusAndError() status = %q, want %q", gotStatus, tt.wantStatus)
			}
			if gotError != "" {
				t.Fatalf("deriveSessionStatusAndError() error = %q, want empty", gotError)
			}
		})
	}
}

func TestDeriveSessionStatusAndError_RemovingOverridesDerivedCommitStates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		session    *service.Session
		wantStatus string
	}{
		{
			name: "removing overrides committed",
			session: &service.Session{
				Status:          model.SessionStatusRemoving,
				CommitStatus:    model.CommitStatusCompleted,
				CommitOperation: service.CommitOperationCommit,
			},
			wantStatus: model.SessionStatusRemoving,
		},
		{
			name: "removing overrides commit failure",
			session: &service.Session{
				Status:          model.SessionStatusRemoving,
				CommitStatus:    model.CommitStatusFailed,
				CommitOperation: service.CommitOperationCommit,
				CommitError:     "commit failed",
			},
			wantStatus: model.SessionStatusRemoving,
		},
		{
			name: "removed overrides completed rebase",
			session: &service.Session{
				Status:          model.SessionStatusRemoved,
				CommitStatus:    model.CommitStatusCompleted,
				CommitOperation: service.CommitOperationRebase,
			},
			wantStatus: model.SessionStatusRemoved,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotStatus, gotError := deriveSessionStatusAndError(tt.session)
			if gotStatus != tt.wantStatus {
				t.Fatalf("deriveSessionStatusAndError() status = %q, want %q", gotStatus, tt.wantStatus)
			}
			if gotError != "" {
				t.Fatalf("deriveSessionStatusAndError() error = %q, want empty", gotError)
			}
		})
	}
}
