import type { ChatMessage } from "$lib/api-types";

export type ConversationTurn = {
	id: string;
	userMessages: ChatMessage[];
	assistantMessage: ChatMessage | null;
};

export type ReservedTurnMinHeightArgs = {
	currentTurnHeight: number;
	contentTopPadding: number;
	turnTopPadding: number;
	viewportClientHeight: number;
	viewportPaddingBottom: number;
	viewportPaddingTop: number;
};

export function groupMessagesIntoTurns(
	messages: ChatMessage[],
): ConversationTurn[] {
	const turns: ConversationTurn[] = [];
	let currentTurn: ConversationTurn | null = null;

	for (const message of messages) {
		if (message.role === "user") {
			if (!currentTurn || currentTurn.assistantMessage) {
				currentTurn = {
					id: message.id,
					userMessages: [message],
					assistantMessage: null,
				};
				turns.push(currentTurn);
				continue;
			}

			currentTurn.userMessages = [...currentTurn.userMessages, message];
			continue;
		}

		if (!currentTurn || currentTurn.assistantMessage) {
			currentTurn = {
				id: message.id,
				userMessages: [],
				assistantMessage: message,
			};
			turns.push(currentTurn);
			continue;
		}

		currentTurn.assistantMessage = message;
	}

	return turns;
}

export function getReservedTurnMinHeight({
	currentTurnHeight,
	contentTopPadding,
	turnTopPadding,
	viewportClientHeight,
	viewportPaddingBottom,
	viewportPaddingTop,
}: ReservedTurnMinHeightArgs): number {
	const viewportInnerHeight = Math.max(
		0,
		viewportClientHeight - viewportPaddingTop - viewportPaddingBottom,
	);
	const availableTurnHeight = Math.max(
		0,
		viewportInnerHeight - Math.max(0, contentTopPadding),
	);
	const compensatedAvailableTurnHeight =
		availableTurnHeight + Math.max(0, turnTopPadding);

	return Math.max(
		Math.ceil(currentTurnHeight),
		Math.floor(compensatedAvailableTurnHeight),
	);
}
