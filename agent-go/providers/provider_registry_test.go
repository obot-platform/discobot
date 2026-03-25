package providers

import (
	"context"
	"iter"
	"testing"

	"github.com/obot-platform/discobot/agent-go/message"
)

// testProvider is a minimal Provider implementation for registry tests.
type testProvider struct {
	id       string
	models   []ModelInfo
	defaults map[string]ModelRef
}

func (p *testProvider) ID() string { return p.id }
func (p *testProvider) Complete(_ context.Context, _ CompleteRequest) iter.Seq2[message.ProviderMessageChunk, error] {
	return func(_ func(message.ProviderMessageChunk, error) bool) {}
}
func (p *testProvider) DefaultModels() map[string]ModelRef { return p.defaults }
func (p *testProvider) ListModels(_ context.Context) ([]ModelInfo, error) {
	return p.models, nil
}

func TestProviderRegistry_Add_Get(t *testing.T) {
	r := NewProviderRegistry(nil)
	p := &testProvider{id: "anthropic"}
	r.Add(p)

	got, err := r.Get("anthropic")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID() != "anthropic" {
		t.Errorf("expected 'anthropic', got %q", got.ID())
	}
}

func TestProviderRegistry_Get_NotFound(t *testing.T) {
	r := NewProviderRegistry(nil)
	_, err := r.Get("missing")
	if err == nil {
		t.Error("expected error for missing provider")
	}
}

func TestProviderRegistry_Add_DuplicatePanics(t *testing.T) {
	r := NewProviderRegistry(nil)
	r.Add(&testProvider{id: "anthropic"})

	defer func() {
		if recover() == nil {
			t.Error("expected panic on duplicate registration")
		}
	}()
	r.Add(&testProvider{id: "anthropic"})
}

func TestProviderRegistry_Resolve(t *testing.T) {
	r := NewProviderRegistry(nil)
	r.Add(&testProvider{id: "anthropic"})

	p, modelID, err := r.Resolve("anthropic/claude-sonnet-4")
	if err != nil {
		t.Fatal(err)
	}
	if p.ID() != "anthropic" {
		t.Errorf("expected provider 'anthropic', got %q", p.ID())
	}
	if modelID != "claude-sonnet-4" {
		t.Errorf("expected model 'claude-sonnet-4', got %q", modelID)
	}
}

func TestProviderRegistry_Resolve_UnknownProvider(t *testing.T) {
	r := NewProviderRegistry(nil)
	_, _, err := r.Resolve("unknown/model")
	if err == nil {
		t.Error("expected error for unknown provider")
	}
}

func TestProviderRegistry_Resolve_InvalidRef(t *testing.T) {
	r := NewProviderRegistry(nil)
	_, _, err := r.Resolve("no-slash")
	if err == nil {
		t.Error("expected error for invalid model ref")
	}
}

func TestProviderRegistry_ListModels(t *testing.T) {
	r := NewProviderRegistry(nil)
	r.Add(&testProvider{
		id: "anthropic",
		models: []ModelInfo{
			{ID: "claude-sonnet-4", DisplayName: "Claude Sonnet 4"},
			{ID: "claude-opus-4", DisplayName: "Claude Opus 4", Reasoning: true},
		},
	})
	r.Add(&testProvider{
		id: "openai",
		models: []ModelInfo{
			{ID: "gpt-4o", DisplayName: "GPT-4o"},
		},
	})

	models, err := r.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(models) != 3 {
		t.Fatalf("expected 3 models, got %d", len(models))
	}

	// Results should be sorted by provider ID (anthropic before openai).
	expected := []struct {
		id         string
		providerID string
	}{
		{"anthropic/claude-sonnet-4", "anthropic"},
		{"anthropic/claude-opus-4", "anthropic"},
		{"openai/gpt-4o", "openai"},
	}

	for i, e := range expected {
		if models[i].ID != e.id {
			t.Errorf("model[%d]: expected ID %q, got %q", i, e.id, models[i].ID)
		}
		if models[i].ProviderID != e.providerID {
			t.Errorf("model[%d]: expected ProviderID %q, got %q", i, e.providerID, models[i].ProviderID)
		}
	}
}

func TestProviderRegistry_IDs(t *testing.T) {
	r := NewProviderRegistry(nil)
	r.Add(&testProvider{id: "openai"})
	r.Add(&testProvider{id: "anthropic"})

	ids := r.IDs()
	if len(ids) != 2 {
		t.Fatalf("expected 2 IDs, got %d", len(ids))
	}
	// Should be sorted.
	if ids[0] != "anthropic" || ids[1] != "openai" {
		t.Errorf("expected [anthropic, openai], got %v", ids)
	}
}

func TestProviderRegistry_ResolveModel_NoProvidersAvailable(t *testing.T) {
	r := NewProviderRegistry(nil)

	_, err := r.ResolveModel("", ModelTaskChat)
	if err == nil {
		t.Fatal("expected error when no providers are available")
	}
	if got := err.Error(); got != "no model providers are available; configure a provider, set MODEL, or pass --model" {
		t.Fatalf("unexpected error: %q", got)
	}
}

func TestProviderRegistry_ResolveModel_NoDefaultIncludesAvailableProviders(t *testing.T) {
	r := NewProviderRegistry(nil)
	r.Add(&testProvider{id: "openai"})
	r.Add(&testProvider{id: "anthropic"})

	_, err := r.ResolveModel("", ModelTaskChat)
	if err == nil {
		t.Fatal("expected error when no provider has a default model")
	}
	if got := err.Error(); got != `no provider available with a default "chat" model; available providers: anthropic, openai; set MODEL or pass --model` {
		t.Fatalf("unexpected error: %q", got)
	}
}

func TestProviderRegistry_ResolveModelInProvider_UsesCurrentProviderForBareModel(t *testing.T) {
	r := NewProviderRegistry(nil)

	got, err := r.ResolveModelInProvider("openai", "gpt-5.4-nano", ModelTaskChat)
	if err != nil {
		t.Fatal(err)
	}
	if got != (ModelRef{ProviderID: "openai", ModelID: "gpt-5.4-nano"}) {
		t.Fatalf("unexpected model ref: %+v", got)
	}
}

func TestProviderRegistry_ResolveModelInProvider_UsesSupportingTypeOnCurrentProvider(t *testing.T) {
	r := NewProviderRegistry(nil)
	r.Add(&testProvider{
		id: "openai",
		defaults: map[string]ModelRef{
			ModelTaskChat:                {ProviderID: "openai", ModelID: "gpt-5.4"},
			ModelTaskThreadSummarization: {ProviderID: "openai", ModelID: "gpt-5.4-nano"},
		},
	})

	got, err := r.ResolveModelInProvider("openai", "thread_summarization", ModelTaskChat)
	if err != nil {
		t.Fatal(err)
	}
	if got != (ModelRef{ProviderID: "openai", ModelID: "gpt-5.4-nano"}) {
		t.Fatalf("unexpected model ref: %+v", got)
	}
}

func TestProviderRegistry_ResolveModelInProvider_PrefersProviderDefaultForProviderID(t *testing.T) {
	r := NewProviderRegistry(nil)
	r.Add(&testProvider{
		id: "openai",
		defaults: map[string]ModelRef{
			ModelTaskChat: {ProviderID: "openai", ModelID: "gpt-5.4"},
		},
	})

	got, err := r.ResolveModelInProvider("anthropic", "openai", ModelTaskChat)
	if err != nil {
		t.Fatal(err)
	}
	if got != (ModelRef{ProviderID: "openai", ModelID: "gpt-5.4"}) {
		t.Fatalf("unexpected model ref: %+v", got)
	}
}

func TestProviderRegistry_ResolveSupportingModel_UsesOverride(t *testing.T) {
	r := NewProviderRegistry(nil)
	r.Add(&testProvider{
		id: "openai",
		defaults: map[string]ModelRef{
			ModelTaskChat:                {ProviderID: "openai", ModelID: "gpt-5.4"},
			ModelTaskThreadSummarization: {ProviderID: "openai", ModelID: "gpt-5.4-mini"},
		},
	})
	r.Add(&testProvider{id: "anthropic"})

	got, err := r.ResolveSupportingModel(
		ModelRef{ProviderID: "openai", ModelID: "gpt-5.4"},
		SupportingModels{SupportingModelThreadSummarization: "anthropic/claude-sonnet-4-6"},
		SupportingModelThreadSummarization,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got != (ModelRef{ProviderID: "anthropic", ModelID: "claude-sonnet-4-6"}) {
		t.Fatalf("unexpected supporting model: %+v", got)
	}
}

func TestProviderRegistry_ResolveSupportingModel_UsesCurrentProviderForBareModelOverride(t *testing.T) {
	r := NewProviderRegistry(nil)

	got, err := r.ResolveSupportingModel(
		ModelRef{ProviderID: "openai", ModelID: "gpt-5.4"},
		SupportingModels{SupportingModelThreadSummarization: "gpt-5.4-nano"},
		SupportingModelThreadSummarization,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got != (ModelRef{ProviderID: "openai", ModelID: "gpt-5.4-nano"}) {
		t.Fatalf("unexpected supporting model: %+v", got)
	}
}

func TestProviderRegistry_ResolveSupportingModel_UsesSupportingTypeOverride(t *testing.T) {
	r := NewProviderRegistry(nil)
	r.Add(&testProvider{
		id: "openai",
		defaults: map[string]ModelRef{
			ModelTaskChat:                {ProviderID: "openai", ModelID: "gpt-5.4"},
			ModelTaskThreadSummarization: {ProviderID: "openai", ModelID: "gpt-5.4-nano"},
		},
	})

	got, err := r.ResolveSupportingModel(
		ModelRef{ProviderID: "openai", ModelID: "gpt-5.4"},
		SupportingModels{SupportingModelThreadSummarization: "thread_summarization"},
		SupportingModelThreadSummarization,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got != (ModelRef{ProviderID: "openai", ModelID: "gpt-5.4-nano"}) {
		t.Fatalf("unexpected supporting model: %+v", got)
	}
}

func TestProviderRegistry_ResolveSupportingModel_UsesProviderDefault(t *testing.T) {
	r := NewProviderRegistry(nil)
	r.Add(&testProvider{
		id: "openai",
		defaults: map[string]ModelRef{
			ModelTaskChat:                {ProviderID: "openai", ModelID: "gpt-5.4"},
			ModelTaskThreadSummarization: {ProviderID: "openai", ModelID: "gpt-5.4-mini"},
		},
	})

	got, err := r.ResolveSupportingModel(
		ModelRef{ProviderID: "openai", ModelID: "gpt-5.4"},
		nil,
		SupportingModelThreadSummarization,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got != (ModelRef{ProviderID: "openai", ModelID: "gpt-5.4-mini"}) {
		t.Fatalf("unexpected supporting model: %+v", got)
	}
}

func TestProviderRegistry_ResolveSupportingModel_FallsBackToMainModel(t *testing.T) {
	r := NewProviderRegistry(nil)
	r.Add(&testProvider{
		id: "openai",
		defaults: map[string]ModelRef{
			ModelTaskChat: {ProviderID: "openai", ModelID: "gpt-5.4"},
		},
	})

	got, err := r.ResolveSupportingModel(
		ModelRef{ProviderID: "openai", ModelID: "gpt-5.4"},
		nil,
		SupportingModelThreadSummarization,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got != (ModelRef{ProviderID: "openai", ModelID: "gpt-5.4"}) {
		t.Fatalf("unexpected supporting model: %+v", got)
	}
}
