<script lang="ts">
	import ChevronRightIcon from "@lucide/svelte/icons/chevron-right";
	import { Button } from "$lib/components/ui/button";
	import { useMessageBranchContext } from "./context";

	type Props = {
		children?: () => any;
	};

	let { children, ...restProps }: Props = $props();
	const messageBranch = useMessageBranchContext();
</script>

<Button
	aria-label="Next branch"
	disabled={messageBranch.totalBranches <= 1}
	onclick={messageBranch.goToNext}
	size="icon-sm"
	type="button"
	variant="ghost"
	{...restProps}
>
	{#if children}
		{@render children()}
	{:else}
		<ChevronRightIcon class="size-3.5" />
	{/if}
</Button>
