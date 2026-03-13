package providers

import (
	"context"
	"iter"
	"reflect"
	"testing"

	"github.com/obot-platform/discobot/agent-go/message"
)

// mockProvider is a minimal Provider implementation for testing.
type mockProvider struct {
	id string
}

func (p *mockProvider) ID() string { return p.id }

func (p *mockProvider) Complete(_ context.Context, _ CompleteRequest) iter.Seq2[message.ProviderMessageChunk, error] {
	return func(_ func(message.ProviderMessageChunk, error) bool) {}
}

func (p *mockProvider) DefaultModels() map[string]ModelRef { return nil }
func (p *mockProvider) ListModels(_ context.Context) ([]ModelInfo, error) {
	return nil, nil
}

func TestRegisterAndNew(t *testing.T) {
	// Use a unique ID to avoid conflicts with other tests.
	const id = "test-register-and-new"
	Register(id, func(_ Config) (Provider, error) {
		return &mockProvider{id: id}, nil
	})

	p, err := New(id, Config{"api_key": "sk-test"})
	if err != nil {
		t.Fatalf("New(%q) error: %v", id, err)
	}
	if p.ID() != id {
		t.Errorf("ID() = %q, want %q", p.ID(), id)
	}
}

func TestNewUnknownProvider(t *testing.T) {
	_, err := New("nonexistent-provider-xyz", nil)
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

func TestRegisterDuplicatePanics(t *testing.T) {
	const id = "test-duplicate-panic"
	Register(id, func(_ Config) (Provider, error) {
		return &mockProvider{id: id}, nil
	})

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on duplicate registration")
		}
	}()

	Register(id, func(_ Config) (Provider, error) {
		return &mockProvider{id: id}, nil
	})
}

func TestHas(t *testing.T) {
	const id = "test-has"
	if Has(id) {
		t.Fatalf("Has(%q) = true before registration", id)
	}

	Register(id, func(_ Config) (Provider, error) {
		return &mockProvider{id: id}, nil
	})

	if !Has(id) {
		t.Errorf("Has(%q) = false after registration", id)
	}
}

func TestRegisteredIDs(t *testing.T) {
	// Register a few providers with predictable names.
	for _, id := range []string{"test-ids-charlie", "test-ids-alpha", "test-ids-bravo"} {
		Register(id, func(_ Config) (Provider, error) {
			return &mockProvider{}, nil
		})
	}

	ids := RegisteredIDs()

	// Verify the test IDs are present and sorted.
	want := []string{"test-ids-alpha", "test-ids-bravo", "test-ids-charlie"}
	var got []string
	for _, id := range ids {
		for _, w := range want {
			if id == w {
				got = append(got, id)
			}
		}
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("filtered RegisteredIDs = %v, want %v", got, want)
	}
}

func TestConfigConvenienceMethods(t *testing.T) {
	cfg := Config{
		"api_key":  "sk-123",
		"base_url": "https://api.example.com",
	}

	if cfg.APIKey() != "sk-123" {
		t.Errorf("APIKey() = %q", cfg.APIKey())
	}
	if cfg.BaseURL() != "https://api.example.com" {
		t.Errorf("BaseURL() = %q", cfg.BaseURL())
	}

	// Missing keys return empty string.
	empty := Config{}
	if empty.APIKey() != "" {
		t.Errorf("empty APIKey() = %q", empty.APIKey())
	}
	if empty.BaseURL() != "" {
		t.Errorf("empty BaseURL() = %q", empty.BaseURL())
	}
}

func TestFactoryReceivesConfig(t *testing.T) {
	const id = "test-factory-config"
	Register(id, func(cfg Config) (Provider, error) {
		if cfg.APIKey() != "my-key" {
			t.Errorf("factory received APIKey = %q, want %q", cfg.APIKey(), "my-key")
		}
		return &mockProvider{id: id}, nil
	})

	_, err := New(id, Config{"api_key": "my-key"})
	if err != nil {
		t.Fatal(err)
	}
}
