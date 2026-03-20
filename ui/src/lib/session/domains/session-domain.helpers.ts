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

export function buildImplicitThread(session: Session | null): ThreadSummary[] {
	if (!session) {
		return [];
	}

	return [{
		id: session.id,
		name: session.displayName || session.name,
		model: session.model,
		reasoning: session.reasoning,
		mode: session.mode,
	}];
}

export function getNextSelectedThreadId(
	threads: ThreadSummary[],
	removedThreadId: string,
	currentSelectedId: string | null,
): string | null {
	const removedIndex = threads.findIndex((thread) => thread.id === removedThreadId);
	if (removedIndex === -1) {
		return currentSelectedId;
	}

	const remainingThreads = threads.filter((thread) => thread.id !== removedThreadId);
	if (currentSelectedId !== removedThreadId) {
		return currentSelectedId;
	}

	return remainingThreads[removedIndex]?.id ?? remainingThreads[removedIndex - 1]?.id ?? null;
}

export function createUserMessage(
	text: string,
	options: {
		provisional?: boolean;
	} = {},
): ChatMessage {
	return {
		id: generateId(),
		role: "user",
		parts: [{ type: "text", text }],
		...(options.provisional ? { provisional: true } : {}),
	};
}

export function getMessageText(message: ChatMessage): string {
	return message.parts
		.filter((part): part is Extract<ChatMessage["parts"][number], { type: "text" }> =>
			part.type === "text",
		)
		.map((part) => part.text)
		.join("\n")
		.trim();
}

export function getReasoningText(message: ChatMessage): string {
	return message.parts
		.filter((part): part is Extract<ChatMessage["parts"][number], { type: "reasoning" }> =>
			part.type === "reasoning",
		)
		.map((part) => part.text)
		.join("\n")
		.trim();
}

export function getDynamicToolParts(message: ChatMessage): DynamicToolPart[] {
	return message.parts.filter((part) => part.type === "dynamic-tool") as unknown as DynamicToolPart[];
}

export function getPlanEntries(messages: ChatMessage[]): PlanEntry[] {
	const latestTodoWrite = [...messages]
		.reverse()
		.flatMap((message) => getDynamicToolParts(message))
		.find((part) => part.toolName === "TodoWrite" && part.state === "output-available");

	if (!latestTodoWrite || !latestTodoWrite.output || typeof latestTodoWrite.output !== "object") {
		return [];
	}

	const todos = (latestTodoWrite.output as { todos?: unknown }).todos;
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
	};
}

export function toHooksStatus(response: HooksStatusResponse | null | undefined): HooksStatus {
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
