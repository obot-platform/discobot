package thread

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/obot-platform/discobot/agent-go/message"
	"github.com/obot-platform/discobot/agent-go/providers"
	"github.com/obot-platform/discobot/agent-go/providers/modelsdev"
)

// CompactionRecord is the on-disk record of a message history compaction.
// Stored at {threadID}/compaction.json. Non-destructive: the original
// message chain remains intact on disk.
type CompactionRecord struct {
	SummaryText   string    `json:"summaryText"`
	LeafMessageID string    `json:"leafMessageId"` // everything up to and including this message is summarized
	SummaryTokens int       `json:"summaryTokens"`
	Model         string    `json:"model"`
	CreatedAt     time.Time `json:"createdAt"`
}

// tokenBudget holds the calculated token budgets for compaction.
type tokenBudget struct {
	InputLimit        int // max tokens for input (history + tools)
	CompactionTrigger int // 80% of InputLimit — threshold to fire compaction
	SummaryMaxTokens  int // 20% of InputLimit — cap on generated summary
}

// computeBudget calculates token budgets from the model's context window.
// Explicit values in cfg take precedence; if unset, metadata is looked up
// from the embedded models.dev data using cfg.ProviderID and cfg.Model.
func computeBudget(cfg *TurnConfig) tokenBudget {
	cw := cfg.ContextWindow
	outputReserve := cfg.MaxOutputTokens

	// Fall back to models.dev lookup when not explicitly configured.
	if cw == 0 {
		if md := modelsdev.Lookup(cfg.ProviderID, cfg.Model); md != nil {
			cw = md.ContextWindow
			if outputReserve == 0 {
				outputReserve = md.MaxOutputTokens
			}
		}
	}

	if cw == 0 {
		return tokenBudget{} // no context window info — skip compaction
	}

	// Reserve for output: use MaxOutputTokens if available, else 25%.
	if outputReserve == 0 {
		outputReserve = cw / 4
	}

	// Input budget is everything minus the output reserve.
	inputLimit := cw - outputReserve

	// Compaction fires at 80% of the input budget.
	compactionTrigger := inputLimit * 80 / 100

	// Summary generation gets at most 20% of the input budget.
	summaryMaxTokens := inputLimit * 20 / 100

	return tokenBudget{
		InputLimit:        inputLimit,
		CompactionTrigger: compactionTrigger,
		SummaryMaxTokens:  summaryMaxTokens,
	}
}

// isContextLengthExceeded reports whether err is a context-window overflow
// error from any provider (e.g. "context_length_exceeded", "context window",
// "maximum context length", Anthropic's "prompt is too long").
func isContextLengthExceeded(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "context_length_exceeded") ||
		strings.Contains(s, "context window") ||
		strings.Contains(s, "maximum context length") ||
		strings.Contains(s, "exceeds the context") ||
		strings.Contains(s, "too many tokens") ||
		strings.Contains(s, "prompt is too long")
}

func isContextCancellation(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "context canceled") ||
		strings.Contains(s, "context cancelled") ||
		strings.Contains(s, "deadline exceeded")
}

// maybeCompact checks if the conversation history approaches the context
// window limit and, if so, summarizes the entire conversation into a compact form.
// Returns the (possibly compacted) history ready for the LLM call.
//
// Non-destructive: never modifies messages on disk. Persists a
// CompactionRecord to {threadDir}/compaction.json for reuse.
func maybeCompact(
	ctx context.Context,
	provider providers.Provider,
	store *Store,
	threadID string,
	_ *TurnState,
	cfg *TurnConfig,
	historyEntries []HistoryEntry,
) ([]message.Message, error) {
	budget := computeBudget(cfg)
	if budget.InputLimit == 0 {
		return entriesToMessages(historyEntries), nil
	}

	// Too few real messages to compact.
	// System-reminder messages are framework-injected per-turn and don't count
	// as conversation content for the purposes of this threshold.
	realMsgCount := 0
	for _, e := range historyEntries {
		if e.Message.Role != "system" && !isSystemReminder(e.Message) {
			realMsgCount++
		}
	}
	if realMsgCount <= 4 {
		return entriesToMessages(historyEntries), nil
	}
	fullHistory := entriesToMessages(historyEntries)

	// Check if existing compaction applies.
	existing, _ := store.LoadCompaction(threadID)
	if existing != nil {
		compacted := applyCompaction(existing, historyEntries)

		tokenCount, err := provider.CountTokens(ctx, providers.CountTokensRequest{
			Model:    providers.ModelRef{ProviderID: cfg.ProviderID, ModelID: cfg.Model},
			Messages: compacted,
			Tools:    cfg.Tools,
		})
		if err != nil {
			return fullHistory, fmt.Errorf("count tokens: %w", err)
		}

		if tokenCount.TotalTokens <= budget.InputLimit {
			return compacted, nil
		}
		// Existing compaction no longer fits — re-compact using it as the base
		// so the LLM sees [old summary + new messages] rather than the full raw
		// history. This avoids re-processing already-summarised messages.
		return performCompaction(ctx, provider, store, threadID, cfg, historyEntries, compacted, budget)
	}

	// No existing compaction — count tokens on the full history.
	tokenCount, err := provider.CountTokens(ctx, providers.CountTokensRequest{
		Model:    providers.ModelRef{ProviderID: cfg.ProviderID, ModelID: cfg.Model},
		Messages: fullHistory,
		Tools:    cfg.Tools,
	})
	if err != nil {
		return fullHistory, fmt.Errorf("count tokens: %w", err)
	}

	// Compact at 80% of input budget (CompactionTrigger).
	if tokenCount.TotalTokens <= budget.CompactionTrigger {
		return fullHistory, nil
	}

	// Perform first-time compaction of the full conversation.
	return performCompaction(ctx, provider, store, threadID, cfg, historyEntries, nil, budget)
}

// forceCompact unconditionally compacts the conversation, ignoring any
// compaction trigger threshold. Used as a recovery path after the provider
// rejects a request with a context_length_exceeded error.
func forceCompact(
	ctx context.Context,
	provider providers.Provider,
	store *Store,
	threadID string,
	cfg *TurnConfig,
	historyEntries []HistoryEntry,
) ([]message.Message, error) {
	budget := computeBudget(cfg)
	if budget.InputLimit == 0 {
		// No context window info: use a conservative synthetic budget to make
		// summarisation possible (128k input, 20% summary cap).
		budget = tokenBudget{
			InputLimit:        128_000,
			CompactionTrigger: 102_400,
			SummaryMaxTokens:  25_600,
		}
	}

	// Check if existing compaction applies and build the base messages.
	existing, _ := store.LoadCompaction(threadID)
	var baseMessages []message.Message
	if existing != nil {
		baseMessages = applyCompaction(existing, historyEntries)
	}

	return performCompaction(ctx, provider, store, threadID, cfg, historyEntries, baseMessages, budget)
}

// ForceCompactThread forces compaction immediately for the specified thread leaf.
// Returns true when compaction ran, or false when there is no real conversation
// content to compact yet.
func ForceCompactThread(
	ctx context.Context,
	provider providers.Provider,
	store *Store,
	threadID string,
	leafID string,
	cfg *TurnConfig,
) (bool, error) {
	if leafID == "" {
		return false, nil
	}

	historyEntries, err := store.BuildHistoryWithIDs(threadID, leafID)
	if err != nil {
		return false, fmt.Errorf("build history: %w", err)
	}

	realMsgCount := 0
	for _, e := range historyEntries {
		if e.Message.Role != "system" && !isSystemReminder(e.Message) {
			realMsgCount++
		}
	}
	if realMsgCount == 0 {
		return false, nil
	}

	if _, err := forceCompact(ctx, provider, store, threadID, cfg, historyEntries); err != nil {
		return false, err
	}

	return true, nil
}

// performCompaction summarizes a conversation and returns compacted history.
//
// baseMessages, when non-nil, is used as the input to summarisation instead of
// the raw full history. Pass the already-compacted form (old summary + new
// messages) here on re-compaction so the LLM builds on the previous summary
// rather than re-processing all raw messages from scratch.
// Pass nil for a first-time compaction of the full conversation.
func performCompaction(
	ctx context.Context,
	provider providers.Provider,
	store *Store,
	threadID string,
	cfg *TurnConfig,
	entries []HistoryEntry,
	baseMessages []message.Message,
	budget tokenBudget,
) ([]message.Message, error) {
	fullHistory := entriesToMessages(entries)

	// Find the system message boundary.
	systemEnd := 0
	for systemEnd < len(entries) && entries[systemEnd].Message.Role == "system" {
		systemEnd++
	}

	// Preserve any leading system-reminder user messages that follow the system
	// messages — skip past them to find the real conversation content.
	reminderEnd := systemEnd
	for reminderEnd < len(fullHistory) && isSystemReminder(fullHistory[reminderEnd]) {
		reminderEnd++
	}

	// Determine what to summarise.
	// On re-compaction (baseMessages != nil) use [old_summary + new_messages]
	// so the LLM sees the previous summary as context.
	// On first compaction use all non-system messages.
	// System-reminder messages are framework-injected per-turn; strip them from
	// every position so the LLM only sees real conversation content.
	var rawToSummarize []message.Message
	if baseMessages != nil {
		for _, m := range baseMessages {
			if m.Role != "system" {
				rawToSummarize = append(rawToSummarize, m)
			}
		}
	} else {
		rawToSummarize = fullHistory[reminderEnd:]
	}

	var messagesToSummarize []message.Message
	for _, m := range rawToSummarize {
		if !isSystemReminder(m) {
			messagesToSummarize = append(messagesToSummarize, m)
		}
	}

	if len(messagesToSummarize) == 0 {
		return fullHistory, nil
	}

	// Generate the summary.
	summaryText, err := generateSummary(ctx, provider, cfg, messagesToSummarize, budget)
	if err != nil {
		return fullHistory, fmt.Errorf("generate summary: %w", err)
	}

	summaryMsg := makeSummaryMessage(summaryText)

	// Measure summary token count.
	summaryTokens := 0
	if tc, err := provider.CountTokens(ctx, providers.CountTokensRequest{
		Model:    providers.ModelRef{ProviderID: cfg.ProviderID, ModelID: cfg.Model},
		Messages: []message.Message{summaryMsg},
	}); err == nil {
		summaryTokens = tc.TotalTokens
	}

	// Persist compaction record.
	record := CompactionRecord{
		SummaryText:   summaryText,
		LeafMessageID: entries[len(entries)-1].ID,
		SummaryTokens: summaryTokens,
		Model:         cfg.Model,
		CreatedAt:     time.Now(),
	}
	if err := store.SaveCompaction(threadID, record); err != nil {
		log.Printf("compaction: failed to save record: %v", err)
	}

	// Build compacted history: [system messages] + [system-reminder messages] + [summary].
	compacted := make([]message.Message, 0, reminderEnd+1)
	compacted = append(compacted, fullHistory[:reminderEnd]...)
	compacted = append(compacted, summaryMsg)
	return compacted, nil
}

// summaryRequestPrompt is appended to the real conversation so the LLM
// summarises from context rather than from a formatted transcript.
const summaryRequestPrompt = `Summarize the conversation above in detail. Your response will replace the conversation history, so it must be thorough enough to continue the work without losing context. Cover all important requests, decisions, code changes (with file paths and snippets), errors and fixes, and any pending or in-progress tasks.`

// safeSplitPoint returns the largest index ≤ n at which messages can safely
// be split for summarisation. A safe split point is after:
//   - a user message (real user turn), or
//   - an assistant message with no pending tool calls, or
//   - a tool-result message where all tool calls from the preceding assistant
//     are resolved (i.e. every tool_call_id has a matching tool_result).
//
// Splitting mid-tool-call (between an assistant tool_call and its tool result)
// is invalid — providers reject it with "no tool output found" errors.
// Returns 0 if no safe point is found (caller should not split).
func safeSplitPoint(messages []message.Message, n int) int {
	if n > len(messages) {
		n = len(messages)
	}
	for i := n; i > 0; i-- {
		msg := messages[i-1]
		switch msg.Role {
		case "user":
			return i // always safe after a real user turn
		case "assistant":
			hasPendingTools := false
			for _, part := range msg.Parts {
				if _, ok := part.(message.ToolCallPart); ok {
					hasPendingTools = true
					break
				}
			}
			if !hasPendingTools {
				return i // safe: assistant replied without invoking tools
			}
		case "tool":
			if allToolCallsResolved(messages, i) {
				return i // safe: every tool call from the preceding assistant is satisfied
			}
		}
		// assistant-with-tool-calls or partially-resolved tool results: keep scanning
	}
	return 0
}

// allToolCallsResolved reports whether every tool call emitted by the nearest
// preceding assistant message has a matching tool result in messages[:endPos].
func allToolCallsResolved(messages []message.Message, endPos int) bool {
	// Find the nearest preceding assistant message.
	assistantPos := -1
	for j := endPos - 2; j >= 0; j-- {
		if messages[j].Role == "assistant" {
			assistantPos = j
			break
		}
	}
	if assistantPos < 0 {
		return true // no preceding assistant — trivially safe
	}

	// Collect its tool call IDs.
	pending := map[string]bool{}
	for _, part := range messages[assistantPos].Parts {
		if tc, ok := part.(message.ToolCallPart); ok {
			pending[tc.ToolCallID] = false
		}
	}
	if len(pending) == 0 {
		return true // assistant made no tool calls
	}

	// Mark resolved by scanning tool-result messages between the assistant and endPos.
	for j := assistantPos + 1; j < endPos; j++ {
		if messages[j].Role == "tool" {
			for _, part := range messages[j].Parts {
				if tr, ok := part.(message.ToolResultPart); ok {
					pending[tr.ToolCallID] = true
				}
			}
		}
	}
	for _, resolved := range pending {
		if !resolved {
			return false
		}
	}
	return true
}

// generateSummary summarises messagesToSummarize using error-driven iterative
// partial compaction.
//
// It tries to summarise the full message list in one call. If the provider
// rejects it (e.g. context_length_exceeded), it splits at the largest safe
// boundary ≤ half the list, summarises that prefix, replaces it with the
// result, and retries. This repeats until the whole remaining list fits.
//
// "Safe boundary" means never splitting between an assistant tool_call and
// its tool_result — see safeSplitPoint.
//
// This approach does not rely on CountTokens, which can significantly
// undercount actual usage (e.g. for tool-heavy conversations on OpenAI).
func generateSummary(
	ctx context.Context,
	provider providers.Provider,
	cfg *TurnConfig,
	messagesToSummarize []message.Message,
	budget tokenBudget,
) (string, error) {
	messages := make([]message.Message, len(messagesToSummarize))
	copy(messages, messagesToSummarize)

	for {
		// Try to summarise everything that remains.
		text, err := doSummaryCall(ctx, provider, cfg, messages, budget)
		if err == nil {
			return text, nil
		}
		if ctx.Err() != nil || isContextCancellation(err) {
			return "", fmt.Errorf("compaction: summarize canceled: %w", err)
		}
		if !isContextLengthExceeded(err) {
			return "", fmt.Errorf("compaction: cannot summarize: %w", err)
		}
		if len(messages) <= 1 {
			return "", fmt.Errorf("compaction: cannot summarize even a single message: %w", err)
		}

		log.Printf("compaction: summary of %d messages failed (%v), splitting", len(messages), err)

		// Find the largest safe split ≤ half and summarise that prefix.
		n := safeSplitPoint(messages, len(messages)/2)
		if n == 0 {
			// No safe split in first half (e.g. one huge uninterrupted tool exchange).
			return "", fmt.Errorf("compaction: no safe split point, cannot reduce: %w", err)
		}
		var subText string
		var subErr error
		for n >= 1 {
			subText, subErr = doSummaryCall(ctx, provider, cfg, messages[:n], budget)
			if subErr == nil {
				break
			}
			if ctx.Err() != nil || isContextCancellation(subErr) {
				return "", fmt.Errorf("compaction: sub-summary canceled: %w", subErr)
			}
			if !isContextLengthExceeded(subErr) {
				return "", fmt.Errorf("compaction: cannot summarize prefix of %d messages: %w", n, subErr)
			}
			if n == 1 {
				return "", fmt.Errorf("compaction: cannot summarize even a single message: %w", subErr)
			}
			log.Printf("compaction: sub-summary of %d messages failed (%v), halving batch", n, subErr)
			n = safeSplitPoint(messages, n/2)
			if n == 0 {
				n = 1 // last resort: try a single message
			}
		}
		if subErr != nil {
			return "", fmt.Errorf("compaction: cannot summarize even a single message: %w", subErr)
		}

		tail := messages[n:]
		messages = make([]message.Message, 0, 1+len(tail))
		messages = append(messages, makeSummaryMessage(subText))
		messages = append(messages, tail...)
	}
}

// doSummaryCall performs a single inline summarisation LLM call and returns
// the assistant's text response.
func doSummaryCall(
	ctx context.Context,
	provider providers.Provider,
	cfg *TurnConfig,
	messages []message.Message,
	budget tokenBudget,
) (string, error) {
	withRequest := make([]message.Message, len(messages)+1)
	copy(withRequest, messages)
	withRequest[len(messages)] = message.Message{
		Role:  "user",
		Parts: []message.Part{message.TextPart{Text: summaryRequestPrompt}},
	}

	maxTokens := budget.SummaryMaxTokens
	req := providers.CompleteRequest{
		Model:     providers.ModelRef{ProviderID: cfg.ProviderID, ModelID: cfg.Model},
		Messages:  withRequest,
		MaxTokens: &maxTokens,
	}

	acc := message.NewChunkAccumulator()
	for chunk, chunkErr := range provider.Complete(ctx, req) {
		if chunkErr != nil {
			acc.Close()
			return "", fmt.Errorf("summary completion: %w", chunkErr)
		}
		acc.Push(chunk)
	}
	acc.Close()

	result := acc.Message()
	var sb strings.Builder
	for _, part := range result.Parts {
		if tp, ok := part.(message.TextPart); ok {
			sb.WriteString(tp.Text)
		}
	}
	text := sb.String()
	if text == "" {
		return "", fmt.Errorf("empty summary generated")
	}
	return text, nil
}

// formatTranscript converts messages into a human-readable transcript.
func formatTranscript(messages []message.Message) string {
	var sb strings.Builder
	for _, msg := range messages {
		sb.WriteString(fmt.Sprintf("[%s]: ", msg.Role))
		for _, part := range msg.Parts {
			switch p := part.(type) {
			case message.TextPart:
				sb.WriteString(p.Text)
			case message.ToolCallPart:
				sb.WriteString(fmt.Sprintf("<tool_call name=%q id=%q>", p.ToolName, p.ToolCallID))
			case message.ToolResultPart:
				output := toolResultOutputToString(p.Output)
				if len(output) > 500 {
					output = output[:500] + "... [truncated]"
				}
				sb.WriteString(fmt.Sprintf("<tool_result name=%q>%s</tool_result>", p.ToolName, output))
			case message.ReasoningPart:
				// Skip reasoning in transcript.
			default:
				sb.WriteString(fmt.Sprintf("<%T>", p))
			}
		}
		sb.WriteString("\n\n")
	}
	return sb.String()
}

// toolResultOutputToString converts a ToolResultOutput to a string representation.
func toolResultOutputToString(output message.ToolResultOutput) string {
	if output == nil {
		return ""
	}
	switch o := output.(type) {
	case message.TextOutput:
		return o.Value
	case message.ErrorTextOutput:
		return "ERROR: " + o.Value
	case message.ExecutionDeniedOutput:
		return "DENIED: " + o.Reason
	case message.JSONOutput:
		return string(o.Value)
	case message.ErrorJSONOutput:
		return "ERROR: " + string(o.Value)
	case message.ContentOutput:
		var parts []string
		for _, item := range o.Value {
			if t, ok := item.(message.ContentTextItem); ok {
				parts = append(parts, t.Text)
			}
		}
		return strings.Join(parts, "\n")
	default:
		return fmt.Sprintf("<%T>", o)
	}
}

// applyCompaction builds a compacted history from an existing record.
// Returns [system messages] + [system-reminder messages] + [summary] + [messages after leaf].
func applyCompaction(record *CompactionRecord, entries []HistoryEntry) []message.Message {
	systemEnd := 0
	for systemEnd < len(entries) && entries[systemEnd].Message.Role == "system" {
		systemEnd++
	}

	// Preserve any leading user messages that are framework-injected system
	// reminders (e.g. <system-reminder> blocks). They sit logically between
	// the system prompt and the conversation and must be re-applied each turn.
	reminderEnd := systemEnd
	for reminderEnd < len(entries) && isSystemReminder(entries[reminderEnd].Message) {
		reminderEnd++
	}

	// Find the compaction leaf in the entry list.
	leafIndex := -1
	for i := reminderEnd; i < len(entries); i++ {
		if entries[i].ID == record.LeafMessageID {
			leafIndex = i
			break
		}
	}

	if leafIndex < 0 {
		// Compaction leaf not found — compaction is stale, return full history.
		return entriesToMessages(entries)
	}

	summaryMsg := makeSummaryMessage(record.SummaryText)

	// Build: [system messages] + [system-reminder messages] + [summary] + [messages after leaf].
	afterLeafStart := leafIndex + 1
	compacted := make([]message.Message, 0, reminderEnd+1+(len(entries)-afterLeafStart))
	for i := 0; i < reminderEnd; i++ {
		compacted = append(compacted, entries[i].Message)
	}
	compacted = append(compacted, summaryMsg)
	for i := afterLeafStart; i < len(entries); i++ {
		compacted = append(compacted, entries[i].Message)
	}
	return compacted
}

// isSystemReminder reports whether a message is a framework-injected system
// reminder (role=user containing a <system-reminder> block). These are
// preserved verbatim across compaction rather than being summarised.
func isSystemReminder(msg message.Message) bool {
	if msg.Role != "user" {
		return false
	}
	for _, part := range msg.Parts {
		if tp, ok := part.(message.TextPart); ok && strings.Contains(tp.Text, "<system-reminder>") {
			return true
		}
	}
	return false
}

// makeSummaryMessage creates the summary message to insert into history.
func makeSummaryMessage(summaryText string) message.Message {
	return message.Message{
		Role: "user",
		Parts: []message.Part{
			message.TextPart{
				Text: "<conversation_summary>\n" + summaryText + "\n</conversation_summary>\n\nThe above is a summary of our earlier conversation. Continue from where we left off.",
			},
		},
		Metadata: json.RawMessage(`{"compaction":true}`),
	}
}

// entriesToMessages extracts just the messages from history entries.
func entriesToMessages(entries []HistoryEntry) []message.Message {
	msgs := make([]message.Message, len(entries))
	for i, e := range entries {
		msgs[i] = e.Message
	}
	return msgs
}
