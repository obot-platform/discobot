<script lang="ts">
	import logoAssetUrl from "./branding-assets/discobot-logo-purple.svg";
	import { cn } from "$lib/utils";

	type Props = {
		class?: string;
		title?: string;
		size?: number;
	};

	let {
		class: className,
		title = "Discobot logo",
		size = 24,
	}: Props = $props();
</script>

<img
	src={logoAssetUrl}
	alt={title}
	{title}
	width={size}
	height={size}
	class={cn("block shrink-0", className)}
/>
