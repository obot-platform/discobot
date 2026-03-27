import type { FileContents, SupportedLanguages } from "@pierre/diffs";

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

export const DIFF_WORKER_LANGUAGES = Array.from(
	new Set(Object.values(LANGUAGE_MAP)),
) satisfies SupportedLanguages[];

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
		cacheKey: buildDiffCacheKey(path, content, cacheKey, language),
	};
}

export function buildDiffCacheKey(
	path: string,
	content: string,
	cacheKey: string | null,
	language = getLanguageFromPath(path),
): string {
	const languageKey = language ?? "text";
	const contentKey = cacheKey ?? `${content.length}`;
	return `${path}:${languageKey}:${contentKey}`;
}
