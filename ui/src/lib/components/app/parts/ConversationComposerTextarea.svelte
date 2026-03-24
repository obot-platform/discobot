<script lang="ts">
	import ConversationPromptHistoryDropdown from "$lib/components/app/ConversationPromptHistoryDropdown.svelte";
	import ConversationFileMentionDropdown from "$lib/components/app/parts/ConversationFileMentionDropdown.svelte";
	import { InputGroupTextarea } from "$lib/components/ui/input-group";

	type FileMentionDropdownHandle = {
		handleInput: (value: string, cursor: number) => void;
		handleKeydown: (event: KeyboardEvent) => boolean;
		closeDropdown: () => void;
	};

	type PromptHistoryDropdownHandle = {
		handleKeydown: (event: KeyboardEvent) => boolean;
		closePromptHistoryDropdown: () => void;
	};

	type Props = {
		draft: string;
		onDraftChange: (value: string) => void;
		sessionId: string | null;
		attachmentCount: number;
		onAddFiles: (files: File[] | FileList) => void;
		onRemoveLastAttachment: () => void;
		onSubmit: () => void | Promise<void>;
	};

	let {
		draft,
		onDraftChange,
		sessionId,
		attachmentCount,
		onAddFiles,
		onRemoveLastAttachment,
		onSubmit,
	}: Props = $props();

	let isComposing = $state(false);
	let fileMentionDropdownRef = $state<FileMentionDropdownHandle | null>(null);
	let promptHistoryDropdownRef = $state<PromptHistoryDropdownHandle | null>(
		null,
	);
	let fileMentionTextareaRef = $state<HTMLTextAreaElement | null>(null);

	function shouldSubmitComposerOnEnter(draft: string): boolean {
		return draft.trim().length > 0;
	}

	function handleTextareaKeydown(event: KeyboardEvent) {
		if (fileMentionDropdownRef?.handleKeydown(event)) {
			return;
		}

		if (promptHistoryDropdownRef?.handleKeydown(event)) {
			return;
		}

		if (event.key === "Enter") {
			if (isComposing || event.isComposing || event.shiftKey) {
				return;
			}
			if (!shouldSubmitComposerOnEnter(draft)) {
				return;
			}
			event.preventDefault();
			void onSubmit();
			return;
		}

		if (
			event.key === "Backspace" &&
			draft.length === 0 &&
			attachmentCount > 0
		) {
			event.preventDefault();
			onRemoveLastAttachment();
		}
	}

	function handleTextareaInput(event: Event) {
		const textarea = event.currentTarget as HTMLTextAreaElement;
		onDraftChange(textarea.value);
		promptHistoryDropdownRef?.closePromptHistoryDropdown();
		fileMentionDropdownRef?.handleInput(
			textarea.value,
			textarea.selectionStart ?? textarea.value.length,
		);
	}

	function handleTextareaPaste(event: ClipboardEvent) {
		const items = event.clipboardData?.items;
		if (!items) {
			return;
		}

		const files: File[] = [];
		for (const item of items) {
			if (item.kind !== "file") {
				continue;
			}
			const file = item.getAsFile();
			if (file) {
				files.push(file);
			}
		}

		if (files.length > 0) {
			event.preventDefault();
			onAddFiles(files);
		}
	}

	export function closeMentionDropdown() {
		fileMentionDropdownRef?.closeDropdown();
	}

	export function closePromptHistoryDropdown() {
		promptHistoryDropdownRef?.closePromptHistoryDropdown();
	}

	export function focus() {
		fileMentionTextareaRef?.focus();
	}
</script>

<ConversationFileMentionDropdown
	bind:this={fileMentionDropdownRef}
	{sessionId}
	textareaRef={fileMentionTextareaRef}
	onDraftChange={(value) => onDraftChange(value)}
/>

<ConversationPromptHistoryDropdown
	bind:this={promptHistoryDropdownRef}
	textareaRef={fileMentionTextareaRef}
	onDraftChange={(value) => onDraftChange(value)}
/>

<InputGroupTextarea
	bind:ref={fileMentionTextareaRef}
	rows={2}
	class="field-sizing-content max-h-48 min-h-16 transition-all"
	value={draft}
	placeholder="Type a message..."
	oncompositionstart={() => {
		isComposing = true;
	}}
	oncompositionend={() => {
		isComposing = false;
	}}
	onkeydown={handleTextareaKeydown}
	onpaste={handleTextareaPaste}
	oninput={handleTextareaInput}
/>
