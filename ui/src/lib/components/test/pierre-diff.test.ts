import assert from "node:assert/strict";
import test from "node:test";

import {
	buildDiffCacheKey,
	buildDiffFileContents,
	getLanguageFromPath,
} from "../../pierre-diff-utils";

test("getLanguageFromPath detects Go files", () => {
	assert.equal(getLanguageFromPath("server/internal/app/service.go"), "go");
});

test("buildDiffCacheKey scopes cache entries by path and language", () => {
	assert.equal(
		buildDiffCacheKey(
			"server/internal/app/service.go",
			"package app",
			"patch-hash",
		),
		"server/internal/app/service.go:go:patch-hash",
	);
	assert.equal(
		buildDiffCacheKey("Dockerfile", "FROM golang:1.26", "patch-hash"),
		"Dockerfile:docker:patch-hash",
	);
});

test("buildDiffFileContents keeps same patch hashes isolated across file types", () => {
	const goFile = buildDiffFileContents(
		"server/internal/app/service.go",
		"package app",
		"shared-patch-hash",
	);
	const tsFile = buildDiffFileContents(
		"ui/src/lib/service.ts",
		"export const value = 1;",
		"shared-patch-hash",
	);

	assert.equal(goFile.lang, "go");
	assert.equal(tsFile.lang, "typescript");
	assert.notEqual(goFile.cacheKey, tsFile.cacheKey);
});

test("buildDiffFileContents falls back to a path-scoped length key without an explicit checksum", () => {
	const file = buildDiffFileContents(
		"ui/src/lib/example.svelte",
		"<div />",
		null,
	);

	assert.equal(file.cacheKey, "ui/src/lib/example.svelte:svelte:7");
});
