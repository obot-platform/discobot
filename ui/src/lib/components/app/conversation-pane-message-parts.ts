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

export type AssistantMessagePartGroups = {
	collapsedParts: AssistantConversationPaneRenderablePart[];
	visibleParts: AssistantConversationPaneRenderablePart[];
	collapsedStepCount: number;
	hasCollapsedSteps: boolean;
};

type AssistantMessagePartGroupOptions = {
	isMessageComplete?: boolean;
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
