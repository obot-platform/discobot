<script lang="ts">
	import type { ComponentProps } from "svelte";
	import { CollapsibleContent } from "$lib/components/ui/collapsible";
	import { cn } from "$lib/utils";

	type Props = ComponentProps<typeof CollapsibleContent> & {
		class?: string;
	};

	let { class: className, ...restProps }: Props = $props();
</script>

<CollapsibleContent
	class={cn(
		"data-[state=closed]:fade-out-0 data-[state=closed]:slide-out-to-top-2 data-[state=open]:slide-in-from-top-2 outline-none data-[state=closed]:animate-out data-[state=open]:animate-in",
		className,
	)}
	{...restProps}
/>
