import assert from "node:assert/strict";
import test from "node:test";

import {
	allQuestionsAnswered,
	isQuestionAnswered,
	shouldShowSubmitAction,
	type AskUserQuestionAnswerState,
	type AskUserQuestionItem,
} from "../ai/tool-renderers/askuserquestion-wizard-state";

const questions: AskUserQuestionItem[] = [
	{
		header: "Runtime",
		question: "Which environment should we target?",
		multiSelect: false,
		options: [
			{ label: "Staging", description: "Validate first" },
			{ label: "Production", description: "Ship immediately" },
		],
	},
	{
		header: "Risk",
		question: "What level of change is acceptable?",
		multiSelect: false,
		options: [
			{ label: "Minimal", description: "Keep the change narrow" },
			{ label: "Moderate", description: "Allow a few supporting edits" },
		],
	},
];

function makeState(
	overrides: Partial<AskUserQuestionAnswerState> = {},
): AskUserQuestionAnswerState {
	return {
		answers: {},
		otherSelected: {},
		otherText: {},
		...overrides,
	};
}

test("single-select questions start unanswered", () => {
	const state = makeState();

	assert.equal(isQuestionAnswered(questions[0], state), false);
	assert.equal(allQuestionsAnswered(questions, state), false);
	assert.equal(
		shouldShowSubmitAction({
			currentStep: 0,
			questions,
			state,
		}),
		false,
	);
});

test("wizard keeps the primary action on Continue until every question is answered", () => {
	const firstAnsweredState = makeState({
		answers: {
			[questions[0].question]: "Staging",
		},
	});

	assert.equal(isQuestionAnswered(questions[0], firstAnsweredState), true);
	assert.equal(isQuestionAnswered(questions[1], firstAnsweredState), false);
	assert.equal(allQuestionsAnswered(questions, firstAnsweredState), false);
	assert.equal(
		shouldShowSubmitAction({
			currentStep: 0,
			questions,
			state: firstAnsweredState,
		}),
		false,
	);
	assert.equal(
		shouldShowSubmitAction({
			currentStep: 1,
			questions,
			state: firstAnsweredState,
		}),
		true,
	);
});

test("wizard enables submission once every question has an answer", () => {
	const allAnsweredState = makeState({
		answers: {
			[questions[0].question]: "Staging",
			[questions[1].question]: "Moderate",
		},
	});

	assert.equal(allQuestionsAnswered(questions, allAnsweredState), true);
	assert.equal(
		shouldShowSubmitAction({
			currentStep: 0,
			questions,
			state: allAnsweredState,
		}),
		true,
	);
});

test("other answers require non-empty text before the step counts as answered", () => {
	const emptyOtherState = makeState({
		otherSelected: {
			[questions[0].question]: true,
		},
		otherText: {
			[questions[0].question]: "   ",
		},
	});

	assert.equal(isQuestionAnswered(questions[0], emptyOtherState), false);

	const filledOtherState = makeState({
		otherSelected: {
			[questions[0].question]: true,
		},
		otherText: {
			[questions[0].question]: "Canary",
		},
	});

	assert.equal(isQuestionAnswered(questions[0], filledOtherState), true);
});
