import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import test from "node:test";

import { SessionStatus } from "../../api-constants";

const SESSION_STATUS_COMPONENT = path.resolve(
	import.meta.dirname,
	"../app/parts/SessionStatus.svelte",
);

function readSessionStatusSource() {
	return readFileSync(SESSION_STATUS_COMPONENT, "utf-8");
}

test("session status constants include committed and rebased", () => {
	assert.equal(SessionStatus.COMMITTED, "committed");
	assert.equal(SessionStatus.REBASED, "rebased");
});

test("session status component renders dedicated git icons for committed and rebased", () => {
	const source = readSessionStatusSource();

	assert.match(source, /import GitCommitIcon from "@lucide\/svelte\/icons\/git-commit"/);
	assert.match(source, /import GitBranchIcon from "@lucide\/svelte\/icons\/git-branch"/);
	assert.match(source, /normalizedStatus\(status\) === "committed"/);
	assert.match(source, /normalizedStatus\(status\) === "rebased"/);
	assert.match(source, /<GitCommitIcon class="size-3\.5" \/>/);
	assert.match(source, /<GitBranchIcon class="size-3\.5" \/>/);
});
