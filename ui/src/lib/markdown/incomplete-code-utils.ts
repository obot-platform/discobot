// Adapted from Vercel Streamdown (packages/streamdown), Apache-2.0.
const CODE_FENCE_PATTERN = /^[ \t]{0,3}(`{3,}|~{3,})/;

export function hasIncompleteCodeFence(markdown: string): boolean {
	const lines = markdown.split("\n");
	let openFenceChar: string | null = null;
	let openFenceLength = 0;

	for (const line of lines) {
		const match = CODE_FENCE_PATTERN.exec(line);

		if (openFenceChar === null) {
			if (match) {
				const fenceRun = match[1];
				openFenceChar = fenceRun[0];
				openFenceLength = fenceRun.length;
			}
		} else if (match) {
			const fenceRun = match[1];
			const char = fenceRun[0];
			const length = fenceRun.length;

			if (char === openFenceChar && length >= openFenceLength) {
				openFenceChar = null;
				openFenceLength = 0;
			}
		}
	}

	return openFenceChar !== null;
}
