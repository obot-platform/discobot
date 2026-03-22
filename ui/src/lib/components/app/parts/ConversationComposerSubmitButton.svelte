<script lang="ts">
	import CornerDownLeftIcon from "@lucide/svelte/icons/corner-down-left";
	import Loader2Icon from "@lucide/svelte/icons/loader-2";
	import PlusIcon from "@lucide/svelte/icons/plus";
	import SquareIcon from "@lucide/svelte/icons/square";
	import XIcon from "@lucide/svelte/icons/x";
	import { InputGroupButton } from "$lib/components/ui/input-group";
	import type { ComposerStatus } from "../conversation-composer.types";

	type Props = {
		status: ComposerStatus;
		inputEmpty: boolean;
		disabled?: boolean;
		onPress: () => void | Promise<void>;
	};

	let { status, inputEmpty, disabled = false, onPress }: Props = $props();

	let hovered = $state(false);

	const isGenerating = $derived.by(() => status === "submitted" || status === "streaming");
	const showPlusIcon = $derived.by(() => hovered && inputEmpty && !isGenerating);
</script>

<InputGroupButton
	type={isGenerating ? "button" : "submit"}
	variant="default"
	size="icon-sm"
	{disabled}
	onclick={(event) => {
		event.preventDefault();
		void onPress();
	}}
	onmouseenter={() => {
		hovered = true;
	}}
	onmouseleave={() => {
		hovered = false;
	}}
	aria-label={showPlusIcon ? "New session" : isGenerating ? "Stop" : "Submit"}
>
	{#if showPlusIcon}
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
