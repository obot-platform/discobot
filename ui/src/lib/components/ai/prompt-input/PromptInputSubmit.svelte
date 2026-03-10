<script lang="ts">
	import CornerDownLeftIcon from "@lucide/svelte/icons/corner-down-left";
	import Loader2Icon from "@lucide/svelte/icons/loader-2";
	import PlusIcon from "@lucide/svelte/icons/plus";
	import SquareIcon from "@lucide/svelte/icons/square";
	import XIcon from "@lucide/svelte/icons/x";
	import { InputGroupButton } from "$lib/components/ui/input-group";
	import { cn } from "$lib/utils";
	import { usePromptInputContext } from "./context";

	type Props = {
		status?: "ready" | "submitted" | "streaming" | "error" | string;
		onStop?: () => void;
		onCreateSession?: () => void;
		class?: string;
		children?: () => any;
	};

	let {
		status,
		onStop,
		onCreateSession,
		class: className,
		children,
		...restProps
	}: Props = $props();

	const promptInput = usePromptInputContext();
	let isHovered = $state(false);

	const isGenerating = $derived.by(
		() => status === "submitted" || status === "streaming",
	);
	const isEmpty = $derived.by(() => !promptInput.text.trim());
	const showPlus = $derived.by(
		() => !!onCreateSession && !isGenerating && isHovered && isEmpty,
	);

	function handleClick(event: MouseEvent) {
		if (isGenerating && onStop) {
			event.preventDefault();
			onStop();
			return;
		}

		if (onCreateSession && isEmpty) {
			event.preventDefault();
			onCreateSession();
		}
	}
</script>

<InputGroupButton
	type={isGenerating && onStop ? "button" : "submit"}
	variant="default"
	size="icon-sm"
	class={cn(className)}
	onclick={handleClick}
	onmouseenter={() => {
		isHovered = true;
	}}
	onmouseleave={() => {
		isHovered = false;
	}}
	aria-label={showPlus ? "New session" : isGenerating ? "Stop" : "Submit"}
	{...restProps}
>
	{#if children}
		{@render children()}
	{:else if showPlus}
		<PlusIcon class="size-4" />
	{:else if status === "submitted"}
		<Loader2Icon class="size-4 animate-spin" />
	{:else if status === "streaming"}
		<SquareIcon class="size-4" />
	{:else if status === "error"}
		<XIcon class="size-4" />
	{:else}
		<CornerDownLeftIcon class="size-4" />
	{/if}
</InputGroupButton>
