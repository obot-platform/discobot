import assert from "node:assert/strict";
import test from "node:test";

import {
	buildEditDiffRows,
	countDiffLinesFast,
	parseUnifiedDiff,
	reconstructOriginalFromPatch,
} from "../../diff-utils";

test("parseUnifiedDiff returns numbered rows for unified patches", () => {
	const patch = [
		"diff --git a/example.ts b/example.ts",
		"--- a/example.ts",
		"+++ b/example.ts",
		"@@ -1,3 +1,4 @@",
		" export function example() {",
		"-\treturn 1;",
		"+\tconst value = 1;",
		"+\treturn value;",
		" }",
	].join("\n");

	const hunks = parseUnifiedDiff(patch);

	assert.equal(hunks.length, 1);
	assert.deepEqual(hunks[0]?.lines, [
		{ left: 1, right: 1, marker: " ", content: "export function example() {" },
		{ left: 2, right: null, marker: "-", content: "\treturn 1;" },
		{ left: null, right: 2, marker: "+", content: "\tconst value = 1;" },
		{ left: null, right: 3, marker: "+", content: "\treturn value;" },
		{ left: 3, right: 4, marker: " ", content: "}" },
	]);
});

test("reconstructOriginalFromPatch reverses a modified file patch", () => {
	const current = [
		"export function example() {",
		"\tconst value = 1;",
		"\treturn value;",
		"}",
	].join("\n");
	const patch = [
		"diff --git a/example.ts b/example.ts",
		"--- a/example.ts",
		"+++ b/example.ts",
		"@@ -1,3 +1,4 @@",
		" export function example() {",
		"-\treturn 1;",
		"+\tconst value = 1;",
		"+\treturn value;",
		" }",
	].join("\n");

	assert.equal(
		reconstructOriginalFromPatch(current, patch),
		["export function example() {", "\treturn 1;", "}"].join("\n"),
	);
});

test("reconstructOriginalFromPatch recovers deleted file content from an empty current file", () => {
	const patch = [
		"diff --git a/example.ts b/example.ts",
		"deleted file mode 100644",
		"--- a/example.ts",
		"+++ /dev/null",
		"@@ -1,3 +0,0 @@",
		"-export function removed() {",
		"-\treturn true;",
		"-}",
	].join("\n");

	assert.equal(
		reconstructOriginalFromPatch("", patch),
		["export function removed() {", "\treturn true;", "}"].join("\n"),
	);
});

test("countDiffLinesFast counts only hunk content lines", () => {
	const patch = [
		"diff --git a/example.ts b/example.ts",
		"--- a/example.ts",
		"+++ b/example.ts",
		"@@ -1,2 +1,3 @@",
		" export const value = 1;",
		"-export const oldValue = 2;",
		"+export const nextValue = 2;",
		"+export const otherValue = 3;",
	].join("\n");

	assert.equal(countDiffLinesFast(patch), 4);
});

test("buildEditDiffRows returns numbered edit rows with inline highlights", () => {
	const rows = buildEditDiffRows(
		[
			"export function example() {",
			"\treturn oldValue;",
			"}",
		].join("\n"),
		[
			"export function example() {",
			"\treturn newValue;",
			"}",
		].join("\n"),
	);

	assert.deepEqual(rows, [
		{
			kind: "context",
			oldLineNumber: 1,
			newLineNumber: 1,
			segments: [{ text: "export function example() {", changed: false }],
		},
		{
			kind: "remove",
			oldLineNumber: 2,
			newLineNumber: null,
			segments: [
				{ text: "\treturn ", changed: false },
				{ text: "old", changed: true },
				{ text: "Value;", changed: false },
			],
		},
		{
			kind: "add",
			oldLineNumber: null,
			newLineNumber: 2,
			segments: [
				{ text: "\treturn ", changed: false },
				{ text: "new", changed: true },
				{ text: "Value;", changed: false },
			],
		},
		{
			kind: "context",
			oldLineNumber: 3,
			newLineNumber: 3,
			segments: [{ text: "}", changed: false }],
		},
	]);
});

test("buildEditDiffRows preserves unmatched insertions and deletions", () => {
	const rows = buildEditDiffRows(
		["alpha", "beta"].join("\n"),
		["alpha", "gamma", "delta"].join("\n"),
	);

	assert.deepEqual(rows, [
		{
			kind: "context",
			oldLineNumber: 1,
			newLineNumber: 1,
			segments: [{ text: "alpha", changed: false }],
		},
		{
			kind: "remove",
			oldLineNumber: 2,
			newLineNumber: null,
			segments: [
				{ text: "bet", changed: true },
				{ text: "a", changed: false },
			],
		},
		{
			kind: "add",
			oldLineNumber: null,
			newLineNumber: 2,
			segments: [
				{ text: "gamm", changed: true },
				{ text: "a", changed: false },
			],
		},
		{
			kind: "add",
			oldLineNumber: null,
			newLineNumber: 3,
			segments: [{ text: "delta", changed: true }],
		},
	]);
});
