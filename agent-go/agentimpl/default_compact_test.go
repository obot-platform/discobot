package agentimpl

import (
	"context"
	"encoding/json"
	"iter"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/obot-platform/discobot/agent-go/agent"
	"github.com/obot-platform/discobot/agent-go/message"
	"github.com/obot-platform/discobot/agent-go/providers"
	"github.com/obot-platform/discobot/agent-go/thread"
)

type compactCommandMockProvider struct {
	mu             sync.Mutex
	completeCalls  int
	responses      []string
	requests       []providers.CompleteRequest
	beforeRespond  func(req providers.CompleteRequest)
	beforeChunk    func(req providers.CompleteRequest, chunkIndex int)
	responseForReq func(req providers.CompleteRequest) string
}

func (m *compactCommandMockProvider) ID() string { return "mock" }

func (m *compactCommandMockProvider) Complete(_ context.Context, req providers.CompleteRequest) iter.Seq2[message.ProviderMessageChunk, error] {
	m.mu.Lock()
	m.requests = append(m.requests, req)
	m.completeCalls++
	completeCalls := m.completeCalls
	beforeRespond := m.beforeRespond
	beforeChunk := m.beforeChunk
	responseForReq := m.responseForReq
	text := "Compacted summary."
	if responseForReq != nil {
		text = responseForReq(req)
	} else if i := completeCalls - 1; i >= 0 && i < len(m.responses) {
		text = m.responses[i]
	}
	m.mu.Unlock()
	return func(yield func(message.ProviderMessageChunk, error) bool) {
		if beforeRespond != nil {
			beforeRespond(req)
		}
		emit := func(chunkIndex int, chunk message.ProviderMessageChunk) bool {
			if beforeChunk != nil {
				beforeChunk(req, chunkIndex)
			}
			return yield(chunk, nil)
		}
		if !emit(0, message.StreamStartChunk{}) {
			return
		}
		if !emit(1, message.TextStartChunk{ID: "s1"}) {
			return
		}
		if !emit(2, message.TextDeltaChunk{ID: "s1", Delta: text}) {
			return
		}
		if !emit(3, message.TextEndChunk{ID: "s1"}) {
			return
		}
		emit(4, message.FinishChunk{FinishReason: message.FinishReason{Unified: "stop"}})
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

func TestPromptLegacyCommand_PreservesOriginalTextInUserMessageMetadata(t *testing.T) {
	root := t.TempDir()
	commandsDir := filepath.Join(root, ".claude", "commands")
	if err := os.MkdirAll(commandsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(commandsDir, "commit.md"),
		[]byte("---\ndescription: Commit work.\n---\nExpanded command body."),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	store := thread.NewStore(t.TempDir())
	registry := providers.NewProviderRegistry(nil)
	registry.Add(&compactCommandMockProvider{responses: []string{"Done."}})
	agentImpl := NewDefaultAgent(store, registry, nil, root, MCPConfig{})

	var userChunk message.UserMessageChunk
	foundUserChunk := false
	for chunk, err := range agentImpl.Prompt(context.Background(), "thread-legacy-command", agent.PromptRequest{
		UserParts: []message.UIPart{message.UITextPart{Text: "/commit fix the bug", State: "done"}},
	}) {
		if err != nil {
			t.Fatal(err)
		}
		if typed, ok := chunk.(message.UserMessageChunk); ok {
			userChunk = typed
			foundUserChunk = true
			break
		}
	}
	if !foundUserChunk {
		t.Fatal("expected user message chunk")
	}

	var metadata struct {
		OriginalText string `json:"originalText"`
	}
	if err := json.Unmarshal(userChunk.Data.Message.Metadata, &metadata); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}
	if metadata.OriginalText != "/commit fix the bug" {
		t.Fatalf("originalText = %q", metadata.OriginalText)
	}
	textPart, ok := userChunk.Data.Message.Parts[0].(message.UITextPart)
	if !ok {
		t.Fatalf("expected first user part to be UITextPart, got %T", userChunk.Data.Message.Parts[0])
	}
	if !strings.HasPrefix(textPart.Text, "Expanded command body.") {
		t.Fatalf("expanded text = %q", textPart.Text)
	}
	if !strings.Contains(textPart.Text, "ARGUMENTS: fix the bug") {
		t.Fatalf("expanded text missing arguments: %q", textPart.Text)
	}
}

func TestListCommands_IncludesOnlyCompactBuiltin(t *testing.T) {
	agentImpl := NewDefaultAgent(thread.NewStore(t.TempDir()), nil, nil, t.TempDir(), MCPConfig{})

	commands, err := agentImpl.ListCommands()
	if err != nil {
		t.Fatal(err)
	}

	foundCompact := false
	for _, cmd := range commands {
		if cmd.Name == "compact" {
			foundCompact = true
		}
	}

	if !foundCompact {
		t.Fatal("expected compact built-in command")
	}
}

func TestPrompt_GeneratesThreadNameInBackground(t *testing.T) {
	store := thread.NewStore(t.TempDir())
	registry := providers.NewProviderRegistry(nil)
	mockProvider := &compactCommandMockProvider{
		beforeRespond: func(req providers.CompleteRequest) {
			if isThreadNameRequest(req) {
				// Sleep long enough that naming completes after streaming starts
				// but well within the pause below.
				time.Sleep(50 * time.Millisecond)
			}
		},
		beforeChunk: func(req providers.CompleteRequest, chunkIndex int) {
			if !isThreadNameRequest(req) && chunkIndex == 3 {
				// Pause long enough for naming (50ms) to finish mid-stream.
				time.Sleep(200 * time.Millisecond)
			}
		},
		responseForReq: func(req providers.CompleteRequest) string {
			if isThreadNameRequest(req) {
				return "Agent-go thread naming fix"
			}
			return "Assistant response."
		},
	}
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

	nameIndex := -1
	startIndex := -1
	finishIndex := -1
	textIndex := -1
	var threadUpdateChunk message.ThreadUpdateChunk
	for i, chunk := range chunks {
		switch chunk := chunk.(type) {
		case message.StartChunk:
			if startIndex < 0 {
				startIndex = i
			}
		case message.ThreadUpdateChunk:
			if nameIndex < 0 {
				nameIndex = i
				threadUpdateChunk = chunk
			}
		case message.TextDeltaChunk:
			if chunk.Delta == "Assistant response." && textIndex < 0 {
				textIndex = i
			}
		case message.ResponseFinishChunk:
			if finishIndex < 0 {
				finishIndex = i
			}
		}
	}
	if startIndex < 0 {
		t.Fatalf("expected assistant start chunk, got %#v", chunks)
	}
	if textIndex < 0 {
		t.Fatalf("expected assistant response chunk, got %#v", chunks)
	}
	if nameIndex < 0 {
		t.Fatalf("expected thread update chunk, got %#v", chunks)
	}
	if finishIndex < 0 {
		t.Fatalf("expected assistant finish chunk, got %#v", chunks)
	}
	if nameIndex <= startIndex {
		t.Fatalf("expected thread update chunk after assistant streaming started, got nameIndex=%d startIndex=%d", nameIndex, startIndex)
	}
	if nameIndex >= finishIndex {
		t.Fatalf("expected thread update chunk before assistant finish, got nameIndex=%d finishIndex=%d", nameIndex, finishIndex)
	}
	if threadUpdateChunk.Data.Thread.Name != "Agent-go thread naming fix" {
		t.Fatalf("unexpected generated thread name %q", threadUpdateChunk.Data.Thread.Name)
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

func TestPrompt_BootstrapsRuntimeReminderWithoutSystemPrompt(t *testing.T) {
	store := thread.NewStore(t.TempDir())
	registry := providers.NewProviderRegistry(nil)
	mockProvider := &compactCommandMockProvider{
		responseForReq: func(_ providers.CompleteRequest) string {
			return "Assistant response."
		},
	}
	registry.Add(mockProvider)

	cwd := t.TempDir()
	agentImpl := NewDefaultAgent(store, registry, nil, cwd, MCPConfig{})
	threadID := "thread-runtime-bootstrap"
	if err := store.SaveConfig(threadID, thread.Config{
		Name:  "existing-name",
		Model: "mock/test-model",
	}); err != nil {
		t.Fatal(err)
	}

	for _, err := range agentImpl.Prompt(context.Background(), threadID, agent.PromptRequest{
		UserParts: []message.UIPart{message.UITextPart{Text: "hello", State: "done"}},
	}) {
		if err != nil {
			t.Fatal(err)
		}
	}

	if len(mockProvider.requests) == 0 {
		t.Fatal("expected provider request")
	}
	req := mockProvider.requests[0]
	if len(req.Messages) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(req.Messages))
	}
	foundRuntimeReminder := false
	for _, msg := range req.Messages {
		if msg.Role != "user" || len(msg.Parts) == 0 {
			continue
		}
		textPart, ok := msg.Parts[0].(message.TextPart)
		if ok && strings.Contains(textPart.Text, "Runtime environment snapshot:") {
			foundRuntimeReminder = true
			break
		}
	}
	if !foundRuntimeReminder {
		t.Fatal("expected runtime reminder bootstrap message in provider history")
	}
}

func TestPrompt_FallsBackToGeneratedThreadNameWhenAIReturnsEmpty(t *testing.T) {
	store := thread.NewStore(t.TempDir())
	registry := providers.NewProviderRegistry(nil)
	mockProvider := &compactCommandMockProvider{
		responseForReq: func(req providers.CompleteRequest) string {
			if isThreadNameRequest(req) {
				return ""
			}
			return "Assistant response."
		},
	}
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
	var threadUpdateChunk message.ThreadUpdateChunk
	foundThreadUpdateChunk := false
	for _, chunk := range chunks {
		var ok bool
		threadUpdateChunk, ok = chunk.(message.ThreadUpdateChunk)
		if ok {
			foundThreadUpdateChunk = true
			break
		}
	}
	if !foundThreadUpdateChunk {
		t.Fatalf("expected a ThreadUpdateChunk, got %#v", chunks)
	}
	if threadUpdateChunk.Data.Thread.Name != "Fix thread naming in agent-go" {
		t.Fatalf("unexpected fallback generated thread name %q", threadUpdateChunk.Data.Thread.Name)
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

func TestPrompt_DoesNotRegenerateThreadNameAfterInitialSet(t *testing.T) {
	store := thread.NewStore(t.TempDir())
	registry := providers.NewProviderRegistry(nil)
	mockProvider := &compactCommandMockProvider{
		responseForReq: func(req providers.CompleteRequest) string {
			if isThreadNameRequest(req) {
				return "Initial generated thread name"
			}
			return "Assistant response."
		},
	}
	registry.Add(mockProvider)

	agentImpl := NewDefaultAgent(store, registry, nil, t.TempDir(), MCPConfig{})
	threadID := "thread-name-once"

	for _, err := range agentImpl.Prompt(context.Background(), threadID, agent.PromptRequest{
		UserParts: []message.UIPart{message.UITextPart{Text: "First user message", State: "done"}},
	}) {
		if err != nil {
			t.Fatal(err)
		}
	}

	var secondChunks []message.MessageChunk
	for chunk, err := range agentImpl.Prompt(context.Background(), threadID, agent.PromptRequest{
		UserParts: []message.UIPart{message.UITextPart{Text: "Second user message", State: "done"}},
	}) {
		if err != nil {
			t.Fatal(err)
		}
		if chunk != nil {
			secondChunks = append(secondChunks, chunk)
		}
	}

	for _, chunk := range secondChunks {
		if _, ok := chunk.(message.ThreadUpdateChunk); ok {
			t.Fatalf("did not expect thread update on second turn, got %#v", chunk)
		}
	}

	threadNameRequests := 0
	for _, req := range mockProvider.requests {
		if isThreadNameRequest(req) {
			threadNameRequests++
		}
	}
	if threadNameRequests != 1 {
		t.Fatalf("expected exactly one thread name request across two turns, got %d", threadNameRequests)
	}

	cfg, err := store.LoadConfig(threadID)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Name != "Initial generated thread name" {
		t.Fatalf("expected initial generated name to persist, got %q", cfg.Name)
	}
}

func isThreadNameRequest(req providers.CompleteRequest) bool {
	if len(req.Messages) != 1 || len(req.Messages[0].Parts) != 1 {
		return false
	}
	textPart, ok := req.Messages[0].Parts[0].(message.TextPart)
	if !ok {
		return false
	}
	return strings.Contains(textPart.Text, "Generate a concise thread title for this conversation starter.")
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
