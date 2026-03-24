import assert from "node:assert/strict";
import test from "node:test";

import {
	extractApplyPatchInput,
	parseApplyPatchInput,
	parseApplyPatchOutput,
	summarizeApplyPatchTitle,
} from "../ai/tool-renderers/apply-patch";

test("extractApplyPatchInput reads wrapped raw input", () => {
	const patch = ["*** Begin Patch", "*** End Patch"].join("\n");

	assert.equal(extractApplyPatchInput({ raw: patch }), patch);
});

test("parseApplyPatchInput parses add, update, rename, and delete operations", () => {
	const patch = [
		"*** Begin Patch",
		"*** Add File: new.txt",
		"+hello",
		"+world",
		"*** Update File: src/example.ts",
		"*** Move to: src/example-renamed.ts",
		"@@ export function example() {",
		"-\treturn oldValue;",
		"+\tconst value = 1;",
		"+\treturn value;",
		" }",
		"*** Delete File: old.txt",
		"*** End Patch",
	].join("\n");

	const parsed = parseApplyPatchInput({ raw: patch });

	assert.equal(parsed.error, undefined);
	assert.equal(parsed.incomplete, false);
	assert.equal(parsed.operations.length, 3);
	assert.deepEqual(
		parsed.operations.map((operation) => operation.kind),
		["add", "update", "delete"],
	);
	assert.equal(parsed.operations[0]?.addLines.length, 2);
	assert.equal(parsed.operations[1]?.movePath, "src/example-renamed.ts");
	assert.equal(parsed.operations[1]?.chunks.length, 1);
	assert.deepEqual(
		parsed.operations[1]?.chunks[0]?.lines.map((line) => line.marker),
		["-", "+", "+", " "],
	);
	assert.equal(parsed.stats.files, 3);
	assert.equal(parsed.stats.additions, 4);
	assert.equal(parsed.stats.removals, 1);
	assert.equal(parsed.stats.addedFiles, 1);
	assert.equal(parsed.stats.updatedFiles, 1);
	assert.equal(parsed.stats.deletedFiles, 1);
});

test("parseApplyPatchInput marks missing end marker as incomplete", () => {
	const patch = [
		"*** Begin Patch",
		"*** Update File: src/example.ts",
		"@@",
		"-old",
		"+new",
	].join("\n");

	const parsed = parseApplyPatchInput({ raw: patch });

	assert.equal(parsed.incomplete, true);
	assert.equal(parsed.operations.length, 1);
	assert.equal(parsed.operations[0]?.kind, "update");
	assert.equal(parsed.error, undefined);
});

test("parseApplyPatchInput returns an error for invalid patch headers", () => {
	const parsed = parseApplyPatchInput({
		raw: "*** Update File: src/example.ts\n@@\n-old\n+new",
	});

	assert.equal(parsed.operations.length, 0);
	assert.match(parsed.error ?? "", /Patch must start/);
});

test("parseApplyPatchOutput parses success summaries", () => {
	const output = [
		"Success. Updated the following files:",
		"A new.txt",
		"M src/example.ts",
		"D old.txt",
	].join("\n");

	const parsed = parseApplyPatchOutput(output);

	assert.equal(parsed.success, true);
	assert.deepEqual(parsed.entries, [
		{ status: "added", marker: "A", path: "new.txt" },
		{ status: "modified", marker: "M", path: "src/example.ts" },
		{ status: "deleted", marker: "D", path: "old.txt" },
	]);
});

test("summarizeApplyPatchTitle uses the first changed file", () => {
	const patch = [
		"*** Begin Patch",
		"*** Update File: src/example.ts",
		"@@",
		"-old",
		"+new",
		"*** Add File: src/other.ts",
		"+export const value = 1;",
		"*** End Patch",
	].join("\n");

	assert.equal(
		summarizeApplyPatchTitle({ raw: patch }),
		"Apply patch: example.ts (+1)",
	);
});
