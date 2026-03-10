<script lang="ts">
	import { InputGroupTextarea } from "$lib/components/ui/input-group";
	import { cn } from "$lib/utils";
	import type { ComponentProps } from "svelte";
	import { usePromptInputContext } from "./context";

	type Props = ComponentProps<typeof InputGroupTextarea>;

	type TextareaInputEvent = Event & {
		currentTarget: EventTarget & HTMLTextAreaElement;
	};

	type TextareaKeyboardEvent = KeyboardEvent & {
		currentTarget: EventTarget & HTMLTextAreaElement;
	};

	type TextareaClipboardEvent = ClipboardEvent & {
		currentTarget: EventTarget & HTMLTextAreaElement;
	};

	let {
		placeholder = "What would you like to know?",
		class: className,
		onkeydown,
		oninput,
		onpaste,
		...restProps
	}: Props = $props();

	const promptInput = usePromptInputContext();
	let isComposing = $state(false);

	function handleInput(event: TextareaInputEvent) {
		promptInput.setText(event.currentTarget.value);
		oninput?.(event);
	}

	function handleKeydown(event: TextareaKeyboardEvent) {
		onkeydown?.(event);
		if (event.defaultPrevented) {
			return;
		}

		if (event.key === "Enter") {
			if (isComposing || event.shiftKey) {
				return;
			}
			event.preventDefault();
			promptInput.requestSubmit();
			return;
		}

		if (
			event.key === "Backspace" &&
			promptInput.text.length === 0 &&
			promptInput.files.length > 0
		) {
			event.preventDefault();
			const last = promptInput.files.at(-1);
			if (last) {
				promptInput.removeFile(last.id);
			}
		}
	}

	function handlePaste(event: TextareaClipboardEvent) {
		onpaste?.(event);
		if (event.defaultPrevented) {
			return;
		}
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
			promptInput.addFiles(files);
		}
	}
</script>

<InputGroupTextarea
	class={cn("field-sizing-content max-h-48 min-h-16", className)}
	name="message"
	value={promptInput.text}
	{placeholder}
	oncompositionstart={() => {
		isComposing = true;
	}}
	oncompositionend={() => {
		isComposing = false;
	}}
	oninput={handleInput}
	onkeydown={handleKeydown}
	onpaste={handlePaste}
	{...restProps}
/>
