import type { Component } from "svelte";
import BotMessageSquareIcon from "@lucide/svelte/icons/bot-message-square";
import BracesIcon from "@lucide/svelte/icons/braces";
import ExternalLinkIcon from "@lucide/svelte/icons/external-link";
import GithubIcon from "@lucide/svelte/icons/github";
import SparklesIcon from "@lucide/svelte/icons/sparkles";

export type OpenInProvider =
	| "chatgpt"
	| "claude"
	| "t3"
	| "scira"
	| "v0"
	| "cursor";

export type ProviderMeta = {
	title: string;
	icon: Component<{ class?: string }>;
	createUrl: (query: string) => string;
};

export const providers: Record<OpenInProvider, ProviderMeta> = {
	chatgpt: {
		title: "Open in ChatGPT",
		icon: BotMessageSquareIcon,
		createUrl: (prompt) =>
			`https://chatgpt.com/?${new URLSearchParams({ hints: "search", prompt })}`,
	},
	claude: {
		title: "Open in Claude",
		icon: SparklesIcon,
		createUrl: (q) => `https://claude.ai/new?${new URLSearchParams({ q })}`,
	},
	t3: {
		title: "Open in T3 Chat",
		icon: BotMessageSquareIcon,
		createUrl: (q) => `https://t3.chat/new?${new URLSearchParams({ q })}`,
	},
	scira: {
		title: "Open in Scira",
		icon: SparklesIcon,
		createUrl: (q) => `https://scira.ai/?${new URLSearchParams({ q })}`,
	},
	v0: {
		title: "Open in v0",
		icon: BracesIcon,
		createUrl: (q) => `https://v0.app?${new URLSearchParams({ q })}`,
	},
	cursor: {
		title: "Open in Cursor",
		icon: GithubIcon,
		createUrl: (text) => {
			const url = new URL("https://cursor.com/link/prompt");
			url.searchParams.set("text", text);
			return url.toString();
		},
	},
};

export { ExternalLinkIcon };
