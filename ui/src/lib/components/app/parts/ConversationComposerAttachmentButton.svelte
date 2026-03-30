<script lang="ts">
	import PaperclipIcon from "@lucide/svelte/icons/paperclip";
	import {
		DropdownMenu,
		DropdownMenuContent,
		DropdownMenuItem,
		DropdownMenuTrigger,
	} from "$lib/components/ui/dropdown-menu";
	import { InputGroupButton } from "$lib/components/ui/input-group";

	type Props = {
		onFilesAdd: (files: File[] | FileList) => void;
		disabled?: boolean;
	};

	let { onFilesAdd, disabled = false }: Props = $props();

	let fileInputRef = $state<HTMLInputElement | null>(null);

	function openFileDialog() {
		if (disabled) {
			return;
		}
		fileInputRef?.click();
	}

	function handleFileInputChange(event: Event) {
		const input = event.currentTarget as HTMLInputElement;
		if (input.files) {
			onFilesAdd(input.files);
		}
		input.value = "";
	}
</script>

<input
	bind:this={fileInputRef}
	type="file"
	class="hidden"
	multiple
	{disabled}
	onchange={handleFileInputChange}
/>

<DropdownMenu>
	<DropdownMenuTrigger class="tauri-no-drag" {disabled}>
		<InputGroupButton
			size="icon-sm"
			variant="ghost"
			aria-label="Attachment actions"
			{disabled}
		>
			<PaperclipIcon class="size-4" />
		</InputGroupButton>
	</DropdownMenuTrigger>
	<DropdownMenuContent align="start" class="w-48">
		<DropdownMenuItem onclick={openFileDialog}
			>Add photos or files</DropdownMenuItem
		>
	</DropdownMenuContent>
</DropdownMenu>
