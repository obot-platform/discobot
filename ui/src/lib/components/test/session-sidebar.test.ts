import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import test from "node:test";

const SESSION_SIDEBAR_COMPONENT = path.resolve(
	import.meta.dirname,
	"../app/SessionSidebar.svelte",
);

function readSessionSidebarSource() {
	return readFileSync(SESSION_SIDEBAR_COMPONENT, "utf-8");
}

test("session sidebar recent threads render lastMessage as the subtitle", () => {
	const source = readSessionSidebarSource();

	assert.match(source, /function hasRecentThreadSubtitle/);
	assert.match(source, /threadObj\.lastMessage \?\? ""/);
	assert.match(source, /\{threadObj\.lastMessage \?\? ""\}/);
	assert.doesNotMatch(source, /\{threadObj\.sessionName \|\| "New Session"\}/);
});
