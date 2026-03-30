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

test("session sidebar recent threads render a state badge when present", () => {
	const source = readSessionSidebarSource();

	assert.match(source, /function recentThreadStateLabel/);
	assert.match(source, /threadObj\.state === "interrupted"/);
	assert.match(source, /threadObj\.state === "cancelled"/);
	assert.match(source, /\{recentThreadStateLabel\(threadObj\)\}/);
});

test("session sidebar keys session and recent thread rows", () => {
	const source = readSessionSidebarSource();

	assert.match(
		source,
		/\{#each sessions\.list as sessionObj \(sessionObj\.id\)\}/,
	);
	assert.match(
		source,
		/\{#each sessions\.recentThreads as threadObj \(`\$\{threadObj\.sessionId\}:\$\{threadObj\.threadId\}`\)\}/,
	);
});
