import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import test from "node:test";

const THREAD_WORKSPACE_HEADER_COMPONENT = path.resolve(
	import.meta.dirname,
	"../app/parts/ThreadWorkspaceHeader.svelte",
);

function readThreadWorkspaceHeaderSource() {
	return readFileSync(THREAD_WORKSPACE_HEADER_COMPONENT, "utf-8");
}

test("thread workspace header renders interrupted and cancelled badges", () => {
	const source = readThreadWorkspaceHeaderSource();

	assert.match(source, /function threadStateLabel/);
	assert.match(source, /value === "interrupted"/);
	assert.match(source, /value === "cancelled"/);
	assert.match(source, /\{threadStateLabel\(state\)\}/);
});
