<script lang="ts">
	import MonitorIcon from "@lucide/svelte/icons/monitor";
	import SmartphoneIcon from "@lucide/svelte/icons/smartphone";
	import TabletIcon from "@lucide/svelte/icons/tablet";
	import { cn } from "$lib/utils";
	import { useWebPreviewContext } from "./context";
	import WebPreviewNavigationButton from "./WebPreviewNavigationButton.svelte";

	const webPreview = useWebPreviewContext();

	const VIEWPORT_OPTIONS = [
		{ value: "mobile", Icon: SmartphoneIcon, label: "Mobile (390px)" },
		{ value: "tablet", Icon: TabletIcon, label: "Tablet (768px)" },
		{ value: "desktop", Icon: MonitorIcon, label: "Desktop" },
	] as const;
</script>

{#each VIEWPORT_OPTIONS as option}
	<WebPreviewNavigationButton
		class={cn(
			webPreview.viewport === option.value && "bg-muted text-foreground",
		)}
		onclick={() => webPreview.setViewport(option.value)}
		tooltip={option.label}
	>
		<option.Icon class="h-4 w-4" />
	</WebPreviewNavigationButton>
{/each}
