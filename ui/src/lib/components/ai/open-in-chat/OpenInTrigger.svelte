<script lang="ts">
	import ChevronDownIcon from "@lucide/svelte/icons/chevron-down";
	import { Button } from "$lib/components/ui/button";
	import { DropdownMenuTrigger } from "$lib/components/ui/dropdown-menu";

	type Props = {
		children?: () => any;
	};
	let { children, ...restProps }: Props = $props();
</script>

<DropdownMenuTrigger {...restProps}>
	{#if children}
		{@render children()}
	{:else}
		<Button type="button" variant="outline">
			Open in chat
			<ChevronDownIcon class="size-4" />
		</Button>
	{/if}
</DropdownMenuTrigger>
