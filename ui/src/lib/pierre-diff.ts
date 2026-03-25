import type {
	FileContents,
	FileDiffOptions,
	SupportedLanguages,
} from "@pierre/diffs";
import { getOrCreateWorkerPoolSingleton } from "@pierre/diffs/worker";
import WorkerUrl from "@pierre/diffs/worker/worker.js?worker&url";

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

const LANGUAGE_MAP: Record<string, SupportedLanguages> = {
	js: "javascript",
	jsx: "javascript",
	ts: "typescript",
	tsx: "typescript",
	py: "python",
	rb: "ruby",
	go: "go",
	rs: "rust",
	java: "java",
	c: "c",
	cpp: "cpp",
	h: "c",
	hpp: "cpp",
	cs: "csharp",
	php: "php",
	swift: "swift",
	kt: "kotlin",
	html: "html",
	css: "css",
	scss: "scss",
	json: "json",
	xml: "xml",
	yaml: "yaml",
	yml: "yaml",
	md: "markdown",
	sql: "sql",
	sh: "bash",
	bash: "bash",
	zsh: "bash",
	dockerfile: "docker",
	makefile: "make",
	toml: "toml",
	graphql: "graphql",
	gql: "graphql",
	svelte: "svelte",
};

const DIFF_WORKER_LANGUAGES = Array.from(
	new Set(Object.values(LANGUAGE_MAP)),
) satisfies SupportedLanguages[];

function workerFactory(): Worker {
	return new Worker(WorkerUrl, { type: "module" });
}

export function getLanguageFromPath(
	path: string,
): SupportedLanguages | undefined {
	const filename = path.split("/").at(-1)?.toLowerCase() ?? "";
	if (filename === "dockerfile") return "docker";
	if (filename === "makefile") return "make";
	const extension = path.split(".").at(-1)?.toLowerCase() ?? "";
	return LANGUAGE_MAP[extension];
}

export function buildDiffFileContents(
	path: string,
	content: string,
	cacheKey: string | null,
): FileContents {
	const language = getLanguageFromPath(path);
	return {
		name: path,
		contents: content,
		lang: language,
		cacheKey: cacheKey ?? `${path}:${content.length}`,
	};
}

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
