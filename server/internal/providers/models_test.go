package providers

import (
	"strings"
	"testing"
)

func TestGetModelsForProvidersUsesProviderSpecificModels(t *testing.T) {
	models, err := GetModelsForProviders([]string{"codex"})
	if err != nil {
		t.Fatalf("GetModelsForProviders returned error: %v", err)
	}
	if len(models) == 0 {
		t.Fatal("expected codex models")
	}

	for _, model := range models {
		if !strings.HasPrefix(model.ID, "codex/") {
			t.Fatalf("expected codex-qualified model ID, got %q", model.ID)
		}
		if model.Provider != "ChatGPT Codex" {
			t.Fatalf("expected codex provider display name, got %q", model.Provider)
		}
	}
}

func TestIsProviderModelToolCallableUsesProviderSpecificMetadata(t *testing.T) {
	if !IsProviderModelToolCallable("codex", "codex/gpt-5.4") {
		t.Fatal("expected codex/gpt-5.4 to be tool callable")
	}

	if IsProviderModelToolCallable("codex", "openai/gpt-4o") {
		t.Fatal("expected openai/gpt-4o not to be treated as a codex model")
	}
}
