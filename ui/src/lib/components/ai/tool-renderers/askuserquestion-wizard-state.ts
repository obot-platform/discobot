import type { AskUserQuestionToolInput } from "$lib/components/ai/tool-schemas/askuserquestion-schema";

export type AskUserQuestionItem = AskUserQuestionToolInput["questions"][number];

export type AskUserQuestionAnswerState = {
	answers: Record<string, string>;
	otherSelected: Record<string, boolean>;
	otherText: Record<string, string>;
};

export function selectedLabels(
	questionKey: string,
	answers: Record<string, string>,
): string[] {
	return (answers[questionKey] ?? "").split(", ").filter(Boolean);
}

export function isQuestionAnswered(
	question: AskUserQuestionItem | undefined,
	state: AskUserQuestionAnswerState,
): boolean {
	if (!question) {
		return false;
	}

	if (state.otherSelected[question.question]) {
		return (state.otherText[question.question]?.trim().length ?? 0) > 0;
	}

	return (state.answers[question.question]?.trim().length ?? 0) > 0;
}

export function allQuestionsAnswered(
	questions: AskUserQuestionItem[],
	state: AskUserQuestionAnswerState,
): boolean {
	return questions.every((question) => isQuestionAnswered(question, state));
}

export function shouldShowSubmitAction(args: {
	currentStep: number;
	questions: AskUserQuestionItem[];
	state: AskUserQuestionAnswerState;
}): boolean {
	if (args.questions.length === 0) {
		return true;
	}

	return (
		args.currentStep >= args.questions.length - 1 ||
		allQuestionsAnswered(args.questions, args.state)
	);
}
