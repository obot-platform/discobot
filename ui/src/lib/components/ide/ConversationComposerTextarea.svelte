<script lang="ts">
	import ConversationFileMentionDropdown from "$lib/components/ide/ConversationFileMentionDropdown.svelte";
	import { shouldSubmitComposerOnEnter } from "$lib/components/ide/conversation-composer.helpers";
	import { InputGroupTextarea } from "$lib/components/ui/input-group";
	import { useSessionContext } from "$lib/context/session-context.svelte";

	type FileMentionDropdownHandle = {
		handleInput: (value: string, cursor: number) => void;
		handleKeydown: (event: KeyboardEvent) => boolean;
		closeDropdown: () => void;
	};

	type Props = {
		sessionFiles: string[];
		attachmentCount: number;
		onAddFiles: (files: File[] | FileList) => void;
		onRemoveLastAttachment: () => void;
		onSubmit: () => void | Promise<void>;
	};

	let { sessionFiles, attachmentCount, onAddFiles, onRemoveLastAttachment, onSubmit }: Props =
		$props();

	const session = useSessionContext();
	const sessionView = session.ui;

	let isComposing = $state(false);
	let fileMentionDropdownRef = $state<FileMentionDropdownHandle | null>(null);
	let fileMentionTextareaRef = $state<HTMLTextAreaElement | null>(null);

	function handleTextareaKeydown(event: KeyboardEvent) {
		if (fileMentionDropdownRef?.handleKeydown(event)) {
			return;
		}

		if (event.key === "Enter") {
			if (isComposing || event.isComposing || event.shiftKey) {
				return;
			}
			if (!shouldSubmitComposerOnEnter(sessionView.composerDraft)) {
				return;
			}
			event.preventDefault();
			void onSubmit();
			return;
		}

		if (event.key === "Backspace" && sessionView.composerDraft.length === 0 && attachmentCount > 0) {
			event.preventDefault();
			onRemoveLastAttachment();
		}
	}

	function handleTextareaInput(event: Event) {
		const textarea = event.currentTarget as HTMLTextAreaElement;
		sessionView.setComposerDraft(textarea.value);
		fileMentionDropdownRef?.handleInput(textarea.value, textarea.selectionStart ?? textarea.value.length);
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
</script>

<ConversationFileMentionDropdown
	bind:this={fileMentionDropdownRef}
	files={sessionFiles}
	textareaRef={fileMentionTextareaRef}
	onDraftChange={(value) => sessionView.setComposerDraft(value)}
/>

<InputGroupTextarea
	bind:ref={fileMentionTextareaRef}
	rows={2}
	class="field-sizing-content max-h-48 min-h-16 transition-all"
	value={sessionView.composerDraft}
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
