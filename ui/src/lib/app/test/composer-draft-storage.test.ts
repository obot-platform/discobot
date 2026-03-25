import assert from "node:assert/strict";
import test from "node:test";

import {
	clearComposerDraft,
	moveComposerDraft,
	PENDING_COMPOSER_DRAFT_STORAGE_KEY,
	readComposerDraft,
	resolveComposerDraftStorageKey,
	writeComposerDraft,
} from "../../composer-draft-storage";

type StorageLike = {
	getItem: (key: string) => string | null;
	setItem: (key: string, value: string) => void;
	removeItem: (key: string) => void;
};

function createLocalStorage(): StorageLike {
	const values = new Map<string, string>();
	return {
		getItem: (key) => values.get(key) ?? null,
		setItem: (key, value) => {
			values.set(key, value);
		},
		removeItem: (key) => {
			values.delete(key);
		},
	};
}

function withLocalStorage(run: (storage: StorageLike) => void) {
	const windowWithStorage = globalThis as typeof globalThis & {
		window?: Window;
	};
	const previousWindow = windowWithStorage.window;
	const storage = createLocalStorage();
	windowWithStorage.window = { localStorage: storage } as unknown as Window &
		typeof globalThis;
	try {
		run(storage);
	} finally {
		windowWithStorage.window = previousWindow;
	}
}

test("pending sessions always use the shared pending draft key", () => {
	assert.equal(
		resolveComposerDraftStorageKey({ isPending: true, threadId: "thread-123" }),
		PENDING_COMPOSER_DRAFT_STORAGE_KEY,
	);
	assert.equal(
		resolveComposerDraftStorageKey({ isPending: true, threadId: null }),
		PENDING_COMPOSER_DRAFT_STORAGE_KEY,
	);
});

test("saved sessions use the active thread id for draft storage", () => {
	assert.equal(
		resolveComposerDraftStorageKey({
			isPending: false,
			threadId: "thread-123",
		}),
		"discobot:composer-draft:thread-123",
	);
});

test("draft storage reads, writes, and clears values", () => {
	withLocalStorage((storage) => {
		const storageKey = resolveComposerDraftStorageKey({
			isPending: false,
			threadId: "thread-123",
		});

		writeComposerDraft(storageKey, "draft text");
		assert.equal(readComposerDraft(storageKey), "draft text");
		assert.equal(storage.getItem(storageKey), "draft text");

		clearComposerDraft(storageKey);
		assert.equal(readComposerDraft(storageKey), "");
		assert.equal(storage.getItem(storageKey), null);
	});
});

test("pending drafts stay isolated from thread drafts", () => {
	withLocalStorage(() => {
		const pendingKey = resolveComposerDraftStorageKey({
			isPending: true,
			threadId: "thread-123",
		});
		const threadKey = resolveComposerDraftStorageKey({
			isPending: false,
			threadId: "thread-123",
		});

		writeComposerDraft(pendingKey, "pending draft");
		writeComposerDraft(threadKey, "thread draft");

		assert.equal(readComposerDraft(pendingKey), "pending draft");
		assert.equal(readComposerDraft(threadKey), "thread draft");
	});
});

test("moveComposerDraft transfers a pending draft to a created thread", () => {
	withLocalStorage((storage) => {
		const pendingKey = resolveComposerDraftStorageKey({
			isPending: true,
			threadId: "pending-thread",
		});
		const threadKey = resolveComposerDraftStorageKey({
			isPending: false,
			threadId: "created-thread",
		});

		writeComposerDraft(pendingKey, "@src/lib");
		moveComposerDraft({
			fromStorageKey: pendingKey,
			toStorageKey: threadKey,
		});

		assert.equal(storage.getItem(pendingKey), null);
		assert.equal(readComposerDraft(threadKey), "@src/lib");
	});
});

test("moveComposerDraft can preserve the in-memory draft value during session creation", () => {
	withLocalStorage((storage) => {
		const pendingKey = resolveComposerDraftStorageKey({
			isPending: true,
			threadId: "pending-thread",
		});
		const threadKey = resolveComposerDraftStorageKey({
			isPending: false,
			threadId: "created-thread",
		});

		writeComposerDraft(pendingKey, "@");
		moveComposerDraft({
			fromStorageKey: pendingKey,
			toStorageKey: threadKey,
			value: "@src/lib/components",
		});

		assert.equal(storage.getItem(pendingKey), null);
		assert.equal(readComposerDraft(threadKey), "@src/lib/components");
	});
});

test("pending submit can capture the shared pending draft key before session state changes", () => {
	const pendingKey = resolveComposerDraftStorageKey({
		isPending: true,
		threadId: "session-123",
	});
	const savedThreadKey = resolveComposerDraftStorageKey({
		isPending: false,
		threadId: "session-123",
	});

	assert.equal(pendingKey, PENDING_COMPOSER_DRAFT_STORAGE_KEY);
	assert.notEqual(savedThreadKey, pendingKey);
});
