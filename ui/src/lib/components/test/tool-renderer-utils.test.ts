import assert from "node:assert/strict";
import test from "node:test";

import { parseNumberedToolOutput } from "../ai/tool-renderers/utils";

test("parseNumberedToolOutput parses numbered lines", () => {
	const parsed = parseNumberedToolOutput([
		"     1→",
		"     2→> discobot@0.0.0-dev check:fix /home/discobot/workspace",
		"     3→> pnpm check:frontend:fix && pnpm check:backend:fix && pnpm check:shell",
	].join("\n"));

	assert.equal(parsed.isTruncated, false);
	assert.equal(parsed.truncationFilePath, undefined);
	assert.deepEqual(parsed.lines, [
		{ lineNumber: "1", text: "" },
		{
			lineNumber: "2",
			text: "> discobot@0.0.0-dev check:fix /home/discobot/workspace",
		},
		{
			lineNumber: "3",
			text: "> pnpm check:frontend:fix && pnpm check:backend:fix && pnpm check:shell",
		},
	]);
});

test("parseNumberedToolOutput parses truncated numbered output", () => {
	const parsed = parseNumberedToolOutput([
		"[Output too long (78308 chars). Full output written to: /home/discobot/.discobot/output/q5umIkXNz0uXeUOx/call_Cxv9colwxGehRIajegv4Pf8e.txt]",
		"",
		"     1→",
		"     2→> discobot@0.0.0-dev check:fix /home/discobot/workspace",
		"     3→> pnpm check:frontend:fix && pnpm check:backend:fix && pnpm check:shell",
	].join("\n"));

	assert.equal(parsed.isTruncated, true);
	assert.equal(
		parsed.truncationFilePath,
		"/home/discobot/.discobot/output/q5umIkXNz0uXeUOx/call_Cxv9colwxGehRIajegv4Pf8e.txt",
	);
	assert.deepEqual(parsed.lines, [
		{ lineNumber: "1", text: "" },
		{
			lineNumber: "2",
			text: "> discobot@0.0.0-dev check:fix /home/discobot/workspace",
		},
		{
			lineNumber: "3",
			text: "> pnpm check:frontend:fix && pnpm check:backend:fix && pnpm check:shell",
		},
	]);
});

test("parseNumberedToolOutput falls back when output is not fully numbered", () => {
	const parsed = parseNumberedToolOutput([
		"[Output too long (120 chars). Full output written to: /tmp/output.txt]",
		"",
		"plain text output",
	].join("\n"));

	assert.equal(parsed.isTruncated, true);
	assert.equal(parsed.truncationFilePath, "/tmp/output.txt");
	assert.deepEqual(parsed.lines, []);
});
