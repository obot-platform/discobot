import remend from "remend";
import type { PreprocessMarkdownOptions } from "./types";

export function preprocessMarkdown(
	markdown: string,
	{ isAnimating = false, mode = "streaming" }: PreprocessMarkdownOptions = {},
): string {
	if (!markdown) {
		return "";
	}

	if (mode === "streaming" && isAnimating) {
		return remend(markdown);
	}

	return markdown;
}
