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

<div class="flex h-10 min-w-0 items-center gap-2 bg-background px-2">
	<Button
		variant={sidebarOpen ? "secondary" : "ghost"}
		size="icon-xs"
		onclick={onToggleSidebar}
		aria-label={sidebarOpen ? "Collapse sessions panel" : "Expand sessions panel"}
		title={sidebarOpen ? "Collapse sessions panel" : "Expand sessions panel"}
	>
		<PanelLeftIcon class="size-3.5" />
	</Button>
	<p class="truncate px-1 text-sm font-medium">
		{title}
	</p>
</div>
