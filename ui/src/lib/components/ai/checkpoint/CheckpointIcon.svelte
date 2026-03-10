<script lang="ts">
	import BookmarkIcon from "@lucide/svelte/icons/bookmark";
	import { cn } from "$lib/utils";

	type Props = {
		class?: string;
		children?: () => any;
	};

	let { class: className, children, ...restProps }: Props = $props();
</script>

{#if children}
	{@render children()}
{:else}
	<BookmarkIcon class={cn("size-4 shrink-0", className)} {...restProps} />
{/if}
