<script lang="ts">
	import LoaderCircleIcon from "@lucide/svelte/icons/loader-circle";
	import PauseIcon from "@lucide/svelte/icons/pause";
	import PlayIcon from "@lucide/svelte/icons/play";
	import { Button } from "$lib/components/ui/button";
	import { cn } from "$lib/utils";

	type Props = {
		playing?: boolean;
		loading?: boolean;
		onPlay?: () => void;
		onclick?: (event: MouseEvent) => void;
		class?: string;
	};

	let {
		playing = false,
		loading = false,
		onPlay,
		onclick,
		class: className,
		...restProps
	}: Props = $props();

	function handleClick(event: MouseEvent) {
		event.stopPropagation();
		onclick?.(event);
		onPlay?.();
	}
</script>

<Button
	aria-label={playing ? "Pause preview" : "Play preview"}
	class={cn("size-6", className)}
	disabled={loading}
	onclick={handleClick}
	size="icon-sm"
	type="button"
	variant="outline"
	{...restProps}
>
	{#if loading}
		<LoaderCircleIcon class="size-3 animate-spin" />
	{:else if playing}
		<PauseIcon class="size-3" />
	{:else}
		<PlayIcon class="size-3" />
	{/if}
</Button>
