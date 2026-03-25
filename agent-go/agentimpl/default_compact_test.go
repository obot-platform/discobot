package agentimpl

import (
	"context"
	"iter"
	"os"
	"path/filepath"
	"testing"

	"github.com/obot-platform/discobot/agent-go/agent"
	"github.com/obot-platform/discobot/agent-go/message"
	"github.com/obot-platform/discobot/agent-go/providers"
	"github.com/obot-platform/discobot/agent-go/thread"
)

type compactCommandMockProvider struct {
	completeCalls int
	responses     []string
	requests      []providers.CompleteRequest
}

func (m *compactCommandMockProvider) ID() string { return "mock" }

func (m *compactCommandMockProvider) Complete(_ context.Context, req providers.CompleteRequest) iter.Seq2[message.ProviderMessageChunk, error] {
	m.requests = append(m.requests, req)
	m.completeCalls++
	text := "Compacted summary."
	if i := m.completeCalls - 1; i >= 0 && i < len(m.responses) {
		text = m.responses[i]
	}
	return func(yield func(message.ProviderMessageChunk, error) bool) {
		yield(message.StreamStartChunk{}, nil)
		yield(message.TextStartChunk{ID: "s1"}, nil)
		yield(message.TextDeltaChunk{ID: "s1", Delta: text}, nil)
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

type modelResolutionMockProvider struct {
	id       string
	defaults map[string]providers.ModelRef
	requests []providers.CompleteRequest
	response string
}

func (m *modelResolutionMockProvider) ID() string { return m.id }

func (m *modelResolutionMockProvider) Complete(_ context.Context, req providers.CompleteRequest) iter.Seq2[message.ProviderMessageChunk, error] {
	m.requests = append(m.requests, req)
	text := m.response
	if text == "" {
		text = "ok"
	}
	return func(yield func(message.ProviderMessageChunk, error) bool) {
		yield(message.StreamStartChunk{}, nil)
		yield(message.TextStartChunk{ID: "s1"}, nil)
		yield(message.TextDeltaChunk{ID: "s1", Delta: text}, nil)
		yield(message.TextEndChunk{ID: "s1"}, nil)
		yield(message.FinishChunk{FinishReason: message.FinishReason{Unified: "stop"}}, nil)
	}
}

func (m *modelResolutionMockProvider) ListModels(_ context.Context) ([]providers.ModelInfo, error) {
	return nil, nil
}

func (m *modelResolutionMockProvider) DefaultModels() map[string]providers.ModelRef {
	return m.defaults
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

func TestPrompt_GeneratesThreadNameBeforeAssistantResponse(t *testing.T) {
	store := thread.NewStore(t.TempDir())
	registry := providers.NewProviderRegistry(nil)
	mockProvider := &compactCommandMockProvider{responses: []string{
		"Agent-go thread naming fix",
		"Assistant response.",
	}}
	registry.Add(mockProvider)

	agentImpl := NewDefaultAgent(store, registry, nil, t.TempDir(), MCPConfig{})
	threadID := "thread-generated-name"

	var chunks []message.MessageChunk
	for chunk, err := range agentImpl.Prompt(context.Background(), threadID, agent.PromptRequest{
		UserParts: []message.UIPart{message.UITextPart{Text: "Fix thread naming in agent-go", State: "done"}},
	}) {
		if err != nil {
			t.Fatal(err)
		}
		if chunk != nil {
			chunks = append(chunks, chunk)
		}
	}

	if len(chunks) == 0 {
		t.Fatal("expected streamed chunks")
	}
	nameChunk, ok := chunks[0].(message.ThreadNameChunk)
	if !ok {
		t.Fatalf("expected first chunk to be ThreadNameChunk, got %T", chunks[0])
	}
	if nameChunk.Data.Name != "Agent-go thread naming fix" {
		t.Fatalf("unexpected generated thread name %q", nameChunk.Data.Name)
	}

	cfg, err := store.LoadConfig(threadID)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Name != "Agent-go thread naming fix" {
		t.Fatalf("expected persisted name, got %q", cfg.Name)
	}
	if cfg.NameSource != thread.ThreadNameSourceGenerated {
		t.Fatalf("expected generated name source, got %q", cfg.NameSource)
	}
}

func TestPrompt_FallsBackToGeneratedThreadNameWhenAIReturnsEmpty(t *testing.T) {
	store := thread.NewStore(t.TempDir())
	registry := providers.NewProviderRegistry(nil)
	mockProvider := &compactCommandMockProvider{responses: []string{
		"",
		"Assistant response.",
	}}
	registry.Add(mockProvider)

	agentImpl := NewDefaultAgent(store, registry, nil, t.TempDir(), MCPConfig{})
	threadID := "thread-fallback-name"

	var chunks []message.MessageChunk
	for chunk, err := range agentImpl.Prompt(context.Background(), threadID, agent.PromptRequest{
		UserParts: []message.UIPart{message.UITextPart{Text: "Fix thread naming in agent-go", State: "done"}},
	}) {
		if err != nil {
			t.Fatal(err)
		}
		if chunk != nil {
			chunks = append(chunks, chunk)
		}
	}

	if len(chunks) == 0 {
		t.Fatal("expected streamed chunks")
	}
	nameChunk, ok := chunks[0].(message.ThreadNameChunk)
	if !ok {
		t.Fatalf("expected first chunk to be ThreadNameChunk, got %T", chunks[0])
	}
	if nameChunk.Data.Name != "Fix thread naming in agent-go" {
		t.Fatalf("unexpected fallback generated thread name %q", nameChunk.Data.Name)
	}

	cfg, err := store.LoadConfig(threadID)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Name != "Fix thread naming in agent-go" {
		t.Fatalf("expected persisted fallback name, got %q", cfg.Name)
	}
	if cfg.NameSource != thread.ThreadNameSourceGenerated {
		t.Fatalf("expected generated name source, got %q", cfg.NameSource)
	}
}

func TestPrompt_SubAgentModelResolvesSupportingTypeOnCurrentProvider(t *testing.T) {
	store := thread.NewStore(t.TempDir())
	threadID := "thread-subagent-relative"
	cwd := t.TempDir()
	agentsDir := filepath.Join(cwd, ".claude", "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentsDir, "reviewer.md"), []byte(`---
name: reviewer
model: thread_summarization
---
Use a smaller summary model.`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveConfig(threadID, thread.Config{
		Name:       "Pinned",
		NameSource: thread.ThreadNameSourceUser,
	}); err != nil {
		t.Fatal(err)
	}

	registry := providers.NewProviderRegistry(nil)
	openaiProvider := &modelResolutionMockProvider{
		id: "openai",
		defaults: map[string]providers.ModelRef{
			providers.ModelTaskChat:                {ProviderID: "openai", ModelID: "gpt-5.4"},
			providers.ModelTaskThreadSummarization: {ProviderID: "openai", ModelID: "gpt-5.4-nano"},
		},
	}
	registry.Add(openaiProvider)

	agentImpl := NewDefaultAgent(store, registry, nil, cwd, MCPConfig{})
	for _, err := range agentImpl.Prompt(context.Background(), threadID, agent.PromptRequest{
		Model:        "openai",
		SubagentType: "reviewer",
		UserParts:    []message.UIPart{message.UITextPart{Text: "hello", State: "done"}},
	}) {
		if err != nil {
			t.Fatal(err)
		}
	}

	if len(openaiProvider.requests) != 1 {
		t.Fatalf("expected 1 provider request, got %d", len(openaiProvider.requests))
	}
	if got := openaiProvider.requests[0].Model.String(); got != "openai/gpt-5.4-nano" {
		t.Fatalf("expected sub-agent model to resolve to current provider summary default, got %q", got)
	}
}

func TestPrompt_SubAgentModelSupportsCrossProviderRef(t *testing.T) {
	store := thread.NewStore(t.TempDir())
	threadID := "thread-subagent-cross-provider"
	cwd := t.TempDir()
	agentsDir := filepath.Join(cwd, ".claude", "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentsDir, "reviewer.md"), []byte(`---
name: reviewer
model: anthropic/claude-sonnet-4-6
---
Use another provider.`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveConfig(threadID, thread.Config{
		Name:       "Pinned",
		NameSource: thread.ThreadNameSourceUser,
	}); err != nil {
		t.Fatal(err)
	}

	registry := providers.NewProviderRegistry(nil)
	openaiProvider := &modelResolutionMockProvider{
		id: "openai",
		defaults: map[string]providers.ModelRef{
			providers.ModelTaskChat: {ProviderID: "openai", ModelID: "gpt-5.4"},
		},
	}
	anthropicProvider := &modelResolutionMockProvider{id: "anthropic"}
	registry.Add(openaiProvider)
	registry.Add(anthropicProvider)

	agentImpl := NewDefaultAgent(store, registry, nil, cwd, MCPConfig{})
	for _, err := range agentImpl.Prompt(context.Background(), threadID, agent.PromptRequest{
		Model:        "openai",
		SubagentType: "reviewer",
		UserParts:    []message.UIPart{message.UITextPart{Text: "hello", State: "done"}},
	}) {
		if err != nil {
			t.Fatal(err)
		}
	}

	if len(openaiProvider.requests) != 0 {
		t.Fatalf("expected openai provider to be unused, got %d calls", len(openaiProvider.requests))
	}
	if len(anthropicProvider.requests) != 1 {
		t.Fatalf("expected anthropic provider to receive 1 request, got %d", len(anthropicProvider.requests))
	}
	if got := anthropicProvider.requests[0].Model.String(); got != "anthropic/claude-sonnet-4-6" {
		t.Fatalf("unexpected cross-provider model ref %q", got)
	}
}
