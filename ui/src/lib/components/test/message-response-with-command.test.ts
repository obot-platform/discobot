import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import test from "node:test";

const MESSAGE_RESPONSE_WITH_COMMAND_COMPONENT = path.resolve(
	import.meta.dirname,
	"../app/parts/MessageResponseWithCommand.svelte",
);
const CONVERSATION_PANE_COMPONENT = path.resolve(
	import.meta.dirname,
	"../app/ConversationPane.svelte",
);

function readComponentSource(filePath: string) {
	return readFileSync(filePath, "utf-8");
}

test("message response with command component renders command and generated text controls", () => {
	const source = readComponentSource(MESSAGE_RESPONSE_WITH_COMMAND_COMPONENT);

	assert.match(
		source,
		/import ChevronRightIcon from "@lucide\/svelte\/icons\/chevron-right"/,
	);
	assert.match(source, /getUserMessageOriginalCommandDisplay/);
	assert.match(source, /getUserMessageOriginalText/);
	assert.match(source, /Command: \{originalCommand\.command\}/);
	assert.match(source, /Generated text/);
	assert.match(source, /Show generated text/);
});

test("conversation pane delegates user text rendering to MessageResponseWithCommand", () => {
	const source = readComponentSource(CONVERSATION_PANE_COMPONENT);

	assert.match(
		source,
		/import MessageResponseWithCommand from "\$lib\/components\/app\/parts\/MessageResponseWithCommand\.svelte"/,
	);
	assert.match(source, /<MessageResponseWithCommand/);
	assert.doesNotMatch(source, /Command: \{originalCommand\.command\}/);
});

test("conversation pane reads the session error directly from the active session", () => {
	const source = readComponentSource(CONVERSATION_PANE_COMPONENT);

	assert.match(
		source,
		/const sessionError = \$derived\.by\(\s*\(\) => sessionErrorOverride \?\? session\?\.current\?\.errorMessage \?\? null,/,
	);
	assert.doesNotMatch(source, /session\?\.current\?\.status === "error"/);
});

test("conversation pane renders expandable top-level error banners", () => {
	const source = readComponentSource(CONVERSATION_PANE_COMPONENT);

	assert.match(
		source,
		/type ConversationPaneErrorBannerKey = "session" \| "thread";/,
	);
	assert.match(
		source,
		/function shouldCollapseErrorBanner\(errorText: string\): boolean/,
	);
	assert.match(source, /line-clamp-3/);
	assert.match(source, /Show full error/);
	assert.match(source, /Show less/);
	assert.match(
		source,
		/\{@render renderErrorBanner\("session", sessionError\)\}/,
	);
	assert.match(
		source,
		/\{@render renderErrorBanner\("thread", threadError\)\}/,
	);
});
