import type { ChatMessage } from "$lib/api-types";

export type ConversationPaneMessagePart = ChatMessage["parts"][number];
export type ConversationPaneRenderablePart = Extract<
	ConversationPaneMessagePart,
	{ type: "text" | "reasoning" | "dynamic-tool" }
>;

export type AssistantMessagePartGroups = {
	collapsedParts: ConversationPaneRenderablePart[];
	visibleParts: ConversationPaneRenderablePart[];
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

function isRenderablePart(
	part: ConversationPaneMessagePart,
): part is ConversationPaneRenderablePart {
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

export function getConversationPaneRenderableParts(
	message: ChatMessage,
): ConversationPaneRenderablePart[] {
	return message.parts.filter(isRenderablePart);
}

export function getAssistantMessagePartGroups(
	message: ChatMessage,
	options: AssistantMessagePartGroupOptions = {},
): AssistantMessagePartGroups {
	const renderableParts = getConversationPaneRenderableParts(message);
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
