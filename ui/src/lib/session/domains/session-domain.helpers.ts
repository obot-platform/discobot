import { generateId } from "ai";

import type {
	ChatMessage,
	HookOutputResponse,
	HooksStatusResponse,
	Service,
	Session,
} from "$lib/api-types";
import type { DynamicToolPart } from "$lib/components/ai/types";
import type {
	HookLastResult,
	HookRunStatus,
	HooksStatus,
	PlanEntry,
	ServiceItem,
	ThreadSummary,
} from "$lib/shell-types";

type UserTextPart = Extract<ChatMessage["parts"][number], { type: "text" }>;
type UserFilePart = Extract<ChatMessage["parts"][number], { type: "file" }>;

export type UserMessageAttachment = {
	filename?: string;
	mediaType: string;
	url: string;
};

const PLAN_FILE_LINE_PATTERN = /Plan file:\s+([^\n]+)/;
const PLAN_FILE_GUIDANCE_PATTERN =
	/Write your complete plan to\s+(.+?)\s+before calling ExitPlanMode\./;
const APPROVED_PLAN_MARKER = "Approved plan:\n\n";
const CURRENT_PLAN_MARKER = "Current plan:\n\n";
const PLAN_FEEDBACK_PREFIX = "Plan feedback from user: ";

export type LatestPlanPhase =
	| "entered"
	| "awaiting_approval"
	| "approved"
	| "auto_exited"
	| "feedback"
	| "rejected"
	| "error";

export type LatestPlanState = {
	toolName: "EnterPlanMode" | "ExitPlanMode";
	toolCallId: string;
	approvalId: string | null;
	partState: DynamicToolPart["state"];
	phase: LatestPlanPhase;
	planFilePath: string | null;
	planMarkdown: string | null;
	feedback: string | null;
	errorText: string | null;
};

export function buildImplicitThread(session: Session | null): ThreadSummary[] {
	if (!session) {
		return [];
	}

	return [
		{
			id: session.id,
			name: session.displayName || session.name,
			model: session.model,
			reasoning: session.reasoning,
			mode: session.mode ?? "build",
			promptQueue: [],
		},
	];
}

export function getNextSelectedThreadId(
	threads: ThreadSummary[],
	removedThreadId: string,
	currentSelectedId: string | null,
): string | null {
	const removedIndex = threads.findIndex(
		(thread) => thread.id === removedThreadId,
	);
	if (removedIndex === -1) {
		return currentSelectedId;
	}

	const remainingThreads = threads.filter(
		(thread) => thread.id !== removedThreadId,
	);
	if (currentSelectedId !== removedThreadId) {
		return currentSelectedId;
	}

	return (
		remainingThreads[removedIndex]?.id ??
		remainingThreads[removedIndex - 1]?.id ??
		null
	);
}

export function createUserMessage(
	text: string,
	options: {
		provisional?: boolean;
	} = {},
): ChatMessage {
	return createUserMessageFromParts(buildUserMessageParts(text), options);
}

export function createUserMessageFromParts(
	parts: ChatMessage["parts"],
	options: {
		provisional?: boolean;
	} = {},
): ChatMessage {
	return {
		id: generateId(),
		role: "user",
		parts,
		...(options.provisional ? { provisional: true } : {}),
	};
}

export function buildUserMessageParts(
	text: string,
	attachments: UserMessageAttachment[] = [],
): ChatMessage["parts"] {
	const parts: Array<UserTextPart | UserFilePart> = [];
	if (text.trim().length > 0) {
		parts.push({ type: "text", text });
	}
	parts.push(
		...attachments.map(({ filename, mediaType, url }) => ({
			type: "file" as const,
			filename,
			mediaType,
			url,
		})),
	);
	return parts;
}

export function hasUserMessageContent(parts: ChatMessage["parts"]): boolean {
	return parts.some((part) => {
		if (part.type === "text") {
			return part.text.trim().length > 0;
		}
		return part.type === "file";
	});
}

function bytesToBase64(bytes: Uint8Array): string {
	const bufferCtor = (
		globalThis as {
			Buffer?: {
				from: (input: Uint8Array) => {
					toString: (encoding: "base64") => string;
				};
			};
		}
	).Buffer;
	if (bufferCtor) {
		return bufferCtor.from(bytes).toString("base64");
	}

	let binary = "";
	const chunkSize = 0x8000;
	for (let index = 0; index < bytes.length; index += chunkSize) {
		binary += String.fromCharCode(...bytes.subarray(index, index + chunkSize));
	}
	return btoa(binary);
}

export async function createUserMessageAttachment(
	file: File,
): Promise<UserMessageAttachment> {
	const mediaType = file.type || "application/octet-stream";
	const base64 = bytesToBase64(new Uint8Array(await file.arrayBuffer()));
	return {
		filename: file.name,
		mediaType,
		url: `data:${mediaType};base64,${base64}`,
	};
}

export function getMessageText(message: ChatMessage): string {
	return message.parts
		.filter(
			(part): part is Extract<ChatMessage["parts"][number], { type: "text" }> =>
				part.type === "text",
		)
		.map((part) => part.text)
		.join("\n")
		.trim();
}

export function getReasoningText(message: ChatMessage): string {
	return message.parts
		.filter(
			(
				part,
			): part is Extract<ChatMessage["parts"][number], { type: "reasoning" }> =>
				part.type === "reasoning",
		)
		.map((part) => part.text)
		.join("\n")
		.trim();
}

export function getDynamicToolParts(message: ChatMessage): DynamicToolPart[] {
	return message.parts.filter(
		(part) => part.type === "dynamic-tool",
	) as unknown as DynamicToolPart[];
}

export function addToolApprovalResponse(
	messages: ChatMessage[],
	options: { id: string; approved: boolean; reason?: string },
): boolean {
	for (
		let messageIndex = messages.length - 1;
		messageIndex >= 0;
		messageIndex -= 1
	) {
		const message = messages[messageIndex];
		if (message.role !== "assistant") {
			continue;
		}

		for (const part of getDynamicToolParts(message)) {
			if (
				part.state === "approval-requested" &&
				part.approval?.id === options.id
			) {
				part.state = "approval-responded";
				part.approval = {
					id: options.id,
					approved: options.approved,
					...(options.reason ? { reason: options.reason } : {}),
				};
				return true;
			}
		}
	}

	return false;
}

export function getPendingQuestionApprovalId(
	messages: ChatMessage[],
): string | null {
	for (
		let messageIndex = messages.length - 1;
		messageIndex >= 0;
		messageIndex -= 1
	) {
		const message = messages[messageIndex];
		if (message.role !== "assistant") {
			continue;
		}

		for (const part of [...getDynamicToolParts(message)].reverse()) {
			if (part.state !== "approval-requested") {
				continue;
			}
			if (typeof part.approval?.id === "string") {
				return part.approval.id;
			}
			return part.toolCallId || null;
		}
	}

	return null;
}

function getToolOutputText(value: unknown): string | null {
	if (typeof value === "string") {
		return value;
	}

	return null;
}

function parsePlanFilePath(text: string | null | undefined): string | null {
	if (!text) {
		return null;
	}

	const lineMatch = text.match(PLAN_FILE_LINE_PATTERN);
	if (lineMatch?.[1]) {
		return lineMatch[1].trim();
	}

	const guidanceMatch = text.match(PLAN_FILE_GUIDANCE_PATTERN);
	if (guidanceMatch?.[1]) {
		return guidanceMatch[1].trim();
	}

	return null;
}

function parsePlanMarkdown(text: string | null | undefined): string | null {
	if (!text) {
		return null;
	}

	for (const marker of [APPROVED_PLAN_MARKER, CURRENT_PLAN_MARKER]) {
		const markerIndex = text.indexOf(marker);
		if (markerIndex !== -1) {
			return text.slice(markerIndex + marker.length).trim() || null;
		}
	}

	return null;
}

function parsePlanFeedback(text: string | null | undefined): string | null {
	if (!text?.startsWith(PLAN_FEEDBACK_PREFIX)) {
		return null;
	}

	const [feedback] = text.slice(PLAN_FEEDBACK_PREFIX.length).split("\n\n", 1);
	return feedback?.trim() || null;
}

function getPlanApprovalId(part: DynamicToolPart): string | null {
	const approval = part.approval;
	if (approval && typeof approval === "object" && "id" in approval) {
		return typeof approval.id === "string" ? approval.id : null;
	}
	return part.toolCallId || null;
}

export function getPlanToolState(
	part: DynamicToolPart,
): LatestPlanState | null {
	if (part.toolName !== "EnterPlanMode" && part.toolName !== "ExitPlanMode") {
		return null;
	}

	const outputText = getToolOutputText(part.output);
	const errorText = part.errorText ?? null;
	const planFilePath =
		parsePlanFilePath(outputText) ?? parsePlanFilePath(errorText);
	const planMarkdown = parsePlanMarkdown(outputText);
	const approvalId = getPlanApprovalId(part);

	if (part.toolName === "EnterPlanMode") {
		return {
			toolName: "EnterPlanMode",
			toolCallId: part.toolCallId,
			approvalId,
			partState: part.state,
			phase: part.state === "output-error" ? "error" : "entered",
			planFilePath,
			planMarkdown: null,
			feedback: null,
			errorText,
		};
	}

	let phase: LatestPlanPhase = "awaiting_approval";
	if (part.state === "output-error") {
		phase = "error";
	} else if (outputText?.startsWith("Plan approved.")) {
		phase = "approved";
	} else if (outputText?.startsWith("Plan mode exited.")) {
		phase = "auto_exited";
	} else if (outputText?.startsWith(PLAN_FEEDBACK_PREFIX)) {
		phase = "feedback";
	} else if (outputText?.startsWith("Plan rejected.")) {
		phase = "rejected";
	} else if (part.state === "approval-requested") {
		phase = "awaiting_approval";
	}

	return {
		toolName: "ExitPlanMode",
		toolCallId: part.toolCallId,
		approvalId,
		partState: part.state,
		phase,
		planFilePath,
		planMarkdown,
		feedback: parsePlanFeedback(outputText),
		errorText,
	};
}

export function getLatestPlanState(
	messages: ChatMessage[],
): LatestPlanState | null {
	for (const message of [...messages].reverse()) {
		for (const part of [...getDynamicToolParts(message)].reverse()) {
			const planState = getPlanToolState(part);
			if (planState) {
				return planState;
			}
		}
	}

	return null;
}

export function getPlanEntries(messages: ChatMessage[]): PlanEntry[] {
	for (
		let messageIndex = messages.length - 1;
		messageIndex >= 0;
		messageIndex -= 1
	) {
		const message = messages[messageIndex];

		for (
			let partIndex = message.parts.length - 1;
			partIndex >= 0;
			partIndex -= 1
		) {
			const part = message.parts[partIndex];
			if (
				part.type !== "dynamic-tool" ||
				part.toolName !== "TodoWrite" ||
				part.state !== "output-available" ||
				!part.input ||
				typeof part.input !== "object"
			) {
				continue;
			}

			const todos = (part.input as { todos?: unknown }).todos;
			if (!Array.isArray(todos)) {
				return [];
			}

			return todos.flatMap((todo) => {
				if (!todo || typeof todo !== "object") {
					return [];
				}

				const candidate = todo as {
					content?: unknown;
					activeForm?: unknown;
					status?: unknown;
				};
				if (
					typeof candidate.content !== "string" ||
					typeof candidate.activeForm !== "string" ||
					(candidate.status !== "pending" &&
						candidate.status !== "in_progress" &&
						candidate.status !== "completed")
				) {
					return [];
				}

				return [
					{
						content: candidate.content,
						activeForm: candidate.activeForm,
						status: candidate.status,
					},
				];
			});
		}
	}

	return [];
}

export function toServiceItem(service: Service): ServiceItem {
	const target =
		typeof service.https === "number"
			? `https://localhost:${service.https}${service.urlPath ?? ""}`
			: typeof service.http === "number"
				? `http://localhost:${service.http}${service.urlPath ?? ""}`
				: service.path;

	return {
		id: service.id,
		label: service.name,
		target,
		description: service.description,
		order: service.order,
		http: service.http,
		https: service.https,
		urlPath: service.urlPath,
		status: service.status,
		passive: service.passive,
		exitCode: service.exitCode,
	};
}

export function sortServiceItems(
	services: readonly ServiceItem[],
): ServiceItem[] {
	return [...services].sort((left, right) => {
		const leftOrder = left.order;
		const rightOrder = right.order;

		if (leftOrder !== undefined && rightOrder === undefined) {
			return -1;
		}
		if (leftOrder === undefined && rightOrder !== undefined) {
			return 1;
		}
		if (
			leftOrder !== undefined &&
			rightOrder !== undefined &&
			leftOrder !== rightOrder
		) {
			return leftOrder - rightOrder;
		}

		const labelCompare = left.label.localeCompare(right.label);
		if (labelCompare !== 0) {
			return labelCompare;
		}

		return left.id.localeCompare(right.id);
	});
}

export function toHooksStatus(
	response: HooksStatusResponse | null | undefined,
): HooksStatus {
	if (!response) {
		return { hooks: [], pendingHookIds: [] };
	}

	return {
		hooks: Object.values(response.hooks).map((hook) => ({
			hookId: hook.hookId,
			hookName: hook.hookName,
			type: hook.type,
			lastResult: hook.lastResult,
			lastRunAt: hook.lastRunAt,
			lastExitCode: hook.lastExitCode,
			runCount: hook.runCount,
			failCount: hook.failCount,
			command: undefined,
		})),
		pendingHookIds: response.pendingHooks,
	};
}

export function getHookDisplayState(
	hook: HookRunStatus,
	pendingHookIds: ReadonlySet<string>,
): HookLastResult {
	if (hook.lastResult === "running" || hook.lastResult === "failure") {
		return hook.lastResult;
	}

	if (pendingHookIds.has(hook.hookId)) {
		return "pending";
	}

	return hook.lastResult;
}

export function mergeHookOutput(
	outputById: Record<string, string>,
	hookId: string,
	response: HookOutputResponse,
): Record<string, string> {
	return {
		...outputById,
		[hookId]: response.output,
	};
}
