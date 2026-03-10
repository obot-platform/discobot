<script lang="ts">
	import FileIcon from "@lucide/svelte/icons/file";
	import { cn } from "$lib/utils";
	import type { Component } from "svelte";
	import FileTreeIcon from "./FileTreeIcon.svelte";
	import FileTreeName from "./FileTreeName.svelte";
	import { useFileTreeContext } from "./context";

	type Props = {
		path: string;
		name: string;
		icon?: Component<{ class?: string }>;
		class?: string;
		children?: () => any;
	};

	let {
		path,
		name,
		icon = FileIcon,
		class: className,
		children,
		...restProps
	}: Props = $props();

	const fileTree = useFileTreeContext();
	const isSelected = $derived.by(() => fileTree.selectedPath === path);
	const Icon = $derived(icon);

	function handleActivate() {
		fileTree.selectPath(path);
	}

	function handleKeydown(event: KeyboardEvent) {
		if (event.key === "Enter" || event.key === " ") {
			event.preventDefault();
			handleActivate();
		}
	}
</script>

<div
	class={cn(
		"flex cursor-pointer items-center gap-1 rounded px-2 py-1 transition-colors hover:bg-muted/50",
		isSelected && "bg-muted",
		className,
	)}
	role="treeitem"
	tabindex={0}
	onclick={handleActivate}
	onkeydown={handleKeydown}
	{...restProps}
>
	{#if children}
		{@render children()}
	{:else}
		<span class="size-4"></span>
		<FileTreeIcon>
			<Icon class="size-4 text-muted-foreground" />
		</FileTreeIcon>
		<FileTreeName>{name}</FileTreeName>
	{/if}
</div>
