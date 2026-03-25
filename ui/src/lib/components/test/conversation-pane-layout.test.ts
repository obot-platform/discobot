import assert from "node:assert/strict";
import test from "node:test";

import type { ChatMessage } from "$lib/api-types";
import {
	getReservedTurnMinHeight,
	groupMessagesIntoTurns,
} from "../app/conversation-pane-layout";

function makeUserMessage(id: string, text: string): ChatMessage {
	return {
		id,
		role: "user",
		parts: [{ type: "text", text }],
	};
}

function makeAssistantMessage(id: string, text: string): ChatMessage {
	return {
		id,
		role: "assistant",
		parts: [{ type: "text", text }],
	};
}

test("groupMessagesIntoTurns groups adjacent user messages into one turn", () => {
	const turns = groupMessagesIntoTurns([
		makeUserMessage("user-1", "first"),
		makeUserMessage("user-2", "second"),
	]);

	assert.deepEqual(turns, [
		{
			id: "user-1",
			userMessages: [
				makeUserMessage("user-1", "first"),
				makeUserMessage("user-2", "second"),
			],
			assistantMessage: null,
		},
	]);
});

test("groupMessagesIntoTurns closes a turn with the first assistant message", () => {
	const turns = groupMessagesIntoTurns([
		makeUserMessage("user-1", "prompt"),
		makeAssistantMessage("assistant-1", "reply"),
		makeUserMessage("user-2", "follow-up"),
	]);

	assert.deepEqual(turns, [
		{
			id: "user-1",
			userMessages: [makeUserMessage("user-1", "prompt")],
			assistantMessage: makeAssistantMessage("assistant-1", "reply"),
		},
		{
			id: "user-2",
			userMessages: [makeUserMessage("user-2", "follow-up")],
			assistantMessage: null,
		},
	]);
});

test("groupMessagesIntoTurns leaves a user-only trailing turn open", () => {
	const turns = groupMessagesIntoTurns([
		makeUserMessage("user-1", "prompt"),
		makeAssistantMessage("assistant-1", "reply"),
		makeUserMessage("user-2", "queued follow-up"),
		makeUserMessage("user-3", "more context"),
	]);

	assert.deepEqual(turns.at(-1), {
		id: "user-2",
		userMessages: [
			makeUserMessage("user-2", "queued follow-up"),
			makeUserMessage("user-3", "more context"),
		],
		assistantMessage: null,
	});
});

test("getReservedTurnMinHeight fills the visible viewport when the turn is short", () => {
	assert.equal(
		getReservedTurnMinHeight({
			currentTurnHeight: 124.2,
			contentTopPadding: 12,
			turnTopPadding: 0,
			viewportClientHeight: 640,
			viewportPaddingBottom: 16,
			viewportPaddingTop: 16,
		}),
		596,
	);
});

test("getReservedTurnMinHeight compensates for turn top padding", () => {
	assert.equal(
		getReservedTurnMinHeight({
			currentTurnHeight: 124.2,
			contentTopPadding: 12,
			turnTopPadding: 64,
			viewportClientHeight: 640,
			viewportPaddingBottom: 16,
			viewportPaddingTop: 16,
		}),
		660,
	);
});

test("getReservedTurnMinHeight preserves taller turns", () => {
	assert.equal(
		getReservedTurnMinHeight({
			currentTurnHeight: 712.4,
			contentTopPadding: 24,
			turnTopPadding: 0,
			viewportClientHeight: 640,
			viewportPaddingBottom: 16,
			viewportPaddingTop: 16,
		}),
		713,
	);
});
