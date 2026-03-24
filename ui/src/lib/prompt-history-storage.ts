export const MAX_PROMPT_HISTORY_SIZE = 100;
export const MAX_VISIBLE_PROMPT_HISTORY = 20;

export function appendPromptHistoryEntry(
	history: string[],
	prompt: string,
): string[] {
	const trimmedPrompt = prompt.trim();
	if (!trimmedPrompt) {
		return history;
	}

	if (history.includes(trimmedPrompt)) {
		return history;
	}

	return [trimmedPrompt, ...history].slice(0, MAX_PROMPT_HISTORY_SIZE);
}

export function removePromptHistoryEntry(
	history: string[],
	prompt: string,
): string[] {
	return history.filter((entry) => entry !== prompt);
}

export function appendPinnedPrompt(
	pinnedPrompts: string[],
	prompt: string,
): string[] {
	const trimmedPrompt = prompt.trim();
	if (!trimmedPrompt || pinnedPrompts.includes(trimmedPrompt)) {
		return pinnedPrompts;
	}

	return [...pinnedPrompts, trimmedPrompt];
}

export function removePinnedPrompt(
	pinnedPrompts: string[],
	prompt: string,
): string[] {
	return pinnedPrompts.filter((entry) => entry !== prompt);
}
