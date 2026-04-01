// API Client for making requests to the backend
import { appendAuthToken, getApiBase, getApiRootBase } from "./api-config";

/** Error thrown when file write fails due to optimistic locking conflict */
export class FileConflictError extends Error {
	constructor(
		message: string,
		public currentContent: string,
	) {
		super(message);
		this.name = "FileConflictError";
	}
}

export class StartChatError extends Error {
	constructor(
		message: string,
		public code: string,
		public questionId?: string,
		public completionId?: string,
	) {
		super(message);
		this.name = "StartChatError";
	}
}

import type {
	AnswerQuestionRequest,
	AnswerQuestionResponse,
	AuthProvider,
	CancelChatResponse,
	CreateThreadRequest,
	CodexAuthorizeResponse,
	CodexExchangeRequest,
	CodexExchangeResponse,
	CreateCredentialRequest,
	CreateWorkspaceRequest,
	CredentialInfo,
	CredentialType,
	DeleteQueuedPromptResponse,
	DeleteSessionFileRequest,
	DeleteSessionFileResponse,
	ValidateWorkspaceRequest,
	GitHubCopilotDeviceCodeRequest,
	GitHubCopilotDeviceCodeResponse,
	GitHubCopilotPollRequest,
	GitHubCopilotPollResponse,
	GitHubDeviceCodeRequest,
	GitHubDeviceCodeResponse,
	GitHubPollRequest,
	GitHubPollResponse,
	HookOutputResponse,
	HookRerunResponse,
	HooksStatusResponse,
	ListThreadsResponse,
	ListServicesResponse,
	ListSessionFilesResponse,
	ModelsResponse,
	OAuthAuthorizeResponse,
	OAuthExchangeRequest,
	OAuthExchangeResponse,
	OAuthRefreshResponse,
	PendingQuestionResponse,
	ProviderStatus,
	ProvidersResponse,
	ReadSessionFileResponse,
	RenameSessionFileRequest,
	RenameSessionFileResponse,
	SearchSessionFilesResponse,
	ServerConfig,
	Session,
	SessionCredentialAssignment,
	SessionDiffFilesResponse,
	SessionDiffResponse,
	SessionSingleFileDiffResponse,
	StartChatErrorResponse,
	StartChatRequest,
	StartChatResponse,
	StartServiceResponse,
	StopServiceResponse,
	Suggestion,
	SupportInfoResponse,
	Thread,
	SystemStatusResponse,
	TerminalExecuteResponse,
	UpdateThreadRequest,
	UpdateSessionRequest,
	UserPreference,
	WorkspaceValidationResult,
	Workspace,
	WriteSessionFileRequest,
	WriteSessionFileResponse,
} from "./api-types";

class ApiClient {
	// Use getters to get current base URL (may change after Tauri init)
	private get base() {
		return getApiBase();
	}
	private get rootBase() {
		return getApiRootBase();
	}

	private async fetch<T>(path: string, options?: RequestInit): Promise<T> {
		const response = await fetch(appendAuthToken(`${this.base}${path}`), {
			...options,
			headers: {
				"Content-Type": "application/json",
				...options?.headers,
			},
		});

		// Treat 404 as success for DELETE requests (resource already gone)
		if (options?.method === "DELETE" && response.status === 404) {
			return undefined as T;
		}

		if (!response.ok) {
			const error = await response
				.json()
				.catch(() => ({ error: "Request failed" }));
			throw new Error(error.error || "Request failed");
		}

		return response.json();
	}

	// Fetch from root API (not project-scoped)
	private async fetchRoot<T>(path: string, options?: RequestInit): Promise<T> {
		const response = await fetch(appendAuthToken(`${this.rootBase}${path}`), {
			...options,
			headers: {
				"Content-Type": "application/json",
				...options?.headers,
			},
		});

		if (!response.ok) {
			const error = await response
				.json()
				.catch(() => ({ error: "Request failed" }));
			throw new Error(error.error || "Request failed");
		}

		return response.json();
	}

	// System Status
	async getSystemStatus(): Promise<SystemStatusResponse> {
		return this.fetchRoot<SystemStatusResponse>("/status");
	}

	async getServerConfig(): Promise<ServerConfig> {
		return this.fetchRoot<ServerConfig>("/server-config");
	}

	async getSupportInfo(): Promise<SupportInfoResponse> {
		return this.fetchRoot<SupportInfoResponse>("/support-info");
	}

	// Providers
	async getProviders(): Promise<ProvidersResponse> {
		return this.fetch<ProvidersResponse>("/workspaces/providers");
	}

	async getProvider(name: string): Promise<ProviderStatus> {
		return this.fetch<ProviderStatus>(`/workspaces/providers/${name}`);
	}

	// Workspaces

	async getWorkspaces(): Promise<{ workspaces: Workspace[] }> {
		return this.fetch<{ workspaces: Workspace[] }>("/workspaces");
	}

	async getWorkspace(id: string): Promise<Workspace> {
		return this.fetch<Workspace>(`/workspaces/${id}`);
	}

	async createWorkspace(data: CreateWorkspaceRequest): Promise<Workspace> {
		return this.fetch<Workspace>("/workspaces", {
			method: "POST",
			body: JSON.stringify(data),
		});
	}

	async validateWorkspace(
		data: ValidateWorkspaceRequest,
	): Promise<WorkspaceValidationResult> {
		return this.fetch<WorkspaceValidationResult>("/workspaces/validate", {
			method: "POST",
			body: JSON.stringify(data),
		});
	}

	async updateWorkspace(
		id: string,
		data: { path?: string; displayName?: string | null },
	): Promise<Workspace> {
		return this.fetch<Workspace>(`/workspaces/${id}`, {
			method: "PUT",
			body: JSON.stringify(data),
		});
	}

	async deleteWorkspace(id: string, deleteFiles = false): Promise<void> {
		const params = deleteFiles ? "?deleteFiles=true" : "";
		await this.fetch(`/workspaces/${id}${params}`, { method: "DELETE" });
	}

	// Sessions
	async getSessions(): Promise<{ sessions: Session[] }> {
		return this.fetch<{ sessions: Session[] }>("/sessions");
	}

	async getSession(id: string): Promise<Session> {
		return this.fetch<Session>(`/sessions/${id}`);
	}

	async createSession(data: {
		id: string;
		workspaceId?: string;
		model?: string;
		reasoning?: string;
	}): Promise<{ id: string }> {
		return this.fetch<{ id: string }>("/sessions", {
			method: "POST",
			body: JSON.stringify(data),
		});
	}

	async updateSession(
		id: string,
		data: UpdateSessionRequest,
	): Promise<Session> {
		return this.fetch<Session>(`/sessions/${id}`, {
			method: "PUT",
			body: JSON.stringify(data),
		});
	}

	async deleteSession(id: string): Promise<void> {
		await this.fetch(`/sessions/${id}`, { method: "DELETE" });
	}

	async commitSession(id: string): Promise<{ success: boolean }> {
		return this.fetch<{ success: boolean }>(`/sessions/${id}/commit`, {
			method: "POST",
		});
	}

	async rebaseSession(id: string): Promise<{ success: boolean }> {
		return this.fetch<{ success: boolean }>(`/sessions/${id}/rebase`, {
			method: "POST",
		});
	}

	// Session Files
	/**
	 * List files in a session's workspace directory.
	 * @param sessionId Session ID
	 * @param path Directory path relative to workspace root (defaults to ".")
	 * @param includeHidden Whether to include hidden files (starting with ".")
	 */
	async listSessionFiles(
		sessionId: string,
		path = ".",
		options?: { includeHidden?: boolean; signal?: AbortSignal },
	): Promise<ListSessionFilesResponse> {
		const params = new URLSearchParams({ path });
		if (options?.includeHidden) params.set("hidden", "true");
		return this.fetch<ListSessionFilesResponse>(
			`/sessions/${sessionId}/files?${params}`,
			options?.signal ? { signal: options.signal } : undefined,
		);
	}

	/**
	 * Fuzzy-search files in a session's workspace.
	 * Uses an fzf-style scoring algorithm. Results include both files and directories.
	 * @param sessionId Session ID
	 * @param query Search query (empty string returns all files)
	 * @param limit Maximum number of results (default 50, max 200)
	 */
	async searchSessionFiles(
		sessionId: string,
		query: string,
		limit = 50,
		options?: { signal?: AbortSignal },
	): Promise<SearchSessionFilesResponse> {
		const params = new URLSearchParams({ q: query, limit: String(limit) });
		return this.fetch<SearchSessionFilesResponse>(
			`/sessions/${sessionId}/files/search?${params}`,
			options?.signal ? { signal: options.signal } : undefined,
		);
	}

	/**
	 * Read a file from a session's workspace.
	 * @param sessionId Session ID
	 * @param path File path relative to workspace root
	 * @param options.fromBase If true, read from base commit (for deleted files)
	 */
	async readSessionFile(
		sessionId: string,
		path: string,
		options?: { fromBase?: boolean },
	): Promise<ReadSessionFileResponse> {
		const params = new URLSearchParams({ path });
		if (options?.fromBase) {
			params.set("fromBase", "true");
		}
		return this.fetch<ReadSessionFileResponse>(
			`/sessions/${sessionId}/files/read?${params}`,
		);
	}

	/**
	 * Write a file to a session's workspace.
	 * @param sessionId Session ID
	 * @param data File content and path (include originalContent for optimistic locking)
	 * @throws {FileConflictError} When originalContent doesn't match current file content
	 */
	async writeSessionFile(
		sessionId: string,
		data: WriteSessionFileRequest,
	): Promise<WriteSessionFileResponse> {
		const response = await fetch(
			appendAuthToken(`${this.base}/sessions/${sessionId}/files/write`),
			{
				method: "PUT",
				headers: { "Content-Type": "application/json" },
				body: JSON.stringify(data),
			},
		);

		const result = await response.json();

		if (!response.ok) {
			// Check for conflict error (optimistic locking failure)
			if (response.status === 409 && result.error === "conflict") {
				throw new FileConflictError(
					result.message || "File has been modified",
					result.currentContent,
				);
			}
			throw new Error(result.error || "Request failed");
		}

		return result;
	}

	/**
	 * Delete a file or directory in a session's workspace.
	 * @param sessionId Session ID
	 * @param data Delete request with file path
	 */
	async deleteSessionFile(
		sessionId: string,
		data: DeleteSessionFileRequest,
	): Promise<DeleteSessionFileResponse> {
		return this.fetch<DeleteSessionFileResponse>(
			`/sessions/${sessionId}/files/delete`,
			{
				method: "POST",
				body: JSON.stringify(data),
			},
		);
	}

	/**
	 * Rename/move a file or directory in a session's workspace.
	 * @param sessionId Session ID
	 * @param data Rename request with old and new paths
	 */
	async renameSessionFile(
		sessionId: string,
		data: RenameSessionFileRequest,
	): Promise<RenameSessionFileResponse> {
		return this.fetch<RenameSessionFileResponse>(
			`/sessions/${sessionId}/files/rename`,
			{
				method: "POST",
				body: JSON.stringify(data),
			},
		);
	}

	/**
	 * Get diff for a session's workspace.
	 * @param sessionId Session ID
	 * @param options.path Single file path for file-specific diff
	 * @param options.format "files" for file list only, undefined for full diff
	 */
	async getSessionDiff(
		sessionId: string,
		options?: { path?: string; format?: "files" },
	): Promise<
		| SessionDiffResponse
		| SessionDiffFilesResponse
		| SessionSingleFileDiffResponse
	> {
		const params = new URLSearchParams();
		if (options?.path) params.set("path", options.path);
		if (options?.format) params.set("format", options.format);
		const query = params.toString();
		return this.fetch(`/sessions/${sessionId}/diff${query ? `?${query}` : ""}`);
	}

	async startChat(data: StartChatRequest): Promise<StartChatResponse> {
		const response = await fetch(
			appendAuthToken(
				`${this.base}/sessions/${data.sessionId}/threads/${data.threadId}/chat`,
			),
			{
				method: "POST",
				headers: { "Content-Type": "application/json" },
				body: JSON.stringify(data),
			},
		);

		const result = await response
			.json()
			.catch(() => ({ error: "Request failed" }));

		if (!response.ok) {
			const error = result as Partial<StartChatErrorResponse> & {
				error?: string;
				message?: string;
			};
			throw new StartChatError(
				error.message || error.error || "Failed to start chat",
				error.error || "request_failed",
				"questionId" in error && typeof error.questionId === "string"
					? error.questionId
					: undefined,
				"completionId" in error && typeof error.completionId === "string"
					? error.completionId
					: undefined,
			);
		}

		return result as StartChatResponse;
	}

	getThreadChatStreamUrl(
		sessionId: string,
		threadId: string,
		replay = false,
	): string {
		const params = new URLSearchParams();
		if (replay) {
			params.set("replay", "true");
		}
		const query = params.toString();
		return appendAuthToken(
			`${this.base}/sessions/${sessionId}/threads/${encodeURIComponent(threadId)}/stream${query ? `?${query}` : ""}`,
		);
	}

	async cancelThreadChat(
		sessionId: string,
		threadId: string,
	): Promise<CancelChatResponse> {
		return this.fetch(
			`/sessions/${sessionId}/threads/${encodeURIComponent(threadId)}/cancel`,
			{
				method: "POST",
			},
		);
	}

	/**
	 * Get the pending AskUserQuestion for a specific approval ID.
	 * @param sessionId Session ID
	 * @param questionId The tool use / approval ID to query for
	 * @returns { status: "pending", question } if still waiting, { status: "answered", question: null } if resolved
	 */
	async getThreadChatQuestion(
		sessionId: string,
		threadId: string,
		questionId: string,
	): Promise<PendingQuestionResponse> {
		return this.fetch<PendingQuestionResponse>(
			`/sessions/${sessionId}/threads/${encodeURIComponent(threadId)}/question/${encodeURIComponent(questionId)}`,
		);
	}

	async submitThreadChatAnswer(
		sessionId: string,
		threadId: string,
		data: AnswerQuestionRequest,
	): Promise<AnswerQuestionResponse> {
		return this.fetch<AnswerQuestionResponse>(
			`/sessions/${sessionId}/threads/${encodeURIComponent(threadId)}/answer/${encodeURIComponent(data.toolUseID)}`,
			{
				method: "POST",
				body: JSON.stringify({ answers: data.answers }),
			},
		);
	}

	// Threads
	async getThreads(sessionId: string): Promise<ListThreadsResponse> {
		return this.fetch<ListThreadsResponse>(`/sessions/${sessionId}/threads`);
	}

	async getThread(sessionId: string, threadId: string): Promise<Thread> {
		return this.fetch<Thread>(
			`/sessions/${sessionId}/threads/${encodeURIComponent(threadId)}`,
		);
	}

	async createThread(
		sessionId: string,
		data: CreateThreadRequest,
	): Promise<Thread> {
		return this.fetch<Thread>(`/sessions/${sessionId}/threads`, {
			method: "POST",
			body: JSON.stringify(data),
		});
	}

	async updateThread(
		sessionId: string,
		threadId: string,
		data: UpdateThreadRequest,
	): Promise<Thread> {
		return this.fetch<Thread>(
			`/sessions/${sessionId}/threads/${encodeURIComponent(threadId)}`,
			{
				method: "PATCH",
				body: JSON.stringify(data),
			},
		);
	}

	async deleteThread(
		sessionId: string,
		threadId: string,
	): Promise<{ success: boolean }> {
		return this.fetch<{ success: boolean }>(
			`/sessions/${sessionId}/threads/${encodeURIComponent(threadId)}`,
			{
				method: "DELETE",
			},
		);
	}

	async deleteQueuedPrompt(
		sessionId: string,
		threadId: string,
		queueId: string,
	): Promise<DeleteQueuedPromptResponse> {
		return this.fetch<DeleteQueuedPromptResponse>(
			`/sessions/${sessionId}/threads/${encodeURIComponent(threadId)}/queue/${encodeURIComponent(queueId)}`,
			{
				method: "DELETE",
			},
		);
	}

	async getProjectModels(): Promise<ModelsResponse> {
		return this.fetch<ModelsResponse>("/models");
	}

	async getSessionModels(sessionId: string): Promise<ModelsResponse> {
		return this.fetch<ModelsResponse>(`/sessions/${sessionId}/models`);
	}

	async getAuthProviders(): Promise<{ authProviders: AuthProvider[] }> {
		return this.fetch("/auth-providers");
	}

	// Terminal
	async executeCommand(
		command: string,
		sessionId?: string,
	): Promise<TerminalExecuteResponse> {
		return this.fetch<TerminalExecuteResponse>("/terminal/execute", {
			method: "POST",
			body: JSON.stringify({ command, sessionId }),
		});
	}

	async getTerminalHistory(): Promise<{
		history: { type: "input" | "output"; content: string }[];
	}> {
		return this.fetch("/terminal/history");
	}

	// Suggestions
	async getSuggestions(
		query: string,
		type?: "path" | "repo",
	): Promise<{ suggestions: Suggestion[] }> {
		const params = new URLSearchParams({ q: query });
		if (type) params.set("type", type);
		return this.fetch<{ suggestions: Suggestion[] }>(`/suggestions?${params}`);
	}

	// Credentials
	async getCredentials(): Promise<{ credentials: CredentialInfo[] }> {
		return this.fetch<{ credentials: CredentialInfo[] }>("/credentials");
	}

	async getCredentialTypes(): Promise<{ credentialTypes: CredentialType[] }> {
		return this.fetch<{ credentialTypes: CredentialType[] }>(
			"/credentials/types",
		);
	}

	async getCredential(identifier: string): Promise<CredentialInfo> {
		return this.fetch<CredentialInfo>(
			`/credentials/${encodeURIComponent(identifier)}`,
		);
	}

	async createCredential(
		data: CreateCredentialRequest,
	): Promise<CredentialInfo> {
		return this.fetch<CredentialInfo>("/credentials", {
			method: "POST",
			body: JSON.stringify(data),
		});
	}

	async deleteCredential(identifier: string): Promise<void> {
		await this.fetch(`/credentials/${encodeURIComponent(identifier)}`, {
			method: "DELETE",
		});
	}

	async getSessionCredentials(sessionId: string): Promise<{
		credentials: SessionCredentialAssignment[];
	}> {
		return this.fetch<{ credentials: SessionCredentialAssignment[] }>(
			`/sessions/${sessionId}/credentials`,
		);
	}

	async setSessionCredentials(
		sessionId: string,
		credentials: Array<{ credentialId: string; agentVisible: boolean }>,
	): Promise<{ credentials: SessionCredentialAssignment[] }> {
		return this.fetch<{ credentials: SessionCredentialAssignment[] }>(
			`/sessions/${sessionId}/credentials`,
			{
				method: "PUT",
				body: JSON.stringify({ credentials }),
			},
		);
	}

	async refreshCredential(providerId: string): Promise<OAuthRefreshResponse> {
		return this.fetch<OAuthRefreshResponse>(
			`/credentials/${providerId}/refresh`,
			{
				method: "POST",
			},
		);
	}

	// Anthropic OAuth
	async anthropicAuthorize(): Promise<OAuthAuthorizeResponse> {
		return this.fetch<OAuthAuthorizeResponse>(
			"/credentials/anthropic/authorize",
			{
				method: "POST",
			},
		);
	}

	async anthropicExchange(
		data: OAuthExchangeRequest,
	): Promise<OAuthExchangeResponse> {
		return this.fetch<OAuthExchangeResponse>(
			"/credentials/anthropic/exchange",
			{
				method: "POST",
				body: JSON.stringify(data),
			},
		);
	}

	// GitHub Copilot OAuth (device flow)
	async githubCopilotDeviceCode(
		data: GitHubCopilotDeviceCodeRequest = {},
	): Promise<GitHubCopilotDeviceCodeResponse> {
		return this.fetch<GitHubCopilotDeviceCodeResponse>(
			"/credentials/github-copilot/device-code",
			{
				method: "POST",
				body: JSON.stringify(data),
			},
		);
	}

	async githubCopilotPoll(
		data: GitHubCopilotPollRequest,
	): Promise<GitHubCopilotPollResponse> {
		return this.fetch<GitHubCopilotPollResponse>(
			"/credentials/github-copilot/poll",
			{
				method: "POST",
				body: JSON.stringify(data),
			},
		);
	}

	// GitHub OAuth (git operations: repo scope, device flow)
	async githubDeviceCode(
		data: GitHubDeviceCodeRequest = {},
	): Promise<GitHubDeviceCodeResponse> {
		return this.fetch<GitHubDeviceCodeResponse>(
			"/credentials/github-git/device-code",
			{
				method: "POST",
				body: JSON.stringify(data),
			},
		);
	}

	async githubPoll(data: GitHubPollRequest): Promise<GitHubPollResponse> {
		return this.fetch<GitHubPollResponse>("/credentials/github-git/poll", {
			method: "POST",
			body: JSON.stringify(data),
		});
	}

	// Codex (ChatGPT) OAuth
	async codexAuthorize(): Promise<CodexAuthorizeResponse> {
		return this.fetch<CodexAuthorizeResponse>("/credentials/codex/authorize", {
			method: "POST",
		});
	}

	async codexExchange(
		data: CodexExchangeRequest,
	): Promise<CodexExchangeResponse> {
		return this.fetch<CodexExchangeResponse>("/credentials/codex/exchange", {
			method: "POST",
			body: JSON.stringify(data),
		});
	}

	// Services
	/**
	 * List all services in a session's sandbox.
	 * @param sessionId Session ID
	 */
	async getServices(sessionId: string): Promise<ListServicesResponse> {
		return this.fetch<ListServicesResponse>(`/sessions/${sessionId}/services`);
	}

	/**
	 * Start a service in a session's sandbox.
	 * @param sessionId Session ID
	 * @param serviceId Service ID (filename in .discobot/services/)
	 */
	async startService(
		sessionId: string,
		serviceId: string,
	): Promise<StartServiceResponse> {
		return this.fetch<StartServiceResponse>(
			`/sessions/${sessionId}/services/${serviceId}/start`,
			{ method: "POST" },
		);
	}

	/**
	 * Stop a service in a session's sandbox.
	 * @param sessionId Session ID
	 * @param serviceId Service ID (filename in .discobot/services/)
	 */
	async stopService(
		sessionId: string,
		serviceId: string,
	): Promise<StopServiceResponse> {
		return this.fetch<StopServiceResponse>(
			`/sessions/${sessionId}/services/${serviceId}/stop`,
			{ method: "POST" },
		);
	}

	/**
	 * Get the URL for streaming service output via SSE.
	 * Use with EventSource to receive real-time output.
	 * @param sessionId Session ID
	 * @param serviceId Service ID (filename in .discobot/services/)
	 */
	getServiceOutputUrl(sessionId: string, serviceId: string): string {
		return appendAuthToken(
			`${this.base}/sessions/${sessionId}/services/${serviceId}/output`,
		);
	}

	// Hooks
	/**
	 * Get hook evaluation status for a session's sandbox.
	 * @param sessionId Session ID
	 */
	async getHooksStatus(sessionId: string): Promise<HooksStatusResponse> {
		return this.fetch<HooksStatusResponse>(
			`/sessions/${sessionId}/hooks/status`,
		);
	}

	/**
	 * Get hook output log for a specific hook.
	 * @param sessionId Session ID
	 * @param hookId Hook ID
	 */
	async getHookOutput(
		sessionId: string,
		hookId: string,
	): Promise<HookOutputResponse> {
		return this.fetch<HookOutputResponse>(
			`/sessions/${sessionId}/hooks/${hookId}/output`,
		);
	}

	/**
	 * Manually rerun a specific hook.
	 * @param sessionId Session ID
	 * @param hookId Hook ID
	 */
	async rerunHook(
		sessionId: string,
		hookId: string,
	): Promise<HookRerunResponse> {
		return this.fetch<HookRerunResponse>(
			`/sessions/${sessionId}/hooks/${hookId}/rerun`,
			{ method: "POST" },
		);
	}

	// User Preferences (user-scoped, not project-scoped)

	/**
	 * Get all preferences for the current user.
	 */
	async getPreferences(): Promise<{ preferences: UserPreference[] }> {
		return this.fetchRoot<{ preferences: UserPreference[] }>("/preferences");
	}

	/**
	 * Get a single preference by key.
	 * @param key Preference key
	 */
	async getPreference(key: string): Promise<UserPreference> {
		return this.fetchRoot<UserPreference>(`/preferences/${key}`);
	}

	/**
	 * Set a single preference.
	 * @param key Preference key
	 * @param value Preference value
	 */
	async setPreference(key: string, value: string): Promise<UserPreference> {
		return this.fetchRoot<UserPreference>(`/preferences/${key}`, {
			method: "PUT",
			body: JSON.stringify({ value }),
		});
	}

	/**
	 * Set multiple preferences at once.
	 * @param preferences Map of key-value pairs
	 */
	async setPreferences(
		preferences: Record<string, string>,
	): Promise<{ preferences: UserPreference[] }> {
		return this.fetchRoot<{ preferences: UserPreference[] }>("/preferences", {
			method: "PUT",
			body: JSON.stringify({ preferences }),
		});
	}

	/**
	 * Delete a preference by key.
	 * @param key Preference key
	 */
	async deletePreference(key: string): Promise<void> {
		await this.fetchRoot(`/preferences/${key}`, { method: "DELETE" });
	}
}

export const api = new ApiClient();
