<script lang="ts">
	import ChevronDownIcon from "@lucide/svelte/icons/chevron-down";
	import ChevronUpIcon from "@lucide/svelte/icons/chevron-up";
	import HistoryIcon from "@lucide/svelte/icons/history";
	import PinIcon from "@lucide/svelte/icons/pin";
	import Trash2Icon from "@lucide/svelte/icons/trash-2";
	import { MAX_VISIBLE_PROMPT_HISTORY } from "$lib/prompt-history-storage";
	import { useAppContext } from "$lib/context/app-context.svelte";

	type Props = {
		textareaRef: HTMLTextAreaElement | null;
		onDraftChange: (value: string) => void;
	};

	let { textareaRef, onDraftChange }: Props = $props();

	const app = useAppContext();

	let open = $state(false);
	let historyIndex = $state(-1);
	let isPinnedSelection = $state(false);
	let dropdownRef = $state<HTMLDivElement | null>(null);

	const visibleHistory = $derived.by(() =>
		[...app.preferences.promptHistory.slice(0, MAX_VISIBLE_PROMPT_HISTORY)].reverse(),
	);
	const pinnedPrompts = $derived.by(() => app.preferences.pinnedPrompts);
	const hasItems = $derived.by(() => pinnedPrompts.length > 0 || visibleHistory.length > 0);

	function closeDropdown() {
		open = false;
		historyIndex = -1;
		isPinnedSelection = false;
	}

	function setSelection(index: number, pinned: boolean) {
		historyIndex = index;
		isPinnedSelection = pinned;
	}

	function selectedPrompt() {
		if (historyIndex < 0) {
			return null;
		}
		return isPinnedSelection ? (pinnedPrompts[historyIndex] ?? null) : (visibleHistory[historyIndex] ?? null);
	}

	function selectPrompt(prompt: string) {
		const textarea = textareaRef;
		if (!textarea) {
			return;
		}

		textarea.value = prompt;
		onDraftChange(prompt);
		closeDropdown();
		textarea.focus();
		const cursor = textarea.value.length;
		textarea.setSelectionRange(cursor, cursor);
	}

	function pinHistoryPrompt(prompt: string) {
		app.preferences.pinPrompt(prompt);
	}

	function unpinHistoryPrompt(prompt: string) {
		app.preferences.unpinPrompt(prompt);
		if (isPinnedSelection && historyIndex >= pinnedPrompts.length) {
			historyIndex = Math.max(pinnedPrompts.length - 1, -1);
			if (historyIndex < 0) {
				isPinnedSelection = false;
			}
		}
	}

	function removeHistoryPrompt(prompt: string, index: number) {
		app.preferences.removePromptFromHistory(prompt);
		const nextVisibleHistoryLength = visibleHistory.length - 1;
		if (isPinnedSelection) {
			return;
		}
		if (nextVisibleHistoryLength <= 0) {
			if (pinnedPrompts.length > 0) {
				setSelection(pinnedPrompts.length - 1, true);
			} else {
				closeDropdown();
			}
			return;
		}
		historyIndex = Math.min(index, nextVisibleHistoryLength - 1);
	}

	export function closePromptHistoryDropdown() {
		closeDropdown();
	}

	export function handleKeydown(event: KeyboardEvent) {
		if (!hasItems) {
			return false;
		}

		if ((event.key === "Enter" || event.key === "Tab") && open && historyIndex >= 0) {
			event.preventDefault();
			const prompt = selectedPrompt();
			if (prompt) {
				selectPrompt(prompt);
			}
			return true;
		}

		if (event.key === "Escape" && open) {
			event.preventDefault();
			closeDropdown();
			return true;
		}

		const textarea = textareaRef;
		const cursorPosition = textarea?.selectionStart ?? 0;
		const pinnedLength = pinnedPrompts.length;
		const visibleHistoryLength = visibleHistory.length;

		if (event.key === "ArrowUp" && cursorPosition === 0) {
			event.preventDefault();
			if (!open) {
				open = true;
				if (visibleHistoryLength > 0) {
					setSelection(visibleHistoryLength - 1, false);
				} else {
					setSelection(pinnedLength - 1, true);
				}
				return true;
			}

			if (isPinnedSelection) {
				if (historyIndex > 0) {
					historyIndex -= 1;
				}
			} else if (historyIndex > 0) {
				historyIndex -= 1;
			} else if (pinnedLength > 0) {
				setSelection(pinnedLength - 1, true);
			}
			return true;
		}

		if (event.key === "ArrowDown" && open) {
			event.preventDefault();
			if (isPinnedSelection) {
				if (historyIndex < pinnedLength - 1) {
					historyIndex += 1;
				} else if (visibleHistoryLength > 0) {
					setSelection(0, false);
				} else {
					closeDropdown();
				}
			} else if (historyIndex < visibleHistoryLength - 1) {
				historyIndex += 1;
			} else {
				closeDropdown();
			}
			return true;
		}

		return false;
	}

	$effect(() => {
		if (!open) {
			return;
		}

		const handlePointerDown = (event: MouseEvent) => {
			const target = event.target as Node;
			if (dropdownRef?.contains(target) || textareaRef?.contains(target)) {
				return;
			}
			closeDropdown();
		};

		document.addEventListener("mousedown", handlePointerDown);
		return () => {
			document.removeEventListener("mousedown", handlePointerDown);
		};
	});

	$effect(() => {
		if (!open || !dropdownRef || historyIndex < 0) {
			return;
		}

		const selector = isPinnedSelection
			? `[data-pinned-index="${historyIndex}"]`
			: `[data-history-index="${historyIndex}"]`;
		const selectedItem = dropdownRef.querySelector(selector);
		if (selectedItem && "scrollIntoView" in selectedItem) {
			(selectedItem as HTMLElement).scrollIntoView({ block: "nearest" });
		}
	});
</script>

{#if open && hasItems}
	<div
		bind:this={dropdownRef}
		class="absolute bottom-full left-0 right-0 z-50 mb-1 flex max-h-96 flex-col overflow-hidden rounded-lg border border-border bg-popover shadow-lg"
	>
		<div class="sticky top-0 z-10 flex items-center gap-2 border-b border-border bg-popover px-3 py-2">
			<HistoryIcon class="size-4 text-muted-foreground" />
			<span class="text-xs font-medium text-muted-foreground">Prompt history</span>
			<span class="ml-auto flex items-center gap-1 text-xs text-muted-foreground">
				<ChevronUpIcon class="size-3" />
				/
				<ChevronDownIcon class="size-3" />
				navigate
			</span>
		</div>

		<div class="overflow-y-auto py-1">
			{#if pinnedPrompts.length > 0}
				<div class="px-3 py-1.5 text-xs font-medium text-muted-foreground">Pinned</div>
				{#each pinnedPrompts as prompt, index (prompt)}
					<div
						data-pinned-index={index}
						class={`group flex items-start gap-2 px-3 py-2 transition-colors ${isPinnedSelection && index === historyIndex ? "bg-accent" : "hover:bg-accent"}`}
					>
						<button
							type="button"
							class="flex-1 text-left text-sm"
							onmouseenter={() => setSelection(index, true)}
							onmousedown={(event) => {
								event.preventDefault();
								selectPrompt(prompt);
							}}
						>
							<span class="line-clamp-2 break-words">{prompt}</span>
						</button>
						<button
							type="button"
							class="shrink-0 opacity-0 transition-opacity group-hover:opacity-100"
							title="Unpin"
							onmousedown={(event) => {
								event.preventDefault();
								event.stopPropagation();
								unpinHistoryPrompt(prompt);
							}}
						>
							<PinIcon class="size-3.5 fill-current text-muted-foreground hover:text-foreground" />
						</button>
					</div>
				{/each}
			{/if}

			{#if visibleHistory.length > 0}
				<div class={`px-3 py-1.5 text-xs font-medium text-muted-foreground ${pinnedPrompts.length > 0 ? "border-t border-border" : ""}`}>
					Recent
				</div>
				{#each visibleHistory as prompt, index (prompt)}
					<div
						data-history-index={index}
						class={`group flex items-start gap-2 px-3 py-2 transition-colors ${!isPinnedSelection && index === historyIndex ? "bg-accent" : "hover:bg-accent"}`}
					>
						<button
							type="button"
							class="flex-1 text-left text-sm"
							onmouseenter={() => setSelection(index, false)}
							onmousedown={(event) => {
								event.preventDefault();
								selectPrompt(prompt);
							}}
						>
							<span class="line-clamp-2 break-words">{prompt}</span>
						</button>
						<div class="flex shrink-0 items-center gap-1 opacity-0 transition-opacity group-hover:opacity-100">
							<button
								type="button"
								title={app.preferences.isPromptPinned(prompt) ? "Unpin" : "Pin"}
								onmousedown={(event) => {
									event.preventDefault();
									event.stopPropagation();
									if (app.preferences.isPromptPinned(prompt)) {
										unpinHistoryPrompt(prompt);
									} else {
										pinHistoryPrompt(prompt);
									}
								}}
							>
								<PinIcon
									class={`size-3.5 text-muted-foreground hover:text-foreground ${app.preferences.isPromptPinned(prompt) ? "fill-current" : ""}`}
								/>
							</button>
							<button
								type="button"
								title="Delete from history"
								onmousedown={(event) => {
									event.preventDefault();
									event.stopPropagation();
									removeHistoryPrompt(prompt, index);
								}}
							>
								<Trash2Icon class="size-3.5 text-muted-foreground hover:text-foreground" />
							</button>
						</div>
					</div>
				{/each}
			{/if}
		</div>
	</div>
{/if}
