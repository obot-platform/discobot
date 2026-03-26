import assert from "node:assert/strict";
import test from "node:test";

import {
	clearComposerDraftState,
	getThreadComposerValues,
	normalizeThreadComposerReasoning,
	parseComposerModelSelection,
	resolveThreadComposerSubmitValues,
} from "./thread-context.svelte";

test("clearComposerDraftState clears storage before resetting the in-memory draft", () => {
	const calls: string[] = [];
	let composerDraft = "Start a new session with this prompt";

	clearComposerDraftState({
		cancelPersist: () => {
			calls.push("cancel");
		},
		clearStoredDraft: () => {
			calls.push("storage");
			assert.equal(composerDraft, "Start a new session with this prompt");
		},
		clearInMemoryDraft: () => {
			calls.push("memory");
			composerDraft = "";
		},
	});

	assert.deepEqual(calls, ["cancel", "storage", "memory"]);
	assert.equal(composerDraft, "");
});

test("getThreadComposerValues restores thread mode, model, and reasoning", () => {
	assert.deepEqual(
		getThreadComposerValues(
			{
				id: "thread-1",
				name: "Main",
				mode: "plan",
				model: "openai/gpt-5",
				reasoning: "high",
			},
			"anthropic/claude-sonnet-4-6",
		),
		{
			mode: "plan",
			modelId: "openai/gpt-5",
			reasoning: "high",
		},
	);
});

test("getThreadComposerValues falls back to build mode and the default model", () => {
	assert.deepEqual(
		getThreadComposerValues(null, "anthropic/claude-sonnet-4-6"),
		{
			mode: "build",
			modelId: "anthropic/claude-sonnet-4-6",
			reasoning: undefined,
		},
	);
});

test("normalizeThreadComposerReasoning keeps explicit levels and drops empty values", () => {
	assert.equal(normalizeThreadComposerReasoning("default"), "default");
	assert.equal(normalizeThreadComposerReasoning("medium"), "medium");
	assert.equal(normalizeThreadComposerReasoning(""), undefined);
	assert.equal(normalizeThreadComposerReasoning(undefined), undefined);
});

test("parseComposerModelSelection keeps only the model identifier", () => {
	assert.deepEqual(parseComposerModelSelection("openai/gpt-5"), {
		modelId: "openai/gpt-5",
	});
	assert.deepEqual(parseComposerModelSelection(null), {
		modelId: null,
	});
});

test("resolveThreadComposerSubmitValues falls back to current values when next values are unset", () => {
	assert.deepEqual(
		resolveThreadComposerSubmitValues({
			mode: "plan",
			modelId: "openai/gpt-5",
			reasoning: "high",
			nextMode: undefined,
			nextModelId: undefined,
			nextReasoning: undefined,
		}),
		{
			mode: "plan",
			modelId: "openai/gpt-5",
			reasoning: "high",
		},
	);
});

test("resolveThreadComposerSubmitValues clears reasoning when using the default model", () => {
	assert.deepEqual(
		resolveThreadComposerSubmitValues({
			mode: "build",
			modelId: "openai/gpt-5",
			reasoning: "high",
			nextMode: undefined,
			nextModelId: null,
			nextReasoning: "default",
		}),
		{
			mode: "build",
			modelId: null,
			reasoning: undefined,
		},
	);
});

test("resolveThreadComposerSubmitValues prefers staged next values", () => {
	assert.deepEqual(
		resolveThreadComposerSubmitValues({
			mode: "build",
			modelId: "anthropic/claude-sonnet-4-6",
			reasoning: "auto",
			nextMode: "plan",
			nextModelId: "openai/gpt-5",
			nextReasoning: "high",
		}),
		{
			mode: "plan",
			modelId: "openai/gpt-5",
			reasoning: "high",
		},
	);
});
