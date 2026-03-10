<script lang="ts">
	import type { ComponentProps } from "svelte";
	import { HoverCardContent } from "$lib/components/ui/hover-card";
	import { cn } from "$lib/utils";

	type Props = ComponentProps<typeof HoverCardContent> & {
		class?: string;
	};

	let { class: className, ...restProps }: Props = $props();
</script>

<HoverCardContent class={cn("relative w-80 p-0", className)} {...restProps} />
