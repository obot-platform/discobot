<script lang="ts">
	import PlusIcon from "@lucide/svelte/icons/plus";
	import { DropdownMenuTrigger } from "$lib/components/ui/dropdown-menu";
	import PromptInputButton from "./PromptInputButton.svelte";

	type Props = { class?: string; children?: () => any };
	let { class: className, children, ...restProps }: Props = $props();
</script>

<DropdownMenuTrigger>
	<PromptInputButton class={className} {...restProps}>
		{#if children}
			{@render children()}
		{:else}
			<PlusIcon class="size-4" />
		{/if}
	</PromptInputButton>
</DropdownMenuTrigger>
