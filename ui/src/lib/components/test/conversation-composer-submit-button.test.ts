import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import test from "node:test";

const SUBMIT_BUTTON_COMPONENT = path.resolve(
	import.meta.dirname,
	"../app/parts/ConversationComposerSubmitButton.svelte",
);

const CONVERSATION_COMPOSER_COMPONENT = path.resolve(
	import.meta.dirname,
	"../app/ConversationComposer.svelte",
);

function readSubmitButtonSource() {
	return readFileSync(SUBMIT_BUTTON_COMPONENT, "utf-8");
}

function readConversationComposerSource() {
	return readFileSync(CONVERSATION_COMPOSER_COMPONENT, "utf-8");
}

test("composer submit button only shows the plus icon for pending empty sessions", () => {
	const source = readSubmitButtonSource();

	assert.match(source, /isPending: boolean;/);
	assert.match(
		source,
		/let\s+\{[\s\S]*status,[\s\S]*inputEmpty,[\s\S]*isPending,[\s\S]*disabled = false,[\s\S]*onPress,[\s\S]*\}: Props = \$props\(\);/,
	);
	assert.match(source, /hovered && isPending && inputEmpty && !isGenerating/);
	assert.doesNotMatch(source, /hovered && inputEmpty && !isGenerating/);
});

test("conversation composer passes pending session state to the submit button", () => {
	const source = readConversationComposerSource();

	assert.match(
		source,
		/<ConversationComposerSubmitButton[\s\S]*isPending=\{session\.isPending\}/,
	);
});
