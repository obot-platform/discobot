<script lang="ts">
	import ImageIcon from "@lucide/svelte/icons/image";
	import { DropdownMenuItem } from "$lib/components/ui/dropdown-menu";
	import { usePromptInputContext } from "./context";

	type Props = {
		label?: string;
	};

	let { label = "Add photos or files", ...restProps }: Props = $props();
	const promptInput = usePromptInputContext();

	function handleSelect(event: Event) {
		event.preventDefault();
		promptInput.openFileDialog();
	}
</script>

<DropdownMenuItem onclick={handleSelect} {...restProps}>
	<ImageIcon class="mr-2 size-4" />
	{label}
</DropdownMenuItem>
