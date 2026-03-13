import assert from "node:assert/strict";
import test from "node:test";

import {
	isEqualQueryKey,
	isPartialQueryKeyMatch,
	normalizeQueryKey,
	stableSerializeQueryKey,
} from "../query-key";

// --- normalizeQueryKey ---

test("normalizeQueryKey returns primitives unchanged", () => {
	assert.deepEqual(normalizeQueryKey(["sessions", 42, true, null]), ["sessions", 42, true, null]);
});

test("normalizeQueryKey sorts object keys for stable ordering", () => {
	const result = normalizeQueryKey([{ z: 1, a: 2 }]);
	assert.deepEqual(result, [{ a: 2, z: 1 }]);
});

test("normalizeQueryKey sorts nested object keys recursively", () => {
	const result = normalizeQueryKey([{ z: { b: 2, a: 1 }, a: 3 }]);
	assert.deepEqual(result, [{ a: 3, z: { a: 1, b: 2 } }]);
});

// --- stableSerializeQueryKey ---

test("stableSerializeQueryKey produces the same string regardless of object key order", () => {
	const a = stableSerializeQueryKey([{ z: 1, a: 2 }]);
	const b = stableSerializeQueryKey([{ a: 2, z: 1 }]);
	assert.equal(a, b);
});

test("stableSerializeQueryKey distinguishes different keys", () => {
	const a = stableSerializeQueryKey(["sessions", "abc"]);
	const b = stableSerializeQueryKey(["sessions", "def"]);
	assert.notEqual(a, b);
});

// --- isEqualQueryKey ---

test("isEqualQueryKey returns true for identical keys", () => {
	assert.ok(isEqualQueryKey(["sessions"], ["sessions"]));
});

test("isEqualQueryKey returns true for equivalent object keys in different insertion order", () => {
	assert.ok(isEqualQueryKey([{ b: 2, a: 1 }], [{ a: 1, b: 2 }]));
});

test("isEqualQueryKey returns false for different keys", () => {
	assert.ok(!isEqualQueryKey(["sessions", "a"], ["sessions", "b"]));
});

// --- isPartialQueryKeyMatch ---

test("isPartialQueryKeyMatch matches when prefix equals the full key", () => {
	assert.ok(isPartialQueryKeyMatch(["sessions", "abc"], ["sessions", "abc"]));
});

test("isPartialQueryKeyMatch matches when prefix is a leading subset", () => {
	assert.ok(isPartialQueryKeyMatch(["sessions", "abc", "messages"], ["sessions", "abc"]));
});

test("isPartialQueryKeyMatch matches a single-element prefix", () => {
	assert.ok(isPartialQueryKeyMatch(["sessions", "abc"], ["sessions"]));
});

test("isPartialQueryKeyMatch returns false when prefix is longer than the candidate", () => {
	assert.ok(!isPartialQueryKeyMatch(["sessions"], ["sessions", "abc"]));
});

test("isPartialQueryKeyMatch returns false when first segment differs", () => {
	assert.ok(!isPartialQueryKeyMatch(["workspaces", "abc"], ["sessions", "abc"]));
});

test("isPartialQueryKeyMatch treats object parts with different key order as equal", () => {
	assert.ok(isPartialQueryKeyMatch([{ b: 2, a: 1 }], [{ a: 1, b: 2 }]));
});
