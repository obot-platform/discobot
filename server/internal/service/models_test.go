package service

import (
	"context"
	"testing"

	"github.com/obot-platform/discobot/server/internal/config"
)

func TestGetModelsForProject_SkipsInactiveCredentials(t *testing.T) {
	st := setupTestStore(t)
	cfg := &config.Config{
		EncryptionKey: []byte("test-key-32-bytes-long-123456789"),
	}

	credSvc, err := NewCredentialService(st, cfg)
	if err != nil {
		t.Fatalf("Failed to create credential service: %v", err)
	}

	modelsSvc := NewModelsService(st, credSvc, nil)
	ctx := context.Background()
	projectID := "test-project"

	_, err = credSvc.SetAPIKeyWithMetadata(
		ctx,
		projectID,
		ProviderAnthropic,
		"Anthropic",
		"",
		"sk-ant-test-123",
		false,
		false,
	)
	if err != nil {
		t.Fatalf("Failed to create active credential: %v", err)
	}

	_, err = credSvc.SetAPIKeyWithMetadata(
		ctx,
		projectID,
		ProviderOpenAI,
		"OpenAI",
		"",
		"sk-openai-test-123",
		false,
		true,
	)
	if err != nil {
		t.Fatalf("Failed to create inactive credential: %v", err)
	}

	models, err := modelsSvc.GetModelsForProject(ctx, projectID)
	if err != nil {
		t.Fatalf("GetModelsForProject returned error: %v", err)
	}
	if len(models) == 0 {
		t.Fatal("expected at least one model from active provider")
	}

	for _, model := range models {
		if model.Provider == "OpenAI" || model.Provider == "openai" {
			t.Fatalf("expected inactive openai credential to be skipped, but got model %q", model.ID)
		}
	}

	foundAnthropic := false
	for _, model := range models {
		if model.Provider == "Anthropic" || model.Provider == "anthropic" {
			foundAnthropic = true
			break
		}
	}
	if !foundAnthropic {
		t.Fatal("expected active anthropic credential to contribute models")
	}
}
