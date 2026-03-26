package modelsdev

import "testing"

func TestLookup(t *testing.T) {
	t.Run("known model", func(t *testing.T) {
		m := Lookup("openai", "gpt-4o")
		if m == nil {
			t.Fatal("expected gpt-4o to be found")
		}
		if m.Name == "" {
			t.Error("expected non-empty name")
		}
		if m.ContextWindow == 0 {
			t.Error("expected non-zero context window")
		}
		if m.MaxOutputTokens == 0 {
			t.Error("expected non-zero max output tokens")
		}
	})

	t.Run("reasoning model", func(t *testing.T) {
		m := Lookup("openai", "o3")
		if m == nil {
			t.Fatal("expected o3 to be found")
		}
		if !m.Reasoning {
			t.Error("expected o3 to have Reasoning=true")
		}
	})

	t.Run("custom tools capability from overlay", func(t *testing.T) {
		m := Lookup("openai", "gpt-5")
		if m == nil {
			t.Fatal("expected gpt-5 to be found")
		}
		if !m.CustomTools {
			t.Error("expected gpt-5 to support custom tools")
		}
	})

	t.Run("custom tools default false", func(t *testing.T) {
		m := Lookup("openai", "gpt-4o")
		if m == nil {
			t.Fatal("expected gpt-4o to be found")
		}
		if m.CustomTools {
			t.Error("expected gpt-4o to not support custom tools by default")
		}
	})

	t.Run("image modality support", func(t *testing.T) {
		m := Lookup("openai", "gpt-4o")
		if m == nil {
			t.Fatal("expected gpt-4o to be found")
		}
		if !m.SupportsInputModality("image") {
			t.Error("expected gpt-4o to support image input modality")
		}
		if m.SupportsInputModality("pdf") {
			t.Log("gpt-4o supports pdf input in current models metadata")
		}
	})

	t.Run("pdf modality support", func(t *testing.T) {
		m := Lookup("anthropic", "claude-3-7-sonnet-20250219")
		if m == nil {
			t.Fatal("expected claude-3-7-sonnet-20250219 to be found")
		}
		if !m.SupportsInputModality("pdf") {
			t.Error("expected claude-3-7-sonnet-20250219 to support pdf input modality")
		}
	})

	t.Run("unknown model", func(t *testing.T) {
		m := Lookup("openai", "nonexistent-model")
		if m != nil {
			t.Error("expected nil for unknown model")
		}
	})

	t.Run("unknown provider", func(t *testing.T) {
		m := Lookup("nonexistent-provider", "gpt-4o")
		if m != nil {
			t.Error("expected nil for unknown provider")
		}
	})
}

func TestAllForProvider(t *testing.T) {
	t.Run("returns models for known provider", func(t *testing.T) {
		models := AllForProvider("openai")
		if len(models) == 0 {
			t.Fatal("expected models for openai")
		}
		// Verify at least one model has full metadata.
		found := false
		for _, m := range models {
			if m.ID == "gpt-4o" && m.ContextWindow > 0 {
				found = true
			}
		}
		if !found {
			t.Error("expected gpt-4o with context window in results")
		}
	})

	t.Run("returns nil for unknown provider", func(t *testing.T) {
		models := AllForProvider("nonexistent")
		if models != nil {
			t.Errorf("expected nil, got %d models", len(models))
		}
	})
}
