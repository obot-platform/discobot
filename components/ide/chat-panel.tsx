import { useChat } from "@ai-sdk/react";
import { DefaultChatTransport, type UIMessage } from "ai";
import { AlertCircle } from "lucide-react";
import * as React from "react";
import type { PromptInputMessage } from "@/components/ai-elements/prompt-input";
import { ChatConversation } from "@/components/ide/chat-conversation";
import {
	ChatHookStatus,
	HookStatusButton,
	HookStatusPanel,
} from "@/components/ide/chat-hook-status";
import { ChatNewContent } from "@/components/ide/chat-new-content";
import {
	ChatPlanQueue,
	QueueButton,
	QueuePanel,
} from "@/components/ide/chat-plan-queue";
import { EnvSetDialog } from "@/components/ide/dialogs/env-set-dialog";
import { EnvSetSelector } from "@/components/ide/env-set-selector";
import { ModeSelector } from "@/components/ide/mode-selector";
import { ModelSelector } from "@/components/ide/model-selector";
import { PromptInputWithHistory } from "@/components/ide/prompt-input-with-history";
import { api } from "@/lib/api-client";
import { appendAuthToken, getApiBase } from "@/lib/api-config";
import {
	CommitStatus,
	SessionStatus as SessionStatusConstants,
} from "@/lib/api-constants";
import { useMainContentContext } from "@/lib/contexts/main-content-context";
import { useSessionViewContext } from "@/lib/contexts/session-view-context";
import { useEnvSets } from "@/lib/hooks/use-env-sets";
import { useHooksStatus } from "@/lib/hooks/use-hooks-status";
import { useAgentModels, useSessionModels } from "@/lib/hooks/use-models";
import { PREFERENCE_KEYS, usePreferences } from "@/lib/hooks/use-preferences";
import { useRetryCountdown } from "@/lib/hooks/use-retry-countdown";
import { useSession } from "@/lib/hooks/use-sessions";
import { useThrottle } from "@/lib/hooks/use-throttle";
import {
	getSessionHoverText,
	getSessionStatusIndicator,
} from "@/lib/session-utils";
import { cn } from "@/lib/utils";

// Plan entry structure from TodoWrite tool
interface PlanEntry {
	content: string;
	status: "pending" | "in_progress" | "completed";
	activeForm: string;
	priority?: "low" | "medium" | "high";
}

// Helper to extract the latest plan from messages
// Looks for TodoWrite tool calls with plan entries in the input
function extractLatestPlan(messages: UIMessage[]): PlanEntry[] | null {
	// Iterate backwards through messages to find the most recent plan
	for (let i = messages.length - 1; i >= 0; i--) {
		const message = messages[i];
		if (message.role !== "assistant") continue;

		// Look through parts backwards to find latest TodoWrite tool call
		for (let j = message.parts.length - 1; j >= 0; j--) {
			const part = message.parts[j];
			if (
				part.type === "dynamic-tool" &&
				part.toolName === "TodoWrite" &&
				typeof part.input === "object" &&
				part.input !== null &&
				"todos" in part.input &&
				Array.isArray(part.input.todos)
			) {
				// Validate that todos array looks like plan entries
				const entries = part.input.todos as unknown[];
				if (
					entries.length > 0 &&
					typeof entries[0] === "object" &&
					entries[0] !== null &&
					"content" in entries[0] &&
					"status" in entries[0] &&
					"activeForm" in entries[0]
				) {
					return entries as PlanEntry[];
				}
			}
		}
	}
	return null;
}

function normalizeSelectedMode(
	mode: string | null | undefined,
): string | null | undefined {
	if (mode === undefined) {
		return undefined;
	}

	if (mode === "" || mode === "build" || mode === null) {
		return null;
	}

	return mode;
}

function getChatRequestOptions(
	modelId: string | null | undefined,
	mode: string | null | undefined,
) {
	let model: string | undefined;
	let reasoning: "enabled" | "disabled" | "" = "";

	if (modelId) {
		if (modelId.endsWith(":thinking")) {
			model = modelId.slice(0, -9);
			reasoning = "enabled";
		} else {
			model = modelId;
			reasoning = "disabled";
		}
	}

	return {
		...(model ? { model } : {}),
		reasoning,
		mode: normalizeSelectedMode(mode) || "",
	};
}

export function buildCreateOnlyChatRequest({
	sessionId,
	workspaceId,
	agentId,
	modelId,
	mode,
}: {
	sessionId: string;
	workspaceId: string;
	agentId: string;
	modelId: string | null | undefined;
	mode: string | null | undefined;
}) {
	return {
		sessionId,
		messages: [],
		workspaceId,
		agentId,
		...getChatRequestOptions(modelId, mode),
	};
}

export function isChatStreamActive(status: string): boolean {
	return status === "streaming" || status === "submitted";
}

export function getHookResumeDecision({
	previousLastEvaluatedAt,
	nextLastEvaluatedAt,
	chatStatus,
	pendingResume,
}: {
	previousLastEvaluatedAt: string | null;
	nextLastEvaluatedAt: string | null;
	chatStatus: string;
	pendingResume: boolean;
}) {
	const nextPendingResume =
		pendingResume ||
		(previousLastEvaluatedAt !== null &&
			nextLastEvaluatedAt !== previousLastEvaluatedAt);

	return {
		nextPendingResume,
		shouldResume: nextPendingResume && !isChatStreamActive(chatStatus),
	};
}

interface ChatPanelProps {
	/** Session ID for the chat (required) */
	sessionId: string;
	/** Initial messages for resuming an existing session. If provided, this is a resume scenario. */
	initialMessages?: UIMessage[];
	/** Initial workspace ID (from context, passed down as prop) */
	initialWorkspaceId: string | null;
	/** Callback when a new session is created - receives session ID, workspace ID, and agent ID */
	onSessionCreated?: (
		sessionId: string,
		workspaceId: string,
		agentId: string,
	) => void;
	/** Callback when chat completes (for refreshing file data) */
	onChatComplete?: () => void;
	/** Callback to register the resumeStream function for external use (e.g., after commit starts) */
	onRegisterResumeStream?: (fn: (() => Promise<void>) | null) => void;
	/** Optional className */
	className?: string;
}

export function ChatPanel({
	sessionId,
	initialMessages,
	initialWorkspaceId,
	onSessionCreated,
	onChatComplete,
	onRegisterResumeStream,
	className,
}: ChatPanelProps) {
	// Determine if this is a resume scenario based on initialMessages
	const resume = initialMessages !== undefined;

	// State for new session workspace selection
	// Agent and model selection is managed entirely within ChatNewContent via persistent storage
	const [localSelectedWorkspaceId, setLocalSelectedWorkspaceId] =
		React.useState<string | null>(null);
	const [localSelectedAgentId, setLocalSelectedAgentId] = React.useState<
		string | null
	>(null);
	const [localSelectedModelId, setLocalSelectedModelId] = React.useState<
		string | null
	>(null);
	// undefined = not yet initialized, null = Build (including "" or "build"), "plan" = Plan
	const [localSelectedMode, setLocalSelectedMode] = React.useState<
		string | null | undefined
	>(undefined);

	// Env set state
	const { envSets } = useEnvSets();
	const [envSetDialogOpen, setEnvSetDialogOpen] = React.useState(false);
	// For new sessions: holds locally selected env set IDs (applied on session creation)
	const [localActiveEnvSetIds, setLocalActiveEnvSetIds] = React.useState<
		string[]
	>([]);

	// Ref for textarea to enable focusing
	const textareaRef = React.useRef<HTMLTextAreaElement>(null);

	// Ref for abort controller to cancel ongoing chat requests
	const abortControllerRef = React.useRef<AbortController | null>(null);

	// Focus textarea when showing new session screen
	React.useEffect(() => {
		if (!resume && textareaRef.current) {
			// Small delay to ensure the element is rendered and transitions are complete
			const timer = setTimeout(() => {
				textareaRef.current?.focus();
			}, 100);
			return () => clearTimeout(timer);
		}
	}, [resume]);

	// Fetch session data to check if session exists (only for existing sessions)
	const { session, mutate: mutateSession } = useSession(
		resume ? sessionId : null,
	);

	// Fetch available models - use agent models for new chats, session models for existing chats
	const { models: agentModels } = useAgentModels(
		!resume ? localSelectedAgentId : null,
	);
	const { models: sessionModels } = useSessionModels(resume ? sessionId : null);
	const models = resume ? sessionModels : agentModels;

	// Get chat width mode from main content context
	const { chatWidthMode } = useMainContentContext();

	// Get default model preference
	const { getPreference } = usePreferences();
	const defaultModelPref = getPreference(PREFERENCE_KEYS.DEFAULT_MODEL);

	// Use refs to store the latest selection values for use in fetch
	// This ensures sendMessage always uses current values even if useChat caches the transport
	const selectionRef = React.useRef({
		workspaceId: localSelectedWorkspaceId,
		agentId: localSelectedAgentId,
		modelId: localSelectedModelId,
		mode: normalizeSelectedMode(localSelectedMode),
		envSetIds: localActiveEnvSetIds,
		resume,
	});

	// Apply default model preference for new chats
	React.useEffect(() => {
		if (!resume && localSelectedAgentId && !localSelectedModelId) {
			// For new chats, use the default model preference if available
			if (defaultModelPref) {
				setLocalSelectedModelId(defaultModelPref);
			}
		}
	}, [resume, localSelectedAgentId, localSelectedModelId, defaultModelPref]);

	// Sync model state with session's saved model when resuming
	React.useEffect(() => {
		if (resume && session?.model && !localSelectedModelId) {
			// Restore the exact reasoning mode from the session
			if (session.reasoning === "enabled") {
				setLocalSelectedModelId(`${session.model}:thinking`);
			} else {
				setLocalSelectedModelId(session.model);
			}
		}
	}, [resume, session?.model, session?.reasoning, localSelectedModelId]);

	// Sync mode state with session's saved mode when resuming (only before user/agent sets it)
	React.useEffect(() => {
		if (resume && session && localSelectedMode === undefined) {
			setLocalSelectedMode(normalizeSelectedMode(session.mode));
		}
	}, [resume, session, localSelectedMode]);

	// Keep refs in sync with state
	React.useEffect(() => {
		selectionRef.current = {
			workspaceId: localSelectedWorkspaceId,
			agentId: localSelectedAgentId,
			modelId: localSelectedModelId,
			mode: normalizeSelectedMode(localSelectedMode),
			envSetIds: localActiveEnvSetIds,
			resume,
		};
	}, [
		localSelectedWorkspaceId,
		localSelectedAgentId,
		localSelectedModelId,
		localSelectedMode,
		localActiveEnvSetIds,
		resume,
	]);

	// Create transport with custom fetch that always uses latest selection values
	const transport = React.useMemo(
		() =>
			new DefaultChatTransport({
				api: `${getApiBase()}/chat`,
				// Use custom fetch to inject latest workspace/agent IDs for new sessions
				fetch: (async (url, options) => {
					const { resume, workspaceId, agentId, modelId, mode, envSetIds } =
						selectionRef.current;
					const requestOptions = getChatRequestOptions(modelId, mode);

					// Create abort controller for this request
					const controller = new AbortController();
					abortControllerRef.current = controller;

					// Only modify body for new sessions
					if (!resume && options?.body) {
						const body = JSON.parse(options.body as string);
						body.workspaceId = workspaceId;
						body.agentId = agentId;
						Object.assign(body, requestOptions);

						const authUrl = appendAuthToken(url as string);
						const response = await fetch(authUrl, {
							...options,
							body: JSON.stringify(body),
							signal: controller.signal,
						});

						// If we got a 200 response, the server has acknowledged the session
						if (response.ok) {
							// Call the callback to notify that the session was created
							if (onSessionCreated && workspaceId && agentId) {
								onSessionCreated(sessionId, workspaceId, agentId);
							}
							// Apply selected env sets to the newly created session
							if (envSetIds.length > 0) {
								api
									.setSessionActiveEnvSets(sessionId, envSetIds)
									.catch((err) =>
										console.error(
											"Failed to apply env sets to new session:",
											err,
										),
									);
							}
						}

						return response;
					}

					// For resumed sessions, also inject reasoning and model flags
					if (resume && options?.body) {
						const body = JSON.parse(options.body as string);
						Object.assign(body, requestOptions);

						return fetch(appendAuthToken(url as string), {
							...options,
							body: JSON.stringify(body),
							signal: controller.signal,
						});
					}

					return fetch(appendAuthToken(url as string), {
						...options,
						signal: controller.signal,
					});
				}) as typeof fetch,
			}),
		[onSessionCreated, sessionId],
	);

	// Use AI SDK's useChat hook
	// Memoize the onError callback to prevent useChat from re-initializing
	const handleChatError = React.useCallback((error: Error) => {
		console.error("Chat stream error:", error);
	}, []);

	// Handle stop button click - cancel ongoing chat request
	const handleStop = React.useCallback(async () => {
		// Abort the SSE fetch request
		abortControllerRef.current?.abort();
		abortControllerRef.current = null;

		// Call backend cancel endpoint to stop sandbox execution
		try {
			await api.cancelChat(sessionId);
		} catch (err) {
			console.error("Failed to cancel chat:", err);
		}
	}, [sessionId]);

	const {
		messages,
		setMessages,
		sendMessage,
		resumeStream,
		addToolApprovalResponse,
		status: chatStatus,
		error: chatError,
	} = useChat({
		transport,
		id: sessionId,
		resume,
		onError: handleChatError,
		messages: initialMessages,
		onFinish: onChatComplete,
		onData: (dataPart) => {
			if (dataPart.type === "data-mode-change") {
				const { mode } = dataPart.data as { mode: string };
				setLocalSelectedMode(normalizeSelectedMode(mode));
			}
			if (dataPart.type === "data-user-message") {
				const payload = dataPart as {
					type: "data-user-message";
					data: {
						message: UIMessage;
						insertBeforeMessageId?: string;
					};
				};
				const userMessage = payload.data.message;
				if (!userMessage?.id) return;
				setMessages((currentMessages) => {
					if (currentMessages.some((m) => m.id === userMessage.id)) {
						return currentMessages;
					}
					if (payload.data.insertBeforeMessageId) {
						const insertBeforeIdx = currentMessages.findIndex(
							(m) => m.id === payload.data.insertBeforeMessageId,
						);
						if (insertBeforeIdx !== -1) {
							return [
								...currentMessages.slice(0, insertBeforeIdx),
								userMessage,
								...currentMessages.slice(insertBeforeIdx),
							];
						}
					}
					return [...currentMessages, userMessage];
				});
			}
		},
	});

	// Throttle message updates to reduce render frequency during rapid streaming
	// Updates at most once every 500ms to improve performance during streaming
	const throttledMessages = useThrottle(messages, 500);

	// Deduplicate messages by ID to prevent React key conflicts.
	// Can occur when initialMessages (from REST) and the SSE stream both contain
	// the same message ID — e.g. a resumed stream replays a start event for a
	// message that is already present in the initialMessages snapshot.
	const uniqueMessages = React.useMemo(() => {
		const seen = new Set<string>();
		return throttledMessages.filter((msg) => {
			if (seen.has(msg.id)) return false;
			seen.add(msg.id);
			return true;
		});
	}, [throttledMessages]);

	// Abort ongoing fetch connections when component unmounts (e.g., user switches sessions).
	// This frees browser connection slots — browsers limit HTTP/1.1 to 6 per origin,
	// and stale SSE connections can exhaust the pool, blocking new requests like GET /messages.
	React.useEffect(() => {
		return () => {
			abortControllerRef.current?.abort();
			abortControllerRef.current = null;
		};
	}, []);

	// Register resumeStream for external use (e.g., after commit starts)
	React.useEffect(() => {
		onRegisterResumeStream?.(resumeStream);
		return () => {
			onRegisterResumeStream?.(null);
		};
	}, [resumeStream, onRegisterResumeStream]);

	// Register addToolApprovalResponse into SessionViewContext
	// so message part renderers can call it
	const { registerAddToolApprovalResponse, selectedSession } =
		useSessionViewContext();

	// True once the sandbox is up and able to serve file-search requests
	const isSessionReady =
		selectedSession?.status === SessionStatusConstants.READY ||
		selectedSession?.status === SessionStatusConstants.RUNNING;
	React.useEffect(() => {
		registerAddToolApprovalResponse(addToolApprovalResponse);
		return () => {
			registerAddToolApprovalResponse(null);
		};
	}, [addToolApprovalResponse, registerAddToolApprovalResponse]);

	// Derive loading state from chat status
	const isLoading = chatStatus === "streaming" || chatStatus === "submitted";
	const hasError = chatStatus === "error";
	const canStop = chatStatus === "streaming" || chatStatus === "submitted"; // Can stop during both submitted and streaming

	const isOperationLocked =
		session?.commitStatus === CommitStatus.PENDING ||
		session?.commitStatus === CommitStatus.COMMITTING;
	const operationLabel =
		session?.commitOperation === "rebase" ? "rebase" : "commit";

	const retryCountdown = useRetryCountdown(hasError, resumeStream);

	// Extract the current plan from deduplicated messages for consistent UI state
	const currentPlan = React.useMemo(
		() => extractLatestPlan(uniqueMessages),
		[uniqueMessages],
	);

	// Poll hook status for existing sessions (only when sandbox is ready)
	const { hooksStatus } = useHooksStatus(
		resume ? sessionId : null,
		session?.status,
	);

	// Queue a reconnect when hook evaluation changes, but avoid replaying the
	// current stream while it is already live.
	const lastEvalRef = React.useRef<string | null>(null);
	const pendingHookResumeRef = React.useRef(false);
	React.useEffect(() => {
		const lastEval = hooksStatus?.lastEvaluatedAt ?? null;
		const decision = getHookResumeDecision({
			previousLastEvaluatedAt: lastEvalRef.current,
			nextLastEvaluatedAt: lastEval,
			chatStatus,
			pendingResume: pendingHookResumeRef.current,
		});

		pendingHookResumeRef.current = decision.nextPendingResume;
		lastEvalRef.current = lastEval;

		if (!decision.shouldResume) {
			return;
		}

		pendingHookResumeRef.current = false;
		resumeStream();
	}, [hooksStatus?.lastEvaluatedAt, chatStatus, resumeStream]);

	// Handle form submission - memoized to prevent PromptInput re-renders
	const handleSubmit = React.useCallback(
		async (message: PromptInputMessage, e: React.FormEvent) => {
			// Validate selections for new sessions
			e.preventDefault();
			const messageText = message.text;
			if (!messageText?.trim() || isLoading) return;

			// Validate selections for new sessions
			if (!resume && (!localSelectedWorkspaceId || !localSelectedAgentId)) {
				return;
			}

			try {
				await sendMessage({
					text: messageText,
					files: message.files || [],
				});
			} catch (err) {
				console.error("Failed to send message:", err);
			}
		},
		[
			isLoading,
			resume,
			localSelectedWorkspaceId,
			localSelectedAgentId,
			sendMessage,
		],
	);

	// Handle creating an empty session (sandbox only, no LLM message)
	const handleCreateEmptySession = React.useCallback(async () => {
		if (resume || !localSelectedWorkspaceId || !localSelectedAgentId) return;

		try {
			const response = await fetch(appendAuthToken(`${getApiBase()}/chat`), {
				method: "POST",
				headers: {
					"Content-Type": "application/json",
				},
				body: JSON.stringify(
					buildCreateOnlyChatRequest({
						sessionId,
						workspaceId: localSelectedWorkspaceId,
						agentId: localSelectedAgentId,
						modelId: localSelectedModelId,
						mode: localSelectedMode,
					}),
				),
			});
			if (!response.ok) {
				throw new Error("Failed to create empty session");
			}
			onSessionCreated?.(
				sessionId,
				localSelectedWorkspaceId,
				localSelectedAgentId,
			);
			if (localActiveEnvSetIds.length > 0) {
				api
					.setSessionActiveEnvSets(sessionId, localActiveEnvSetIds)
					.catch((err) =>
						console.error("Failed to apply env sets to new session:", err),
					);
			}
		} catch (err) {
			console.error("Failed to create empty session:", err);
			throw err;
		}
	}, [
		resume,
		sessionId,
		localSelectedWorkspaceId,
		localSelectedAgentId,
		localSelectedModelId,
		localSelectedMode,
		localActiveEnvSetIds,
		onSessionCreated,
	]);

	const handleCopy = React.useCallback((content: string) => {
		navigator.clipboard.writeText(content);
	}, []);

	return (
		<div
			className={cn(
				"relative flex flex-col h-full bg-background transition-all duration-300 ease-in-out",
				className,
			)}
		>
			{/* Error messages and status - always at top */}
			<div className="shrink-0">
				{/* Session status header - shows when not ready or running */}
				{session &&
					session.status !== SessionStatusConstants.READY &&
					session.status !== SessionStatusConstants.RUNNING && (
						<div
							className={cn(
								"flex items-center gap-2 py-3 px-4 border-b",
								session.status === SessionStatusConstants.ERROR ||
									session.status === SessionStatusConstants.REMOVING
									? "bg-destructive/10 border-destructive/20"
									: session.status === SessionStatusConstants.STOPPED
										? "bg-yellow-500/10 border-yellow-500/20"
										: session.status === SessionStatusConstants.REMOVED
											? "bg-muted/30 border-border"
											: "bg-muted/50 border-border",
							)}
						>
							{getSessionStatusIndicator(session)}
							<span
								className={cn(
									"text-sm font-medium",
									session.status === SessionStatusConstants.ERROR ||
										session.status === SessionStatusConstants.REMOVING
										? "text-destructive"
										: session.status === SessionStatusConstants.STOPPED
											? "text-yellow-600 dark:text-yellow-500"
											: "text-muted-foreground",
								)}
							>
								{getSessionHoverText(session)}
							</span>
						</div>
					)}

				{/* Chat stream error indicator */}
				{hasError && chatError && (
					<div className="flex items-center gap-2 py-3 px-4 border-b bg-destructive/10 border-destructive/20 text-destructive">
						<AlertCircle className="h-4 w-4 shrink-0" />
						<span className="text-sm font-medium">Error</span>
						<span className="text-sm">: {chatError.message}</span>
						<div className="ml-auto flex items-center gap-2 shrink-0">
							{retryCountdown !== null && (
								<span className="text-xs opacity-70">
									Retrying in {retryCountdown}s…
								</span>
							)}
							<button
								type="button"
								onClick={() => resumeStream()}
								className="text-sm px-3 py-1 rounded border border-destructive/30 hover:bg-destructive/20 transition-colors"
							>
								Retry
							</button>
						</div>
					</div>
				)}
			</div>

			{/* Content area - centered when new/empty, normal flow otherwise */}
			<div
				className={cn(
					"flex flex-col flex-1 overflow-hidden",
					uniqueMessages.length === 0 && "justify-center",
				)}
			>
				{/* Welcome UI - header and selectors for new sessions */}
				<ChatNewContent
					show={!resume && uniqueMessages.length === 0}
					initialWorkspaceId={initialWorkspaceId}
					onWorkspaceChange={setLocalSelectedWorkspaceId}
					onAgentChange={setLocalSelectedAgentId}
				/>

				{/* Conversation area */}
				<ChatConversation
					messages={uniqueMessages}
					messagesLoading={false}
					isChatActive={isLoading}
					onCopy={handleCopy}
				/>
			</div>

			{/* Input area - only show when agent and workspace are selected (or for existing sessions) */}
			{(resume || (localSelectedAgentId && localSelectedWorkspaceId)) && (
				<ChatPlanQueue plan={currentPlan}>
					<ChatHookStatus
						hooksStatus={hooksStatus}
						sessionId={resume ? sessionId : null}
					>
						<div
							className={cn(
								"shrink-0 transition-all duration-300 ease-in-out bg-background relative z-10",
								"px-4 pb-4 w-full",
								chatWidthMode === "constrained" && "max-w-3xl mx-auto",
							)}
						>
							{/* Expanded panels - show above input when expanded */}
							{currentPlan && <QueuePanel plan={currentPlan} />}
							{hooksStatus && <HookStatusPanel hooksStatus={hooksStatus} />}

							<PromptInputWithHistory
								ref={textareaRef}
								sessionId={sessionId}
								isNewSession={!resume}
								isSessionReady={isSessionReady}
								onSubmit={handleSubmit}
								onStop={canStop ? handleStop : undefined}
								onCreateSession={!resume ? handleCreateEmptySession : undefined}
								onTriggerSessionCreate={
									!resume ? handleCreateEmptySession : undefined
								}
								status={chatStatus}
								isLocked={isOperationLocked}
								placeholder={
									isOperationLocked
										? `Chat disabled during ${operationLabel}...`
										: "Type a message..."
								}
								textareaClassName={cn(
									"transition-all duration-300",
									"min-h-[60px]",
								)}
								submitDisabled={false}
								hookStatusButton={<HookStatusButton />}
								queueButton={<QueueButton />}
								modeSelector={
									<ModeSelector
										selectedMode={localSelectedMode ?? null}
										onSelectMode={setLocalSelectedMode}
										compact
									/>
								}
								modelSelector={
									models.length > 0 ? (
										<ModelSelector
											models={models}
											selectedModelId={localSelectedModelId}
											onSelectModel={setLocalSelectedModelId}
											compact
										/>
									) : undefined
								}
								envSetSelector={
									<EnvSetSelector
										activeEnvSetIds={
											resume
												? (session?.activeEnvSetIds ?? [])
												: localActiveEnvSetIds
										}
										envSets={envSets}
										onToggleEnvSet={async (id) => {
											if (resume) {
												const current = session?.activeEnvSetIds ?? [];
												const next = current.includes(id)
													? current.filter((x) => x !== id)
													: [...current, id];
												await api.setSessionActiveEnvSets(sessionId, next);
												mutateSession();
											} else {
												setLocalActiveEnvSetIds((prev) =>
													prev.includes(id)
														? prev.filter((x) => x !== id)
														: [...prev, id],
												);
											}
										}}
										onManage={() => setEnvSetDialogOpen(true)}
									/>
								}
							/>
						</div>
					</ChatHookStatus>
				</ChatPlanQueue>
			)}

			{/* Env set management dialog */}
			<EnvSetDialog
				open={envSetDialogOpen}
				onOpenChange={setEnvSetDialogOpen}
			/>

			{/* Session ID - subtle display in lower right */}
			<div className="absolute bottom-2 right-2 select-text">
				<span className="text-[10px] text-muted-foreground/50 font-mono">
					{sessionId}
				</span>
			</div>
		</div>
	);
}
