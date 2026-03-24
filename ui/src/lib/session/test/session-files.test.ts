import assert from "node:assert/strict";
import test from "node:test";

import {
	hasDirtyBufferAtOrWithinPath,
	isPathAtOrWithin,
	remapRecordKeys,
	removeRecordKeys,
	renamePath,
} from "../domains/session-files.svelte";
import type { SessionFileBufferState } from "../session-context.types";

function makeBufferState(isDirty: boolean): SessionFileBufferState {
	return {
		content: "",
		originalContent: "",
		encoding: "utf8",
		isDirty,
		isSaving: false,
		saveError: null,
		hasConflict: false,
		conflictContent: null,
		fromBase: false,
	};
}

test("isPathAtOrWithin matches exact paths and descendants", () => {
	assert.equal(isPathAtOrWithin("src/app.ts", "src/app.ts"), true);
	assert.equal(isPathAtOrWithin("src/components/Button.svelte", "src"), true);
	assert.equal(isPathAtOrWithin("tests/app.test.ts", "src"), false);
});

test("renamePath remaps exact paths and descendants", () => {
	assert.equal(
		renamePath("src/app.ts", "src/app.ts", "src/main.ts"),
		"src/main.ts",
	);
	assert.equal(
		renamePath("src/utils/math.ts", "src", "lib"),
		"lib/utils/math.ts",
	);
	assert.equal(renamePath("README.md", "src", "lib"), "README.md");
});

test("remapRecordKeys renames matching keys", () => {
	const result = remapRecordKeys(
		{
			"src/app.ts": "app",
			"src/utils/math.ts": "math",
			"README.md": "readme",
		},
		"src",
		"lib",
	);

	assert.deepEqual(result, {
		"lib/app.ts": "app",
		"lib/utils/math.ts": "math",
		"README.md": "readme",
	});
});

test("removeRecordKeys removes exact paths and descendants", () => {
	const result = removeRecordKeys(
		{
			"src/app.ts": "app",
			"src/utils/math.ts": "math",
			"README.md": "readme",
		},
		"src",
	);

	assert.deepEqual(result, {
		"README.md": "readme",
	});
});

test("hasDirtyBufferAtOrWithinPath detects dirty descendants", () => {
	const result = hasDirtyBufferAtOrWithinPath(
		{
			"src/app.ts": makeBufferState(false),
			"src/utils/math.ts": makeBufferState(true),
			"README.md": makeBufferState(true),
		},
		"src",
	);

	assert.equal(result, true);
	assert.equal(
		hasDirtyBufferAtOrWithinPath(
			{
				"src/app.ts": makeBufferState(false),
				"src/utils/math.ts": makeBufferState(false),
			},
			"src",
		),
		false,
	);
});
