<script lang="ts">
	import { cn } from "$lib/utils";
	import DiscobotLogo from "$lib/components/ide/DiscobotLogo.svelte";

	type Props = {
		class?: string;
		logoClass?: string;
		logoSize?: number;
		textSizeClass?: string;
	};

	let {
		class: className,
		logoClass,
		logoSize = 22,
		textSizeClass = "text-base",
	}: Props = $props();
</script>

<div class={cn("flex items-center gap-1 shrink-0", className)}>
	<DiscobotLogo size={logoSize} class={cn("text-purple-500", logoClass)} />
	<span
		class={cn(
			"bg-gradient-to-r from-purple-500 to-pink-500 bg-clip-text font-bold text-transparent",
			textSizeClass,
		)}
	>
		DISCOBOT
	</span>
</div>
