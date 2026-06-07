package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	acpclient "github.com/obot-platform/discobot/agent-go/acp/client"
	"github.com/obot-platform/discobot/agent-go/acp/protocol"
	discobotagent "github.com/obot-platform/discobot/agent-go/agent"
	"github.com/obot-platform/discobot/agent-go/internal/api"
	"github.com/obot-platform/discobot/agent-go/message"
	"github.com/obot-platform/discobot/agent-go/thread"
)

type sessionManager struct {
	client *acpclient.Client
	cwd    string
	store  ThreadStore
	state  *sessionStore

	mu                    sync.Mutex
	supportsLoadSession   bool
	supportsResumeSession bool
	supportsListSessions  bool
	pendingPermissions    map[string]*pendingPermission
	answeredPermissions   map[string]string
}

func newSessionManager(client *acpclient.Client, cwd string, store ThreadStore) *sessionManager {
	return &sessionManager{
		client:              client,
		cwd:                 cwd,
		store:               store,
		state:               newSessionStore(),
		pendingPermissions:  make(map[string]*pendingPermission),
		answeredPermissions: make(map[string]string),
	}
}

type pendingPermission struct {
	approvalID string
	toolCallID string
	question   discobotagent.PendingQuestion
	options    []protocol.PermissionOption
	answer     chan permissionAnswer
	prompt     *parkedPrompt
	submitted  *permissionAnswer
	resumed    bool
}

type permissionAnswer struct {
	response protocol.RequestPermissionResponse
	approved bool
}

func (m *sessionManager) setCapabilities(load, resume, list bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.supportsLoadSession = load
	m.supportsResumeSession = resume
	m.supportsListSessions = list
}

type sessionStore struct {
	mu       sync.Mutex
	sessions map[string]*sessionState
}

type sessionState struct {
	ThreadID  string
	SessionID protocol.SessionID
	Messages  []message.UIMessage
}

func newSessionStore() *sessionStore {
	return &sessionStore{sessions: make(map[string]*sessionState)}
}

func (s *sessionStore) Get(threadID string) (*sessionState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.sessions[threadID]
	if !ok {
		return nil, false
	}
	copyState := *state
	copyState.Messages = append([]message.UIMessage(nil), state.Messages...)
	return &copyState, true
}

func (s *sessionStore) Set(threadID string, sessionID protocol.SessionID, messages []message.UIMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[threadID] = &sessionState{
		ThreadID:  threadID,
		SessionID: sessionID,
		Messages:  append([]message.UIMessage(nil), messages...),
	}
}

func (s *sessionStore) UpdateSessionID(threadID string, sessionID protocol.SessionID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.sessions[threadID]
	if !ok {
		state = &sessionState{ThreadID: threadID}
		s.sessions[threadID] = state
	}
	state.SessionID = sessionID
}

func (s *sessionStore) AppendMessages(threadID string, messages []message.UIMessage) {
	if len(messages) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.sessions[threadID]
	if !ok {
		state = &sessionState{ThreadID: threadID}
		s.sessions[threadID] = state
	}
	state.Messages = append(state.Messages, messages...)
}

func (s *sessionStore) ThreadIDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	threadIDs := make([]string, 0, len(s.sessions))
	for threadID := range s.sessions {
		threadIDs = append(threadIDs, threadID)
	}
	return threadIDs
}

func (s *sessionStore) BySessionID() map[protocol.SessionID]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	bySessionID := make(map[protocol.SessionID]string, len(s.sessions))
	for threadID, state := range s.sessions {
		if state.SessionID != "" {
			bySessionID[state.SessionID] = threadID
		}
	}
	return bySessionID
}

func (m *sessionManager) sessionID(threadID string) (protocol.SessionID, bool) {
	if state, ok := m.state.Get(threadID); ok && state.SessionID != "" {
		return state.SessionID, true
	}
	session, ok := m.loadStoredSession(threadID)
	if !ok {
		return "", false
	}
	return session.SessionID, true
}

// Cancel sends an ACP session/cancel notification when threadID has a mapped ACP
// session. Active prompts are also cancelled through Prompt's context hook.
func (m *sessionManager) Cancel(threadID string) bool {
	if m.cancelPendingPermission(threadID) {
		return true
	}
	sessionID, ok := m.sessionID(threadID)
	if !ok {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return m.client.Cancel(ctx, protocol.CancelNotification{SessionID: sessionID}) == nil
}

func (m *sessionManager) cancelPendingPermission(threadID string) bool {
	m.mu.Lock()
	pending := m.pendingPermissions[threadID]
	if pending == nil {
		m.mu.Unlock()
		return false
	}
	delete(m.pendingPermissions, threadID)
	prompt := pending.prompt
	m.mu.Unlock()

	prompt.cancel()
	return true
}

func (m *sessionManager) Messages(threadID, _ string) ([]message.UIMessage, error) {
	if state, ok := m.state.Get(threadID); ok {
		return state.Messages, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := m.ensure(ctx, threadID); err != nil {
		return nil, err
	}
	state, _ := m.state.Get(threadID)
	if state == nil {
		return nil, nil
	}
	return state.Messages, nil
}

func (m *sessionManager) ListThreads() ([]string, error) {
	m.mu.Lock()
	supportsListSessions := m.supportsListSessions
	m.mu.Unlock()
	if supportsListSessions {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := m.syncSessions(ctx); err != nil {
			return nil, err
		}
	}
	return m.mappedThreads(), nil
}

func (m *sessionManager) mappedThreads() []string {
	sessionThreadIDs := m.state.ThreadIDs()
	threadSet := make(map[string]struct{}, len(sessionThreadIDs))
	for _, threadID := range sessionThreadIDs {
		threadSet[threadID] = struct{}{}
	}

	storedThreads, err := m.store.ListThreads()
	if err == nil {
		for _, threadID := range storedThreads {
			cfg, err := m.store.LoadConfig(threadID)
			if err == nil && strings.TrimSpace(cfg.Metadata.ACPSession.SessionID) != "" {
				threadSet[threadID] = struct{}{}
			}
		}
	}

	threads := make([]string, 0, len(threadSet))
	for threadID := range threadSet {
		threads = append(threads, threadID)
	}
	sort.Strings(threads)
	return threads
}

func (m *sessionManager) syncSessions(ctx context.Context) error {
	var cursor *string
	for {
		result, err := m.client.ListSessions(ctx, protocol.ListSessionsRequest{
			Cursor: cursor,
			Cwd:    stringPtr(m.cwd),
		})
		if err != nil {
			return err
		}
		if err := m.reconcileSessions(ctx, result.Sessions); err != nil {
			return err
		}
		if result.NextCursor == nil {
			return nil
		}
		cursor = result.NextCursor
	}
}

func (m *sessionManager) reconcileSessions(ctx context.Context, sessions []protocol.SessionInfo) error {
	bySessionID := m.state.BySessionID()
	stored, err := m.storedSessionThreads()
	if err != nil {
		return err
	}
	for _, session := range sessions {
		if threadID, ok := bySessionID[session.SessionID]; ok {
			_ = m.saveThreadSession(threadID, session, nil)
			continue
		}
		if threadID, ok := stored[session.SessionID]; ok {
			m.state.UpdateSessionID(threadID, session.SessionID)
			_ = m.saveThreadSession(threadID, session, nil)
			continue
		}
		if err := m.importSession(ctx, session); err != nil {
			return err
		}
	}
	return nil
}

func (m *sessionManager) storedSessionThreads() (map[protocol.SessionID]string, error) {
	threads, err := m.store.ListThreads()
	if err != nil {
		return nil, err
	}
	bySessionID := make(map[protocol.SessionID]string, len(threads))
	for _, threadID := range threads {
		cfg, err := m.store.LoadConfig(threadID)
		if err != nil {
			continue
		}
		sessionID := strings.TrimSpace(cfg.Metadata.ACPSession.SessionID)
		if sessionID != "" {
			bySessionID[protocol.SessionID(sessionID)] = threadID
		}
	}
	return bySessionID, nil
}

func (m *sessionManager) importSession(ctx context.Context, session protocol.SessionInfo) error {
	m.mu.Lock()
	supportsLoad := m.supportsLoadSession
	m.mu.Unlock()
	if !supportsLoad {
		return nil
	}

	threadID := "thread-" + discobotagent.GenerateID()
	projection := newSessionProjection()
	response, err := m.client.LoadSession(ctx, protocol.LoadSessionRequest{
		Cwd:        nonEmpty(session.Cwd, m.cwd),
		MCPServers: []protocol.MCPServer{},
		SessionID:  session.SessionID,
	}, acpclient.WithOnUpdate(func(notification protocol.SessionNotification) error {
		m.applyUpdate(threadID, notification)
		if _, err := projection.push(notification.Update); err != nil {
			return err
		}
		return nil
	}))
	if err != nil {
		return err
	}
	messages, err := projectedUIMessages(projection.messages())
	if err != nil {
		return err
	}
	if updated, ok := m.loadStoredSession(threadID); ok {
		session = updated
	}
	m.state.Set(threadID, session.SessionID, messages)
	if err := m.saveThreadSession(threadID, session, &response); err != nil {
		return err
	}
	return nil
}

func (m *sessionManager) ensure(ctx context.Context, threadID string) (protocol.SessionID, error) {
	if state, ok := m.state.Get(threadID); ok {
		return state.SessionID, nil
	}

	if session, ok := m.loadStoredSession(threadID); ok {
		loaded, response, messages, err := m.restoreStoredSession(ctx, threadID, session)
		if err != nil {
			return "", err
		}
		if !loaded {
			return m.createSession(ctx, threadID)
		}
		if updated, ok := m.loadStoredSession(threadID); ok {
			session = updated
		}
		m.state.Set(threadID, session.SessionID, messages)
		_ = m.saveThreadSession(threadID, session, &response)
		return session.SessionID, nil
	}

	return m.createSession(ctx, threadID)
}

func (m *sessionManager) createSession(ctx context.Context, threadID string) (protocol.SessionID, error) {
	// MCPServers must serialize as an array, not null: strict ACP servers
	// (e.g. claude-code-acp) reject `"mcpServers": null` with Invalid params.
	result, err := m.client.NewSession(ctx, protocol.NewSessionRequest{
		Cwd:        m.cwd,
		MCPServers: []protocol.MCPServer{},
	})
	if err != nil {
		return "", err
	}
	session := protocol.SessionInfo{
		Cwd:       m.cwd,
		SessionID: result.SessionID,
	}

	if state, ok := m.state.Get(threadID); ok {
		return state.SessionID, nil
	}
	m.state.Set(threadID, result.SessionID, nil)

	emptyResponse := protocol.LoadSessionResponse{}
	_ = m.saveThreadSession(threadID, session, &emptyResponse)
	return result.SessionID, nil
}

func (m *sessionManager) restoreStoredSession(ctx context.Context, threadID string, session protocol.SessionInfo) (bool, protocol.LoadSessionResponse, []message.UIMessage, error) {
	m.mu.Lock()
	supportsLoad := m.supportsLoadSession
	supportsResume := m.supportsResumeSession
	m.mu.Unlock()

	projection := newSessionProjection()
	cwd := nonEmpty(session.Cwd, m.cwd)
	onUpdate := func(notification protocol.SessionNotification) error {
		m.applyUpdate(threadID, notification)
		if _, err := projection.push(notification.Update); err != nil {
			return err
		}
		return nil
	}
	if supportsLoad {
		response, err := m.client.LoadSession(ctx, protocol.LoadSessionRequest{
			Cwd:        cwd,
			MCPServers: []protocol.MCPServer{},
			SessionID:  session.SessionID,
		}, acpclient.WithOnUpdate(onUpdate))
		messages, projectErr := projectedUIMessages(projection.messages())
		if err == nil {
			err = projectErr
		}
		return true, response, messages, err
	}
	if supportsResume {
		response, err := m.client.ResumeSession(ctx, protocol.ResumeSessionRequest{
			Cwd:        cwd,
			MCPServers: []protocol.MCPServer{},
			SessionID:  session.SessionID,
		}, acpclient.WithOnUpdate(onUpdate))
		messages, projectErr := projectedUIMessages(projection.messages())
		if err == nil {
			err = projectErr
		}
		return true, loadResponseFromResume(response), messages, err
	}
	return false, protocol.LoadSessionResponse{}, nil, nil
}

func (m *sessionManager) loadStoredSession(threadID string) (protocol.SessionInfo, bool) {
	cfg, err := m.store.LoadConfig(threadID)
	if err != nil {
		return protocol.SessionInfo{}, false
	}
	session := cfg.Metadata.ACPSession
	if strings.TrimSpace(session.SessionID) == "" {
		return protocol.SessionInfo{}, false
	}
	return protocol.SessionInfo{
		Meta:      session.Meta,
		Cwd:       session.CWD,
		SessionID: protocol.SessionID(session.SessionID),
		Title:     session.Title,
		UpdatedAt: session.UpdatedAt,
	}, true
}

func (m *sessionManager) saveThreadSession(threadID string, session protocol.SessionInfo, loadResponse *protocol.LoadSessionResponse) error {
	if err := m.store.CreateThread(threadID); err != nil {
		return err
	}
	cfg, err := m.store.LoadConfig(threadID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(cfg.CWD) == "" {
		cfg.CWD = m.cwd
	}
	existingACP := cfg.Metadata.ACPSession
	if session.Title != nil {
		title := strings.TrimSpace(*session.Title)
		existingTitle := ""
		if existingACP.Title != nil {
			existingTitle = strings.TrimSpace(*existingACP.Title)
		}
		if title != "" && (strings.TrimSpace(cfg.Name) == "" || strings.TrimSpace(cfg.Name) == existingTitle) {
			cfg.Name = title
		}
	}
	cfg.Metadata.ACPSession = thread.ACPSessionMetadata{
		Meta:          session.Meta,
		CWD:           session.Cwd,
		SessionID:     string(session.SessionID),
		Title:         session.Title,
		UpdatedAt:     session.UpdatedAt,
		ResponseMeta:  existingACP.ResponseMeta,
		ConfigOptions: existingACP.ConfigOptions,
	}
	if loadResponse != nil {
		cfg.Metadata.ACPSession.ResponseMeta = loadResponse.Meta
		cfg.Metadata.ACPSession.ConfigOptions = nil
	}
	if loadResponse != nil && len(loadResponse.ConfigOptions) > 0 {
		cfg.Metadata.ACPSession.ConfigOptions = make([]json.RawMessage, 0, len(loadResponse.ConfigOptions))
		for _, option := range loadResponse.ConfigOptions {
			cfg.Metadata.ACPSession.ConfigOptions = append(cfg.Metadata.ACPSession.ConfigOptions, option.Raw())
		}
	}
	return m.store.SaveConfig(threadID, cfg)
}

func (m *sessionManager) startPermissionRequest(threadID string, request protocol.RequestPermissionRequest, prompt *parkedPrompt) ([]message.MessageChunk, string, string, error) {
	approvalID := "acp-permission-" + string(request.ToolCall.ToolCallID)
	if strings.TrimSpace(approvalID) == "acp-permission-" {
		approvalID = "acp-permission-" + discobotagent.GenerateID()
	}

	questionText := permissionQuestionText(request)
	options := make([]api.AskUserQuestionOption, 0, len(request.Options))
	for _, option := range request.Options {
		options = append(options, api.AskUserQuestionOption{
			Label:       option.Name,
			Description: permissionOptionDescription(option),
		})
	}

	pending := &pendingPermission{
		approvalID: approvalID,
		toolCallID: string(request.ToolCall.ToolCallID),
		question: discobotagent.PendingQuestion{
			ApprovalID: approvalID,
			Questions: []api.AskUserQuestion{{
				Header:      "Permission required",
				Question:    questionText,
				Options:     options,
				MultiSelect: false,
				Notes:       permissionNotes(request),
			}},
			Context: permissionContext(request),
		},
		options: append([]protocol.PermissionOption(nil), request.Options...),
		answer:  make(chan permissionAnswer, 1),
		prompt:  prompt,
	}

	m.mu.Lock()
	m.pendingPermissions[threadID] = pending
	delete(m.answeredPermissions, threadID)
	m.mu.Unlock()

	input, err := json.Marshal(map[string]any{"questions": pending.question.Questions})
	if err != nil {
		m.clearPendingPermission(threadID, approvalID)
		return nil, "", "", err
	}
	dynamic := true
	chunks := []message.MessageChunk{
		message.ToolCallChunk{
			ToolCallID: pending.toolCallID,
			ToolName:   "AskUserQuestion",
			Input:      string(input),
			Dynamic:    &dynamic,
		},
		message.ToolApprovalRequestChunk{
			ApprovalID: approvalID,
			ToolCallID: pending.toolCallID,
		},
	}
	return chunks, approvalID, pending.toolCallID, nil
}

func (m *sessionManager) waitPermissionResponse(ctx context.Context, threadID, approvalID string) (protocol.RequestPermissionResponse, bool, error) {
	m.mu.Lock()
	pending := m.pendingPermissions[threadID]
	m.mu.Unlock()
	if pending == nil || pending.approvalID != approvalID {
		return cancelledPermissionResponse(), false, fmt.Errorf("pending permission %s not found for thread %s", approvalID, threadID)
	}

	select {
	case answer := <-pending.answer:
		m.clearPendingPermission(threadID, approvalID)
		return answer.response, answer.approved, nil
	case <-ctx.Done():
		m.clearPendingPermission(threadID, approvalID)
		return cancelledPermissionResponse(), false, nil
	}
}

func (m *sessionManager) clearPendingPermission(threadID, approvalID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if pending := m.pendingPermissions[threadID]; pending != nil && pending.approvalID == approvalID {
		delete(m.pendingPermissions, threadID)
		m.answeredPermissions[threadID] = approvalID
	}
}

func (m *sessionManager) PendingQuestion(threadID string) (*discobotagent.PendingQuestion, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	pending := m.pendingPermissions[threadID]
	if pending == nil {
		return nil, nil
	}
	question := pending.question
	question.Questions = append([]api.AskUserQuestion(nil), pending.question.Questions...)
	question.Credentials = append([]api.RequestedCredential(nil), pending.question.Credentials...)
	question.Metadata = append(json.RawMessage(nil), pending.question.Metadata...)
	return &question, nil
}

func (m *sessionManager) SubmitAnswer(threadID, approvalID string, req api.AnswerQuestionRequest) error {
	m.mu.Lock()
	pending := m.pendingPermissions[threadID]
	if pending == nil || pending.approvalID != approvalID {
		if m.answeredPermissions[threadID] == approvalID {
			m.mu.Unlock()
			return nil
		}
		m.mu.Unlock()
		return fmt.Errorf("pending permission %s not found for thread %s", approvalID, threadID)
	}
	if pending.submitted != nil {
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()

	selected, err := pending.selectedOption(req.Answers)
	if err != nil {
		return err
	}
	answer := permissionAnswer{
		response: protocol.RequestPermissionResponse{
			Outcome: protocol.RequestPermissionOutcomeSelected{
				SelectedPermissionOutcome: protocol.SelectedPermissionOutcome{
					OptionID: selected.OptionID,
				},
			}.RequestPermissionOutcome(),
		},
		approved: permissionOptionApproved(selected.Kind),
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	current := m.pendingPermissions[threadID]
	if current == nil || current.approvalID != approvalID {
		if m.answeredPermissions[threadID] == approvalID {
			return nil
		}
		return fmt.Errorf("pending permission %s not found for thread %s", approvalID, threadID)
	}
	if current.submitted != nil {
		return nil
	}
	current.submitted = &answer
	return nil
}

func (m *sessionManager) resumePermission(threadID string) (*parkedPrompt, error) {
	m.mu.Lock()
	pending := m.pendingPermissions[threadID]
	if pending == nil {
		m.mu.Unlock()
		return nil, discobotagent.ErrInterruptedTurnRequiresResume
	}
	if pending.submitted == nil {
		m.mu.Unlock()
		return nil, discobotagent.ErrPendingQuestionRequiresAnswer
	}
	if pending.resumed {
		prompt := pending.prompt
		m.mu.Unlock()
		return prompt, nil
	}
	answer := *pending.submitted
	pending.resumed = true
	prompt := pending.prompt
	answerCh := pending.answer
	m.mu.Unlock()

	answerCh <- answer
	return prompt, nil
}

func (p *pendingPermission) selectedOption(answers map[string]string) (protocol.PermissionOption, error) {
	if len(p.options) == 0 {
		return protocol.PermissionOption{}, fmt.Errorf("pending permission %s has no options", p.approvalID)
	}
	answer := strings.TrimSpace(answers[p.question.Questions[0].Question])
	if answer == "" {
		answer = strings.TrimSpace(answers[p.approvalID])
	}
	if answer == "" {
		return protocol.PermissionOption{}, fmt.Errorf("answer for pending permission %s is required", p.approvalID)
	}
	for _, option := range p.options {
		if answer == option.Name || answer == string(option.OptionID) {
			return option, nil
		}
	}
	return protocol.PermissionOption{}, fmt.Errorf("answer %q does not match permission options for %s", answer, p.approvalID)
}

func cancelledPermissionResponse() protocol.RequestPermissionResponse {
	return protocol.RequestPermissionResponse{
		Outcome: protocol.RequestPermissionOutcomeCancelled{}.RequestPermissionOutcome(),
	}
}

func permissionOptionApproved(kind protocol.PermissionOptionKind) bool {
	return kind == protocol.PermissionOptionKindAllowOnce || kind == protocol.PermissionOptionKindAllowAlways
}

func permissionQuestionText(request protocol.RequestPermissionRequest) string {
	if request.ToolCall.Title != nil && strings.TrimSpace(*request.ToolCall.Title) != "" {
		return fmt.Sprintf("Allow %s?", strings.TrimSpace(*request.ToolCall.Title))
	}
	if request.ToolCall.ToolCallID != "" {
		return fmt.Sprintf("Allow tool call %s?", request.ToolCall.ToolCallID)
	}
	return "Allow this tool call?"
}

func permissionOptionDescription(option protocol.PermissionOption) string {
	switch option.Kind {
	case protocol.PermissionOptionKindAllowOnce:
		return "Allow this operation once."
	case protocol.PermissionOptionKindAllowAlways:
		return "Allow this operation and remember the choice."
	case protocol.PermissionOptionKindRejectOnce:
		return "Reject this operation once."
	case protocol.PermissionOptionKindRejectAlways:
		return "Reject this operation and remember the choice."
	default:
		return string(option.Kind)
	}
}

func permissionNotes(request protocol.RequestPermissionRequest) string {
	if len(request.ToolCall.RawInput) == 0 {
		return ""
	}
	return "```json\n" + strings.TrimSpace(string(request.ToolCall.RawInput)) + "\n```"
}

func permissionContext(request protocol.RequestPermissionRequest) string {
	if request.ToolCall.Title != nil && strings.TrimSpace(*request.ToolCall.Title) != "" {
		return strings.TrimSpace(*request.ToolCall.Title)
	}
	if request.ToolCall.ToolCallID != "" {
		return string(request.ToolCall.ToolCallID)
	}
	return "ACP permission request"
}

func nonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func loadResponseFromResume(response protocol.ResumeSessionResponse) protocol.LoadSessionResponse {
	return protocol.LoadSessionResponse(response)
}
