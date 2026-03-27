import type { FileContents, FileDiffOptions } from "@pierre/diffs";
import { getOrCreateWorkerPoolSingleton } from "@pierre/diffs/worker";
import WorkerUrl from "@pierre/diffs/worker/worker.js?worker&url";

import { DIFF_WORKER_LANGUAGES } from "$lib/pierre-diff-utils";
import type { ResolvedTheme } from "$lib/theme";

export const DIFF_WARNING_THRESHOLD = 10000;
export const DIFF_HARD_LIMIT = 20000;
export const DIFF_LINE_DIFF_TYPE = "word";
export const DIFF_THEME = {
	light: "github-light",
	dark: "github-dark",
} as const;

export type DiffStyle = "split" | "unified";

export type DiffRendererParams = {
	diffStyle: DiffStyle;
	resolvedTheme: ResolvedTheme;
	oldFile: FileContents;
	newFile: FileContents;
	virtualized: boolean;
};

function workerFactory(): Worker {
	return new Worker(WorkerUrl, { type: "module" });
}

export { buildDiffFileContents } from "$lib/pierre-diff-utils";

export function getDiffRendererOptions(
	style: DiffStyle,
	theme: ResolvedTheme,
): FileDiffOptions<undefined> {
	return {
		diffStyle: style,
		theme: DIFF_THEME,
		themeType: theme === "dark" ? "dark" : "light",
		disableFileHeader: true,
		hunkSeparators: "line-info",
		expandUnchanged: false,
		collapsedContextThreshold: 3,
		expansionLineCount: 20,
		lineDiffType: DIFF_LINE_DIFF_TYPE,
		overflow: "scroll",
	};
}

export function getDiffWorkerPool() {
	return getOrCreateWorkerPoolSingleton({
		poolOptions: { workerFactory },
		highlighterOptions: {
			theme: DIFF_THEME,
			langs: DIFF_WORKER_LANGUAGES,
			lineDiffType: DIFF_LINE_DIFF_TYPE,
		},
	});
}
