package agentimpl

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/obot-platform/discobot/agent-go/agent"
	"github.com/obot-platform/discobot/agent-go/internal/api"
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

	var chunks []message.MessageChunk
	var deltas []string
	for chunk, err := range agentImpl.Compact(context.Background(), threadID, agent.PromptRequest{
		UserParts: []message.UIPart{message.UITextPart{Text: "/compact"}},
	}) {
		if err != nil {
			t.Fatal(err)
		}
		chunks = append(chunks, chunk)
		if td, ok := chunk.(message.TextDeltaChunk); ok {
			deltas = append(deltas, td.Delta)
		}
	}

	assertTextStartsAfterStart(t, chunks)
	if len(deltas) != 2 || deltas[0] != "Compacting conversation...\n" || deltas[1] != "Conversation compacted." {
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

	var chunks []message.MessageChunk
	var deltas []string
	for chunk, err := range agentImpl.Compact(context.Background(), "thread-empty", agent.PromptRequest{
		UserParts: []message.UIPart{message.UITextPart{Text: "/compact"}},
	}) {
		if err != nil {
			t.Fatal(err)
		}
		chunks = append(chunks, chunk)
		if td, ok := chunk.(message.TextDeltaChunk); ok {
			deltas = append(deltas, td.Delta)
		}
	}

	assertTextStartsAfterStart(t, chunks)
	if len(deltas) != 1 || deltas[0] != "Nothing to compact yet." {
		t.Fatalf("unexpected /compact response deltas: %#v", deltas)
	}
}

func assertTextStartsAfterStart(t *testing.T, chunks []message.MessageChunk) {
	t.Helper()
	sawStart := false
	for _, chunk := range chunks {
		switch chunk.(type) {
		case message.StartChunk:
			sawStart = true
		case message.TextStartChunk, message.TextDeltaChunk, message.TextEndChunk:
			if !sawStart {
				t.Fatalf("text chunk %T appeared before start chunk", chunk)
			}
		}
	}
	if !sawStart {
		t.Fatal("expected start chunk")
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
	sawActiveCommand := false
	sawClearedActiveCommand := false
	for chunk, err := range agentImpl.Prompt(context.Background(), "thread-legacy-command", agent.PromptRequest{
		UserParts: []message.UIPart{message.UITextPart{Text: "/commit fix the bug", State: "done"}},
	}) {
		if err != nil {
			t.Fatal(err)
		}
		if typed, ok := chunk.(message.ThreadUpdateChunk); ok {
			if typed.Data.Thread.ActiveCommand == "commit" {
				sawActiveCommand = true
			}
			if sawActiveCommand && typed.Data.Thread.ActiveCommand == "" {
				sawClearedActiveCommand = true
			}
		}
		if typed, ok := chunk.(message.UserMessageChunk); ok {
			userChunk = typed
			foundUserChunk = true
		}
	}
	if !foundUserChunk {
		t.Fatal("expected user message chunk")
	}
	if !sawActiveCommand {
		t.Fatal("expected thread update with active command")
	}
	if !sawClearedActiveCommand {
		t.Fatal("expected thread update clearing active command")
	}

	var metadata struct {
		OriginalText string `json:"originalText"`
		SlashCommand struct {
			Name string `json:"name"`
			Kind string `json:"kind"`
			Text string `json:"text"`
		} `json:"slashCommand"`
	}
	if err := json.Unmarshal(userChunk.Data.Message.Metadata, &metadata); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}
	if metadata.OriginalText != "/commit fix the bug" {
		t.Fatalf("originalText = %q", metadata.OriginalText)
	}
	if metadata.SlashCommand.Name != "commit" {
		t.Fatalf("slashCommand.name = %q", metadata.SlashCommand.Name)
	}
	if metadata.SlashCommand.Kind != string(agent.CommandKindCommand) {
		t.Fatalf("slashCommand.kind = %q", metadata.SlashCommand.Kind)
	}
	if metadata.SlashCommand.Text != "" {
		t.Fatalf("slashCommand.text = %q", metadata.SlashCommand.Text)
	}
	textPart, ok := userChunk.Data.Message.Parts[0].(message.UITextPart)
	if !ok {
		t.Fatalf("expected first user part to be UITextPart, got %T", userChunk.Data.Message.Parts[0])
	}
	meta, ok := message.UnmarshalProviderMetadata(textPart.ProviderMetadata)
	if !ok {
		t.Fatal("expected provider metadata on expanded legacy command")
	}
	if meta.OriginalCommand != "/commit fix the bug" {
		t.Fatalf("originalCommand = %q", meta.OriginalCommand)
	}
	if meta.CommandKind != string(agent.CommandKindCommand) {
		t.Fatalf("commandKind = %q", meta.CommandKind)
	}
	if !strings.HasPrefix(textPart.Text, "Expanded command body.") {
		t.Fatalf("expanded text = %q", textPart.Text)
	}
	if !strings.Contains(textPart.Text, "ARGUMENTS: fix the bug") {
		t.Fatalf("expanded text missing arguments: %q", textPart.Text)
	}
}

func TestPrompt_SkillSlashCommandPassesThroughToModel(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude", "skills", "commit"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(root, ".claude", "skills", "commit", "SKILL.md"),
		[]byte("---\ndescription: Commit pending changes\n---\n# Commit\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	store := thread.NewStore(t.TempDir())
	registry := providers.NewProviderRegistry(nil)
	mockProvider := &compactCommandMockProvider{responses: []string{"Done."}}
	registry.Add(mockProvider)
	agentImpl := NewDefaultAgent(store, registry, nil, root, MCPConfig{})

	var userChunk message.UserMessageChunk
	foundUserChunk := false
	sawActiveCommand := false
	sawClearedActiveCommand := false
	for chunk, err := range agentImpl.Prompt(context.Background(), "thread-skill-command", agent.PromptRequest{
		UserParts: []message.UIPart{message.UITextPart{Text: "/commit fix the bug", State: "done"}},
	}) {
		if err != nil {
			t.Fatal(err)
		}
		if typed, ok := chunk.(message.ThreadUpdateChunk); ok {
			if typed.Data.Thread.ActiveCommand == "commit" {
				sawActiveCommand = true
			}
			if sawActiveCommand && typed.Data.Thread.ActiveCommand == "" {
				sawClearedActiveCommand = true
			}
		}
		if typed, ok := chunk.(message.UserMessageChunk); ok {
			userChunk = typed
			foundUserChunk = true
		}
	}
	if !foundUserChunk {
		t.Fatal("expected user message chunk")
	}
	if !sawActiveCommand {
		t.Fatal("expected thread update with active command")
	}
	if !sawClearedActiveCommand {
		t.Fatal("expected thread update clearing active command")
	}
	var metadata struct {
		OriginalText string `json:"originalText"`
		SlashCommand struct {
			Name string `json:"name"`
			Kind string `json:"kind"`
			Text string `json:"text"`
		} `json:"slashCommand"`
	}
	if err := json.Unmarshal(userChunk.Data.Message.Metadata, &metadata); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}
	if metadata.OriginalText != "/commit fix the bug" {
		t.Fatalf("originalText = %q", metadata.OriginalText)
	}
	if metadata.SlashCommand.Name != "commit" {
		t.Fatalf("slashCommand.name = %q", metadata.SlashCommand.Name)
	}
	if metadata.SlashCommand.Kind != string(agent.CommandKindSkill) {
		t.Fatalf("slashCommand.kind = %q", metadata.SlashCommand.Kind)
	}
	if metadata.SlashCommand.Text != "# Commit" {
		t.Fatalf("slashCommand.text = %q", metadata.SlashCommand.Text)
	}
	textPart, ok := userChunk.Data.Message.Parts[0].(message.UITextPart)
	if !ok {
		t.Fatalf("expected first user part to be UITextPart, got %T", userChunk.Data.Message.Parts[0])
	}
	if textPart.Text != "/commit fix the bug" {
		t.Fatalf("user text = %q", textPart.Text)
	}
	meta, ok := message.UnmarshalProviderMetadata(textPart.ProviderMetadata)
	if !ok {
		t.Fatal("expected provider metadata on skill slash command")
	}
	if meta.OriginalCommand != "/commit fix the bug" {
		t.Fatalf("originalCommand = %q", meta.OriginalCommand)
	}
	if meta.CommandKind != string(agent.CommandKindSkill) {
		t.Fatalf("commandKind = %q", meta.CommandKind)
	}

	mockProvider.mu.Lock()
	defer mockProvider.mu.Unlock()
	if len(mockProvider.requests) == 0 {
		t.Fatal("expected provider request")
	}
	var providerText string
	for _, req := range mockProvider.requests {
		if isThreadNameRequest(req) {
			continue
		}
		for _, msg := range req.Messages {
			if msg.Role != "user" {
				continue
			}
			for _, rawPart := range msg.Parts {
				part, ok := rawPart.(message.TextPart)
				if !ok {
					continue
				}
				if strings.TrimSpace(part.Text) == "/commit fix the bug" {
					providerText = part.Text
					break
				}
			}
			if providerText != "" {
				break
			}
		}
		if providerText != "" {
			break
		}
	}
	if providerText != "/commit fix the bug" {
		t.Fatalf("provider saw %q", providerText)
	}
}

func TestListCommands_IncludesBuiltinCommands(t *testing.T) {
	agentImpl := NewDefaultAgent(thread.NewStore(t.TempDir()), nil, nil, t.TempDir(), MCPConfig{})

	commands, err := agentImpl.ListCommands()
	if err != nil {
		t.Fatal(err)
	}

	foundCompact := false
	foundReset := false
	for _, cmd := range commands {
		if cmd.Name == "compact" {
			foundCompact = true
		}
		if cmd.Name == "reset" {
			foundReset = true
		}
	}

	if !foundCompact {
		t.Fatal("expected compact built-in command")
	}
	if !foundReset {
		t.Fatal("expected reset built-in command")
	}
}

func TestReset_ClearsActiveLeafForNextPrompt(t *testing.T) {
	store := thread.NewStore(t.TempDir())
	threadID := "thread-reset"
	if err := store.SaveMessage(threadID, thread.StoredMessage{
		ID: "msg1",
		Message: message.Message{
			Role:  "user",
			Parts: []message.Part{message.TextPart{Text: "before reset"}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveConfig(threadID, thread.Config{
		LastMessage:                  "before reset",
		ActiveLeafID:                 "msg1",
		CommunicatedCredentials:      []thread.CommunicatedCredentialBinding{{CredentialID: "cred", EnvVar: "TOKEN"}},
		CommunicatedSkillLikeEntries: []thread.CommunicatedSkillLikeEntry{{Name: "build"}},
	}); err != nil {
		t.Fatal(err)
	}

	agentImpl := NewDefaultAgent(store, nil, nil, t.TempDir(), MCPConfig{})
	if _, err := agentImpl.Reset(context.Background(), threadID); err != nil {
		t.Fatal(err)
	}

	cfg, err := store.LoadConfig(threadID)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ActiveLeafID != "" {
		t.Fatalf("expected active leaf to be cleared, got %q", cfg.ActiveLeafID)
	}
	if !cfg.ContextReset {
		t.Fatal("expected context reset marker")
	}
	leafID, err := agentImpl.resolveCurrentLeaf(threadID)
	if err != nil {
		t.Fatal(err)
	}
	if leafID != "" {
		t.Fatalf("expected no active leaf after reset, got %q", leafID)
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

func TestPrompt_SubAgentInvalidRequestedModelFallsBackToDefault(t *testing.T) {
	store := thread.NewStore(t.TempDir())
	threadID := "thread-subagent-invalid-model"
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
	registry.Add(openaiProvider)

	agentImpl := NewDefaultAgent(store, registry, nil, t.TempDir(), MCPConfig{})
	for _, err := range agentImpl.Prompt(context.Background(), threadID, agent.PromptRequest{
		Model:         "sonnet",
		ParentTaskID:  "task-1",
		SubagentDepth: 1,
		UserParts:     []message.UIPart{message.UITextPart{Text: "hello", State: "done"}},
	}) {
		if err != nil {
			t.Fatal(err)
		}
	}

	if len(openaiProvider.requests) != 1 {
		t.Fatalf("expected 1 provider request, got %d", len(openaiProvider.requests))
	}
	if got := openaiProvider.requests[0].Model.String(); got != "openai/gpt-5.4" {
		t.Fatalf("expected fallback model openai/gpt-5.4, got %q", got)
	}
}

func TestPrompt_SubAgentInvalidConfiguredModelFallsBackToResolvedModel(t *testing.T) {
	store := thread.NewStore(t.TempDir())
	threadID := "thread-subagent-invalid-configured-model"
	cwd := t.TempDir()
	agentsDir := filepath.Join(cwd, ".claude", "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentsDir, "reviewer.md"), []byte(`---
name: reviewer
model: openai/
---
Use a provider-specific task agent.`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveConfig(threadID, thread.Config{
		Name:       "Pinned",
		NameSource: thread.ThreadNameSourceUser,
		Model:      "openai/gpt-5.4",
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
	if got := openaiProvider.requests[0].Model.String(); got != "openai/gpt-5.4" {
		t.Fatalf("expected configured-model fallback to keep openai/gpt-5.4, got %q", got)
	}
}

func TestResume_UsesRequestModelReasoningOverrides(t *testing.T) {
	store := thread.NewStore(t.TempDir())
	threadID := "thread-resume-overrides"
	if err := store.SaveConfig(threadID, thread.Config{
		Name:       "Pinned",
		NameSource: thread.ThreadNameSourceUser,
	}); err != nil {
		t.Fatal(err)
	}

	registry := providers.NewProviderRegistry(nil)
	provider := &resumeOverrideProvider{
		id: "openai",
		defaults: map[string]providers.ModelRef{
			providers.ModelTaskChat: {ProviderID: "openai", ModelID: "gpt-5.4"},
		},
	}
	registry.Add(provider)

	agentImpl := NewDefaultAgent(store, registry, nil, t.TempDir(), MCPConfig{})

	var initialErr error
	for _, err := range agentImpl.Prompt(context.Background(), threadID, agent.PromptRequest{
		Model:     "openai/gpt-bad",
		UserParts: []message.UIPart{message.UITextPart{Text: "hello", State: "done"}},
	}) {
		if err != nil {
			initialErr = err
		}
	}
	if initialErr == nil {
		t.Fatal("expected initial invalid-model error")
	}
	state, err := store.LoadTurnState(threadID)
	if err != nil {
		t.Fatal(err)
	}
	if state == nil {
		t.Fatal("expected interrupted turn state after initial provider error")
	}

	var resumedChunks []message.MessageChunk
	resumed, err := agentImpl.Resume(context.Background(), threadID, agent.PromptRequest{
		Model:     "openai/gpt-5.4",
		Reasoning: "high",
	})
	if err != nil {
		t.Fatal(err)
	}
	for chunk, err := range resumed.Stream {
		if err != nil {
			t.Fatal(err)
		}
		if chunk != nil {
			resumedChunks = append(resumedChunks, chunk)
		}
	}
	if len(resumedChunks) == 0 {
		t.Fatal("expected resumed chunks")
	}
	startIndex := -1
	for i, chunk := range resumedChunks {
		if _, ok := chunk.(message.StartChunk); ok {
			startIndex = i
			break
		}
	}
	if startIndex == -1 {
		t.Fatalf("expected resumed chunks to include StartChunk, got %#v", resumedChunks)
	}
	start, ok := resumedChunks[startIndex].(message.StartChunk)
	if !ok {
		t.Fatalf("expected resumed chunk %d to be StartChunk, got %T", startIndex, resumedChunks[startIndex])
	}
	if start.MessageID != state.AssistantMsgID {
		t.Fatalf("expected resumed start message id %q, got %q", state.AssistantMsgID, start.MessageID)
	}

	if len(provider.requests) != 2 {
		t.Fatalf("expected 2 provider calls, got %d", len(provider.requests))
	}
	if got := provider.requests[0].Model.String(); got != "openai/gpt-bad" {
		t.Fatalf("expected first request to use invalid model, got %q", got)
	}
	if got := provider.requests[1].Model.String(); got != "openai/gpt-5.4" {
		t.Fatalf("expected resumed request to use override model, got %q", got)
	}
	if got := string(provider.requests[1].Reasoning); got != "high" {
		t.Fatalf("expected resumed request to use override reasoning, got %q", got)
	}

	cfg, err := store.LoadConfig(threadID)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "openai/gpt-5.4" {
		t.Fatalf("expected thread config model to be updated, got %q", cfg.Model)
	}
	if cfg.Reasoning != providers.Reasoning("high") {
		t.Fatalf("expected thread config reasoning to be updated, got %q", cfg.Reasoning)
	}
	state, err = store.LoadTurnState(threadID)
	if err != nil {
		t.Fatal(err)
	}
	if state != nil {
		t.Fatalf("expected resumed turn state to be cleaned up, got %#v", state)
	}
}

func TestResume_ContinuesAnsweredPendingQuestion(t *testing.T) {
	store := thread.NewStore(t.TempDir())
	threadID := "thread-resume-answer"
	turnID := "turn-answer"
	approvalID := "approval-1"
	assistantMsgID := "msg-asst"
	userMsgID := "msg-user"

	if err := store.SaveMessage(threadID, thread.StoredMessage{
		ID: userMsgID,
		Message: message.Message{
			Role:  "user",
			Parts: []message.Part{message.TextPart{Text: "delete it"}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveMessage(threadID, thread.StoredMessage{
		ID:       assistantMsgID,
		ParentID: userMsgID,
		Message: message.Message{
			Role: "assistant",
			Parts: []message.Part{
				message.ToolCallPart{ToolCallID: "tc1", ToolName: "dangerous_tool", Input: `{"action":"delete"}`},
				message.ToolApprovalRequest{ApprovalID: approvalID, ToolCallID: "tc1"},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	sf, err := store.CreateStepFile(threadID, turnID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := sf.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveStepResult(threadID, turnID, 0, thread.StepResult{
		AssistantMessage: message.Message{
			Role: "assistant",
			Parts: []message.Part{
				message.ToolCallPart{ToolCallID: "tc1", ToolName: "dangerous_tool", Input: `{"action":"delete"}`},
			},
		},
		ToolCalls: []thread.ToolCallInfo{
			{ToolCallID: "tc1", ToolName: "dangerous_tool", Input: `{"action":"delete"}`},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveQuestion(threadID, turnID, thread.PendingQuestionState{
		ApprovalID:  approvalID,
		ToolCallID:  "tc1",
		StepIndex:   0,
		ResumePhase: thread.PhaseTools,
		Questions:   json.RawMessage(`[{"question":"Are you sure?"}]`),
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveTurnState(threadID, thread.TurnState{
		ID:                turnID,
		ThreadID:          threadID,
		CurrentStep:       0,
		Phase:             thread.PhaseWaitingForAnswer,
		LeafMsgID:         assistantMsgID,
		AssistantMsgID:    assistantMsgID,
		PendingApprovalID: approvalID,
		Config: thread.TurnConfig{
			ProviderID: "openai",
			Model:      "gpt-5.4",
		},
	}); err != nil {
		t.Fatal(err)
	}

	registry := providers.NewProviderRegistry(nil)
	provider := &resumeOverrideProvider{
		id: "openai",
		defaults: map[string]providers.ModelRef{
			providers.ModelTaskChat: {ProviderID: "openai", ModelID: "gpt-5.4"},
		},
	}
	registry.Add(provider)

	executor := &answeredPendingQuestionExecutor{}
	agentImpl := NewDefaultAgent(store, registry, executor, t.TempDir(), MCPConfig{})

	if err := agentImpl.SubmitAnswer(threadID, approvalID, api.AnswerQuestionRequest{
		Answers: map[string]string{"q1": "yes"},
	}); err != nil {
		t.Fatal(err)
	}

	resumed, err := agentImpl.Resume(context.Background(), threadID, agent.PromptRequest{})
	if err != nil {
		t.Fatal(err)
	}

	var resumedChunks []message.MessageChunk
	for chunk, err := range resumed.Stream {
		if err != nil {
			t.Fatal(err)
		}
		if chunk != nil {
			resumedChunks = append(resumedChunks, chunk)
		}
	}

	if len(provider.requests) != 1 {
		t.Fatalf("expected provider to receive 1 request, got %d", len(provider.requests))
	}
	if executor.answerReq == nil {
		t.Fatal("expected executor to receive saved answer during resume")
	}
	if got := executor.answerReq.Answers["q1"]; got != "yes" {
		t.Fatalf("expected resumed answer q1=yes, got %#v", executor.answerReq.Answers)
	}

	var hasAnchor bool
	var hasToolOutput bool
	var hasFinalText bool
	for _, chunk := range resumedChunks {
		switch value := chunk.(type) {
		case message.ThreadResumeChunk:
			if value.Data.MessageID == assistantMsgID {
				hasAnchor = true
			}
		case message.StartChunk:
			if value.MessageID == assistantMsgID {
				hasAnchor = true
			}
		case message.ToolOutputAvailableChunk:
			if value.ToolCallID == "tc1" {
				hasToolOutput = true
			}
		case message.TextDeltaChunk:
			if value.Delta == "ok" {
				hasFinalText = true
			}
		}
	}
	if !hasAnchor {
		t.Fatalf("expected resumed stream to anchor to assistant message %q, got %#v", assistantMsgID, resumedChunks)
	}
	if !hasToolOutput {
		t.Fatalf("expected resumed stream to emit tool output, got %#v", resumedChunks)
	}
	if !hasFinalText {
		t.Fatalf("expected resumed stream to continue with assistant text, got %#v", resumedChunks)
	}

	state, err := store.LoadTurnState(threadID)
	if err != nil {
		t.Fatal(err)
	}
	if state != nil {
		t.Fatalf("expected resumed turn state to be cleaned up, got %#v", state)
	}
}

type resumeOverrideProvider struct {
	id       string
	defaults map[string]providers.ModelRef
	requests []providers.CompleteRequest
}

func (p *resumeOverrideProvider) ID() string { return p.id }

func (p *resumeOverrideProvider) Complete(_ context.Context, req providers.CompleteRequest) iter.Seq2[message.ProviderMessageChunk, error] {
	p.requests = append(p.requests, req)
	return func(yield func(message.ProviderMessageChunk, error) bool) {
		if req.Model.ModelID == "gpt-bad" {
			yield(nil, fmt.Errorf("openai: api error 400: invalid_request_error: requested model %q does not exist", req.Model.ModelID))
			return
		}
		yield(message.StreamStartChunk{}, nil)
		yield(message.TextStartChunk{ID: "s1"}, nil)
		yield(message.TextDeltaChunk{ID: "s1", Delta: "ok"}, nil)
		yield(message.TextEndChunk{ID: "s1"}, nil)
		yield(message.FinishChunk{FinishReason: message.FinishReason{Unified: "stop"}}, nil)
	}
}

func (p *resumeOverrideProvider) ListModels(_ context.Context) ([]providers.ModelInfo, error) {
	return nil, nil
}

func (p *resumeOverrideProvider) DefaultModels() map[string]providers.ModelRef {
	return p.defaults
}

type answeredPendingQuestionExecutor struct {
	answerReq *api.AnswerQuestionRequest
}

func (e *answeredPendingQuestionExecutor) Execute(context.Context, *thread.ToolContext, message.ToolCallPart) (thread.ToolExecuteResult, error) {
	return thread.ToolExecuteResult{}, fmt.Errorf("unexpected Execute call")
}

func (e *answeredPendingQuestionExecutor) Continue(_ context.Context, _ *thread.ToolContext, call message.ToolCallPart, _ json.RawMessage, req *api.AnswerQuestionRequest) (thread.ToolExecuteResult, error) {
	e.answerReq = req
	return thread.ToolExecuteResult{
		Result: message.ToolResultPart{
			ToolCallID: call.ToolCallID,
			ToolName:   call.ToolName,
			Output:     message.TextOutput{Value: "item deleted"},
		},
	}, nil
}
