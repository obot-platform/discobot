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
	EnvSetWithVars,
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
			mode: session.mode,
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

export function getPlanEntries(messages: ChatMessage[]): PlanEntry[] {
	const latestTodoWrite = [...messages]
		.reverse()
		.flatMap((message) => getDynamicToolParts(message))
		.find(
			(part) =>
				part.toolName === "TodoWrite" && part.state === "output-available",
		);

	if (
		!latestTodoWrite ||
		!latestTodoWrite.input ||
		typeof latestTodoWrite.input !== "object"
	) {
		return [];
	}

	const todos = (latestTodoWrite.input as { todos?: unknown }).todos;
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
		http: service.http,
		https: service.https,
		urlPath: service.urlPath,
		status: service.status,
		passive: service.passive,
		exitCode: service.exitCode,
	};
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
			type:
				hook.type === "session"
					? "user_prompt_submit"
					: hook.type === "file"
						? "post_tool_use"
						: "pre_tool_use",
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

export function getActiveEnvSets(
	envSets: EnvSetWithVars[],
	activeIds: string[],
): EnvSetWithVars[] {
	const byId = new Map(envSets.map((envSet) => [envSet.id, envSet]));
	return activeIds
		.map((envSetId) => byId.get(envSetId))
		.filter((envSet): envSet is EnvSetWithVars => !!envSet);
}
