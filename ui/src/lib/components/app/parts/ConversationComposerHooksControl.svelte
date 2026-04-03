<script lang="ts">
	import AlertTriangleIcon from "@lucide/svelte/icons/alert-triangle";
	import Loader2Icon from "@lucide/svelte/icons/loader-2";
	import ZapIcon from "@lucide/svelte/icons/zap";
	import { Button } from "$lib/components/ui/button";
	import { getHookDisplayState } from "$lib/session/domains/session-domain.helpers";
	import type { HooksStatus } from "$lib/shell-types";

	type Props = {
		expanded?: boolean;
		hooksStatus: HooksStatus;
	};

	let { expanded = $bindable(false), hooksStatus }: Props = $props();

	function hooks() {
		return hooksStatus.hooks;
	}

	function pendingHookSet() {
		return new Set(hooksStatus.pendingHookIds);
	}

	function isHookPassing(hook: HooksStatus["hooks"][number]) {
		return getHookDisplayState(hook, pendingHookSet()) === "success";
	}

	function hookPassedCount() {
		return hooks().filter((hook) => isHookPassing(hook)).length;
	}

	function hookHasRunning() {
		return hooks().some(
			(hook) => getHookDisplayState(hook, pendingHookSet()) === "running",
		);
	}

	function hookHasFailures() {
		return hooks().some(
			(hook) => getHookDisplayState(hook, pendingHookSet()) === "failure",
		);
	}
</script>

{#if hooks().length > 0}
	<Button
		variant="ghost"
		size="xs"
		class="h-8 gap-1.5 px-2"
		onclick={() => {
			expanded = !expanded;
		}}
	>
		{#if hookHasRunning()}
			<Loader2Icon class="size-3.5 animate-spin text-blue-500" />
		{:else if hookHasFailures()}
			<AlertTriangleIcon class="size-3.5 text-yellow-500" />
		{:else}
			<ZapIcon class="size-3.5 text-green-500" />
		{/if}
		<span class="text-xs font-medium">{hookPassedCount()}</span>
	</Button>
{/if}
