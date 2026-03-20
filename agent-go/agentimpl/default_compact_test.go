package agentimpl

import (
	"context"
	"iter"
	"testing"

	"github.com/obot-platform/discobot/agent-go/agent"
	"github.com/obot-platform/discobot/agent-go/message"
	"github.com/obot-platform/discobot/agent-go/providers"
	"github.com/obot-platform/discobot/agent-go/thread"
)

type compactCommandMockProvider struct {
	completeCalls int
}

func (m *compactCommandMockProvider) ID() string { return "mock" }

func (m *compactCommandMockProvider) Complete(_ context.Context, _ providers.CompleteRequest) iter.Seq2[message.ProviderMessageChunk, error] {
	m.completeCalls++
	return func(yield func(message.ProviderMessageChunk, error) bool) {
		yield(message.StreamStartChunk{}, nil)
		yield(message.TextStartChunk{ID: "s1"}, nil)
		yield(message.TextDeltaChunk{ID: "s1", Delta: "Compacted summary."}, nil)
		yield(message.TextEndChunk{ID: "s1"}, nil)
		yield(message.FinishChunk{FinishReason: message.FinishReason{Unified: "stop"}}, nil)
	}
}

func (m *compactCommandMockProvider) ListModels(_ context.Context) ([]providers.ModelInfo, error) {
	return nil, nil
}

func (m *compactCommandMockProvider) DefaultModels() map[string]providers.ModelRef {
	return map[string]providers.ModelRef{
		providers.ModelTaskChat: {ProviderID: "mock", ModelID: "test-model"},
	}
}

func TestPromptCompactCommand_ForceCompactsImmediately(t *testing.T) {
	store := thread.NewStore(t.TempDir())
	threadID := "thread-compact"

	messages := []thread.StoredMessage{
		{ID: "sys", Message: message.Message{Role: "system", Parts: []message.Part{message.TextPart{Text: "system"}}}},
		{ID: "msg1", ParentID: "sys", Message: message.Message{Role: "user", Parts: []message.Part{message.TextPart{Text: "hello"}}}},
	}
	for _, sm := range messages {
		if err := store.SaveMessage(threadID, sm); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SaveConfig(threadID, thread.Config{Model: "mock/test-model", ActiveLeafID: "msg1"}); err != nil {
		t.Fatal(err)
	}

	registry := providers.NewProviderRegistry(nil)
	mockProvider := &compactCommandMockProvider{}
	registry.Add(mockProvider)

	agentImpl := NewDefaultAgent(store, registry, nil, t.TempDir(), MCPConfig{})

	var deltas []string
	for chunk, err := range agentImpl.Prompt(context.Background(), threadID, agent.PromptRequest{
		UserParts: []message.UIPart{message.UITextPart{Text: "/compact"}},
	}) {
		if err != nil {
			t.Fatal(err)
		}
		if td, ok := chunk.(message.TextDeltaChunk); ok {
			deltas = append(deltas, td.Delta)
		}
	}

	if len(deltas) != 1 || deltas[0] != "Conversation compacted." {
		t.Fatalf("unexpected /compact response deltas: %#v", deltas)
	}
	if mockProvider.completeCalls != 1 {
		t.Fatalf("expected exactly one summary call, got %d", mockProvider.completeCalls)
	}

	record, err := store.LoadCompaction(threadID)
	if err != nil {
		t.Fatal(err)
	}
	if record == nil {
		t.Fatal("expected compaction record after /compact")
	}
	if record.SummaryText != "Compacted summary." {
		t.Fatalf("unexpected summary text %q", record.SummaryText)
	}
}

func TestPromptCompactCommand_NoHistory(t *testing.T) {
	store := thread.NewStore(t.TempDir())
	agentImpl := NewDefaultAgent(store, nil, nil, t.TempDir(), MCPConfig{})

	var deltas []string
	for chunk, err := range agentImpl.Prompt(context.Background(), "thread-empty", agent.PromptRequest{
		UserParts: []message.UIPart{message.UITextPart{Text: "/compact"}},
	}) {
		if err != nil {
			t.Fatal(err)
		}
		if td, ok := chunk.(message.TextDeltaChunk); ok {
			deltas = append(deltas, td.Delta)
		}
	}

	if len(deltas) != 1 || deltas[0] != "Nothing to compact yet." {
		t.Fatalf("unexpected /compact response deltas: %#v", deltas)
	}
}

func TestListCommands_IncludesCompactBuiltin(t *testing.T) {
	agentImpl := NewDefaultAgent(thread.NewStore(t.TempDir()), nil, nil, t.TempDir(), MCPConfig{})

	commands, err := agentImpl.ListCommands()
	if err != nil {
		t.Fatal(err)
	}

	foundClear := false
	foundCompact := false
	for _, cmd := range commands {
		if cmd.Name == "clear" {
			foundClear = true
		}
		if cmd.Name == "compact" {
			foundCompact = true
		}
	}

	if !foundClear {
		t.Fatal("expected clear built-in command")
	}
	if !foundCompact {
		t.Fatal("expected compact built-in command")
	}
}
