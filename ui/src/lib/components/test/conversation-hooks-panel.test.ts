import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import test from "node:test";

const CONVERSATION_HOOKS_PANEL_COMPONENT = path.resolve(
	import.meta.dirname,
	"../app/ConversationHooksPanel.svelte",
);

function readConversationHooksPanelSource() {
	return readFileSync(CONVERSATION_HOOKS_PANEL_COMPONENT, "utf-8");
}

test("conversation hooks panel shows a tail preview and full download for oversized hook logs", () => {
	const source = readConversationHooksPanelSource();

	assert.match(source, /selectedHookOutputData\?\.tooLarge/);
	assert.match(
		source,
		/Showing the last \{formatBytes\([\s\S]*selectedHookOutputData\.displayedBytes,[\s\S]*\)\} of \{formatBytes\(selectedHookOutputData\.sizeBytes\)\}\./,
	);
	assert.match(source, /api\.downloadHookOutput\(/);
	assert.match(source, /filename: `\$\{sessionView\.selectedHookId\}\.log`/);
	assert.match(source, /Download full log/);
});
