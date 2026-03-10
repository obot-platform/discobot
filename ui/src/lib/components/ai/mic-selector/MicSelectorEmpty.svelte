<script lang="ts">
	import { CommandEmpty } from "$lib/components/ui/command";

	type Props = {
		children?: () => any;
	};

	let { children, ...restProps }: Props = $props();
</script>

<CommandEmpty {...restProps}>
	{#if children}
		{@render children()}
	{:else}
		No microphone found.
	{/if}
</CommandEmpty>
