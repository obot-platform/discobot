<script lang="ts">
	import { CommandList } from "$lib/components/ui/command";
	import MicSelectorEmpty from "./MicSelectorEmpty.svelte";
	import MicSelectorItem from "./MicSelectorItem.svelte";
	import MicSelectorLabel from "./MicSelectorLabel.svelte";
	import { useMicSelectorContext } from "./context";

	type Props = {
		children?: () => any;
	};

	let { children, ...restProps }: Props = $props();
	const micSelector = useMicSelectorContext();
</script>

<CommandList {...restProps}>
	{#if children}
		{@render children()}
	{:else if micSelector.loading}
		<div class="px-2 py-3 text-muted-foreground text-sm">Loading microphones...</div>
	{:else if micSelector.error}
		<div class="px-2 py-3 text-destructive text-sm">{micSelector.error}</div>
	{:else if micSelector.data.length === 0}
		<MicSelectorEmpty />
	{:else}
		{#each micSelector.data as device (device.deviceId)}
			<MicSelectorItem value={device.deviceId}>
				<MicSelectorLabel class="w-full" {device} />
			</MicSelectorItem>
		{/each}
	{/if}
</CommandList>
