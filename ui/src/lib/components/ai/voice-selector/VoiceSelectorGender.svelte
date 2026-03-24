<script lang="ts">
	import CircleSmallIcon from "@lucide/svelte/icons/circle-small";
	import MarsIcon from "@lucide/svelte/icons/mars";
	import MarsStrokeIcon from "@lucide/svelte/icons/mars-stroke";
	import NonBinaryIcon from "@lucide/svelte/icons/non-binary";
	import TransgenderIcon from "@lucide/svelte/icons/transgender";
	import VenusAndMarsIcon from "@lucide/svelte/icons/venus-and-mars";
	import VenusIcon from "@lucide/svelte/icons/venus";
	import { cn } from "$lib/utils";
	import type { Component } from "svelte";

	type VoiceGender =
		| "male"
		| "female"
		| "transgender"
		| "androgyne"
		| "non-binary"
		| "intersex";

	type Props = {
		value?: VoiceGender;
		class?: string;
		children?: () => any;
	};

	let { value, class: className, children, ...restProps }: Props = $props();

	const iconByGender: Record<VoiceGender, Component> = {
		male: MarsIcon,
		female: VenusIcon,
		transgender: TransgenderIcon,
		androgyne: MarsStrokeIcon,
		"non-binary": NonBinaryIcon,
		intersex: VenusAndMarsIcon,
	};

	const Icon = $derived.by(() =>
		value ? iconByGender[value] : CircleSmallIcon,
	);
</script>

<span class={cn("text-muted-foreground text-xs", className)} {...restProps}>
	{#if children}
		{@render children()}
	{:else}
		<Icon class="size-4" />
	{/if}
</span>
