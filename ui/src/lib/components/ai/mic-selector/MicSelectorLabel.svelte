<script lang="ts">
	const deviceIdRegex = /\(([\da-fA-F]{4}:[\da-fA-F]{4})\)$/;

	type Props = {
		device: MediaDeviceInfo;
		class?: string;
	};

	let { device, class: className, ...restProps }: Props = $props();

	const matches = $derived.by(() => device.label.match(deviceIdRegex));
	const deviceId = $derived.by(() => matches?.[1]);
	const name = $derived.by(() =>
		deviceId ? device.label.replace(deviceIdRegex, "") : device.label,
	);
</script>

<span class={className} {...restProps}>
	{#if deviceId}
		<span>{name}</span>
		<span class="text-muted-foreground"> ({deviceId})</span>
	{:else}
		{device.label}
	{/if}
</span>
