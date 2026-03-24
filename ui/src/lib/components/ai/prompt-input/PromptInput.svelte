<script lang="ts">
	import { nanoid } from "nanoid";
	import { onDestroy } from "svelte";
	import { InputGroup } from "$lib/components/ui/input-group";
	import { cn } from "$lib/utils";
	import {
		type PromptInputContextValue,
		type PromptInputFile,
		type PromptInputSubmitMessage,
		setPromptInputContext,
	} from "./context";

	type PromptInputError = {
		code: "max_files" | "max_file_size" | "accept";
		message: string;
	};

	type Props = {
		accept?: string;
		multiple?: boolean;
		maxFiles?: number;
		maxFileSize?: number;
		globalDrop?: boolean;
		onError?: (error: PromptInputError) => void;
		onSubmit: (
			message: PromptInputSubmitMessage,
			event: SubmitEvent,
		) => void | Promise<void>;
		class?: string;
		children?: () => any;
	};

	let {
		accept,
		multiple,
		maxFiles,
		maxFileSize,
		globalDrop = false,
		onError,
		onSubmit,
		class: className,
		children,
		...restProps
	}: Props = $props();

	let formRef = $state<HTMLFormElement | null>(null);
	let inputRef = $state<HTMLInputElement | null>(null);
	let text = $state("");
	let files = $state<PromptInputFile[]>([]);

	function matchesAccept(file: File): boolean {
		if (!accept || accept.trim() === "") {
			return true;
		}
		const patterns = accept
			.split(",")
			.map((part) => part.trim())
			.filter(Boolean);

		for (const pattern of patterns) {
			if (pattern.endsWith("/*")) {
				const prefix = pattern.slice(0, -1);
				if (file.type.startsWith(prefix)) {
					return true;
				}
			} else if (file.type === pattern) {
				return true;
			}
		}
		return false;
	}

	function addFiles(fileList: File[] | FileList) {
		const incoming = Array.from(fileList);
		const accepted = incoming.filter((file) => matchesAccept(file));
		if (incoming.length > 0 && accepted.length === 0) {
			onError?.({
				code: "accept",
				message: "No files match the accepted types.",
			});
			return;
		}

		const sized = accepted.filter((file) =>
			maxFileSize ? file.size <= maxFileSize : true,
		);
		if (accepted.length > 0 && sized.length === 0) {
			onError?.({
				code: "max_file_size",
				message: "All files exceed the maximum size.",
			});
			return;
		}

		const capacity =
			typeof maxFiles === "number"
				? Math.max(0, maxFiles - files.length)
				: undefined;
		const capped =
			typeof capacity === "number" ? sized.slice(0, capacity) : sized;

		if (typeof capacity === "number" && sized.length > capacity) {
			onError?.({
				code: "max_files",
				message: "Too many files. Some were not added.",
			});
		}

		files = [
			...files,
			...capped.map((file) => ({
				id: nanoid(),
				type: "file" as const,
				url: URL.createObjectURL(file),
				mediaType: file.type,
				filename: file.name,
			})),
		];
	}

	function removeFile(id: string) {
		const found = files.find((file) => file.id === id);
		if (found?.url) {
			URL.revokeObjectURL(found.url);
		}
		files = files.filter((file) => file.id !== id);
	}

	function clearFiles() {
		for (const file of files) {
			if (file.url) {
				URL.revokeObjectURL(file.url);
			}
		}
		files = [];
	}

	function openFileDialog() {
		inputRef?.click();
	}

	function requestSubmit() {
		formRef?.requestSubmit();
	}

	async function handleSubmit(event: SubmitEvent) {
		event.preventDefault();
		const message: PromptInputSubmitMessage = {
			text,
			files: files.map(({ id: _id, ...file }) => file),
		};

		try {
			const result = onSubmit(message, event);
			if (result instanceof Promise) {
				await result;
			}
			text = "";
			clearFiles();
			if (inputRef) {
				inputRef.value = "";
			}
		} catch {
			// keep input for retry
		}
	}

	function handleFileInputChange(event: Event) {
		const target = event.currentTarget as HTMLInputElement;
		if (target.files) {
			addFiles(target.files);
		}
		target.value = "";
	}

	function handleLocalDrop(event: DragEvent) {
		if (!event.dataTransfer?.types?.includes("Files")) {
			return;
		}
		event.preventDefault();
		if (event.dataTransfer.files.length > 0) {
			addFiles(event.dataTransfer.files);
		}
	}

	function handleLocalDragOver(event: DragEvent) {
		if (event.dataTransfer?.types?.includes("Files")) {
			event.preventDefault();
		}
	}

	$effect(() => {
		if (!globalDrop) {
			return;
		}
		document.addEventListener("dragover", handleLocalDragOver);
		document.addEventListener("drop", handleLocalDrop);
		return () => {
			document.removeEventListener("dragover", handleLocalDragOver);
			document.removeEventListener("drop", handleLocalDrop);
		};
	});

	onDestroy(() => {
		clearFiles();
	});

	const promptInput = $state<PromptInputContextValue>({
		text: "",
		setText: (value: string) => {
			text = value;
		},
		files: [],
		addFiles,
		removeFile,
		clearFiles,
		openFileDialog,
		requestSubmit,
	});

	$effect(() => {
		promptInput.text = text;
		promptInput.files = files;
	});

	setPromptInputContext(promptInput);
</script>

<input
	bind:this={inputRef}
	type="file"
	class="hidden"
	{accept}
	{multiple}
	onchange={handleFileInputChange}
/>

<form
	bind:this={formRef}
	class={cn("w-full", className)}
	onsubmit={handleSubmit}
	ondragover={globalDrop ? undefined : handleLocalDragOver}
	ondrop={globalDrop ? undefined : handleLocalDrop}
	{...restProps}
>
	<InputGroup class="overflow-hidden">
		{@render children?.()}
	</InputGroup>
</form>
