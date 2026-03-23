import assert from "node:assert/strict";
import test from "node:test";

import {
	MAX_PROMPT_HISTORY_SIZE,
	appendPinnedPrompt,
	appendPromptHistoryEntry,
	removePinnedPrompt,
	removePromptHistoryEntry,
} from "../../prompt-history-storage";

type StorageLike = {
	getItem: (key: string) => string | null;
	setItem: (key: string, value: string) => void;
	removeItem: (key: string) => void;
	clear: () => void;
};

const PROMPT_HISTORY_STORAGE_KEY = "discobot:composer-history";
const PINNED_PROMPTS_STORAGE_KEY = "discobot:composer-history:pinned";

function readPromptHistoryFromStorage(storage: StorageLike): string[] {
	const stored = storage.getItem(PROMPT_HISTORY_STORAGE_KEY);
	if (!stored) {
		return [];
	}

	try {
		const parsed = JSON.parse(stored);
		return Array.isArray(parsed)
			? parsed.filter((item): item is string => typeof item === "string")
			: [];
	} catch {
		return [];
	}
}

function readPinnedPromptsFromStorage(storage: StorageLike): string[] {
	const stored = storage.getItem(PINNED_PROMPTS_STORAGE_KEY);
	if (!stored) {
		return [];
	}

	try {
		const parsed = JSON.parse(stored);
		return Array.isArray(parsed)
			? parsed.filter((item): item is string => typeof item === "string")
			: [];
	} catch {
		return [];
	}
}

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
		clear: () => {
			values.clear();
		},
	};
}

function withLocalStorage(run: (storage: StorageLike) => void) {
	const windowWithStorage = globalThis as typeof globalThis & {
		window?: Window;
	};
	const previousWindow = windowWithStorage.window;
	const storage = createLocalStorage();
	windowWithStorage.window = { localStorage: storage } as unknown as Window & typeof globalThis;
	try {
		run(storage);
	} finally {
		windowWithStorage.window = previousWindow;
	}
}

test("appendPromptHistoryEntry prepends new prompts and skips duplicates", () => {
	assert.deepEqual(appendPromptHistoryEntry([], "  first prompt  "), ["first prompt"]);
	assert.deepEqual(appendPromptHistoryEntry(["first prompt"], "first prompt"), ["first prompt"]);
	assert.deepEqual(appendPromptHistoryEntry(["first prompt"], "second prompt"), ["second prompt", "first prompt"]);
});

test("appendPromptHistoryEntry enforces the max history size", () => {
	const history = Array.from({ length: MAX_PROMPT_HISTORY_SIZE }, (_, index) => `prompt-${index}`);
	const nextHistory = appendPromptHistoryEntry(history, "latest prompt");

	assert.equal(nextHistory.length, MAX_PROMPT_HISTORY_SIZE);
	assert.equal(nextHistory[0], "latest prompt");
	assert.equal(nextHistory.at(-1), `prompt-${MAX_PROMPT_HISTORY_SIZE - 2}`);
});

test("removePromptHistoryEntry removes a prompt from history", () => {
	assert.deepEqual(removePromptHistoryEntry(["first", "second", "third"], "second"), ["first", "third"]);
	assert.deepEqual(removePromptHistoryEntry(["first"], "missing"), ["first"]);
});

test("stores prompt history globally", () => {
	withLocalStorage((storage) => {
		let promptHistory = appendPromptHistoryEntry([], "first prompt");
		promptHistory = appendPromptHistoryEntry(promptHistory, "other prompt");
		storage.setItem(PROMPT_HISTORY_STORAGE_KEY, JSON.stringify(promptHistory));

		assert.deepEqual(readPromptHistoryFromStorage(storage), ["other prompt", "first prompt"]);
	});
});

test("stores pinned prompts globally", () => {
	withLocalStorage((storage) => {
		let pinnedPrompts = appendPinnedPrompt([], "favorite one");
		pinnedPrompts = appendPinnedPrompt(pinnedPrompts, "favorite two");
		pinnedPrompts = appendPinnedPrompt(pinnedPrompts, "favorite one");
		storage.setItem(PINNED_PROMPTS_STORAGE_KEY, JSON.stringify(pinnedPrompts));

		assert.deepEqual(readPinnedPromptsFromStorage(storage), ["favorite one", "favorite two"]);

		storage.setItem(
			PINNED_PROMPTS_STORAGE_KEY,
			JSON.stringify(removePinnedPrompt(readPinnedPromptsFromStorage(storage), "favorite one")),
		);
		assert.deepEqual(readPinnedPromptsFromStorage(storage), ["favorite two"]);
	});
});

test("appendPinnedPrompt and removePinnedPrompt keep pin state unique", () => {
	assert.deepEqual(appendPinnedPrompt([], "  favorite  "), ["favorite"]);
	assert.deepEqual(appendPinnedPrompt(["favorite"], "favorite"), ["favorite"]);
	assert.deepEqual(removePinnedPrompt(["favorite", "second"], "favorite"), ["second"]);
});

test("ignores malformed stored values", () => {
	withLocalStorage((storage) => {
		storage.setItem(PROMPT_HISTORY_STORAGE_KEY, "not json");
		storage.setItem(PINNED_PROMPTS_STORAGE_KEY, "{\"bad\":true}");

		assert.deepEqual(readPromptHistoryFromStorage(storage), []);
		assert.deepEqual(readPinnedPromptsFromStorage(storage), []);
	});
});
