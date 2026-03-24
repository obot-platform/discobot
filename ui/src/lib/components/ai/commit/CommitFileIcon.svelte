<script lang="ts">
	import FileIcon from "@lucide/svelte/icons/file";
	import { cn } from "$lib/utils";

	type Props = {
		class?: string;
	};

	let { class: className, ...restProps }: Props = $props();
</script>

<FileIcon
	class={cn("size-3.5 shrink-0 text-muted-foreground", className)}
	{...restProps}
/>
