<script lang="ts">
	import PanelLeftIcon from "@lucide/svelte/icons/panel-left";
	import { Button } from "$lib/components/ui/button";
	import type { ThreadState } from "$lib/api-types";

	type Props = {
		sidebarOpen: boolean;
		onToggleSidebar: () => void;
		title: string;
		state?: ThreadState;
	};

	let { sidebarOpen, onToggleSidebar, title, state }: Props = $props();

	function threadStateLabel(value: ThreadState | undefined) {
		if (value === "interrupted") {
			return "Interrupted";
		}
		if (value === "cancelled") {
			return "Cancelled";
		}
		return null;
	}

	function threadStateClass(value: ThreadState | undefined) {
		if (value === "interrupted") {
			return "border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300";
		}
		if (value === "cancelled") {
			return "border-muted-foreground/20 bg-muted text-muted-foreground";
		}
		return "";
	}
</script>

<div
	class="flex min-h-10 min-w-0 items-center gap-1 bg-background px-3 pb-2 pt-6"
>
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
	<div class="flex min-w-0 items-center gap-2">
		<p class="truncate text-sm font-medium">
			{title}
		</p>
		{#if threadStateLabel(state)}
			<span
				class={`inline-flex shrink-0 items-center rounded-full border px-2 py-0.5 text-[11px] font-medium ${threadStateClass(
					state,
				)}`}
			>
				{threadStateLabel(state)}
			</span>
		{/if}
	</div>
</div>
