<script lang="ts">
	import PlusIcon from "@lucide/svelte/icons/plus";
	import { cn } from "$lib/utils";

	type Props = {
		count: number;
		class?: string;
		children?: () => any;
	};

	let { count, class: className, children, ...restProps }: Props = $props();
</script>

{#if count > 0}
	<span class={cn("text-green-600 dark:text-green-400", className)} {...restProps}>
		{#if children}
			{@render children()}
		{:else}
			<PlusIcon class="inline-block size-3" />
			{count}
		{/if}
	</span>
{/if}
