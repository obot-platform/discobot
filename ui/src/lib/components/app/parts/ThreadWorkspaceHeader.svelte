<script lang="ts">
	import PanelLeftIcon from "@lucide/svelte/icons/panel-left";
	import { Button } from "$lib/components/ui/button";

	type Props = {
		sidebarOpen: boolean;
		onToggleSidebar: () => void;
		title: string;
	};

	let { sidebarOpen, onToggleSidebar, title }: Props = $props();
</script>

<div class="flex h-10 min-w-0 items-center gap-1 bg-background px-3 pt-6">
	{#if !sidebarOpen}
		<Button
			variant="ghost"
			size="icon-xs"
			onclick={onToggleSidebar}
			aria-label="Expand sessions panel"
			title="Expand sessions panel"
		>
			<PanelLeftIcon class="size-3.5" />
		</Button>
	{/if}
	<p class="truncate text-sm font-medium">
		{title}
	</p>
</div>
