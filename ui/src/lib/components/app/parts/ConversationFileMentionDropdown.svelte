<script lang="ts">
	import FileIcon from "@lucide/svelte/icons/file";
	import FolderIcon from "@lucide/svelte/icons/folder";
	import { api } from "$lib/api-client";

	type FileMentionItem = {
		path: string;
		type: "file" | "directory";
	};

	type Props = {
		sessionId: string | null;
		textareaRef: HTMLTextAreaElement | null;
		onDraftChange: (value: string) => void;
	};

	let { sessionId, textareaRef, onDraftChange }: Props = $props();

	let open = $state(false);
	let query = $state("");
	let triggerIndex = $state(0);
	let selectedIndex = $state(0);
	let dropdownRef = $state<HTMLDivElement | null>(null);
	let suggestions = $state<FileMentionItem[]>([]);
	let isLoading = $state(false);
	let requestSequence = 0;

	const showEmpty = $derived.by(
		() => !isLoading && suggestions.length === 0 && query.length > 0,
	);
	const shouldRender = $derived.by(
		() =>
			open && !!sessionId && (isLoading || suggestions.length > 0 || showEmpty),
	);

	function updateMentionState(value: string, cursor: number) {
		const beforeCursor = value.slice(0, cursor);
		const match = beforeCursor.match(/@([^\s@]*)$/);
		if (!match) {
			open = false;
			return;
		}

		query = match[1] ?? "";
		triggerIndex = cursor - match[0].length;
		open = true;
		selectedIndex = 0;
	}

	function selectMentionPath(path: string) {
		const textarea = textareaRef;
		if (!textarea) {
			return;
		}

		const endIndex = textarea.selectionStart ?? triggerIndex;
		textarea.setRangeText(`@${path} `, triggerIndex, endIndex, "end");
		onDraftChange(textarea.value);
		open = false;
		textarea.focus();
	}

	export function handleInput(value: string, cursor: number) {
		updateMentionState(value, cursor);
	}

	export function handleKeydown(event: KeyboardEvent) {
		if (!open) {
			return false;
		}

		if (suggestions.length > 0) {
			if (event.key === "ArrowDown") {
				event.preventDefault();
				selectedIndex = Math.min(selectedIndex + 1, suggestions.length - 1);
				return true;
			}
			if (event.key === "ArrowUp") {
				event.preventDefault();
				selectedIndex = Math.max(selectedIndex - 1, 0);
				return true;
			}
			if (event.key === "Enter" || event.key === "Tab") {
				event.preventDefault();
				const selected = suggestions[selectedIndex];
				if (selected) {
					selectMentionPath(selected.path);
				}
				return true;
			}
		}

		if (event.key === "Escape") {
			event.preventDefault();
			open = false;
			return true;
		}

		return false;
	}

	export function closeDropdown() {
		open = false;
	}

	$effect(() => {
		if (!open || !sessionId) {
			suggestions = [];
			isLoading = false;
			return;
		}

		const requestId = requestSequence + 1;
		requestSequence = requestId;
		const currentQuery = query;
		const controller = new AbortController();
		const timeout = window.setTimeout(
			async () => {
				isLoading = true;
				try {
					const response = await api.searchSessionFiles(
						sessionId,
						currentQuery,
						50,
						{
							signal: controller.signal,
						},
					);
					if (requestSequence !== requestId) {
						return;
					}
					suggestions = response.results.map((result) => ({
						path: result.path,
						type: result.type,
					}));
				} catch (error) {
					if (controller.signal.aborted || requestSequence !== requestId) {
						return;
					}
					suggestions = [];
				} finally {
					if (requestSequence === requestId) {
						isLoading = false;
					}
				}
			},
			currentQuery === "" ? 0 : 80,
		);

		return () => {
			window.clearTimeout(timeout);
			controller.abort();
		};
	});

	$effect(() => {
		if (!open) {
			return;
		}

		if (suggestions.length === 0) {
			selectedIndex = 0;
			return;
		}

		if (selectedIndex > suggestions.length - 1) {
			selectedIndex = suggestions.length - 1;
		}
	});

	$effect(() => {
		if (!open || !dropdownRef) {
			return;
		}

		const selectedItem = dropdownRef.querySelector(
			`[data-index="${selectedIndex}"]`,
		);
		if (selectedItem && "scrollIntoView" in selectedItem) {
			(selectedItem as HTMLElement).scrollIntoView({ block: "nearest" });
		}
	});

	$effect(() => {
		if (!open) {
			return;
		}

		const handlePointerDown = (event: MouseEvent) => {
			const target = event.target as Node;
			if (dropdownRef?.contains(target) || textareaRef?.contains(target)) {
				return;
			}
			open = false;
		};

		document.addEventListener("mousedown", handlePointerDown);
		return () => {
			document.removeEventListener("mousedown", handlePointerDown);
		};
	});
</script>

{#if shouldRender}
	<div
		bind:this={dropdownRef}
		class="absolute bottom-full left-0 right-0 z-50 mb-1 flex max-h-64 flex-col overflow-hidden rounded-lg border border-border bg-popover shadow-lg"
	>
		<div
			class="sticky top-0 z-10 flex items-center gap-2 border-b border-border bg-popover px-3 py-2"
		>
			<FileIcon class="size-4 text-muted-foreground" />
			<span class="text-xs font-medium text-muted-foreground">Files</span>
			<span class="ml-auto text-xs text-muted-foreground"
				>↑/↓ navigate · Tab to select</span
			>
		</div>

		{#if showEmpty}
			<div class="px-3 py-3 text-xs text-muted-foreground">
				No results for &ldquo;{query}&rdquo;
			</div>
		{:else}
			<div class="overflow-y-auto py-1">
				{#each suggestions as item, index (item.path)}
					<button
						type="button"
						data-index={index}
						class={`flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm transition-colors ${index === selectedIndex ? "bg-accent" : "hover:bg-accent"}`}
						onmousedown={(event) => {
							event.preventDefault();
							selectMentionPath(item.path);
						}}
					>
						{#if item.type === "directory"}
							<FolderIcon
								class="size-3.5 shrink-0 text-blue-400 dark:text-blue-300"
							/>
						{:else}
							<FileIcon class="size-3.5 shrink-0 text-muted-foreground" />
						{/if}
						<span class="min-w-0 truncate font-mono text-xs">
							{item.path}{item.type === "directory" ? "/" : ""}
						</span>
					</button>
				{/each}
			</div>
		{/if}
	</div>
{/if}
