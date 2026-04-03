package thread

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/obot-platform/discobot/agent-go/message"
	"github.com/obot-platform/discobot/agent-go/providers"
)

// writeFileAtomic writes data to path atomically using a temp-file + rename.
// The temp file is created in the same directory as path so the rename is
// always within the same filesystem (guaranteed atomic on Linux/macOS).
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	if _, err = tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err = tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err = os.Chmod(tmpName, perm); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}

// StoredMessage is a single message persisted on disk.
// Walking the ParentID chain from leaf to root (then reversing)
// gives the chronological conversation history.
type StoredMessage struct {
	ID       string          `json:"id"`
	ParentID string          `json:"parentId,omitempty"`
	Message  message.Message `json:"message"`
}

// Store handles file I/O for thread persistence.
// Each thread is a directory under baseDir containing message files and
// step JSONL files for replay/recovery.
type Store struct {
	baseDir string
}

var ErrMessageExists = errors.New("message already exists")

// NewStore creates a new Store rooted at baseDir.
func NewStore(baseDir string) *Store {
	return &Store{baseDir: baseDir}
}

// messagesDir returns the path to the messages directory for a thread.
func (s *Store) messagesDir(threadID string) string {
	return filepath.Join(s.baseDir, threadID, "messages")
}

// turnsDir returns the path to the turns directory for a thread.
func (s *Store) turnsDir(threadID string) string {
	return filepath.Join(s.baseDir, threadID, "turns")
}

// SaveMessage persists a single StoredMessage to disk.
func (s *Store) SaveMessage(threadID string, msg StoredMessage) error {
	dir := s.messagesDir(threadID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create messages dir: %w", err)
	}
	path := filepath.Join(dir, msg.ID+".json")
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%w: %s", ErrMessageExists, msg.ID)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat message %s: %w", msg.ID, err)
	}
	if msg.Message.CreatedAt == nil {
		now := time.Now().UTC()
		msg.Message.CreatedAt = &now
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}
	return writeFileAtomic(path, data, 0o644)
}

// LoadMessage reads a single StoredMessage from disk.
func (s *Store) LoadMessage(threadID, msgID string) (StoredMessage, error) {
	path := filepath.Join(s.messagesDir(threadID), msgID+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return StoredMessage{}, fmt.Errorf("read message %s: %w", msgID, err)
	}
	var msg StoredMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return StoredMessage{}, fmt.Errorf("unmarshal message %s: %w", msgID, err)
	}
	return msg, nil
}

// BuildHistory loads the parent chain from leafID to root, then reverses
// to produce chronological conversation history.
func (s *Store) BuildHistory(threadID, leafID string) ([]message.Message, error) {
	var chain []message.Message
	seen := make(map[string]struct{})
	currentID := leafID
	for currentID != "" {
		if _, dup := seen[currentID]; dup {
			return nil, fmt.Errorf("build history: cycle detected at message %s", currentID)
		}
		seen[currentID] = struct{}{}
		msg, err := s.LoadMessage(threadID, currentID)
		if err != nil {
			return nil, fmt.Errorf("build history: %w", err)
		}
		chain = append(chain, msg.Message)
		currentID = msg.ParentID
	}
	// Reverse to chronological order.
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain, nil
}

// HistoryEntry pairs a message with its stored ID and parent ID.
type HistoryEntry struct {
	ID       string
	ParentID string
	Message  message.Message
}

// BuildHistoryWithIDs is like BuildHistory but also returns message IDs.
// Used by compaction to map history indices to message IDs.
func (s *Store) BuildHistoryWithIDs(threadID, leafID string) ([]HistoryEntry, error) {
	var chain []HistoryEntry
	seen := make(map[string]struct{})
	currentID := leafID
	for currentID != "" {
		if _, dup := seen[currentID]; dup {
			return nil, fmt.Errorf("build history: cycle detected at message %s", currentID)
		}
		seen[currentID] = struct{}{}
		msg, err := s.LoadMessage(threadID, currentID)
		if err != nil {
			return nil, fmt.Errorf("build history: %w", err)
		}
		chain = append(chain, HistoryEntry(msg))
		currentID = msg.ParentID
	}
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain, nil
}

// ListThreads returns the IDs of all threads in the store.
func (s *Store) ListThreads() ([]string, error) {
	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list threads: %w", err)
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() {
			ids = append(ids, e.Name())
		}
	}
	return ids, nil
}

func (s *Store) threadDir(threadID string) string {
	return filepath.Join(s.baseDir, threadID)
}

// ThreadExists reports whether a thread directory exists.
func (s *Store) ThreadExists(threadID string) (bool, error) {
	info, err := os.Stat(s.threadDir(threadID))
	if err == nil {
		return info.IsDir(), nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("thread exists: %w", err)
}

// CreateThread ensures a thread directory exists.
func (s *Store) CreateThread(threadID string) error {
	if strings.TrimSpace(threadID) == "" {
		return fmt.Errorf("thread ID is required")
	}
	if err := os.MkdirAll(s.threadDir(threadID), 0o755); err != nil {
		return fmt.Errorf("create thread dir: %w", err)
	}
	return nil
}

// DeleteThread removes all persisted data for a thread.
func (s *Store) DeleteThread(threadID string) error {
	if strings.TrimSpace(threadID) == "" {
		return fmt.Errorf("thread ID is required")
	}
	if err := os.RemoveAll(s.threadDir(threadID)); err != nil {
		return fmt.Errorf("delete thread: %w", err)
	}
	return nil
}

// CreateStepFile creates (or truncates) a JSONL file for a given step
// within a turn. The caller is responsible for closing the file.
func (s *Store) CreateStepFile(threadID, turnID string, step int) (*os.File, error) {
	dir := filepath.Join(s.turnsDir(threadID), turnID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create turn dir: %w", err)
	}
	path := filepath.Join(dir, fmt.Sprintf("step-%03d.jsonl", step))
	return os.Create(path)
}

// AppendChunk writes a single ProviderMessageChunk as a JSON line to the given file.
func (s *Store) AppendChunk(f *os.File, chunk message.ProviderMessageChunk) error {
	data, err := message.MarshalProviderChunk(chunk)
	if err != nil {
		return fmt.Errorf("marshal chunk: %w", err)
	}
	data = append(data, '\n')
	_, err = f.Write(data)
	return err
}

// LoadStepChunks reads all chunks from a JSONL step file.
// Lines that cannot be unmarshalled are silently skipped — this tolerates a
// partially written last record that can occur when the process crashes mid-write.
// Returns nil, nil if the file does not exist.
func (s *Store) LoadStepChunks(threadID, turnID string, step int) ([]message.ProviderMessageChunk, error) {
	path := filepath.Join(s.turnsDir(threadID), turnID, fmt.Sprintf("step-%03d.jsonl", step))
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open step file: %w", err)
	}
	defer f.Close()

	var chunks []message.ProviderMessageChunk
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		chunk, err := message.UnmarshalProviderChunk(line)
		if err != nil {
			// Partial or corrupted record — skip it. This is expected when the
			// process crashes while writing the last line of a JSONL file.
			continue
		}
		chunks = append(chunks, chunk)
	}
	if err := scanner.Err(); err != nil {
		return chunks, fmt.Errorf("scan step file: %w", err)
	}
	return chunks, nil
}

// --- Turn State Persistence ---

// turnStatePath returns the path to the turn state file for a thread.
func (s *Store) turnStatePath(threadID string) string {
	return filepath.Join(s.baseDir, threadID, "turn.json")
}

// turnRecordPath returns the durable per-turn state path under the turn directory.
func (s *Store) turnRecordPath(threadID, turnID string) string {
	return filepath.Join(s.turnsDir(threadID), turnID, "turn.json")
}

// SaveTurnState persists the active turn state to disk.
func (s *Store) SaveTurnState(threadID string, state TurnState) error {
	now := time.Now().UTC()
	if state.StartedAt == nil {
		state.StartedAt = &now
	}
	state.UpdatedAt = &now

	threadDir := filepath.Join(s.baseDir, threadID)
	if err := os.MkdirAll(threadDir, 0o755); err != nil {
		return fmt.Errorf("create thread dir: %w", err)
	}
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal turn state: %w", err)
	}
	if err := writeFileAtomic(s.turnStatePath(threadID), data, 0o644); err != nil {
		return err
	}
	if state.ID == "" {
		return nil
	}
	turnDir := filepath.Join(s.turnsDir(threadID), state.ID)
	if err := os.MkdirAll(turnDir, 0o755); err != nil {
		return fmt.Errorf("create turn dir: %w", err)
	}
	return writeFileAtomic(s.turnRecordPath(threadID, state.ID), data, 0o644)
}

// LoadTurnState loads the active turn state from disk.
// Returns nil if no active turn exists.
func (s *Store) LoadTurnState(threadID string) (*TurnState, error) {
	data, err := os.ReadFile(s.turnStatePath(threadID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read turn state: %w", err)
	}
	var state TurnState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("unmarshal turn state: %w", err)
	}
	return &state, nil
}

// LoadTurnRecord loads the durable per-turn state from the turn directory.
// Returns nil if not found.
func (s *Store) LoadTurnRecord(threadID, turnID string) (*TurnState, error) {
	data, err := os.ReadFile(s.turnRecordPath(threadID, turnID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read turn record: %w", err)
	}
	var state TurnState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("unmarshal turn record: %w", err)
	}
	return &state, nil
}

// DeleteTurnState removes the active turn state file.
func (s *Store) DeleteTurnState(threadID string) error {
	err := os.Remove(s.turnStatePath(threadID))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete turn state: %w", err)
	}
	return nil
}

// --- Step Result Persistence ---

// stepResultPath returns the path to the step result file.
func (s *Store) stepResultPath(threadID, turnID string, step int) string {
	return filepath.Join(s.turnsDir(threadID), turnID, fmt.Sprintf("step-%03d-result.json", step))
}

// StepLogReqPath returns the path for the raw HTTP request body log for a step.
// Written by the transport layer as step-NNN-req.json alongside other step files.
func (s *Store) StepLogReqPath(threadID, turnID string, step int) string {
	return filepath.Join(s.turnsDir(threadID), turnID, fmt.Sprintf("step-%03d-req.json", step))
}

// StepLogRespPath returns the path for the raw HTTP response body log for a step.
// Written by the transport layer as step-NNN-resp.jsonl alongside other step files.
func (s *Store) StepLogRespPath(threadID, turnID string, step int) string {
	return filepath.Join(s.turnsDir(threadID), turnID, fmt.Sprintf("step-%03d-resp.jsonl", step))
}

// SaveStepResult persists the result of a completed step (assistant message + tool call list).
func (s *Store) SaveStepResult(threadID, turnID string, step int, result StepResult) error {
	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal step result: %w", err)
	}
	return writeFileAtomic(s.stepResultPath(threadID, turnID, step), data, 0o644)
}

// LoadStepResult loads a step result from disk. Returns nil if not found.
func (s *Store) LoadStepResult(threadID, turnID string, step int) (*StepResult, error) {
	data, err := os.ReadFile(s.stepResultPath(threadID, turnID, step))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read step result: %w", err)
	}
	var result StepResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("unmarshal step result: %w", err)
	}
	return &result, nil
}

// --- Tool Results Persistence ---

// toolResultsPath returns the path to the tool results file for a step.
func (s *Store) toolResultsPath(threadID, turnID string, step int) string {
	return filepath.Join(s.turnsDir(threadID), turnID, fmt.Sprintf("step-%03d-tools.json", step))
}

// SaveToolResults persists tool results for a step.
func (s *Store) SaveToolResults(threadID, turnID string, step int, results StepToolResults) error {
	data, err := json.Marshal(results)
	if err != nil {
		return fmt.Errorf("marshal tool results: %w", err)
	}
	return writeFileAtomic(s.toolResultsPath(threadID, turnID, step), data, 0o644)
}

// LoadToolResults loads tool results for a step. Returns empty if not found.
func (s *Store) LoadToolResults(threadID, turnID string, step int) (StepToolResults, error) {
	data, err := os.ReadFile(s.toolResultsPath(threadID, turnID, step))
	if err != nil {
		if os.IsNotExist(err) {
			return StepToolResults{}, nil
		}
		return StepToolResults{}, fmt.Errorf("read tool results: %w", err)
	}
	var results StepToolResults
	if err := json.Unmarshal(data, &results); err != nil {
		return StepToolResults{}, fmt.Errorf("unmarshal tool results: %w", err)
	}
	return results, nil
}

// --- Async Continuation Persistence ---

// asyncContinuationsPath returns the path to the async continuation file for a
// step.
func (s *Store) asyncContinuationsPath(threadID, turnID string, step int) string {
	return filepath.Join(s.turnsDir(threadID), turnID, fmt.Sprintf("step-%03d-async.json", step))
}

// stepEventsPath returns the path to the immutable event message index for a step.
func (s *Store) stepEventsPath(threadID, turnID string, step int) string {
	return filepath.Join(s.turnsDir(threadID), turnID, fmt.Sprintf("step-%03d-events.json", step))
}

// SaveAsyncContinuations persists async continuation metadata for a step.
func (s *Store) SaveAsyncContinuations(threadID, turnID string, step int, continuations StepAsyncContinuations) error {
	dir := filepath.Join(s.turnsDir(threadID), turnID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create turn dir: %w", err)
	}
	data, err := json.Marshal(continuations)
	if err != nil {
		return fmt.Errorf("marshal async continuations: %w", err)
	}
	return writeFileAtomic(s.asyncContinuationsPath(threadID, turnID, step), data, 0o644)
}

// LoadAsyncContinuations loads async continuation metadata for a step. Returns
// empty if not found.
func (s *Store) LoadAsyncContinuations(threadID, turnID string, step int) (StepAsyncContinuations, error) {
	data, err := os.ReadFile(s.asyncContinuationsPath(threadID, turnID, step))
	if err != nil {
		if os.IsNotExist(err) {
			return StepAsyncContinuations{}, nil
		}
		return StepAsyncContinuations{}, fmt.Errorf("read async continuations: %w", err)
	}
	var continuations StepAsyncContinuations
	if err := json.Unmarshal(data, &continuations); err != nil {
		return StepAsyncContinuations{}, fmt.Errorf("unmarshal async continuations: %w", err)
	}
	return continuations, nil
}

// SaveStepEventMessages persists ordered immutable event message IDs for a step.
func (s *Store) SaveStepEventMessages(threadID, turnID string, step int, events StepEventMessages) error {
	dir := filepath.Join(s.turnsDir(threadID), turnID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create turn dir: %w", err)
	}
	data, err := json.Marshal(events)
	if err != nil {
		return fmt.Errorf("marshal step event messages: %w", err)
	}
	return writeFileAtomic(s.stepEventsPath(threadID, turnID, step), data, 0o644)
}

// LoadStepEventMessages loads ordered immutable event message IDs for a step.
// Returns empty if not found.
func (s *Store) LoadStepEventMessages(threadID, turnID string, step int) (StepEventMessages, error) {
	data, err := os.ReadFile(s.stepEventsPath(threadID, turnID, step))
	if err != nil {
		if os.IsNotExist(err) {
			return StepEventMessages{}, nil
		}
		return StepEventMessages{}, fmt.Errorf("read step event messages: %w", err)
	}
	var events StepEventMessages
	if err := json.Unmarshal(data, &events); err != nil {
		return StepEventMessages{}, fmt.Errorf("unmarshal step event messages: %w", err)
	}
	return events, nil
}

// --- Question/Answer Persistence ---

// questionPath returns the path to the approval question file for a specific
// tool call. Files are named approve-{approvalID}.json so that approval history
// is preserved and parallel approvals within a turn don't overwrite each other.
func (s *Store) questionPath(threadID, turnID, approvalID string) string {
	return filepath.Join(s.turnsDir(threadID), turnID, "approve-"+approvalID+".json")
}

// answerPath returns the path to the answer file for a specific approval.
func (s *Store) answerPath(threadID, turnID, approvalID string) string {
	return filepath.Join(s.turnsDir(threadID), turnID, "approve-"+approvalID+"-answer.json")
}

// SaveQuestion persists a pending approval to disk as approve-{approvalId}.json.
func (s *Store) SaveQuestion(threadID, turnID string, q PendingQuestionState) error {
	dir := filepath.Join(s.turnsDir(threadID), turnID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create turn dir: %w", err)
	}
	if q.ApprovalID == "" {
		q.ApprovalID = generateID()
	}
	data, err := json.Marshal(q)
	if err != nil {
		return fmt.Errorf("marshal question: %w", err)
	}
	return writeFileAtomic(s.questionPath(threadID, turnID, q.ApprovalID), data, 0o644)
}

// LoadQuestion loads a pending question from disk by approval ID. Returns nil if not found.
func (s *Store) LoadQuestion(threadID, turnID, approvalID string) (*PendingQuestionState, error) {
	data, err := os.ReadFile(s.questionPath(threadID, turnID, approvalID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read question: %w", err)
	}
	var q PendingQuestionState
	if err := json.Unmarshal(data, &q); err != nil {
		return nil, fmt.Errorf("unmarshal question: %w", err)
	}
	return &q, nil
}

// SaveAnswer persists the user's response to disk as approve-{approvalId}-answer.json.
func (s *Store) SaveAnswer(threadID, turnID string, a QuestionAnswer) error {
	dir := filepath.Join(s.turnsDir(threadID), turnID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create turn dir: %w", err)
	}
	data, err := json.Marshal(a)
	if err != nil {
		return fmt.Errorf("marshal answer: %w", err)
	}
	return writeFileAtomic(s.answerPath(threadID, turnID, a.ApprovalID), data, 0o644)
}

// LoadAnswer loads the user's answer from disk by approval ID. Returns nil if not found.
func (s *Store) LoadAnswer(threadID, turnID, approvalID string) (*QuestionAnswer, error) {
	data, err := os.ReadFile(s.answerPath(threadID, turnID, approvalID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read answer: %w", err)
	}
	var a QuestionAnswer
	if err := json.Unmarshal(data, &a); err != nil {
		return nil, fmt.Errorf("unmarshal answer: %w", err)
	}
	return &a, nil
}

// --- Compaction Persistence ---

// compactionPath returns the path to the compaction record for a thread.
func (s *Store) compactionPath(threadID string) string {
	return filepath.Join(s.baseDir, threadID, "compaction.json")
}

// SaveCompaction persists a compaction record to disk.
func (s *Store) SaveCompaction(threadID string, record CompactionRecord) error {
	dir := filepath.Join(s.baseDir, threadID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create thread dir: %w", err)
	}
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal compaction: %w", err)
	}
	return writeFileAtomic(s.compactionPath(threadID), data, 0o644)
}

// LoadCompaction loads a compaction record from disk. Returns nil if not found.
func (s *Store) LoadCompaction(threadID string) (*CompactionRecord, error) {
	data, err := os.ReadFile(s.compactionPath(threadID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read compaction: %w", err)
	}
	var record CompactionRecord
	if err := json.Unmarshal(data, &record); err != nil {
		// Corrupted file — delete and return nil.
		os.Remove(s.compactionPath(threadID))
		return nil, nil
	}
	return &record, nil
}

// --- Thread Config Persistence ---

// Config holds durable per-thread settings that persist across sessions.
// Unlike TurnState (which is ephemeral), Config survives turn completion
// and is used to remember things like the last-used model so that new sessions
// continue with the same provider/model without the user needing to re-select.
type Config struct {
	// Name is the display name for this thread.
	Name string `json:"name,omitempty"`
	// NameSource tracks whether the persisted name was provided by the user or
	// auto-generated by the agent.
	NameSource string `json:"nameSource,omitempty"`
	// LastMessage is the most recent user-authored text prompt for this thread.
	LastMessage string `json:"lastMessage,omitempty"`
	// ErrorMessage is the latest persisted thread-scoped error banner text.
	ErrorMessage string `json:"errorMessage,omitempty"`
	// Model is the full "providerId/modelId" ref (e.g. "anthropic/claude-sonnet-4-6").
	Model string `json:"model,omitempty"`
	// Reasoning is the extended thinking setting (e.g. "", "auto", "low", "medium", "high", "none").
	Reasoning providers.Reasoning `json:"reasoning,omitempty"`
	// CWD is the working directory associated with this thread.
	CWD string `json:"cwd,omitempty"`
	// Mode is the canonical durable mode state ("build" | "plan" with metadata).
	Mode ModeState `json:"mode,omitempty"`
	// LastTurnState stores the last user-visible terminal turn outcome that
	// should surface in thread chrome. Empty means no special state.
	LastTurnState State `json:"lastTurnState,omitempty"`
	// ActiveLeafID tracks the currently selected branch head for this thread.
	ActiveLeafID string `json:"activeLeafId,omitempty"`
	// PromptQueue stores queued follow-up prompts waiting to run after the
	// current completion finishes.
	PromptQueue []QueuedPrompt `json:"promptQueue,omitempty"`
}

// QueuedPrompt stores one queued user submission for a thread.
type QueuedPrompt struct {
	ID        string            `json:"id"`
	CreatedAt time.Time         `json:"createdAt,omitempty"`
	Message   message.UIMessage `json:"message"`
	Model     string            `json:"model,omitempty"`
	Reasoning string            `json:"reasoning,omitempty"`
	Mode      string            `json:"mode,omitempty"`
}

const (
	ThreadNameSourceUser      = "user"
	ThreadNameSourceGenerated = "generated"
)

type State string

const (
	StateInterrupted State = "interrupted"
	StateCancelled   State = "cancelled"
)

// ModeState captures the current mode with provenance information.
type ModeState struct {
	// Value is "build" or "plan".
	Value string `json:"value,omitempty"`
	// SetBy indicates who last set the mode: "user", "llm", or "system".
	SetBy string `json:"setBy,omitempty"`
	// ChangedAt is when the mode last changed.
	ChangedAt time.Time `json:"changedAt,omitempty"`
}

// threadConfigPath returns the path to the thread config file.
func (s *Store) threadConfigPath(threadID string) string {
	return filepath.Join(s.baseDir, threadID, "config.json")
}

// SaveConfig persists durable thread-level config.
func (s *Store) SaveConfig(threadID string, cfg Config) error {
	dir := filepath.Join(s.baseDir, threadID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create thread dir: %w", err)
	}
	if cfg.PromptQueue == nil {
		if existing, err := s.LoadConfig(threadID); err == nil {
			cfg.PromptQueue = existing.PromptQueue
		}
	}
	// Ensure Mode.Value is always set; default to build if empty.
	if strings.TrimSpace(cfg.Mode.Value) == "" {
		cfg.Mode.Value = "build"
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal thread config: %w", err)
	}
	return writeFileAtomic(s.threadConfigPath(threadID), data, 0o644)
}

// LoadConfig loads durable thread-level config.
// Returns zero value if no config exists yet.
// Handles migration from old format where model was stored as a bare ID
// alongside a separate providerId field.
func (s *Store) LoadConfig(threadID string) (Config, error) {
	data, err := os.ReadFile(s.threadConfigPath(threadID))
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, nil
		}
		return Config{}, fmt.Errorf("read thread config: %w", err)
	}
	// Use a raw struct for migration: old format had separate providerId + bare model.
	var raw struct {
		Name          string              `json:"name"`
		NameSource    string              `json:"nameSource"`
		LastMessage   string              `json:"lastMessage"`
		ErrorMessage  string              `json:"errorMessage"`
		Model         string              `json:"model"`
		ProviderID    string              `json:"providerId"`
		Reasoning     providers.Reasoning `json:"reasoning"`
		CWD           string              `json:"cwd"`
		Mode          ModeState           `json:"mode"`
		LastTurnState State               `json:"lastTurnState"`
		ActiveLeafID  string              `json:"activeLeafId"`
		PromptQueue   []QueuedPrompt      `json:"promptQueue"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return Config{}, fmt.Errorf("unmarshal thread config: %w", err)
	}
	// If model already contains "/" it's a full ref; otherwise combine with providerId.
	model := raw.Model
	if model != "" && !strings.Contains(model, "/") && raw.ProviderID != "" {
		model = raw.ProviderID + "/" + model
	}
	// Ensure Mode has a value.
	mode := raw.Mode
	if strings.TrimSpace(mode.Value) == "" {
		mode = ModeState{Value: "build"}
	}
	return Config{
		Name:          raw.Name,
		NameSource:    raw.NameSource,
		LastMessage:   raw.LastMessage,
		ErrorMessage:  raw.ErrorMessage,
		Model:         model,
		Reasoning:     raw.Reasoning,
		CWD:           raw.CWD,
		Mode:          mode,
		LastTurnState: raw.LastTurnState,
		ActiveLeafID:  raw.ActiveLeafID,
		PromptQueue:   raw.PromptQueue,
	}, nil
}

// AppendQueuedPrompt adds a queued prompt to the end of the thread queue.
func (s *Store) AppendQueuedPrompt(threadID string, prompt QueuedPrompt) (Config, QueuedPrompt, error) {
	cfg, err := s.LoadConfig(threadID)
	if err != nil {
		return Config{}, QueuedPrompt{}, err
	}
	if prompt.ID == "" {
		prompt.ID = generateID()
	}
	if prompt.CreatedAt.IsZero() {
		prompt.CreatedAt = time.Now().UTC()
	}
	cfg.PromptQueue = append(append([]QueuedPrompt{}, cfg.PromptQueue...), prompt)
	if err := s.SaveConfig(threadID, cfg); err != nil {
		return Config{}, QueuedPrompt{}, err
	}
	return cfg, prompt, nil
}

// DeleteQueuedPrompt removes one queued prompt by ID.
func (s *Store) DeleteQueuedPrompt(threadID, promptID string) (Config, bool, error) {
	cfg, err := s.LoadConfig(threadID)
	if err != nil {
		return Config{}, false, err
	}
	nextQueue := make([]QueuedPrompt, 0, len(cfg.PromptQueue))
	removed := false
	for _, prompt := range cfg.PromptQueue {
		if prompt.ID == promptID {
			removed = true
			continue
		}
		nextQueue = append(nextQueue, prompt)
	}
	if !removed {
		return cfg, false, nil
	}
	cfg.PromptQueue = nextQueue
	if err := s.SaveConfig(threadID, cfg); err != nil {
		return Config{}, false, err
	}
	return cfg, true, nil
}

// PopQueuedPrompt removes and returns the next queued prompt.
func (s *Store) PopQueuedPrompt(threadID string) (Config, *QueuedPrompt, error) {
	cfg, err := s.LoadConfig(threadID)
	if err != nil {
		return Config{}, nil, err
	}
	if len(cfg.PromptQueue) == 0 {
		return cfg, nil, nil
	}
	prompt := cfg.PromptQueue[0]
	cfg.PromptQueue = append([]QueuedPrompt{}, cfg.PromptQueue[1:]...)
	if err := s.SaveConfig(threadID, cfg); err != nil {
		return Config{}, nil, err
	}
	return cfg, &prompt, nil
}

// PrependQueuedPrompt pushes a prompt onto the front of the queue.
func (s *Store) PrependQueuedPrompt(threadID string, prompt QueuedPrompt) (Config, error) {
	cfg, err := s.LoadConfig(threadID)
	if err != nil {
		return Config{}, err
	}
	cfg.PromptQueue = append([]QueuedPrompt{prompt}, cfg.PromptQueue...)
	if err := s.SaveConfig(threadID, cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// FindLeaf returns the leaf message ID for a thread — the message that is not
// a parent of any other message. Returns "" if the thread has no messages.
func (s *Store) FindLeaf(threadID string) (string, error) {
	dir := s.messagesDir(threadID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read messages dir: %w", err)
	}

	parentIDs := make(map[string]bool)
	var allIDs []string
	for _, e := range entries {
		name := e.Name()
		if len(name) < 6 || name[len(name)-5:] != ".json" {
			continue
		}
		id := name[:len(name)-5]
		allIDs = append(allIDs, id)
		msg, err := s.LoadMessage(threadID, id)
		if err != nil {
			continue
		}
		if msg.ParentID != "" {
			parentIDs[msg.ParentID] = true
		}
	}

	// Find all leaf messages (not a parent of any other message).
	// When multiple leaves exist (e.g. after a /clear that started a new branch),
	// return the one whose backing file was most recently modified so that the
	// active branch is preferred over archived history.
	var bestLeaf string
	var bestMtime time.Time
	for _, id := range allIDs {
		if parentIDs[id] {
			continue
		}
		path := filepath.Join(dir, id+".json")
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if bestLeaf == "" || info.ModTime().After(bestMtime) {
			bestLeaf = id
			bestMtime = info.ModTime()
		}
	}
	return bestLeaf, nil
}

// IsLeaf reports whether msgID exists and is not a parent of any other message
// (i.e. it is a leaf node in the message tree).
// Returns false (no error) when msgID does not exist.
func (s *Store) IsLeaf(threadID, msgID string) (bool, error) {
	if _, err := s.LoadMessage(threadID, msgID); err != nil {
		return false, nil // message not found → not a leaf
	}

	dir := s.messagesDir(threadID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, fmt.Errorf("read messages dir: %w", err)
	}

	for _, e := range entries {
		name := e.Name()
		if len(name) < 6 || name[len(name)-5:] != ".json" {
			continue
		}
		id := name[:len(name)-5]
		if id == msgID {
			continue
		}
		msg, err := s.LoadMessage(threadID, id)
		if err != nil {
			continue
		}
		if msg.ParentID == msgID {
			return false, nil // msgID has a child → not a leaf
		}
	}
	return true, nil
}
