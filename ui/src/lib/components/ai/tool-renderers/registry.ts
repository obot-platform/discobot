import type { Component } from "svelte";
import type { DynamicToolPart } from "$lib/components/ai/types";
import AskUserQuestionToolRenderer from "./AskUserQuestionToolRenderer.svelte";
import BashToolRenderer from "./BashToolRenderer.svelte";
import EditToolRenderer from "./EditToolRenderer.svelte";
import GlobToolRenderer from "./GlobToolRenderer.svelte";
import GrepToolRenderer from "./GrepToolRenderer.svelte";
import ReadToolRenderer from "./ReadToolRenderer.svelte";
import SkillToolRenderer from "./SkillToolRenderer.svelte";
import TaskToolRenderer from "./TaskToolRenderer.svelte";
import TodoWriteToolRenderer from "./TodoWriteToolRenderer.svelte";
import WebFetchToolRenderer from "./WebFetchToolRenderer.svelte";
import WebSearchToolRenderer from "./WebSearchToolRenderer.svelte";
import WriteToolRenderer from "./WriteToolRenderer.svelte";
import type { ToolRendererComponentProps } from "./types";
import { shortenPath } from "./utils";

type RendererComponent = Component<ToolRendererComponentProps>;

const TOOL_RENDERERS: Record<string, RendererComponent> = {
	AskUserQuestion: AskUserQuestionToolRenderer,
	Bash: BashToolRenderer,
	Read: ReadToolRenderer,
	Write: WriteToolRenderer,
	Edit: EditToolRenderer,
	Grep: GrepToolRenderer,
	Glob: GlobToolRenderer,
	WebSearch: WebSearchToolRenderer,
	WebFetch: WebFetchToolRenderer,
	TodoWrite: TodoWriteToolRenderer,
	Task: TaskToolRenderer,
	Skill: SkillToolRenderer,
};

export function getToolRenderer(
	toolName: string,
): RendererComponent | undefined {
	return TOOL_RENDERERS[toolName];
}

export function hasOptimizedRenderer(toolName: string): boolean {
	return getToolRenderer(toolName) !== undefined;
}

export function getToolTitle(toolPart: DynamicToolPart): string | undefined {
	const { toolName, input } = toolPart;

	if (toolPart.title) {
		return toolPart.title;
	}

	if (!input || typeof input !== "object") {
		return undefined;
	}

	const safeInput = input as Record<string, unknown>;

	switch (toolName) {
		case "Bash": {
			const command = safeInput.command;
			if (typeof command === "string") {
				const truncated =
					command.length > 60 ? `${command.slice(0, 60)}...` : command;
				return `Run: ${truncated}`;
			}
			break;
		}

		case "Read":
		case "Write":
		case "Edit": {
			const filePath = safeInput.file_path;
			if (typeof filePath === "string") {
				const fileName = filePath.split("/").pop() || filePath;
				return `${toolName}: ${fileName}`;
			}
			break;
		}

		case "Grep":
		case "Glob": {
			const pattern = safeInput.pattern;
			if (typeof pattern === "string") {
				const truncated =
					pattern.length > 50 ? `${pattern.slice(0, 50)}...` : pattern;
				return `${toolName === "Grep" ? "Search" : "Find"}: ${truncated}`;
			}
			break;
		}

		case "WebSearch": {
			const query = safeInput.query;
			if (typeof query === "string") {
				const truncated =
					query.length > 50 ? `${query.slice(0, 50)}...` : query;
				return `Search: ${truncated}`;
			}
			break;
		}

		case "WebFetch": {
			const url = safeInput.url;
			if (typeof url === "string") {
				try {
					const hostname = new URL(url).hostname;
					return `Fetch: ${hostname}`;
				} catch {
					const truncated = url.length > 50 ? `${url.slice(0, 50)}...` : url;
					return `Fetch: ${truncated}`;
				}
			}
			break;
		}

		case "TodoWrite": {
			const todos = safeInput.todos;
			if (Array.isArray(todos)) {
				return `Track: ${todos.length} ${todos.length === 1 ? "task" : "tasks"}`;
			}
			break;
		}

		case "Task": {
			const description = safeInput.description;
			if (typeof description === "string") {
				const truncated =
					description.length > 50
						? `${description.slice(0, 50)}...`
						: description;
				return `Launch: ${truncated}`;
			}
			break;
		}

		case "Skill": {
			const skill = safeInput.skill;
			if (typeof skill === "string") {
				return `Run: ${skill}`;
			}
			break;
		}

		case "AskUserQuestion": {
			const questions = safeInput.questions;
			if (Array.isArray(questions) && questions.length > 0) {
				const first = questions[0];
				if (first && typeof first === "object") {
					const header = (first as Record<string, unknown>).header;
					if (typeof header === "string") {
						return questions.length > 1
							? `Question: ${header} (+${questions.length - 1} more)`
							: `Question: ${header}`;
					}
				}
			}
			return "Agent Question";
		}
	}

	return undefined;
}

export { shortenPath };
