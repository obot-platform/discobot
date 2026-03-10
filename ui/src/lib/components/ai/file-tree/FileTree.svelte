<script lang="ts">
	import { cn } from "$lib/utils";
	import { setFileTreeContext } from "./context";

	type Props = {
		expandedPaths?: string[];
		defaultExpandedPaths?: string[];
		selectedPath?: string;
		onSelect?: (path: string) => void;
		onExpandedChange?: (expandedPaths: string[]) => void;
		class?: string;
		children?: () => any;
	};

	let {
		expandedPaths: expandedPathsProp,
		defaultExpandedPaths = [],
		selectedPath = $bindable(undefined),
		onSelect,
		onExpandedChange,
		class: className,
		children,
		...restProps
	}: Props = $props();

	let expandedPaths = $state<string[]>([]);
	let expandedInitialized = $state(false);

	const fileTree = $state({
		expandedPaths: [] as string[],
		togglePath: (path: string) => {
			const current = expandedPathsProp ?? expandedPaths;
			const next = current.includes(path)
				? current.filter((item) => item !== path)
				: [...current, path];
			expandedPaths = next;
			onExpandedChange?.(next);
		},
		selectedPath: undefined as string | undefined,
		selectPath: (path: string) => {
			selectedPath = path;
			onSelect?.(path);
		},
	});

	$effect(() => {
		if (!expandedInitialized) {
			expandedPaths = [...defaultExpandedPaths];
			expandedInitialized = true;
		}
	});

	$effect(() => {
		fileTree.expandedPaths = expandedPathsProp ?? expandedPaths;
		fileTree.selectedPath = selectedPath;
	});

	setFileTreeContext(fileTree);
</script>

<div
	class={cn("rounded-lg border bg-background font-mono text-sm", className)}
	role="tree"
	{...restProps}
>
	<div class="p-2">{@render children?.()}</div>
</div>
