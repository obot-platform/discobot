import type { Pluggable } from "unified";

export type MarkdownMode = "static" | "streaming";

export type HighlightToken = {
	bgColor?: string;
	color?: string;
	content: string;
	htmlAttrs?: Record<string, string>;
	htmlStyle?: Record<string, string>;
	offset?: number;
};

export type HighlightResult = {
	bg?: string;
	fg?: string;
	rootStyle?: string | false;
	tokens: HighlightToken[][];
};

export type HighlightOptions = {
	code: string;
	language: any;
	themes: [string, string];
};

export type MathPlugin = {
	getStyles?: () => string;
	name: "katex";
	rehypePlugin: Pluggable;
	remarkPlugin: Pluggable;
	type: "math";
};

export type CjkPlugin = {
	name: "cjk";
	remarkPlugins: Pluggable[];
	remarkPluginsAfter: Pluggable[];
	remarkPluginsBefore: Pluggable[];
	type: "cjk";
};

export type CodeHighlighterPlugin = {
	getSupportedLanguages?: () => string[];
	getThemes?: () => [string, string];
	highlight: (
		options: HighlightOptions,
		callback?: (result: HighlightResult) => void,
	) => HighlightResult | null;
	name: "shiki";
	supportsLanguage?: (language: any) => boolean;
	type: "code-highlighter";
};

export type DiagramPlugin = {
	language: string;
	name: "mermaid";
	type: "diagram";
};

export type MarkdownPluginConfig = {
	cjk?: CjkPlugin;
	code?: CodeHighlighterPlugin;
	math?: MathPlugin;
	mermaid?: DiagramPlugin;
};

export type PreprocessMarkdownOptions = {
	isAnimating?: boolean;
	mode?: MarkdownMode;
};

export type RenderMarkdownOptions = {
	isIncompleteCodeFence?: boolean;
	onLinkClick?: (url: string) => void;
	plugins?: MarkdownPluginConfig;
};
