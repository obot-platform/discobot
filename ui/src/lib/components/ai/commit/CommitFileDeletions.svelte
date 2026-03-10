<script lang="ts">
	import MinusIcon from "@lucide/svelte/icons/minus";
	import { cn } from "$lib/utils";

	type Props = {
		count: number;
		class?: string;
		children?: () => any;
	};

	let { count, class: className, children, ...restProps }: Props = $props();
</script>

{#if count > 0}
	<span class={cn("text-red-600 dark:text-red-400", className)} {...restProps}>
		{#if children}
			{@render children()}
		{:else}
			<MinusIcon class="inline-block size-3" />
			{count}
		{/if}
	</span>
{/if}
