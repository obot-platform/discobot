<script lang="ts">
	import ChevronRightIcon from "@lucide/svelte/icons/chevron-right";
	import FolderIcon from "@lucide/svelte/icons/folder";
	import FolderOpenIcon from "@lucide/svelte/icons/folder-open";
	import {
		Collapsible,
		CollapsibleContent,
		CollapsibleTrigger,
	} from "$lib/components/ui/collapsible";
	import { cn } from "$lib/utils";
	import FileTreeIcon from "./FileTreeIcon.svelte";
	import FileTreeName from "./FileTreeName.svelte";
	import { useFileTreeContext } from "./context";

	type Props = {
		path: string;
		name: string;
		class?: string;
		children?: () => any;
	};

	let {
		path,
		name,
		class: className,
		children,
		...restProps
	}: Props = $props();

	const fileTree = useFileTreeContext();
	const isExpanded = $derived.by(() => fileTree.expandedPaths.includes(path));
	const isSelected = $derived.by(() => fileTree.selectedPath === path);
</script>

<Collapsible open={isExpanded} onOpenChange={() => fileTree.togglePath(path)}>
	<div class={cn("", className)} role="treeitem" {...restProps}>
		<CollapsibleTrigger class="w-full">
			<button
				type="button"
				class={cn(
					"flex w-full items-center gap-1 rounded px-2 py-1 text-left transition-colors hover:bg-muted/50",
					isSelected && "bg-muted",
				)}
				onclick={() => fileTree.selectPath(path)}
			>
				<ChevronRightIcon
					class={cn(
						"size-4 shrink-0 text-muted-foreground transition-transform",
						isExpanded && "rotate-90",
					)}
				/>
				<FileTreeIcon>
					{#if isExpanded}
						<FolderOpenIcon class="size-4 text-blue-500" />
					{:else}
						<FolderIcon class="size-4 text-blue-500" />
					{/if}
				</FileTreeIcon>
				<FileTreeName>{name}</FileTreeName>
			</button>
		</CollapsibleTrigger>
		<CollapsibleContent>
			<div class="ml-4 border-l pl-2">
				{@render children?.()}
			</div>
		</CollapsibleContent>
	</div>
</Collapsible>
