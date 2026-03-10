package thread

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"strings"
	"testing"

	"github.com/obot-platform/discobot/agent-go/message"
	"github.com/obot-platform/discobot/agent-go/providers"
)

// --- Compaction-aware mock provider ---

// compactionMockProvider extends mockProvider with configurable CountTokens.
type compactionMockProvider struct {
	responses    [][]message.ProviderMessageChunk
	callIndex    int
	requests     []providers.CompleteRequest
	tokenCountFn func(providers.CountTokensRequest) (providers.CountTokensResponse, error)
}

func (m *compactionMockProvider) ID() string { return "mock" }

func (m *compactionMockProvider) Complete(_ context.Context, req providers.CompleteRequest) iter.Seq2[message.ProviderMessageChunk, error] {
	m.requests = append(m.requests, req)
	idx := m.callIndex
	m.callIndex++
	return func(yield func(message.ProviderMessageChunk, error) bool) {
		if idx >= len(m.responses) {
			yield(nil, fmt.Errorf("no more mock responses"))
			return
		}
		for _, chunk := range m.responses[idx] {
			if !yield(chunk, nil) {
				return
			}
		}
	}
}

func (m *compactionMockProvider) CountTokens(_ context.Context, req providers.CountTokensRequest) (providers.CountTokensResponse, error) {
	if m.tokenCountFn != nil {
		return m.tokenCountFn(req)
	}
	return providers.CountTokensResponse{}, nil
}

func (m *compactionMockProvider) DefaultModels() map[string]providers.ModelRef { return nil }
func (m *compactionMockProvider) ListModels(_ context.Context) ([]providers.ModelInfo, error) {
	return nil, nil
}

// --- Helper: count characters as pseudo-tokens (1 char ≈ 1 token) ---

func charBasedTokenCount(req providers.CountTokensRequest) (providers.CountTokensResponse, error) {
	total := 0
	for _, msg := range req.Messages {
		for _, part := range msg.Parts {
			switch p := part.(type) {
			case message.TextPart:
				total += len(p.Text)
			case message.ToolCallPart:
				total += len(p.ToolName) + len(p.Input) + 20
			case message.ToolResultPart:
				total += 50 // rough estimate
			}
		}
	}
	for _, tool := range req.Tools {
		total += len(tool.Name) + 20
	}
	return providers.CountTokensResponse{TotalTokens: total}, nil
}

// --- Tests ---

func TestComputeBudget(t *testing.T) {
	t.Run("with MaxOutputTokens", func(t *testing.T) {
		cfg := &TurnConfig{ContextWindow: 10000, MaxOutputTokens: 2000}
		b := computeBudget(cfg)
		// InputLimit = 10000 - 2000 = 8000
		if b.InputLimit != 8000 {
			t.Errorf("expected InputLimit=8000, got %d", b.InputLimit)
		}
		// CompactionTrigger = 8000 * 80/100 = 6400
		if b.CompactionTrigger != 6400 {
			t.Errorf("expected CompactionTrigger=6400, got %d", b.CompactionTrigger)
		}
		// SummaryMaxTokens = 8000 * 20/100 = 1600
		if b.SummaryMaxTokens != 1600 {
			t.Errorf("expected SummaryMaxTokens=1600, got %d", b.SummaryMaxTokens)
		}
	})

	t.Run("without MaxOutputTokens", func(t *testing.T) {
		cfg := &TurnConfig{ContextWindow: 8000}
		b := computeBudget(cfg)
		// outputReserve = 8000/4 = 2000
		// InputLimit = 8000 - 2000 = 6000
		if b.InputLimit != 6000 {
			t.Errorf("expected InputLimit=6000, got %d", b.InputLimit)
		}
		// CompactionTrigger = 6000 * 80/100 = 4800
		if b.CompactionTrigger != 4800 {
			t.Errorf("expected CompactionTrigger=4800, got %d", b.CompactionTrigger)
		}
	})

	t.Run("zero context window", func(t *testing.T) {
		cfg := &TurnConfig{ContextWindow: 0}
		b := computeBudget(cfg)
		if b.InputLimit != 0 {
			t.Errorf("expected InputLimit=0, got %d", b.InputLimit)
		}
		if b.CompactionTrigger != 0 {
			t.Errorf("expected CompactionTrigger=0, got %d", b.CompactionTrigger)
		}
	})
}

func TestSaveLoadCompaction(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread1"

	// No compaction initially.
	record, err := store.LoadCompaction(threadID)
	if err != nil {
		t.Fatal(err)
	}
	if record != nil {
		t.Error("expected nil compaction initially")
	}

	// Save and load.
	rec := CompactionRecord{
		SummaryText:   "This is a summary",
		LeafMessageID: "msg10",
		SummaryTokens: 100,
		Model:         "test-model",
	}
	if err := store.SaveCompaction(threadID, rec); err != nil {
		t.Fatal(err)
	}

	loaded, err := store.LoadCompaction(threadID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded == nil {
		t.Fatal("expected non-nil compaction")
	}
	if loaded.SummaryText != "This is a summary" {
		t.Errorf("expected summary text 'This is a summary', got %q", loaded.SummaryText)
	}
	if loaded.LeafMessageID != "msg10" {
		t.Errorf("expected LeafMessageID=msg10, got %s", loaded.LeafMessageID)
	}
}

func TestBuildHistoryWithIDs(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread1"

	// Create chain: msg1 → msg2 → msg3.
	for _, sm := range []StoredMessage{
		{ID: "msg1", Message: message.Message{Role: "system", Parts: []message.Part{message.TextPart{Text: "system"}}}},
		{ID: "msg2", ParentID: "msg1", Message: message.Message{Role: "user", Parts: []message.Part{message.TextPart{Text: "hello"}}}},
		{ID: "msg3", ParentID: "msg2", Message: message.Message{Role: "assistant", Parts: []message.Part{message.TextPart{Text: "hi"}}}},
	} {
		if err := store.SaveMessage(threadID, sm); err != nil {
			t.Fatal(err)
		}
	}

	entries, err := store.BuildHistoryWithIDs(threadID, "msg3")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	if entries[0].ID != "msg1" || entries[1].ID != "msg2" || entries[2].ID != "msg3" {
		t.Errorf("expected msg1,msg2,msg3, got %s,%s,%s", entries[0].ID, entries[1].ID, entries[2].ID)
	}
	if entries[0].Message.Role != "system" {
		t.Errorf("expected first entry role=system, got %s", entries[0].Message.Role)
	}
}

func TestApplyCompaction(t *testing.T) {
	entries := []HistoryEntry{
		{ID: "msg1", Message: message.Message{Role: "system", Parts: []message.Part{message.TextPart{Text: "sys"}}}},
		{ID: "msg2", ParentID: "msg1", Message: message.Message{Role: "user", Parts: []message.Part{message.TextPart{Text: "old1"}}}},
		{ID: "msg3", ParentID: "msg2", Message: message.Message{Role: "assistant", Parts: []message.Part{message.TextPart{Text: "old2"}}}},
		{ID: "msg4", ParentID: "msg3", Message: message.Message{Role: "user", Parts: []message.Part{message.TextPart{Text: "recent1"}}}},
		{ID: "msg5", ParentID: "msg4", Message: message.Message{Role: "assistant", Parts: []message.Part{message.TextPart{Text: "recent2"}}}},
	}

	// Compaction covers through msg3; msg4 and msg5 are after the leaf.
	record := &CompactionRecord{
		SummaryText:   "Summary of old conversation",
		LeafMessageID: "msg3",
	}

	result := applyCompaction(record, entries)

	// Should be: system, summary, msg4, msg5.
	if len(result) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(result))
	}
	if result[0].Role != "system" {
		t.Errorf("expected system, got %s", result[0].Role)
	}
	// Second message should be the summary.
	tp, ok := result[1].Parts[0].(message.TextPart)
	if !ok {
		t.Fatal("expected TextPart for summary")
	}
	if !strings.Contains(tp.Text, "Summary of old conversation") {
		t.Error("expected summary text in second message")
	}
	if !strings.Contains(tp.Text, "<conversation_summary>") {
		t.Error("expected <conversation_summary> wrapper")
	}
	// Messages after the leaf preserved.
	tp3, _ := result[2].Parts[0].(message.TextPart)
	if tp3.Text != "recent1" {
		t.Errorf("expected 'recent1', got %q", tp3.Text)
	}
	tp4, _ := result[3].Parts[0].(message.TextPart)
	if tp4.Text != "recent2" {
		t.Errorf("expected 'recent2', got %q", tp4.Text)
	}
}

func TestApplyCompaction_LeafIsLast(t *testing.T) {
	entries := []HistoryEntry{
		{ID: "msg1", Message: message.Message{Role: "system", Parts: []message.Part{message.TextPart{Text: "sys"}}}},
		{ID: "msg2", ParentID: "msg1", Message: message.Message{Role: "user", Parts: []message.Part{message.TextPart{Text: "hello"}}}},
		{ID: "msg3", ParentID: "msg2", Message: message.Message{Role: "assistant", Parts: []message.Part{message.TextPart{Text: "world"}}}},
	}

	// Compaction covers everything — leaf is the last entry.
	record := &CompactionRecord{
		SummaryText:   "Full conversation summary",
		LeafMessageID: "msg3",
	}

	result := applyCompaction(record, entries)

	// Should be: system + summary only, no messages after.
	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result))
	}
	if result[0].Role != "system" {
		t.Errorf("expected system, got %s", result[0].Role)
	}
	tp, ok := result[1].Parts[0].(message.TextPart)
	if !ok {
		t.Fatal("expected TextPart for summary")
	}
	if !strings.Contains(tp.Text, "Full conversation summary") {
		t.Error("expected summary text")
	}
}

func TestApplyCompaction_StaleRecord(t *testing.T) {
	entries := []HistoryEntry{
		{ID: "msg1", Message: message.Message{Role: "user", Parts: []message.Part{message.TextPart{Text: "a"}}}},
		{ID: "msg2", ParentID: "msg1", Message: message.Message{Role: "assistant", Parts: []message.Part{message.TextPart{Text: "b"}}}},
	}

	record := &CompactionRecord{
		SummaryText:   "old summary",
		LeafMessageID: "nonexistent", // stale
	}

	result := applyCompaction(record, entries)

	// Should return full history since leaf not found.
	if len(result) != 2 {
		t.Fatalf("expected 2 messages (full history), got %d", len(result))
	}
}

func TestApplyCompaction_SystemReminder(t *testing.T) {
	reminder := "<system-reminder>tool list</system-reminder>"
	entries := []HistoryEntry{
		{ID: "sys", Message: message.Message{Role: "system", Parts: []message.Part{message.TextPart{Text: "sys"}}}},
		{ID: "rem", ParentID: "sys", Message: message.Message{Role: "user", Parts: []message.Part{message.TextPart{Text: reminder}}}},
		{ID: "msg1", ParentID: "rem", Message: message.Message{Role: "user", Parts: []message.Part{message.TextPart{Text: "hello"}}}},
		{ID: "msg2", ParentID: "msg1", Message: message.Message{Role: "assistant", Parts: []message.Part{message.TextPart{Text: "world"}}}},
		{ID: "msg3", ParentID: "msg2", Message: message.Message{Role: "user", Parts: []message.Part{message.TextPart{Text: "new"}}}},
	}

	record := &CompactionRecord{SummaryText: "summary", LeafMessageID: "msg2"}
	result := applyCompaction(record, entries)

	// Expected: [sys] + [reminder] + [summary] + [msg3]
	if len(result) != 4 {
		t.Fatalf("expected 4 messages, got %d: %v", len(result), func() []string {
			var roles []string
			for _, m := range result {
				roles = append(roles, m.Role)
			}
			return roles
		}())
	}
	if result[0].Role != "system" {
		t.Errorf("result[0]: want system, got %s", result[0].Role)
	}
	tp1, _ := result[1].Parts[0].(message.TextPart)
	if !strings.Contains(tp1.Text, "<system-reminder>") {
		t.Error("result[1]: expected system-reminder to be preserved")
	}
	tp2, _ := result[2].Parts[0].(message.TextPart)
	if !strings.Contains(tp2.Text, "<conversation_summary>") {
		t.Error("result[2]: expected compaction summary")
	}
	tp3, _ := result[3].Parts[0].(message.TextPart)
	if tp3.Text != "new" {
		t.Errorf("result[3]: want 'new', got %q", tp3.Text)
	}
}

func TestFormatTranscript(t *testing.T) {
	messages := []message.Message{
		{Role: "user", Parts: []message.Part{message.TextPart{Text: "Hello"}}},
		{Role: "assistant", Parts: []message.Part{
			message.ToolCallPart{ToolCallID: "tc1", ToolName: "read_file", Input: `{"path":"x.txt"}`},
		}},
		{Role: "tool", Parts: []message.Part{
			message.ToolResultPart{ToolCallID: "tc1", ToolName: "read_file", Output: message.TextOutput{Value: "file contents"}},
		}},
	}

	transcript := formatTranscript(messages)

	if !strings.Contains(transcript, "[user]: Hello") {
		t.Error("expected user message in transcript")
	}
	if !strings.Contains(transcript, "read_file") {
		t.Error("expected tool name in transcript")
	}
	if !strings.Contains(transcript, "file contents") {
		t.Error("expected tool result in transcript")
	}
}

func TestFormatTranscript_LongToolResult(t *testing.T) {
	longOutput := strings.Repeat("x", 600)
	messages := []message.Message{
		{Role: "tool", Parts: []message.Part{
			message.ToolResultPart{ToolCallID: "tc1", ToolName: "read", Output: message.TextOutput{Value: longOutput}},
		}},
	}

	transcript := formatTranscript(messages)

	if !strings.Contains(transcript, "... [truncated]") {
		t.Error("expected truncation marker for long tool result")
	}
}

func TestToolResultOutputToString(t *testing.T) {
	tests := []struct {
		name   string
		output message.ToolResultOutput
		want   string
	}{
		{"nil", nil, ""},
		{"text", message.TextOutput{Value: "hello"}, "hello"},
		{"error", message.ErrorTextOutput{Value: "fail"}, "ERROR: fail"},
		{"denied", message.ExecutionDeniedOutput{Reason: "no"}, "DENIED: no"},
		{"json", message.JSONOutput{Value: json.RawMessage(`{"a":1}`)}, `{"a":1}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toolResultOutputToString(tt.output)
			if got != tt.want {
				t.Errorf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestMaybeCompact_NoCompactionNeeded(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread1"

	// Save a short history.
	for _, sm := range []StoredMessage{
		{ID: "msg1", Message: message.Message{Role: "user", Parts: []message.Part{message.TextPart{Text: "hi"}}}},
		{ID: "msg2", ParentID: "msg1", Message: message.Message{Role: "assistant", Parts: []message.Part{message.TextPart{Text: "hello"}}}},
	} {
		if err := store.SaveMessage(threadID, sm); err != nil {
			t.Fatal(err)
		}
	}

	entries, _ := store.BuildHistoryWithIDs(threadID, "msg2")

	cfg := &TurnConfig{Model: "test", ContextWindow: 100000, MaxOutputTokens: 4000}
	turnState := &TurnState{LeafMsgID: "msg2"}

	prov := &compactionMockProvider{tokenCountFn: charBasedTokenCount}

	result, err := maybeCompact(context.Background(), prov, store, threadID, turnState, cfg, entries)
	if err != nil {
		t.Fatal(err)
	}

	// No compaction — too few messages.
	if len(result) != 2 {
		t.Errorf("expected 2 messages (no compaction), got %d", len(result))
	}
}

func TestMaybeCompact_SkipsWhenNoContextWindow(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread1"

	for _, sm := range []StoredMessage{
		{ID: "msg1", Message: message.Message{Role: "user", Parts: []message.Part{message.TextPart{Text: "hi"}}}},
		{ID: "msg2", ParentID: "msg1", Message: message.Message{Role: "assistant", Parts: []message.Part{message.TextPart{Text: "hello"}}}},
	} {
		if err := store.SaveMessage(threadID, sm); err != nil {
			t.Fatal(err)
		}
	}

	entries, _ := store.BuildHistoryWithIDs(threadID, "msg2")

	cfg := &TurnConfig{Model: "test", ContextWindow: 0} // no context window
	turnState := &TurnState{LeafMsgID: "msg2"}

	prov := &compactionMockProvider{}

	result, err := maybeCompact(context.Background(), prov, store, threadID, turnState, cfg, entries)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 {
		t.Errorf("expected 2 messages, got %d", len(result))
	}
}

func TestMaybeCompact_CompactionTriggered(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread1"

	// Build history: 1 system + 10 conversation messages.
	msgs := []StoredMessage{
		{ID: "sys", Message: message.Message{Role: "system", Parts: []message.Part{message.TextPart{Text: "You are helpful."}}}},
	}
	prevID := "sys"
	for i := 1; i <= 10; i++ {
		role := "user"
		if i%2 == 0 {
			role = "assistant"
		}
		id := fmt.Sprintf("msg%d", i)
		msgs = append(msgs, StoredMessage{
			ID:       id,
			ParentID: prevID,
			Message: message.Message{
				Role:  role,
				Parts: []message.Part{message.TextPart{Text: strings.Repeat("x", 100)}},
			},
		})
		prevID = id
	}

	for _, sm := range msgs {
		if err := store.SaveMessage(threadID, sm); err != nil {
			t.Fatal(err)
		}
	}

	entries, _ := store.BuildHistoryWithIDs(threadID, prevID)

	// Total chars ≈ 16 (system) + 10*100 = 1016.
	// Set context window so CompactionTrigger < 1016.
	// InputLimit = cw - cw/4 = cw * 3/4. CompactionTrigger = InputLimit * 80/100 = cw * 0.6.
	// Need cw * 0.6 < 1016 → cw < 1694. Use 1600.
	cfg := &TurnConfig{Model: "test", ContextWindow: 1600}
	turnState := &TurnState{LeafMsgID: prevID}

	prov := &compactionMockProvider{
		tokenCountFn: charBasedTokenCount,
		responses: [][]message.ProviderMessageChunk{
			// Summary generation call.
			{
				message.StreamStartChunk{},
				message.TextStartChunk{ID: "s1"},
				message.TextDeltaChunk{ID: "s1", Delta: "Summary: user did stuff."},
				message.TextEndChunk{ID: "s1"},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "stop"}},
			},
		},
	}

	result, err := maybeCompact(context.Background(), prov, store, threadID, turnState, cfg, entries)
	if err != nil {
		t.Fatal(err)
	}

	// Full-conversation compaction: result should be [system] + [summary] = 2 messages.
	if len(result) != 2 {
		t.Fatalf("expected 2 messages (system + summary), got %d", len(result))
	}
	if result[0].Role != "system" {
		t.Errorf("expected first message to be system, got %s", result[0].Role)
	}

	// Second message should be the summary.
	tp, ok := result[1].Parts[0].(message.TextPart)
	if !ok {
		t.Fatal("expected TextPart for summary message")
	}
	if !strings.Contains(tp.Text, "Summary: user did stuff.") {
		t.Errorf("expected summary text, got %q", tp.Text)
	}

	// Compaction record should be persisted.
	record, err := store.LoadCompaction(threadID)
	if err != nil {
		t.Fatal(err)
	}
	if record == nil {
		t.Fatal("expected compaction record to be persisted")
	}
	if record.SummaryText != "Summary: user did stuff." {
		t.Errorf("expected persisted summary, got %q", record.SummaryText)
	}
	// LeafMessageID should be the last message in the chain.
	if record.LeafMessageID != prevID {
		t.Errorf("expected LeafMessageID=%s, got %s", prevID, record.LeafMessageID)
	}
}

func TestMaybeCompact_ExistingCompactionValid(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread1"

	// 6 messages: sys, msg1-msg5.
	msgs := []StoredMessage{
		{ID: "sys", Message: message.Message{Role: "system", Parts: []message.Part{message.TextPart{Text: "sys"}}}},
		{ID: "msg1", ParentID: "sys", Message: message.Message{Role: "user", Parts: []message.Part{message.TextPart{Text: "old"}}}},
		{ID: "msg2", ParentID: "msg1", Message: message.Message{Role: "assistant", Parts: []message.Part{message.TextPart{Text: "old"}}}},
		{ID: "msg3", ParentID: "msg2", Message: message.Message{Role: "user", Parts: []message.Part{message.TextPart{Text: "recent1"}}}},
		{ID: "msg4", ParentID: "msg3", Message: message.Message{Role: "assistant", Parts: []message.Part{message.TextPart{Text: "recent2"}}}},
		{ID: "msg5", ParentID: "msg4", Message: message.Message{Role: "user", Parts: []message.Part{message.TextPart{Text: "recent3"}}}},
	}
	for _, sm := range msgs {
		if err := store.SaveMessage(threadID, sm); err != nil {
			t.Fatal(err)
		}
	}

	// Pre-existing compaction that summarizes through msg2.
	if err := store.SaveCompaction(threadID, CompactionRecord{
		SummaryText:   "Old stuff happened.",
		LeafMessageID: "msg2",
	}); err != nil {
		t.Fatal(err)
	}

	entries, _ := store.BuildHistoryWithIDs(threadID, "msg5")

	// Context window large enough that compacted version fits.
	cfg := &TurnConfig{Model: "test", ContextWindow: 100000, MaxOutputTokens: 1000}
	turnState := &TurnState{LeafMsgID: "msg5"}

	prov := &compactionMockProvider{tokenCountFn: charBasedTokenCount}

	result, err := maybeCompact(context.Background(), prov, store, threadID, turnState, cfg, entries)
	if err != nil {
		t.Fatal(err)
	}

	// Should reuse existing compaction: sys, summary, msg3, msg4, msg5.
	if len(result) != 5 {
		t.Fatalf("expected 5 messages, got %d", len(result))
	}
	if result[0].Role != "system" {
		t.Errorf("expected system, got %s", result[0].Role)
	}
	tp, _ := result[1].Parts[0].(message.TextPart)
	if !strings.Contains(tp.Text, "Old stuff happened.") {
		t.Error("expected existing summary reused")
	}

	// Provider should NOT have been called for completion (no re-summarization).
	if prov.callIndex != 0 {
		t.Errorf("expected 0 Complete calls (reusing existing compaction), got %d", prov.callIndex)
	}
}

func TestMaybeCompact_ReCompaction(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread1"

	// Build history: sys + 10 long messages.
	msgs := []StoredMessage{
		{ID: "sys", Message: message.Message{Role: "system", Parts: []message.Part{message.TextPart{Text: "sys"}}}},
	}
	prevID := "sys"
	for i := 1; i <= 10; i++ {
		role := "user"
		if i%2 == 0 {
			role = "assistant"
		}
		id := fmt.Sprintf("msg%d", i)
		msgs = append(msgs, StoredMessage{
			ID:       id,
			ParentID: prevID,
			Message: message.Message{
				Role:  role,
				Parts: []message.Part{message.TextPart{Text: strings.Repeat("z", 480)}},
			},
		})
		prevID = id
	}
	for _, sm := range msgs {
		if err := store.SaveMessage(threadID, sm); err != nil {
			t.Fatal(err)
		}
	}

	// Pre-existing compaction that covers through msg4.
	if err := store.SaveCompaction(threadID, CompactionRecord{
		SummaryText:   "Old partial summary.",
		LeafMessageID: "msg4",
	}); err != nil {
		t.Fatal(err)
	}

	entries, _ := store.BuildHistoryWithIDs(threadID, prevID)

	// Context window sized so the compacted version (summary + msg5-msg10 @ 480 chars each)
	// exceeds InputLimit (triggering re-compaction), but messagesToSummarize fits within
	// SummarizationLimit for a single summarisation call.
	// compacted chars ≈ sys(3) + summary(155) + 6*480 = 3038. InputLimit = 4000*0.75 = 3000 < 3038.
	// messagesToSummarize candidate ≈ 155 + 6*480 + summaryRequestPrompt = 3348. SummarizationLimit = 3400.
	cfg := &TurnConfig{Model: "test", ContextWindow: 4000}
	turnState := &TurnState{LeafMsgID: prevID}

	prov := &compactionMockProvider{
		tokenCountFn: charBasedTokenCount,
		responses: [][]message.ProviderMessageChunk{
			// Fresh summary generation.
			{
				message.StreamStartChunk{},
				message.TextStartChunk{ID: "s1"},
				message.TextDeltaChunk{ID: "s1", Delta: "Fresh full summary."},
				message.TextEndChunk{ID: "s1"},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "stop"}},
			},
		},
	}

	result, err := maybeCompact(context.Background(), prov, store, threadID, turnState, cfg, entries)
	if err != nil {
		t.Fatal(err)
	}

	// Re-compaction: [system] + [summary] = 2 messages.
	if len(result) != 2 {
		t.Fatalf("expected 2 messages after re-compaction, got %d", len(result))
	}

	// Summary should be the fresh one, not the old one.
	tp, _ := result[1].Parts[0].(message.TextPart)
	if !strings.Contains(tp.Text, "Fresh full summary.") {
		t.Error("expected fresh summary after re-compaction")
	}

	// Record should be updated with new leaf and summary.
	record, _ := store.LoadCompaction(threadID)
	if record == nil {
		t.Fatal("expected compaction record")
	}
	if record.SummaryText != "Fresh full summary." {
		t.Errorf("expected fresh summary in record, got %q", record.SummaryText)
	}
	if record.LeafMessageID != prevID {
		t.Errorf("expected LeafMessageID=%s, got %s", prevID, record.LeafMessageID)
	}

	// The LLM should have received the compacted base (old summary + new messages)
	// rather than the full raw history. In the inline approach the messages are
	// passed directly, so verify by message count and content.
	if len(prov.requests) == 0 {
		t.Fatal("expected LLM to be called for summarization")
	}
	summaryReq := prov.requests[0]

	// Compacted base: [summary] + [msg5-msg10] + [summaryRequestPrompt] = 8 messages.
	// Full raw would be [msg1-msg10] + [summaryRequestPrompt] = 11 messages.
	if len(summaryReq.Messages) >= 11 {
		t.Errorf("expected compacted input (<11 messages), got %d — old raw history was not replaced", len(summaryReq.Messages))
	}

	var transcript string
	for _, msg := range summaryReq.Messages {
		for _, part := range msg.Parts {
			if tp, ok := part.(message.TextPart); ok {
				transcript += tp.Text
			}
		}
	}
	if !strings.Contains(transcript, "Old partial summary.") {
		t.Error("expected old summary text to appear in summarization input")
	}
}

// TestMaybeCompact_SystemRemindersDontInflateCount verifies that system-reminder
// messages do not count towards the real-message threshold. A thread with many
// reminders but few real messages should not be compacted.
func TestMaybeCompact_SystemRemindersDontInflateCount(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread1"

	reminder := "<system-reminder>date: today</system-reminder>"
	msgs := []StoredMessage{
		{ID: "sys", Message: message.Message{Role: "system", Parts: []message.Part{message.TextPart{Text: "sys"}}}},
	}
	// Add 6 system-reminder messages interspersed with only 3 real messages.
	prevID := "sys"
	for i := 1; i <= 6; i++ {
		id := fmt.Sprintf("rem%d", i)
		msgs = append(msgs, StoredMessage{
			ID:       id,
			ParentID: prevID,
			Message:  message.Message{Role: "user", Parts: []message.Part{message.TextPart{Text: reminder}}},
		})
		prevID = id
		if i <= 3 {
			msgID := fmt.Sprintf("msg%d", i)
			role := "user"
			if i%2 == 0 {
				role = "assistant"
			}
			msgs = append(msgs, StoredMessage{
				ID:       msgID,
				ParentID: prevID,
				Message:  message.Message{Role: role, Parts: []message.Part{message.TextPart{Text: "real content"}}},
			})
			prevID = msgID
		}
	}
	for _, sm := range msgs {
		if err := store.SaveMessage(threadID, sm); err != nil {
			t.Fatal(err)
		}
	}

	entries, _ := store.BuildHistoryWithIDs(threadID, prevID)

	// Large context window so token count is not the trigger — only the
	// real-message threshold matters.
	cfg := &TurnConfig{Model: "test", ContextWindow: 1_000_000}
	turnState := &TurnState{LeafMsgID: prevID}

	prov := &compactionMockProvider{tokenCountFn: charBasedTokenCount}

	result, err := maybeCompact(context.Background(), prov, store, threadID, turnState, cfg, entries)
	if err != nil {
		t.Fatal(err)
	}

	// Should return full history untouched — only 3 real messages, below threshold.
	if len(result) != len(entries) {
		t.Errorf("expected full history (%d msgs), got %d — system reminders incorrectly inflated count",
			len(entries), len(result))
	}
}

// TestMaybeCompact_SystemRemindersFilteredFromSummaryInput verifies that
// system-reminder messages are stripped from messagesToSummarize so the LLM
// only receives real conversation content.
func TestMaybeCompact_SystemRemindersFilteredFromSummaryInput(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread1"

	reminder := "<system-reminder>date: today</system-reminder>"
	msgs := []StoredMessage{
		{ID: "sys", Message: message.Message{Role: "system", Parts: []message.Part{message.TextPart{Text: "sys"}}}},
	}
	// 5 real messages interleaved with reminders — enough to trigger compaction.
	prevID := "sys"
	for i := 1; i <= 5; i++ {
		remID := fmt.Sprintf("rem%d", i)
		msgs = append(msgs, StoredMessage{
			ID:       remID,
			ParentID: prevID,
			Message:  message.Message{Role: "user", Parts: []message.Part{message.TextPart{Text: reminder}}},
		})
		prevID = remID
		msgID := fmt.Sprintf("msg%d", i)
		role := "user"
		if i%2 == 0 {
			role = "assistant"
		}
		msgs = append(msgs, StoredMessage{
			ID:       msgID,
			ParentID: prevID,
			Message:  message.Message{Role: role, Parts: []message.Part{message.TextPart{Text: strings.Repeat("x", 200)}}},
		})
		prevID = msgID
	}
	for _, sm := range msgs {
		if err := store.SaveMessage(threadID, sm); err != nil {
			t.Fatal(err)
		}
	}

	entries, _ := store.BuildHistoryWithIDs(threadID, prevID)

	// Context window sized so compaction is triggered but messagesToSummarize fits
	// in a single summarisation call (SummarizationLimit = 1700 - 255 = 1445, candidate ≈ 1313).
	cfg := &TurnConfig{Model: "test", ContextWindow: 1700}
	turnState := &TurnState{LeafMsgID: prevID}

	prov := &compactionMockProvider{
		tokenCountFn: charBasedTokenCount,
		responses: [][]message.ProviderMessageChunk{
			{
				message.StreamStartChunk{},
				message.TextStartChunk{ID: "s1"},
				message.TextDeltaChunk{ID: "s1", Delta: "Clean summary."},
				message.TextEndChunk{ID: "s1"},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "stop"}},
			},
		},
	}

	_, err := maybeCompact(context.Background(), prov, store, threadID, turnState, cfg, entries)
	if err != nil {
		t.Fatal(err)
	}

	// Verify the transcript sent to the LLM contains no system-reminder content.
	if len(prov.requests) == 0 {
		t.Fatal("expected LLM to be called for summarization")
	}
	var transcript string
	for _, msg := range prov.requests[0].Messages {
		for _, part := range msg.Parts {
			if tp, ok := part.(message.TextPart); ok {
				transcript += tp.Text
			}
		}
	}
	if strings.Contains(transcript, "<system-reminder>") {
		t.Error("system-reminder content leaked into summarization input")
	}
}

// TestSafeSplitPoint verifies that split points never land mid-tool-call.
func TestSafeSplitPoint(t *testing.T) {
	tc := func(role string, toolCalls ...string) message.Message {
		msg := message.Message{Role: role}
		for _, id := range toolCalls {
			msg.Parts = append(msg.Parts, message.ToolCallPart{ToolCallID: id, ToolName: "fn"})
		}
		return msg
	}
	tr := func(id string) message.Message {
		return message.Message{Role: "tool", Parts: []message.Part{
			message.ToolResultPart{ToolCallID: id, ToolName: "fn"},
		}}
	}
	user := message.Message{Role: "user", Parts: []message.Part{message.TextPart{Text: "hi"}}}
	asst := message.Message{Role: "assistant", Parts: []message.Part{message.TextPart{Text: "ok"}}} // no tools

	msgs := []message.Message{
		user,                          // 0 — safe after index 1
		tc("assistant", "tc1"),        // 1 — NOT safe (pending tool call)
		tr("tc1"),                     // 2 — safe after index 3 (all resolved)
		user,                          // 3 — safe after index 4
		tc("assistant", "tc2", "tc3"), // 4 — NOT safe
		tr("tc2"),                     // 5 — NOT safe (tc3 still pending)
		tr("tc3"),                     // 6 — safe after index 7 (all resolved)
		asst,                          // 7 — safe after index 8 (no tool calls)
	}

	tests := []struct {
		n    int
		want int
	}{
		{n: 8, want: 8}, // last msg is assistant without tools — safe
		{n: 7, want: 7}, // after tr("tc3") — all resolved, safe
		{n: 6, want: 4}, // after tr("tc2") — tc3 still pending, scan back to after user at idx 3
		{n: 5, want: 4}, // after tc("assistant","tc2","tc3") — not safe, back to user at idx 3
		{n: 4, want: 4}, // after user — safe
		{n: 3, want: 3}, // after tr("tc1") — all resolved, safe
		{n: 2, want: 1}, // after tc("assistant","tc1") — not safe, back to user at idx 0
		{n: 1, want: 1}, // after user — safe
		{n: 0, want: 0}, // nothing
	}
	for _, tt := range tests {
		got := safeSplitPoint(msgs, tt.n)
		if got != tt.want {
			t.Errorf("safeSplitPoint(msgs, %d) = %d, want %d", tt.n, got, tt.want)
		}
	}
}

// TestGenerateSummary_IterativeCompaction verifies that generateSummary splits
// when the first full-list call fails, then succeeds with partial+final calls.
func TestGenerateSummary_IterativeCompaction(t *testing.T) {
	// failFirstCallProvider intercepts call 1 (returns context_length error).
	// The inner provider handles the remaining calls:
	//   Call 2: first-half partial summary → succeeds.
	//   Call 3: final summary on reduced list → succeeds.
	prov := &compactionMockProvider{
		tokenCountFn: charBasedTokenCount,
		responses: [][]message.ProviderMessageChunk{
			// Call 2: first half partial summary.
			{
				message.StreamStartChunk{},
				message.TextStartChunk{ID: "s1"},
				message.TextDeltaChunk{ID: "s1", Delta: "Partial summary of early messages."},
				message.TextEndChunk{ID: "s1"},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "stop"}},
			},
			// Call 3: final summary on reduced list.
			{
				message.StreamStartChunk{},
				message.TextStartChunk{ID: "s2"},
				message.TextDeltaChunk{ID: "s2", Delta: "Full conversation summary."},
				message.TextEndChunk{ID: "s2"},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "stop"}},
			},
		},
	}

	// failFirstCallProvider intercepts the first Complete call with a
	// context_length_exceeded error; subsequent calls go to the inner provider.
	failFirstProv := &failFirstCallProvider{inner: prov}

	var msgs []message.Message
	for i := 0; i < 6; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		msgs = append(msgs, message.Message{
			Role:  role,
			Parts: []message.Part{message.TextPart{Text: strings.Repeat("x", 100)}},
		})
	}

	budget := tokenBudget{InputLimit: 400, SummaryMaxTokens: 80}
	cfg := &TurnConfig{Model: "test"}

	text, err := generateSummary(context.Background(), failFirstProv, cfg, msgs, budget)
	if err != nil {
		t.Fatal(err)
	}
	if text != "Full conversation summary." {
		t.Errorf("expected final summary text, got %q", text)
	}
	// Should have made exactly 3 calls: 1 failed full + 1 partial + 1 final.
	if failFirstProv.callCount != 3 {
		t.Errorf("expected 3 LLM calls, got %d", failFirstProv.callCount)
	}
}

// failFirstCallProvider wraps compactionMockProvider and fails the very first
// Complete call with a context_length_exceeded error.
type failFirstCallProvider struct {
	inner     *compactionMockProvider
	callCount int
}

func (f *failFirstCallProvider) ID() string { return f.inner.ID() }
func (f *failFirstCallProvider) Complete(ctx context.Context, req providers.CompleteRequest) iter.Seq2[message.ProviderMessageChunk, error] {
	f.callCount++
	if f.callCount == 1 {
		return func(yield func(message.ProviderMessageChunk, error) bool) {
			yield(nil, fmt.Errorf("openai: stream error: context_length_exceeded"))
		}
	}
	return f.inner.Complete(ctx, req)
}
func (f *failFirstCallProvider) CountTokens(ctx context.Context, req providers.CountTokensRequest) (providers.CountTokensResponse, error) {
	return f.inner.CountTokens(ctx, req)
}
func (f *failFirstCallProvider) DefaultModels() map[string]providers.ModelRef {
	return f.inner.DefaultModels()
}
func (f *failFirstCallProvider) ListModels(ctx context.Context) ([]providers.ModelInfo, error) {
	return f.inner.ListModels(ctx)
}

func TestMaybeCompact_CountTokensFails(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread1"

	msgs := []StoredMessage{
		{ID: "msg1", Message: message.Message{Role: "user", Parts: []message.Part{message.TextPart{Text: "a"}}}},
		{ID: "msg2", ParentID: "msg1", Message: message.Message{Role: "assistant", Parts: []message.Part{message.TextPart{Text: "b"}}}},
		{ID: "msg3", ParentID: "msg2", Message: message.Message{Role: "user", Parts: []message.Part{message.TextPart{Text: "c"}}}},
		{ID: "msg4", ParentID: "msg3", Message: message.Message{Role: "assistant", Parts: []message.Part{message.TextPart{Text: "d"}}}},
		{ID: "msg5", ParentID: "msg4", Message: message.Message{Role: "user", Parts: []message.Part{message.TextPart{Text: "e"}}}},
	}
	for _, sm := range msgs {
		if err := store.SaveMessage(threadID, sm); err != nil {
			t.Fatal(err)
		}
	}

	entries, _ := store.BuildHistoryWithIDs(threadID, "msg5")

	cfg := &TurnConfig{Model: "test", ContextWindow: 100}
	turnState := &TurnState{LeafMsgID: "msg5"}

	// CountTokens always fails.
	prov := &compactionMockProvider{
		tokenCountFn: func(_ providers.CountTokensRequest) (providers.CountTokensResponse, error) {
			return providers.CountTokensResponse{}, fmt.Errorf("network error")
		},
	}

	result, err := maybeCompact(context.Background(), prov, store, threadID, turnState, cfg, entries)

	// Should fall back to full history.
	if err == nil {
		t.Error("expected error from CountTokens failure")
	}
	if len(result) != 5 {
		t.Errorf("expected 5 messages (full history fallback), got %d", len(result))
	}
}

func TestMakeSummaryMessage(t *testing.T) {
	msg := makeSummaryMessage("User asked about Go.")

	if msg.Role != "user" {
		t.Errorf("expected role=user, got %s", msg.Role)
	}
	tp, ok := msg.Parts[0].(message.TextPart)
	if !ok {
		t.Fatal("expected TextPart")
	}
	if !strings.Contains(tp.Text, "<conversation_summary>") {
		t.Error("expected <conversation_summary> wrapper")
	}
	if !strings.Contains(tp.Text, "User asked about Go.") {
		t.Error("expected summary text")
	}
	if !strings.Contains(tp.Text, "Continue from where we left off") {
		t.Error("expected continuation instruction")
	}
	if string(msg.Metadata) != `{"compaction":true}` {
		t.Errorf("expected compaction metadata, got %s", string(msg.Metadata))
	}
}

func TestEntriesToMessages(t *testing.T) {
	entries := []HistoryEntry{
		{ID: "a", Message: message.Message{Role: "user"}},
		{ID: "b", Message: message.Message{Role: "assistant"}},
	}
	msgs := entriesToMessages(entries)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[1].Role != "assistant" {
		t.Error("message roles don't match")
	}
}

// TestRunTurn_WithCompaction verifies end-to-end compaction within a RunTurn call.
func TestRunTurn_WithCompaction(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread1"

	// Pre-populate a long conversation history.
	prevID := ""
	for i := 0; i < 10; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		id := fmt.Sprintf("msg%d", i)
		if err := store.SaveMessage(threadID, StoredMessage{
			ID:       id,
			ParentID: prevID,
			Message: message.Message{
				Role:  role,
				Parts: []message.Part{message.TextPart{Text: strings.Repeat("x", 200)}},
			},
		}); err != nil {
			t.Fatal(err)
		}
		prevID = id
	}

	prov := &compactionMockProvider{
		tokenCountFn: charBasedTokenCount,
		responses: [][]message.ProviderMessageChunk{
			// Summary generation call.
			{
				message.StreamStartChunk{},
				message.TextStartChunk{ID: "s1"},
				message.TextDeltaChunk{ID: "s1", Delta: "Conversation summary."},
				message.TextEndChunk{ID: "s1"},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "stop"}},
			},
			// Actual turn completion (after compaction).
			{
				message.StreamStartChunk{},
				message.TextStartChunk{ID: "t1"},
				message.TextDeltaChunk{ID: "t1", Delta: "Response after compaction"},
				message.TextEndChunk{ID: "t1"},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "stop"}},
			},
		},
	}

	// Context window sized so compaction is triggered and messagesToSummarize
	// fits in a single summarisation call.
	// Total chars ≈ 10*200 + "new message"(11) = 2011. CompactionTrigger = 3000*0.6 = 1800 < 2011.
	// SummarizationLimit = 3000 - 450 = 2550 > candidate(2011+313 = 2324).
	chunks := collectChunks(t, RunTurn(
		context.Background(), prov, &mockExecutor{}, store,
		threadID, prevID, TurnConfig{
			Model:         "test-model",
			UserParts:     []message.Part{message.TextPart{Text: "new message"}},
			ContextWindow: 3000,
		},
	))

	// Should have received text from the main completion.
	var hasResponse bool
	for _, c := range chunks {
		if v, ok := c.(message.TextDeltaChunk); ok && v.Delta == "Response after compaction" {
			hasResponse = true
		}
	}
	if !hasResponse {
		t.Error("expected response after compaction")
	}

	// Compaction record should exist.
	record, _ := store.LoadCompaction(threadID)
	if record == nil {
		t.Error("expected compaction record to be saved")
	}

	// The second LLM call (main completion) should have received the compacted history.
	if len(prov.requests) < 2 {
		t.Fatalf("expected at least 2 LLM calls, got %d", len(prov.requests))
	}
	mainReq := prov.requests[1]

	// Compacted: summary + user message (no full 11-message history).
	if len(mainReq.Messages) >= 11 {
		t.Errorf("expected compacted history (fewer than 11 messages), got %d", len(mainReq.Messages))
	}

	// The compacted history should contain the summary.
	hasSummary := false
	for _, msg := range mainReq.Messages {
		for _, part := range msg.Parts {
			if tp, ok := part.(message.TextPart); ok && strings.Contains(tp.Text, "Conversation summary.") {
				hasSummary = true
			}
		}
	}
	if !hasSummary {
		t.Error("expected summary message in compacted history")
	}
}

func TestCrashRecovery_WithCompaction(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread1"

	// Pre-populate long history.
	prevID := ""
	for i := 0; i < 10; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		id := fmt.Sprintf("msg%d", i)
		if err := store.SaveMessage(threadID, StoredMessage{
			ID: id, ParentID: prevID,
			Message: message.Message{Role: role, Parts: []message.Part{message.TextPart{Text: strings.Repeat("y", 200)}}},
		}); err != nil {
			t.Fatal(err)
		}
		prevID = id
	}

	// Existing compaction covering through msg5.
	if err := store.SaveCompaction(threadID, CompactionRecord{
		SummaryText:   "Earlier conversation about coding.",
		LeafMessageID: "msg5",
	}); err != nil {
		t.Fatal(err)
	}

	// Simulate a user message that was saved before crash.
	userMsgID := "msg-user"
	if err := store.SaveMessage(threadID, StoredMessage{
		ID: userMsgID, ParentID: prevID,
		Message: message.Message{Role: "user", Parts: []message.Part{message.TextPart{Text: "continue"}}},
	}); err != nil {
		t.Fatal(err)
	}

	turnState := &TurnState{
		ID: "turn1", ThreadID: threadID,
		CurrentStep: 0, Phase: PhaseStreaming,
		LeafMsgID: userMsgID,
		Config: TurnConfig{
			Model:         "test-model",
			ContextWindow: 100000, // large enough that existing compaction suffices
		},
	}
	if err := store.SaveTurnState(threadID, *turnState); err != nil {
		t.Fatal(err)
	}

	prov := &compactionMockProvider{
		tokenCountFn: charBasedTokenCount,
		responses: [][]message.ProviderMessageChunk{
			{
				message.StreamStartChunk{},
				message.TextStartChunk{ID: "t1"},
				message.TextDeltaChunk{ID: "t1", Delta: "Resumed!"},
				message.TextEndChunk{ID: "t1"},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "stop"}},
			},
		},
	}

	chunks := collectChunks(t, ResumeTurn(context.Background(), prov, &mockExecutor{}, store, turnState))

	var hasText bool
	for _, c := range chunks {
		if v, ok := c.(message.TextDeltaChunk); ok && v.Delta == "Resumed!" {
			hasText = true
		}
	}
	if !hasText {
		t.Error("expected text after resume with compaction")
	}

	// The LLM should have received the compacted history (with summary, not full).
	if len(prov.requests) < 1 {
		t.Fatal("expected at least 1 LLM call")
	}
	msgs := prov.requests[0].Messages
	hasSummary := false
	for _, msg := range msgs {
		for _, part := range msg.Parts {
			if tp, ok := part.(message.TextPart); ok && strings.Contains(tp.Text, "Earlier conversation about coding.") {
				hasSummary = true
			}
		}
	}
	if !hasSummary {
		t.Error("expected existing compaction summary in resumed turn's history")
	}
}

// TestRunTurn_EmergencyCompaction verifies that a context_length_exceeded error
// during the main LLM call triggers emergency compaction and a retry.
func TestRunTurn_EmergencyCompaction(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread1"

	// Pre-populate history with enough messages to compact.
	prevID := ""
	for i := 0; i < 8; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		id := fmt.Sprintf("msg%d", i)
		if err := store.SaveMessage(threadID, StoredMessage{
			ID:       id,
			ParentID: prevID,
			Message: message.Message{
				Role:  role,
				Parts: []message.Part{message.TextPart{Text: strings.Repeat("x", 100)}},
			},
		}); err != nil {
			t.Fatal(err)
		}
		prevID = id
	}

	prov := &compactionMockProvider{
		tokenCountFn: charBasedTokenCount,
		responses: [][]message.ProviderMessageChunk{
			// Call 1: main completion fails with context_length_exceeded.
			// (Handled by failFirstCallProvider below.)
			// Call 2: summary generation (emergency compaction).
			{
				message.StreamStartChunk{},
				message.TextStartChunk{ID: "s1"},
				message.TextDeltaChunk{ID: "s1", Delta: "Emergency summary."},
				message.TextEndChunk{ID: "s1"},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "stop"}},
			},
			// Call 3: retry main completion with compacted history.
			{
				message.StreamStartChunk{},
				message.TextStartChunk{ID: "t1"},
				message.TextDeltaChunk{ID: "t1", Delta: "Response after emergency compaction"},
				message.TextEndChunk{ID: "t1"},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "stop"}},
			},
		},
	}

	// failFirstCallProvider makes the very first Complete call fail with context_length_exceeded.
	failProv := &failFirstCallProvider{inner: prov}

	// No ContextWindow set — so pre-emptive compaction is skipped.
	// Emergency compaction should still fire on the context_length_exceeded error.
	chunks := collectChunks(t, RunTurn(
		context.Background(), failProv, &mockExecutor{}, store,
		threadID, prevID, TurnConfig{
			Model:     "test-model",
			UserParts: []message.Part{message.TextPart{Text: "hi"}},
			// No ContextWindow — forces code to rely on error-driven compaction.
		},
	))

	var hasResponse bool
	for _, c := range chunks {
		if v, ok := c.(message.TextDeltaChunk); ok && v.Delta == "Response after emergency compaction" {
			hasResponse = true
		}
	}
	if !hasResponse {
		t.Error("expected response after emergency compaction")
	}

	// A compaction record must have been saved.
	record, _ := store.LoadCompaction(threadID)
	if record == nil {
		t.Error("expected emergency compaction record to be saved")
	}
}

func TestForceCompactThread_CompactsImmediately(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread-force-compact"

	msgs := []StoredMessage{
		{ID: "sys", Message: message.Message{Role: "system", Parts: []message.Part{message.TextPart{Text: "system"}}}},
		{ID: "msg1", ParentID: "sys", Message: message.Message{Role: "user", Parts: []message.Part{message.TextPart{Text: "hello"}}}},
		{ID: "msg2", ParentID: "msg1", Message: message.Message{Role: "assistant", Parts: []message.Part{message.TextPart{Text: "world"}}}},
	}
	for _, sm := range msgs {
		if err := store.SaveMessage(threadID, sm); err != nil {
			t.Fatal(err)
		}
	}

	prov := &compactionMockProvider{
		responses: [][]message.ProviderMessageChunk{
			{
				message.StreamStartChunk{},
				message.TextStartChunk{ID: "s1"},
				message.TextDeltaChunk{ID: "s1", Delta: "Forced summary."},
				message.TextEndChunk{ID: "s1"},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "stop"}},
			},
		},
	}

	compacted, err := ForceCompactThread(context.Background(), prov, store, threadID, "msg2", &TurnConfig{ProviderID: "mock", Model: "test-model"})
	if err != nil {
		t.Fatal(err)
	}
	if !compacted {
		t.Fatal("expected compaction to run")
	}
	if prov.callIndex != 1 {
		t.Fatalf("expected 1 summary call, got %d", prov.callIndex)
	}

	record, err := store.LoadCompaction(threadID)
	if err != nil {
		t.Fatal(err)
	}
	if record == nil {
		t.Fatal("expected compaction record")
	}
	if record.SummaryText != "Forced summary." {
		t.Fatalf("expected summary to be persisted, got %q", record.SummaryText)
	}
}

func TestForceCompactThread_NoConversationContent(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread-empty"

	if err := store.SaveMessage(threadID, StoredMessage{
		ID:      "sys",
		Message: message.Message{Role: "system", Parts: []message.Part{message.TextPart{Text: "system"}}},
	}); err != nil {
		t.Fatal(err)
	}

	prov := &compactionMockProvider{}

	compacted, err := ForceCompactThread(context.Background(), prov, store, threadID, "sys", &TurnConfig{ProviderID: "mock", Model: "test-model"})
	if err != nil {
		t.Fatal(err)
	}
	if compacted {
		t.Fatal("expected no compaction when there is no real conversation content")
	}
	if prov.callIndex != 0 {
		t.Fatalf("expected no summary calls, got %d", prov.callIndex)
	}

	record, err := store.LoadCompaction(threadID)
	if err != nil {
		t.Fatal(err)
	}
	if record != nil {
		t.Fatal("expected no compaction record")
	}
}
