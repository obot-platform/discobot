<script lang="ts">
	import { cn } from "$lib/utils";
	import MicSelectorLabel from "./MicSelectorLabel.svelte";
	import { useMicSelectorContext } from "./context";

	type Props = {
		class?: string;
	};

	let { class: className, ...restProps }: Props = $props();
	const micSelector = useMicSelectorContext();

	const currentDevice = $derived.by(
		() =>
			micSelector.data.find(
				(device) => device.deviceId === micSelector.value,
			) ?? null,
	);
</script>

{#if currentDevice}
	<MicSelectorLabel
		class={cn("flex-1 text-left", className)}
		device={currentDevice}
		{...restProps}
	/>
{:else}
	<span class={cn("flex-1 text-left", className)} {...restProps}
		>Select microphone...</span
	>
{/if}
