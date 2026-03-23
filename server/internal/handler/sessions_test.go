package handler

import (
	"testing"

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
