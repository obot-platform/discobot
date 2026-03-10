<script lang="ts">
	import PaperclipIcon from "@lucide/svelte/icons/paperclip";
	import { cn } from "$lib/utils";

	type Props = {
		class?: string;
		children?: () => any;
	};

	let { class: className, children, ...restProps }: Props = $props();
</script>

<span
	class={cn(
		"flex items-center gap-1 rounded border bg-muted px-2 py-1 text-xs",
		className,
	)}
	{...restProps}
>
	<PaperclipIcon class="size-3" />
	<span class="max-w-[100px] truncate">{@render children?.()}</span>
</span>
