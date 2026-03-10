<script lang="ts">
	import { cn } from "$lib/utils";

	type FileStatus = "added" | "modified" | "deleted" | "renamed";

	const fileStatusStyles: Record<FileStatus, string> = {
		added: "text-green-600 dark:text-green-400",
		modified: "text-yellow-600 dark:text-yellow-400",
		deleted: "text-red-600 dark:text-red-400",
		renamed: "text-blue-600 dark:text-blue-400",
	};

	const fileStatusLabels: Record<FileStatus, string> = {
		added: "A",
		modified: "M",
		deleted: "D",
		renamed: "R",
	};

	type Props = {
		status: FileStatus;
		class?: string;
		children?: () => any;
	};

	let { status, class: className, children, ...restProps }: Props = $props();
</script>

<span
	class={cn("font-medium font-mono text-xs", fileStatusStyles[status], className)}
	{...restProps}
>
	{#if children}
		{@render children()}
	{:else}
		{fileStatusLabels[status]}
	{/if}
</span>
