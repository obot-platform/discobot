<script lang="ts">
	import ChevronLeftIcon from "@lucide/svelte/icons/chevron-left";
	import { Button } from "$lib/components/ui/button";
	import { useMessageBranchContext } from "./context";

	type Props = {
		children?: () => any;
	};

	let { children, ...restProps }: Props = $props();
	const messageBranch = useMessageBranchContext();
</script>

<Button
	aria-label="Previous branch"
	disabled={messageBranch.totalBranches <= 1}
	onclick={messageBranch.goToPrevious}
	size="icon-sm"
	type="button"
	variant="ghost"
	{...restProps}
>
	{#if children}
		{@render children()}
	{:else}
		<ChevronLeftIcon class="size-3.5" />
	{/if}
</Button>
