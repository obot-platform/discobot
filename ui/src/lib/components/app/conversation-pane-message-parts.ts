import type { ChatMessage } from "$lib/api-types";

export type ConversationPaneMessagePart = ChatMessage["parts"][number];
export type AssistantConversationPaneRenderablePart = Extract<
	ConversationPaneMessagePart,
	{ type: "text" | "reasoning" | "dynamic-tool" }
>;
export type UserConversationPaneRenderablePart = Extract<
	ConversationPaneMessagePart,
	{ type: "text" | "file" }
>;

export type UserOriginalCommandDisplay = {
	command: string;
	args: string | null;
	rawText: string;
};

export type AssistantMessagePartGroups = {
	collapsedParts: AssistantConversationPaneRenderablePart[];
	visibleParts: AssistantConversationPaneRenderablePart[];
	collapsedStepCount: number;
	hasCollapsedSteps: boolean;
};

type AssistantMessagePartGroupOptions = {
	isMessageComplete?: boolean;
};

export type HookFailureMessageMetadata = {
	kind: "hook-failure";
	hookName: string;
	exitCode: number;
	pattern?: string;
	hookPath?: string;
	files?: string[];
	extraFileCount?: number;
	output?: string;
	outputPath?: string;
	outputTruncated?: boolean;
};

export function isConversationPaneMessageStreaming(
	message: ChatMessage,
): boolean {
	return (message as { status?: string } | undefined)?.status === "streaming";
}

function isAssistantRenderablePart(
	part: ConversationPaneMessagePart,
): part is AssistantConversationPaneRenderablePart {
	switch (part.type) {
		case "text":
		case "reasoning":
			return part.text.length > 0;
		case "dynamic-tool":
			return true;
		default:
			return false;
	}
}

function isUserRenderablePart(
	part: ConversationPaneMessagePart,
): part is UserConversationPaneRenderablePart {
	switch (part.type) {
		case "text":
			return part.text.length > 0;
		case "file":
			return true;
		default:
			return false;
	}
}

export function getUserMessageRenderableParts(
	message: ChatMessage,
): UserConversationPaneRenderablePart[] {
	return message.parts.filter(isUserRenderablePart);
}

export function getUserMessageOriginalText(
	message: ChatMessage,
): string | null {
	const originalText =
		message.role === "user" ? message.metadata?.originalText : undefined;
	return originalText ? originalText : null;
}

export function getUserMessageOriginalCommandDisplay(
	message: ChatMessage,
): UserOriginalCommandDisplay | null {
	const originalText = getUserMessageOriginalText(message);
	if (!originalText) {
		return null;
	}

	const trimmed = originalText.trim();
	if (!trimmed.startsWith("/")) {
		return null;
	}

	const withoutSlash = trimmed.slice(1).trimStart();
	if (withoutSlash.length === 0) {
		return null;
	}

	const firstWhitespaceIndex = withoutSlash.search(/\s/);
	if (firstWhitespaceIndex === -1) {
		return {
			command: withoutSlash,
			args: null,
			rawText: originalText,
		};
	}

	const command = withoutSlash.slice(0, firstWhitespaceIndex);
	const args = withoutSlash.slice(firstWhitespaceIndex).trim();
	return {
		command,
		args: args.length > 0 ? args : null,
		rawText: originalText,
	};
}

function normalizeHookPathFromMetadata(
	path: string | undefined,
): string | undefined {
	if (!path) {
		return undefined;
	}
	if (!path.startsWith("/")) {
		return path;
	}

	for (const marker of ["/.discobot/hooks/", "/.claude/hooks/"]) {
		const markerIndex = path.indexOf(marker);
		if (markerIndex !== -1) {
			return path.slice(markerIndex + 1);
		}
	}

	return path;
}

export function getHookPathDisplayLabel(
	path: string | undefined,
): string | undefined {
	if (!path) {
		return undefined;
	}

	for (const marker of [".discobot/hooks/", "/.discobot/hooks/"]) {
		if (path.startsWith(marker)) {
			return path.slice(marker.length);
		}
	}

	return path;
}

export function getHookFailureMessageMetadata(
	message: ChatMessage,
): HookFailureMessageMetadata | null {
	if (message.role !== "user") {
		return null;
	}

	const metadata = (message as { metadata?: unknown }).metadata;
	if (!metadata || typeof metadata !== "object") {
		return null;
	}

	const discobot = (metadata as { discobot?: unknown }).discobot;
	if (!discobot || typeof discobot !== "object") {
		return null;
	}

	const candidate = discobot as Record<string, unknown>;
	if (
		candidate.kind !== "hook-failure" ||
		typeof candidate.hookName !== "string" ||
		typeof candidate.exitCode !== "number"
	) {
		return null;
	}

	const files = Array.isArray(candidate.files)
		? candidate.files.filter((file): file is string => typeof file === "string")
		: undefined;

	return {
		kind: "hook-failure",
		hookName: candidate.hookName,
		exitCode: candidate.exitCode,
		pattern:
			typeof candidate.pattern === "string" ? candidate.pattern : undefined,
		hookPath: normalizeHookPathFromMetadata(
			typeof candidate.hookPath === "string" ? candidate.hookPath : undefined,
		),
		files,
		extraFileCount:
			typeof candidate.extraFileCount === "number"
				? candidate.extraFileCount
				: undefined,
		output: typeof candidate.output === "string" ? candidate.output : undefined,
		outputPath:
			typeof candidate.outputPath === "string"
				? candidate.outputPath
				: undefined,
		outputTruncated:
			typeof candidate.outputTruncated === "boolean"
				? candidate.outputTruncated
				: undefined,
	};
}

export function isHookFailureMessage(message: ChatMessage): boolean {
	return getHookFailureMessageMetadata(message) !== null;
}

export function getAssistantMessagePartGroups(
	message: ChatMessage,
	options: AssistantMessagePartGroupOptions = {},
): AssistantMessagePartGroups {
	const renderableParts = message.parts.filter(isAssistantRenderablePart);
	const isMessageComplete =
		options.isMessageComplete ?? !isConversationPaneMessageStreaming(message);

	if (message.role !== "assistant" || !isMessageComplete) {
		return {
			collapsedParts: [],
			visibleParts: renderableParts,
			collapsedStepCount: 0,
			hasCollapsedSteps: false,
		};
	}

	let trailingTextStart = renderableParts.length;
	while (
		trailingTextStart > 0 &&
		renderableParts[trailingTextStart - 1]?.type === "text"
	) {
		trailingTextStart -= 1;
	}

	const collapsedParts = renderableParts.slice(0, trailingTextStart);
	const visibleParts = renderableParts.slice(trailingTextStart);

	if (collapsedParts.length === 0 || visibleParts.length === 0) {
		return {
			collapsedParts: [],
			visibleParts: renderableParts,
			collapsedStepCount: 0,
			hasCollapsedSteps: false,
		};
	}

	return {
		collapsedParts,
		visibleParts,
		collapsedStepCount: collapsedParts.length,
		hasCollapsedSteps: true,
	};
}
