package thread

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/obot-platform/discobot/agent-go/message"
)

func TestSaveAndLoadMessage(t *testing.T) {
	store := NewStore(t.TempDir())

	msg := StoredMessage{
		ID:       "msg1",
		ParentID: "",
		Message: message.Message{
			Role: "user",
			Parts: []message.Part{
				message.TextPart{Text: "hello"},
			},
		},
	}

	if err := store.SaveMessage("thread1", msg); err != nil {
		t.Fatal(err)
	}

	loaded, err := store.LoadMessage("thread1", "msg1")
	if err != nil {
		t.Fatal(err)
	}

	if loaded.ID != "msg1" {
		t.Errorf("expected ID=msg1, got %s", loaded.ID)
	}
	if loaded.ParentID != "" {
		t.Errorf("expected empty parentID, got %s", loaded.ParentID)
	}
	if loaded.Message.Role != "user" {
		t.Errorf("expected role=user, got %s", loaded.Message.Role)
	}
	if len(loaded.Message.Parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(loaded.Message.Parts))
	}
	tp, ok := loaded.Message.Parts[0].(message.TextPart)
	if !ok {
		t.Fatalf("expected TextPart, got %T", loaded.Message.Parts[0])
	}
	if tp.Text != "hello" {
		t.Errorf("expected text=hello, got %s", tp.Text)
	}
	if loaded.Message.CreatedAt == nil {
		t.Fatal("expected CreatedAt to be set")
	}
}

func TestLoadMessage_NotFound(t *testing.T) {
	store := NewStore(t.TempDir())
	_, err := store.LoadMessage("thread1", "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent message")
	}
}

func TestSaveMessage_DuplicateIDFails(t *testing.T) {
	store := NewStore(t.TempDir())
	msg := StoredMessage{
		ID:      "msg1",
		Message: message.Message{Role: "user"},
	}
	if err := store.SaveMessage("thread1", msg); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveMessage("thread1", msg); !errors.Is(err, ErrMessageExists) {
		t.Fatalf("expected ErrMessageExists, got %v", err)
	}
}

func TestBuildHistory(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread1"

	// Create a chain: msg1 -> msg2 -> msg3
	messages := []StoredMessage{
		{
			ID:       "msg1",
			ParentID: "",
			Message:  message.Message{Role: "system", Parts: []message.Part{message.TextPart{Text: "system prompt"}}},
		},
		{
			ID:       "msg2",
			ParentID: "msg1",
			Message:  message.Message{Role: "user", Parts: []message.Part{message.TextPart{Text: "hi"}}},
		},
		{
			ID:       "msg3",
			ParentID: "msg2",
			Message:  message.Message{Role: "assistant", Parts: []message.Part{message.TextPart{Text: "hello"}}},
		},
	}

	for _, msg := range messages {
		if err := store.SaveMessage(threadID, msg); err != nil {
			t.Fatal(err)
		}
	}

	history, err := store.BuildHistory(threadID, "msg3")
	if err != nil {
		t.Fatal(err)
	}

	if len(history) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(history))
	}

	// Verify chronological order: system, user, assistant
	expectedRoles := []string{"system", "user", "assistant"}
	for i, role := range expectedRoles {
		if history[i].Role != role {
			t.Errorf("history[%d]: expected role=%s, got %s", i, role, history[i].Role)
		}
	}
}

func TestBuildHistory_SingleMessage(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread1"

	if err := store.SaveMessage(threadID, StoredMessage{
		ID:      "msg1",
		Message: message.Message{Role: "user"},
	}); err != nil {
		t.Fatal(err)
	}

	history, err := store.BuildHistory(threadID, "msg1")
	if err != nil {
		t.Fatal(err)
	}

	if len(history) != 1 {
		t.Fatalf("expected 1 message, got %d", len(history))
	}
}

func TestBuildHistory_BrokenChain(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread1"

	// msg2 points to msg1 which doesn't exist
	if err := store.SaveMessage(threadID, StoredMessage{
		ID:       "msg2",
		ParentID: "msg1",
		Message:  message.Message{Role: "user"},
	}); err != nil {
		t.Fatal(err)
	}

	_, err := store.BuildHistory(threadID, "msg2")
	if err == nil {
		t.Error("expected error for broken parent chain")
	}
}

func TestListThreads(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	// No threads yet
	threads, err := store.ListThreads()
	if err != nil {
		t.Fatal(err)
	}
	if len(threads) != 0 {
		t.Errorf("expected 0 threads, got %d", len(threads))
	}

	// Create two threads by saving messages
	if err := store.SaveMessage("thread-a", StoredMessage{
		ID:      "m1",
		Message: message.Message{Role: "user"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveMessage("thread-b", StoredMessage{
		ID:      "m1",
		Message: message.Message{Role: "user"},
	}); err != nil {
		t.Fatal(err)
	}

	threads, err = store.ListThreads()
	if err != nil {
		t.Fatal(err)
	}
	if len(threads) != 2 {
		t.Fatalf("expected 2 threads, got %d", len(threads))
	}

	// Check both exist (order not guaranteed)
	found := map[string]bool{}
	for _, id := range threads {
		found[id] = true
	}
	if !found["thread-a"] || !found["thread-b"] {
		t.Errorf("expected thread-a and thread-b, got %v", threads)
	}
}

func TestListThreads_EmptyBaseDir(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "nonexistent"))
	threads, err := store.ListThreads()
	if err != nil {
		t.Fatal(err)
	}
	if len(threads) != 0 {
		t.Errorf("expected 0 threads, got %d", len(threads))
	}
}

func TestCreateStepFileAndAppendChunk(t *testing.T) {
	store := NewStore(t.TempDir())

	f, err := store.CreateStepFile("thread1", "turn1", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	chunks := []message.ProviderMessageChunk{
		message.TextStartChunk{ID: "t1"},
		message.TextDeltaChunk{ID: "t1", Delta: "hello"},
		message.TextEndChunk{ID: "t1"},
	}

	for _, chunk := range chunks {
		if err := store.AppendChunk(f, chunk); err != nil {
			t.Fatal(err)
		}
	}

	f.Close()

	// Read back and verify
	readF, err := os.Open(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer readF.Close()

	scanner := bufio.NewScanner(readF)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}

	// Verify each line is valid JSON with a type field
	for i, line := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("line %d: invalid JSON: %v", i, err)
		}
		if _, ok := m["type"]; !ok {
			t.Fatalf("line %d: missing type field", i)
		}
	}

	// Verify round-trip via UnmarshalProviderChunk
	chunk0, err := message.UnmarshalProviderChunk([]byte(lines[0]))
	if err != nil {
		t.Fatal(err)
	}
	ts, ok := chunk0.(message.TextStartChunk)
	if !ok {
		t.Fatalf("expected TextStartChunk, got %T", chunk0)
	}
	if ts.ID != "t1" {
		t.Errorf("expected ID=t1, got %s", ts.ID)
	}
}

func TestLoadStepChunks(t *testing.T) {
	store := NewStore(t.TempDir())

	f, err := store.CreateStepFile("thread1", "turn1", 0)
	if err != nil {
		t.Fatal(err)
	}

	chunks := []message.ProviderMessageChunk{
		message.TextStartChunk{ID: "t1"},
		message.TextDeltaChunk{ID: "t1", Delta: "hello"},
		message.TextEndChunk{ID: "t1"},
	}
	for _, chunk := range chunks {
		if err := store.AppendChunk(f, chunk); err != nil {
			t.Fatal(err)
		}
	}
	f.Close()

	loaded, err := store.LoadStepChunks("thread1", "turn1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(loaded))
	}
	ts, ok := loaded[0].(message.TextStartChunk)
	if !ok {
		t.Fatalf("expected TextStartChunk, got %T", loaded[0])
	}
	if ts.ID != "t1" {
		t.Errorf("expected ID=t1, got %s", ts.ID)
	}
}

func TestLoadStepChunks_NotFound(t *testing.T) {
	store := NewStore(t.TempDir())
	chunks, err := store.LoadStepChunks("thread1", "turn1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if chunks != nil {
		t.Errorf("expected nil for nonexistent step file, got %v", chunks)
	}
}

func TestLoadStepChunks_PartialLastRecord(t *testing.T) {
	store := NewStore(t.TempDir())

	f, err := store.CreateStepFile("thread1", "turn1", 0)
	if err != nil {
		t.Fatal(err)
	}

	// Write two valid chunks.
	validChunks := []message.ProviderMessageChunk{
		message.TextStartChunk{ID: "t1"},
		message.TextDeltaChunk{ID: "t1", Delta: "hello"},
	}
	for _, chunk := range validChunks {
		if err := store.AppendChunk(f, chunk); err != nil {
			t.Fatal(err)
		}
	}

	// Simulate a crash mid-write: append a partial (invalid) JSON line.
	if _, err := f.WriteString(`{"type":"text-delta","id":"t1","delta":"wor`); err != nil {
		t.Fatal(err)
	}
	f.Close()

	loaded, err := store.LoadStepChunks("thread1", "turn1", 0)
	if err != nil {
		t.Fatal(err)
	}
	// The partial last record must be skipped; only the two valid chunks returned.
	if len(loaded) != 2 {
		t.Fatalf("expected 2 chunks (partial skipped), got %d", len(loaded))
	}
}

// --- Turn State Tests ---

func TestSaveAndLoadTurnState(t *testing.T) {
	store := NewStore(t.TempDir())

	startedAt := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	state := TurnState{
		ID:          "turn1",
		ThreadID:    "thread1",
		LeafID:      "msg0",
		CurrentStep: 0,
		Phase:       PhaseStreaming,
		StartedAt:   &startedAt,
		Config: TurnConfig{
			Model:     "test-model",
			Reasoning: "enabled",
		},
	}

	if err := store.SaveTurnState("thread1", state); err != nil {
		t.Fatal(err)
	}

	loaded, err := store.LoadTurnState("thread1")
	if err != nil {
		t.Fatal(err)
	}
	if loaded == nil {
		t.Fatal("expected non-nil turn state")
	}
	if loaded.ID != "turn1" {
		t.Errorf("expected ID=turn1, got %s", loaded.ID)
	}
	if loaded.Phase != PhaseStreaming {
		t.Errorf("expected phase=streaming, got %s", loaded.Phase)
	}
	if loaded.Config.Model != "test-model" {
		t.Errorf("expected model=test-model, got %s", loaded.Config.Model)
	}
	if loaded.StartedAt == nil || !loaded.StartedAt.Equal(startedAt) {
		t.Fatalf("expected startedAt=%v, got %v", startedAt, loaded.StartedAt)
	}
	if loaded.UpdatedAt == nil {
		t.Fatal("expected updatedAt to be set")
	}

	record, err := store.LoadTurnRecord("thread1", "turn1")
	if err != nil {
		t.Fatal(err)
	}
	if record == nil {
		t.Fatal("expected non-nil turn record")
	}
	if record.StartedAt == nil || !record.StartedAt.Equal(startedAt) {
		t.Fatalf("expected turn record startedAt=%v, got %v", startedAt, record.StartedAt)
	}
}

func TestLoadTurnState_NoActiveTurn(t *testing.T) {
	store := NewStore(t.TempDir())
	state, err := store.LoadTurnState("thread1")
	if err != nil {
		t.Fatal(err)
	}
	if state != nil {
		t.Error("expected nil for no active turn")
	}
}

func TestDeleteTurnState(t *testing.T) {
	store := NewStore(t.TempDir())
	state := TurnState{ID: "turn1", ThreadID: "thread1", Phase: "streaming"}
	if err := store.SaveTurnState("thread1", state); err != nil {
		t.Fatal(err)
	}

	if err := store.DeleteTurnState("thread1"); err != nil {
		t.Fatal(err)
	}

	loaded, err := store.LoadTurnState("thread1")
	if err != nil {
		t.Fatal(err)
	}
	if loaded != nil {
		t.Error("expected nil after delete")
	}
}

func TestDeleteTurnState_NoFile(t *testing.T) {
	store := NewStore(t.TempDir())
	// Should not error if file doesn't exist.
	if err := store.DeleteTurnState("thread1"); err != nil {
		t.Fatal(err)
	}
}

func TestSaveAndLoadConfig(t *testing.T) {
	store := NewStore(t.TempDir())

	cfg := Config{
		Name:         "Friendly thread name",
		NameSource:   ThreadNameSourceGenerated,
		LastMessage:  "latest prompt",
		Model:        "anthropic/claude-sonnet-4-6",
		CWD:          "/tmp/project",
		PlanMode:     true,
		ActiveLeafID: "msg-active",
	}

	if err := store.SaveConfig("thread1", cfg); err != nil {
		t.Fatal(err)
	}

	loaded, err := store.LoadConfig("thread1")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Model != cfg.Model {
		t.Errorf("expected model=%q, got %q", cfg.Model, loaded.Model)
	}
	if loaded.Name != cfg.Name {
		t.Errorf("expected name=%q, got %q", cfg.Name, loaded.Name)
	}
	if loaded.NameSource != cfg.NameSource {
		t.Errorf("expected nameSource=%q, got %q", cfg.NameSource, loaded.NameSource)
	}
	if loaded.LastMessage != cfg.LastMessage {
		t.Errorf("expected lastMessage=%q, got %q", cfg.LastMessage, loaded.LastMessage)
	}
	if loaded.CWD != cfg.CWD {
		t.Errorf("expected cwd=%q, got %q", cfg.CWD, loaded.CWD)
	}
	if loaded.PlanMode != cfg.PlanMode {
		t.Errorf("expected planMode=%v, got %v", cfg.PlanMode, loaded.PlanMode)
	}
	if loaded.ActiveLeafID != cfg.ActiveLeafID {
		t.Errorf("expected activeLeafId=%q, got %q", cfg.ActiveLeafID, loaded.ActiveLeafID)
	}
}

// --- Step Result Tests ---

func TestSaveAndLoadStepResult(t *testing.T) {
	store := NewStore(t.TempDir())

	result := StepResult{
		AssistantMessage: message.Message{
			Role: "assistant",
			Parts: []message.Part{
				message.TextPart{Text: "hello"},
				message.ToolCallPart{ToolCallID: "tc1", ToolName: "bash", Input: `{"cmd":"ls"}`},
			},
		},
		ToolCalls: []ToolCallInfo{
			{ToolCallID: "tc1", ToolName: "bash", Input: `{"cmd":"ls"}`},
		},
	}

	// Need to create the turn dir first.
	f, err := store.CreateStepFile("thread1", "turn1", 0)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	if err := store.SaveStepResult("thread1", "turn1", 0, result); err != nil {
		t.Fatal(err)
	}

	loaded, err := store.LoadStepResult("thread1", "turn1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if loaded == nil {
		t.Fatal("expected non-nil step result")
	}
	if loaded.AssistantMessage.Role != "assistant" {
		t.Errorf("expected role=assistant, got %s", loaded.AssistantMessage.Role)
	}
	if len(loaded.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(loaded.ToolCalls))
	}
	if loaded.ToolCalls[0].ToolCallID != "tc1" {
		t.Errorf("expected tool call ID=tc1, got %s", loaded.ToolCalls[0].ToolCallID)
	}
}

func TestLoadStepResult_NotFound(t *testing.T) {
	store := NewStore(t.TempDir())
	result, err := store.LoadStepResult("thread1", "turn1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Error("expected nil for nonexistent step result")
	}
}

// --- Tool Results Tests ---

func TestSaveAndLoadToolResults(t *testing.T) {
	store := NewStore(t.TempDir())

	// Create turn dir.
	f, err := store.CreateStepFile("thread1", "turn1", 0)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	results := StepToolResults{
		Results: []message.ToolResultPart{
			{
				ToolCallID: "tc1",
				ToolName:   "bash",
				Output:     message.TextOutput{Value: "file1.txt\nfile2.txt"},
			},
			{
				ToolCallID: "tc2",
				ToolName:   "read_file",
				Output:     message.ErrorTextOutput{Value: "not found"},
			},
		},
	}

	if err := store.SaveToolResults("thread1", "turn1", 0, results); err != nil {
		t.Fatal(err)
	}

	loaded, err := store.LoadToolResults("thread1", "turn1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(loaded.Results))
	}
	if loaded.Results[0].ToolCallID != "tc1" {
		t.Errorf("expected tool call ID=tc1, got %s", loaded.Results[0].ToolCallID)
	}
	if loaded.Results[0].ToolName != "bash" {
		t.Errorf("expected tool name=bash, got %s", loaded.Results[0].ToolName)
	}
	textOut, ok := loaded.Results[0].Output.(message.TextOutput)
	if !ok {
		t.Fatalf("expected TextOutput, got %T", loaded.Results[0].Output)
	}
	if textOut.Value != "file1.txt\nfile2.txt" {
		t.Errorf("unexpected output: %s", textOut.Value)
	}
	errOut, ok := loaded.Results[1].Output.(message.ErrorTextOutput)
	if !ok {
		t.Fatalf("expected ErrorTextOutput, got %T", loaded.Results[1].Output)
	}
	if errOut.Value != "not found" {
		t.Errorf("unexpected error output: %s", errOut.Value)
	}
}

func TestLoadToolResults_NotFound(t *testing.T) {
	store := NewStore(t.TempDir())
	results, err := store.LoadToolResults("thread1", "turn1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(results.Results) != 0 {
		t.Errorf("expected empty results, got %d", len(results.Results))
	}
}

func TestSaveAndLoadStepEventMessages(t *testing.T) {
	store := NewStore(t.TempDir())
	events := StepEventMessages{MessageIDs: []string{"m1", "m2"}}
	if err := store.SaveStepEventMessages("thread1", "turn1", 0, events); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadStepEventMessages("thread1", "turn1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.MessageIDs) != 2 || loaded.MessageIDs[0] != "m1" || loaded.MessageIDs[1] != "m2" {
		t.Fatalf("unexpected step event messages: %+v", loaded.MessageIDs)
	}
}

func TestLoadStepEventMessages_NotFound(t *testing.T) {
	store := NewStore(t.TempDir())
	events, err := store.LoadStepEventMessages("thread1", "turn1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events.MessageIDs) != 0 {
		t.Fatalf("expected no step event messages, got %v", events.MessageIDs)
	}
}

func TestStepFileNaming(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	f, err := store.CreateStepFile("thread1", "turn1", 5)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	expected := filepath.Join(dir, "thread1", "turns", "turn1", "step-005.jsonl")
	if f.Name() != expected {
		t.Errorf("expected path %s, got %s", expected, f.Name())
	}
}
