<script lang="ts">
	import AlertTriangleIcon from "@lucide/svelte/icons/alert-triangle";
	import Loader2Icon from "@lucide/svelte/icons/loader-2";
	import ZapIcon from "@lucide/svelte/icons/zap";
	import { Button } from "$lib/components/ui/button";
	import { useSessionContext } from "$lib/context/session-context.svelte";

	type Props = {
		expanded?: boolean;
	};

	const session = useSessionContext();
	const sessionHooks = session.hooks;

	let { expanded = $bindable(false) }: Props = $props();

	function hooks() {
		return sessionHooks.status.hooks;
	}

	function pendingHookSet() {
		return new Set(sessionHooks.status.pendingHookIds);
	}

	function isHookPending(hookId: string) {
		return pendingHookSet().has(hookId);
	}

	function hookPassedCount() {
		return hooks().filter((hook) => hook.lastResult === "success" && !isHookPending(hook.hookId))
			.length;
	}

	function hookHasRunning() {
		return hooks().some((hook) => hook.lastResult === "running");
	}

	function hookHasFailures() {
		return hooks().some((hook) => hook.lastResult === "failure");
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
