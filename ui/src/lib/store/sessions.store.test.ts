import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import test from "node:test";

function readStoreSource(fileName: string) {
	return readFileSync(path.resolve(import.meta.dirname, fileName), "utf-8");
}

function assertStoreUsesInPlaceFetchOneUpdates(options: {
	fileName: string;
	pushPattern: RegExp;
	assignPattern: RegExp;
	spreadPattern: RegExp;
}) {
	const source = readStoreSource(options.fileName);

	assert.match(source, options.assignPattern);
	assert.match(source, options.pushPattern);
	assert.doesNotMatch(source, /this\.#items = this\.#items\.map\(/);
	assert.doesNotMatch(source, options.spreadPattern);
}

test("SessionStore.fetchOne mutates the existing array for updates", () => {
	assertStoreUsesInPlaceFetchOneUpdates({
		fileName: "sessions.store.svelte.ts",
		pushPattern: /this\.#items\.push\(session\)/,
		assignPattern: /this\.#items\[idx\] = session/,
		spreadPattern: /this\.#items = \[\.\.\.this\.#items, session\]/,
	});
});

test("WorkspaceStore.fetchOne mutates the existing array for updates", () => {
	assertStoreUsesInPlaceFetchOneUpdates({
		fileName: "workspaces.store.svelte.ts",
		pushPattern: /this\.#items\.push\(workspace\)/,
		assignPattern: /this\.#items\[idx\] = workspace/,
		spreadPattern: /this\.#items = \[\.\.\.this\.#items, workspace\]/,
	});
});

test("EnvSetStore.fetchOne mutates the existing array for updates", () => {
	assertStoreUsesInPlaceFetchOneUpdates({
		fileName: "env-sets.store.svelte.ts",
		pushPattern: /this\.#items\.push\(envSet\)/,
		assignPattern: /this\.#items\[idx\] = envSet/,
		spreadPattern: /this\.#items = \[\.\.\.this\.#items, envSet\]/,
	});
});

test("ThreadStore.fetchOne mutates the existing array for updates", () => {
	assertStoreUsesInPlaceFetchOneUpdates({
		fileName: "threads.store.svelte.ts",
		pushPattern: /this\.#items\.push\(thread\)/,
		assignPattern: /this\.#items\[idx\] = thread/,
		spreadPattern: /this\.#items = \[\.\.\.this\.#items, thread\]/,
	});
});
