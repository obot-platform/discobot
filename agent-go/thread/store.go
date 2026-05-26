package thread

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/obot-platform/discobot/agent-go/message"
	"github.com/obot-platform/discobot/agent-go/providers"
)

var ErrCorruptMessage = errors.New("corrupt message")

// WriteFileAtomic writes data to path atomically using a temp-file + rename.
// The temp file is created in the same directory as path so the rename is
// always within the same filesystem (guaranteed atomic on Linux/macOS).
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
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
	return WriteFileAtomic(path, data, 0o644)
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
		return StoredMessage{}, fmt.Errorf("%w %s: %v", ErrCorruptMessage, msgID, err)
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
			if errors.Is(err, ErrCorruptMessage) {
				log.Printf("thread store: skipping corrupt message %s in thread %s while building history: %v", currentID, threadID, err)
				break
			}
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

// HistoryTurnIDs returns a map from persisted message ID to stable backend turn
// ID for completed turns in a thread. This lets replay consumers associate
// replayed UI messages with persisted per-turn artifacts such as browser events.
func (s *Store) HistoryTurnIDs(threadID string) (map[string]string, error) {
	turnsDir := s.turnsDir(threadID)
	entries, err := os.ReadDir(turnsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("read turns dir: %w", err)
	}

	turnIDsByMessageID := map[string]string{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		turnID := entry.Name()
		pattern := filepath.Join(turnsDir, turnID, "step-*-result.json")
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, fmt.Errorf("glob step results for turn %s: %w", turnID, err)
		}
		sort.Strings(matches)
		for index, match := range matches {
			base := filepath.Base(match)
			var step int
			if _, err := fmt.Sscanf(base, "step-%03d-result.json", &step); err != nil {
				continue
			}
			result, err := s.LoadStepResult(threadID, turnID, step)
			if err != nil {
				return nil, err
			}
			if result == nil {
				continue
			}
			assistantMessageID := strings.TrimSpace(result.AssistantMessageID)
			if assistantMessageID == "" {
				continue
			}
			turnIDsByMessageID[assistantMessageID] = turnID
			if index == 0 {
				stored, err := s.LoadMessage(threadID, assistantMessageID)
				if err != nil {
					return nil, fmt.Errorf("load first assistant message %s for turn %s: %w", assistantMessageID, turnID, err)
				}
				userMessageID := strings.TrimSpace(stored.ParentID)
				if userMessageID != "" {
					turnIDsByMessageID[userMessageID] = turnID
				}
			}
			events, err := s.LoadStepEventMessages(threadID, turnID, step)
			if err != nil {
				return nil, err
			}
			for _, messageID := range events.MessageIDs {
				messageID = strings.TrimSpace(messageID)
				if messageID != "" {
					turnIDsByMessageID[messageID] = turnID
				}
			}
		}
	}
	return turnIDsByMessageID, nil
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
			if errors.Is(err, ErrCorruptMessage) {
				log.Printf("thread store: skipping corrupt message %s in thread %s while building history with IDs: %v", currentID, threadID, err)
				break
			}
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

// ListTurnIDs returns durable turn IDs for a thread.
func (s *Store) ListTurnIDs(threadID string) ([]string, error) {
	entries, err := os.ReadDir(s.turnsDir(threadID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list turns: %w", err)
	}

	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			ids = append(ids, entry.Name())
		}
	}
	sort.Strings(ids)
	return ids, nil
}

// ListStepResultIndexes returns completed step result indexes for a turn.
func (s *Store) ListStepResultIndexes(threadID, turnID string) ([]int, error) {
	pattern := filepath.Join(s.turnsDir(threadID), turnID, "step-*-result.json")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("glob step results for turn %s: %w", turnID, err)
	}
	sort.Strings(matches)

	indexes := make([]int, 0, len(matches))
	for _, match := range matches {
		var step int
		if _, err := fmt.Sscanf(filepath.Base(match), "step-%03d-result.json", &step); err != nil {
			continue
		}
		indexes = append(indexes, step)
	}
	return indexes, nil
}

func (s *Store) threadDir(threadID string) string {
	return filepath.Join(s.baseDir, threadID)
}

// ThreadDir returns the path to the thread directory on disk.
func (s *Store) ThreadDir(threadID string) string {
	return s.threadDir(threadID)
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
	if err := WriteFileAtomic(s.turnStatePath(threadID), data, 0o644); err != nil {
		return err
	}
	if state.ID == "" {
		return nil
	}
	turnDir := filepath.Join(s.turnsDir(threadID), state.ID)
	if err := os.MkdirAll(turnDir, 0o755); err != nil {
		return fmt.Errorf("create turn dir: %w", err)
	}
	return WriteFileAtomic(s.turnRecordPath(threadID, state.ID), data, 0o644)
}

// LoadTurnState loads the active turn state from disk.
// Returns nil if no active turn exists or the state file is truncated/corrupt.
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
		log.Printf("thread store: ignoring corrupt turn state for thread %s: %v", threadID, err)
		return nil, nil
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
	return WriteFileAtomic(s.stepResultPath(threadID, turnID, step), data, 0o644)
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
	return WriteFileAtomic(s.toolResultsPath(threadID, turnID, step), data, 0o644)
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
	return WriteFileAtomic(s.asyncContinuationsPath(threadID, turnID, step), data, 0o644)
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
	return WriteFileAtomic(s.stepEventsPath(threadID, turnID, step), data, 0o644)
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
	return WriteFileAtomic(s.questionPath(threadID, turnID, q.ApprovalID), data, 0o644)
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
	return WriteFileAtomic(s.answerPath(threadID, turnID, a.ApprovalID), data, 0o644)
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
	return WriteFileAtomic(s.compactionPath(threadID), data, 0o644)
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
	// ServiceTier is the provider latency tier selected for this thread.
	ServiceTier string `json:"serviceTier,omitempty"`
	// CWD is the working directory associated with this thread.
	CWD string `json:"cwd,omitempty"`
	// LastTurnState stores the last user-visible terminal turn outcome that
	// should surface in thread chrome. Empty means no special state.
	LastTurnState State `json:"lastTurnState,omitempty"`
	// TokenUsage stores aggregate token usage across completed turns.
	TokenUsage TokenUsageInfo `json:"tokenUsage,omitzero"`
	// ActiveLeafID tracks the currently selected branch head for this thread.
	ActiveLeafID string `json:"activeLeafId,omitempty"`
	// ContextReset records that the user reset the active thread context. When
	// set, an empty ActiveLeafID is intentional and should not fall back to the
	// historical leaf on disk.
	ContextReset bool `json:"contextReset,omitempty"`
	// ActiveCommand is the slash-command name currently driving thread work.
	// It is cleared when the thread is no longer actively processing that
	// command, including when a turn pauses for user input.
	ActiveCommand string `json:"activeCommand,omitempty"`
	// CommunicatedCredentials tracks which session-scoped credential and use IDs
	// have already been reported to the LLM for this thread.
	CommunicatedCredentials []CommunicatedCredentialBinding `json:"communicatedCredentials,omitempty"`
	// CommunicatedSkillLikeEntries tracks which visible skill-like entries have
	// already been reported to the LLM for this thread.
	CommunicatedSkillLikeEntries []CommunicatedSkillLikeEntry `json:"communicatedSkillLikeEntries,omitempty"`
	// Metadata carries thread-scoped structured data for UI features such as task threads.
	Metadata ConfigMetadata `json:"metadata,omitzero"`
}

// ConfigMetadata stores durable typed metadata for a thread.
type ConfigMetadata struct {
	Type            string             `json:"type,omitempty"`
	TaskID          string             `json:"taskId,omitempty"`
	ParentThreadID  string             `json:"parentThreadId,omitempty"`
	ParentTaskID    string             `json:"parentTaskId,omitempty"`
	SubagentType    string             `json:"subagentType,omitempty"`
	Description     string             `json:"description,omitempty"`
	Prompt          string             `json:"prompt,omitempty"`
	Model           string             `json:"model,omitempty"`
	RunInBackground bool               `json:"runInBackground,omitempty"`
	StartedAt       time.Time          `json:"startedAt,omitzero"`
	ACPSession      ACPSessionMetadata `json:"acpSession,omitzero"`
}

// ACPSessionMetadata stores the ACP session state mapped to this Discobot
// thread. It mirrors ACP's session info without making the core thread package
// depend on the ACP protocol package.
type ACPSessionMetadata struct {
	Meta          map[string]any    `json:"_meta,omitempty"`
	CWD           string            `json:"cwd,omitempty"`
	SessionID     string            `json:"sessionId,omitempty"`
	Title         *string           `json:"title,omitempty"`
	UpdatedAt     *string           `json:"updatedAt,omitempty"`
	ResponseMeta  map[string]any    `json:"responseMeta,omitempty"`
	ConfigOptions []json.RawMessage `json:"configOptions,omitempty"`
}

func (m ACPSessionMetadata) IsZero() bool {
	return len(m.Meta) == 0 &&
		strings.TrimSpace(m.CWD) == "" &&
		strings.TrimSpace(m.SessionID) == "" &&
		m.Title == nil &&
		m.UpdatedAt == nil &&
		len(m.ResponseMeta) == 0 &&
		len(m.ConfigOptions) == 0
}

func (m ConfigMetadata) IsZero() bool {
	return strings.TrimSpace(m.Type) == "" &&
		strings.TrimSpace(m.TaskID) == "" &&
		strings.TrimSpace(m.ParentThreadID) == "" &&
		strings.TrimSpace(m.ParentTaskID) == "" &&
		strings.TrimSpace(m.SubagentType) == "" &&
		strings.TrimSpace(m.Description) == "" &&
		strings.TrimSpace(m.Prompt) == "" &&
		strings.TrimSpace(m.Model) == "" &&
		!m.RunInBackground &&
		m.StartedAt.IsZero() &&
		m.ACPSession.IsZero()
}

func (m ConfigMetadata) RawMessage() json.RawMessage {
	if m.IsZero() {
		return nil
	}
	data, err := json.Marshal(m)
	if err != nil {
		return nil
	}
	return data
}

// CommunicatedSkillLikeEntry records one visible skill-like command that has
// been explicitly reported to the LLM.
type CommunicatedSkillLikeEntry struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

func communicatedSkillLikeEntryKey(entry CommunicatedSkillLikeEntry) string {
	return strings.TrimSpace(entry.Name)
}

// NormalizeCommunicatedSkillLikeEntries returns a deterministic, deduplicated
// copy of communicated visible skill-like entries.
func NormalizeCommunicatedSkillLikeEntries(entries []CommunicatedSkillLikeEntry) []CommunicatedSkillLikeEntry {
	if len(entries) == 0 {
		return nil
	}
	normalized := make([]CommunicatedSkillLikeEntry, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		entry.Name = strings.TrimSpace(entry.Name)
		entry.Description = strings.TrimSpace(entry.Description)
		if entry.Name == "" {
			continue
		}
		key := communicatedSkillLikeEntryKey(entry)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, entry)
	}
	sort.Slice(normalized, func(i, j int) bool {
		return normalized[i].Name < normalized[j].Name
	})
	return normalized
}

// DiffCommunicatedSkillLikeEntries computes added, removed, and description-only
// changes between two communicated visible skill-like snapshots.
func DiffCommunicatedSkillLikeEntries(
	before []CommunicatedSkillLikeEntry,
	after []CommunicatedSkillLikeEntry,
) (added []CommunicatedSkillLikeEntry, removed []CommunicatedSkillLikeEntry, changed []CommunicatedSkillLikeEntry) {
	before = NormalizeCommunicatedSkillLikeEntries(before)
	after = NormalizeCommunicatedSkillLikeEntries(after)

	beforeByName := make(map[string]CommunicatedSkillLikeEntry, len(before))
	for _, entry := range before {
		beforeByName[communicatedSkillLikeEntryKey(entry)] = entry
	}
	afterByName := make(map[string]CommunicatedSkillLikeEntry, len(after))
	for _, entry := range after {
		afterByName[communicatedSkillLikeEntryKey(entry)] = entry
	}

	for name, current := range afterByName {
		previous, ok := beforeByName[name]
		if !ok {
			added = append(added, current)
			continue
		}
		if previous.Description != current.Description {
			changed = append(changed, current)
		}
	}
	for name, previous := range beforeByName {
		if _, ok := afterByName[name]; !ok {
			removed = append(removed, previous)
		}
	}

	return NormalizeCommunicatedSkillLikeEntries(added), NormalizeCommunicatedSkillLikeEntries(removed), NormalizeCommunicatedSkillLikeEntries(changed)
}

// CommunicatedCredentialBinding records one session-scoped credential binding
// that has been explicitly reported to the LLM.
type CommunicatedCredentialBinding struct {
	CredentialID string                      `json:"credentialId"`
	EnvVar       string                      `json:"envVar,omitempty"`
	Uses         []CommunicatedCredentialUse `json:"uses,omitempty"`
}

// CommunicatedCredentialUse records one approved use ID that has been reported
// to the LLM for a communicated credential binding.
type CommunicatedCredentialUse struct {
	ID          string `json:"id"`
	Description string `json:"description,omitempty"`
}

func communicatedCredentialBindingKey(binding CommunicatedCredentialBinding) string {
	return strings.TrimSpace(binding.CredentialID) + "\x00" + strings.TrimSpace(binding.EnvVar)
}

func communicatedCredentialUseKey(use CommunicatedCredentialUse) string {
	return strings.TrimSpace(use.ID)
}

func normalizeCommunicatedCredentialUses(uses []CommunicatedCredentialUse) []CommunicatedCredentialUse {
	if len(uses) == 0 {
		return nil
	}
	normalized := make([]CommunicatedCredentialUse, 0, len(uses))
	seen := make(map[string]struct{}, len(uses))
	for _, use := range uses {
		use.ID = strings.TrimSpace(use.ID)
		use.Description = strings.TrimSpace(use.Description)
		if use.ID == "" {
			continue
		}
		if _, ok := seen[use.ID]; ok {
			continue
		}
		seen[use.ID] = struct{}{}
		normalized = append(normalized, use)
	}
	sort.Slice(normalized, func(i, j int) bool {
		return normalized[i].ID < normalized[j].ID
	})
	return normalized
}

// NormalizeCommunicatedCredentialBindings returns a deterministic,
// deduplicated copy of communicated credential bindings.
func NormalizeCommunicatedCredentialBindings(bindings []CommunicatedCredentialBinding) []CommunicatedCredentialBinding {
	if len(bindings) == 0 {
		return nil
	}
	normalized := make([]CommunicatedCredentialBinding, 0, len(bindings))
	seen := make(map[string]struct{}, len(bindings))
	for _, binding := range bindings {
		binding.CredentialID = strings.TrimSpace(binding.CredentialID)
		binding.EnvVar = strings.TrimSpace(binding.EnvVar)
		if binding.CredentialID == "" {
			continue
		}
		binding.Uses = normalizeCommunicatedCredentialUses(binding.Uses)
		key := communicatedCredentialBindingKey(binding)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, binding)
	}
	sort.Slice(normalized, func(i, j int) bool {
		if normalized[i].CredentialID != normalized[j].CredentialID {
			return normalized[i].CredentialID < normalized[j].CredentialID
		}
		return normalized[i].EnvVar < normalized[j].EnvVar
	})
	return normalized
}

// MergeCommunicatedCredentialBindings replaces or adds bindings from updates
// into the existing communicated set.
func MergeCommunicatedCredentialBindings(
	existing []CommunicatedCredentialBinding,
	updates []CommunicatedCredentialBinding,
) []CommunicatedCredentialBinding {
	merged := NormalizeCommunicatedCredentialBindings(existing)
	if len(updates) == 0 {
		return merged
	}
	byKey := make(map[string]CommunicatedCredentialBinding, len(merged)+len(updates))
	for _, binding := range merged {
		byKey[communicatedCredentialBindingKey(binding)] = binding
	}
	for _, binding := range NormalizeCommunicatedCredentialBindings(updates) {
		byKey[communicatedCredentialBindingKey(binding)] = binding
	}
	result := make([]CommunicatedCredentialBinding, 0, len(byKey))
	for _, binding := range byKey {
		result = append(result, binding)
	}
	return NormalizeCommunicatedCredentialBindings(result)
}

// DiffCommunicatedCredentialBindings computes added and removed binding or use
// IDs between two communicated credential snapshots.
func DiffCommunicatedCredentialBindings(
	before []CommunicatedCredentialBinding,
	after []CommunicatedCredentialBinding,
) (added []CommunicatedCredentialBinding, removed []CommunicatedCredentialBinding) {
	before = NormalizeCommunicatedCredentialBindings(before)
	after = NormalizeCommunicatedCredentialBindings(after)

	beforeByKey := make(map[string]CommunicatedCredentialBinding, len(before))
	for _, binding := range before {
		beforeByKey[communicatedCredentialBindingKey(binding)] = binding
	}
	afterByKey := make(map[string]CommunicatedCredentialBinding, len(after))
	for _, binding := range after {
		afterByKey[communicatedCredentialBindingKey(binding)] = binding
	}

	for key, current := range afterByKey {
		previous, ok := beforeByKey[key]
		if !ok {
			added = append(added, current)
			continue
		}
		prevUses := make(map[string]CommunicatedCredentialUse, len(previous.Uses))
		for _, use := range previous.Uses {
			prevUses[communicatedCredentialUseKey(use)] = use
		}
		var addedUses []CommunicatedCredentialUse
		for _, use := range current.Uses {
			if _, ok := prevUses[communicatedCredentialUseKey(use)]; ok {
				continue
			}
			addedUses = append(addedUses, use)
		}
		if len(addedUses) > 0 {
			added = append(added, CommunicatedCredentialBinding{
				CredentialID: current.CredentialID,
				EnvVar:       current.EnvVar,
				Uses:         addedUses,
			})
		}
	}

	for key, previous := range beforeByKey {
		current, ok := afterByKey[key]
		if !ok {
			removed = append(removed, previous)
			continue
		}
		currentUses := make(map[string]CommunicatedCredentialUse, len(current.Uses))
		for _, use := range current.Uses {
			currentUses[communicatedCredentialUseKey(use)] = use
		}
		var removedUses []CommunicatedCredentialUse
		for _, use := range previous.Uses {
			if _, ok := currentUses[communicatedCredentialUseKey(use)]; ok {
				continue
			}
			removedUses = append(removedUses, use)
		}
		if len(removedUses) > 0 {
			removed = append(removed, CommunicatedCredentialBinding{
				CredentialID: previous.CredentialID,
				EnvVar:       previous.EnvVar,
				Uses:         removedUses,
			})
		}
	}

	return NormalizeCommunicatedCredentialBindings(added), NormalizeCommunicatedCredentialBindings(removed)
}

const (
	ThreadNameSourceUser      = "user"
	ThreadNameSourceGenerated = "generated"
)

type State string

const (
	StateNone        State = ""
	StateInterrupted State = "interrupted"
	StateCancelled   State = "cancelled"
)

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
	cfg.CommunicatedCredentials = NormalizeCommunicatedCredentialBindings(cfg.CommunicatedCredentials)
	cfg.CommunicatedSkillLikeEntries = NormalizeCommunicatedSkillLikeEntries(cfg.CommunicatedSkillLikeEntries)
	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal thread config: %w", err)
	}
	return WriteFileAtomic(s.threadConfigPath(threadID), data, 0o644)
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
		Name                         string                          `json:"name"`
		NameSource                   string                          `json:"nameSource"`
		LastMessage                  string                          `json:"lastMessage"`
		ErrorMessage                 string                          `json:"errorMessage"`
		Model                        string                          `json:"model"`
		ProviderID                   string                          `json:"providerId"`
		Reasoning                    providers.Reasoning             `json:"reasoning"`
		ServiceTier                  string                          `json:"serviceTier"`
		CWD                          string                          `json:"cwd"`
		LastTurnState                State                           `json:"lastTurnState"`
		TokenUsage                   TokenUsageInfo                  `json:"tokenUsage"`
		ActiveLeafID                 string                          `json:"activeLeafId"`
		ContextReset                 bool                            `json:"contextReset"`
		ActiveCommand                string                          `json:"activeCommand"`
		CommunicatedCredentials      []CommunicatedCredentialBinding `json:"communicatedCredentials"`
		CommunicatedSkillLikeEntries []CommunicatedSkillLikeEntry    `json:"communicatedSkillLikeEntries"`
		Metadata                     ConfigMetadata                  `json:"metadata"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return Config{}, fmt.Errorf("unmarshal thread config: %w", err)
	}
	// If model already contains "/" it's a full ref; otherwise combine with providerId.
	model := raw.Model
	if model != "" && !strings.Contains(model, "/") && raw.ProviderID != "" {
		model = raw.ProviderID + "/" + model
	}
	return Config{
		Name:                         raw.Name,
		NameSource:                   raw.NameSource,
		LastMessage:                  raw.LastMessage,
		ErrorMessage:                 raw.ErrorMessage,
		Model:                        model,
		Reasoning:                    raw.Reasoning,
		ServiceTier:                  raw.ServiceTier,
		CWD:                          raw.CWD,
		LastTurnState:                raw.LastTurnState,
		TokenUsage:                   raw.TokenUsage,
		ActiveLeafID:                 raw.ActiveLeafID,
		ContextReset:                 raw.ContextReset,
		ActiveCommand:                strings.TrimSpace(raw.ActiveCommand),
		CommunicatedCredentials:      NormalizeCommunicatedCredentialBindings(raw.CommunicatedCredentials),
		CommunicatedSkillLikeEntries: NormalizeCommunicatedSkillLikeEntries(raw.CommunicatedSkillLikeEntries),
		Metadata:                     raw.Metadata,
	}, nil
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
		msg, err := s.LoadMessage(threadID, id)
		if err != nil {
			if errors.Is(err, ErrCorruptMessage) {
				log.Printf("thread store: skipping corrupt message %s in thread %s while finding leaf: %v", id, threadID, err)
			}
			continue
		}
		allIDs = append(allIDs, id)
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
