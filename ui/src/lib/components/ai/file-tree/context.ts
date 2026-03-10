import { getContext, setContext } from "svelte";

const FILE_TREE_CONTEXT_KEY = Symbol.for("discobot-ui-ai-file-tree-context");

export type FileTreeContextValue = {
	expandedPaths: string[];
	togglePath: (path: string) => void;
	selectedPath?: string;
	selectPath: (path: string) => void;
};

export function setFileTreeContext(
	value: FileTreeContextValue,
): FileTreeContextValue {
	return setContext(FILE_TREE_CONTEXT_KEY, value);
}

export function useFileTreeContext(): FileTreeContextValue {
	const context = getContext<FileTreeContextValue | undefined>(
		FILE_TREE_CONTEXT_KEY,
	);
	if (!context) {
		throw new Error("FileTree components must be used within FileTree");
	}
	return context;
}
