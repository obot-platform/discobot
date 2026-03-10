<script lang="ts">
	import AlertTriangleIcon from "@lucide/svelte/icons/alert-triangle";
	import CheckCircleIcon from "@lucide/svelte/icons/check-circle";
	import CornerDownLeftIcon from "@lucide/svelte/icons/corner-down-left";
	import Loader2Icon from "@lucide/svelte/icons/loader-2";
	import PaperclipIcon from "@lucide/svelte/icons/paperclip";
	import PlusIcon from "@lucide/svelte/icons/plus";
	import SquareIcon from "@lucide/svelte/icons/square";
	import XIcon from "@lucide/svelte/icons/x";
	import ZapIcon from "@lucide/svelte/icons/zap";
	import { onDestroy } from "svelte";
	import { Button } from "$lib/components/ui/button";
	import ConversationEnvSetsControl from "$lib/components/ide/ConversationEnvSetsControl.svelte";
	import ConversationFileMentionDropdown from "$lib/components/ide/ConversationFileMentionDropdown.svelte";
	import ConversationHooksPanel from "$lib/components/ide/ConversationHooksPanel.svelte";
	import ConversationQueuePanel from "$lib/components/ide/ConversationQueuePanel.svelte";
	import {
		DropdownMenu,
		DropdownMenuContent,
		DropdownMenuItem,
		DropdownMenuTrigger,
	} from "$lib/components/ui/dropdown-menu";
	import {
		InputGroup,
		InputGroupAddon,
		InputGroupButton,
		InputGroupTextarea,
	} from "$lib/components/ui/input-group";
	import { useAppContext } from "$lib/context/app-context.svelte";
	import { useSessionContext } from "$lib/context/session-context.svelte";
	import { useThreadContext } from "$lib/context/thread-context.svelte";

	type ComposerStatus = "ready" | "submitted" | "streaming" | "error";
	type ComposerAttachment = {
		id: string;
		filename: string;
		mediaType: string;
		url: string;
	};
	type FileMentionDropdownHandle = {
		handleInput: (value: string, cursor: number) => void;
		handleKeydown: (event: KeyboardEvent) => boolean;
		closeDropdown: () => void;
	};

	const app = useAppContext();
	const session = useSessionContext();
	const thread = useThreadContext();
	const threadUi = thread.ui;
	const sessionThreads = session.threads;
	const sessionHooks = session.hooks;
	const sessionFiles = $derived.by(() => session.files);

	let isComposing = $state(false);
	let submitHovered = $state(false);
	let submitStatus = $state<ComposerStatus>("ready");
	let attachmentFiles = $state<ComposerAttachment[]>([]);
	let fileInputRef = $state<HTMLInputElement | null>(null);
	let submitTimeout = $state<ReturnType<typeof setTimeout> | null>(null);
	let hooksExpanded = $state(false);
	let queueExpanded = $state(false);
	let fileMentionDropdownRef = $state<FileMentionDropdownHandle | null>(null);
	let fileMentionTextareaRef = $state<HTMLTextAreaElement | null>(null);

	function activeToolLabel() {
		if (threadUi.centerPanel === "chat") {
			return "No tool selected";
		}

		if (threadUi.centerPanel === "diff-review") {
			return "diff review";
		}

		return threadUi.centerPanel.replace("service:", "service ");
	}

	function conversationItems() {
		if (thread.conversation.length > 0) {
			return thread.conversation;
		}

		return (
			[
				{
					id: "fallback-user",
					role: "user" as const,
					text: `Can we make ${sessionThreads.selected?.name ?? session.current?.name ?? "this session"} feel more like an assistant-led IDE than a dashboard?`,
				},
				{
					id: "fallback-assistant-1",
					role: "assistant" as const,
					text: "Yes — keep the conversation timeline central, keep the active thread visible, and let tools dock beside the chat instead of replacing it.",
				},
				{
					id: "fallback-assistant-2",
					role: "assistant" as const,
					text: "That way terminal, desktop preview, files, and services stay available without losing the conversational flow.",
				},
			]
		);
	}

	function planEntries() {
		return thread.planEntries;
	}

	function hooks() {
		return sessionHooks.status.hooks;
	}

	function pendingHookSet() {
		return new Set(sessionHooks.status.pendingHookIds);
	}

	function isHookPending(hookId: string) {
		return pendingHookSet().has(hookId);
	}

	function queueCompletedCount() {
		return planEntries().filter((entry) => entry.status === "completed").length;
	}

	function queueTotalCount() {
		return planEntries().length;
	}

	function hookPassedCount() {
		return hooks().filter((hook) => hook.lastResult === "success" && !isHookPending(hook.hookId))
			.length;
	}

	function hookHasRunning() {
		return hooks().some((hook) => hook.lastResult === "running");
	}

	function hookHasFailures() {
		return hooks().some((hook) => hook.lastResult === "failure");
	}

	function isGenerating() {
		return submitStatus === "submitted" || submitStatus === "streaming";
	}

	function inputEmpty() {
		return threadUi.composerDraft.trim().length === 0;
	}

	function showPlusIcon() {
		return submitHovered && inputEmpty() && !isGenerating();
	}

	function openFileDialog() {
		fileInputRef?.click();
	}

	function addFiles(files: File[] | FileList) {
		const incoming = Array.from(files);
		if (incoming.length === 0) {
			return;
		}

		attachmentFiles = attachmentFiles.concat(
			incoming.map((file) => ({
				id: `${Date.now()}-${Math.floor(Math.random() * 10_000)}`,
				filename: file.name,
				mediaType: file.type,
				url: URL.createObjectURL(file),
			})),
		);
	}

	function removeAttachment(id: string) {
		const target = attachmentFiles.find((item) => item.id === id);
		if (target?.url) {
			URL.revokeObjectURL(target.url);
		}
		attachmentFiles = attachmentFiles.filter((item) => item.id !== id);
	}

	function clearAttachments() {
		for (const file of attachmentFiles) {
			if (file.url) {
				URL.revokeObjectURL(file.url);
			}
		}
		attachmentFiles = [];
	}

	function clearSubmitTimeout() {
		if (!submitTimeout) {
			return;
		}
		clearTimeout(submitTimeout);
		submitTimeout = null;
	}

	function stopSubmitCycle() {
		clearSubmitTimeout();
		submitStatus = "ready";
		fileMentionDropdownRef?.closeDropdown();
	}

	function runSubmitCycle() {
		submitStatus = "submitted";
		clearSubmitTimeout();
		submitTimeout = setTimeout(() => {
			submitStatus = "streaming";
			submitTimeout = setTimeout(() => {
				submitStatus = "ready";
				submitTimeout = null;
			}, 1000);
		}, 240);
	}

	function submitComposer() {
		if (isGenerating()) {
			stopSubmitCycle();
			return;
		}

		if (inputEmpty() && attachmentFiles.length === 0) {
			sessionThreads.create();
			return;
		}

		threadUi.setComposerDraft("");
		fileMentionDropdownRef?.closeDropdown();
		clearAttachments();
		runSubmitCycle();
	}

	function handleFileInputChange(event: Event) {
		const input = event.currentTarget as HTMLInputElement;
		if (input.files) {
			addFiles(input.files);
		}
		input.value = "";
	}

	function handleTextareaKeydown(event: KeyboardEvent) {
		if (fileMentionDropdownRef?.handleKeydown(event)) {
			return;
		}

		if (event.key === "Enter") {
			if (isComposing || event.isComposing || event.shiftKey) {
				return;
			}
			event.preventDefault();
			submitComposer();
			return;
		}

		if (
			event.key === "Backspace" &&
			threadUi.composerDraft.length === 0 &&
			attachmentFiles.length > 0
		) {
			event.preventDefault();
			const lastAttachment = attachmentFiles.at(-1);
			if (lastAttachment) {
				removeAttachment(lastAttachment.id);
			}
		}
	}

	function handleTextareaInput(event: Event) {
		const textarea = event.currentTarget as HTMLTextAreaElement;
		threadUi.setComposerDraft(textarea.value);
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
			addFiles(files);
		}
	}

	onDestroy(() => {
		clearSubmitTimeout();
		clearAttachments();
	});
</script>

<div class="flex h-full min-h-0 flex-col overflow-hidden bg-background">
	<div class="flex-1 overflow-auto p-4">
		<div
			class={`w-full space-y-4 ${app.chatWidthMode === "constrained" ? "mx-auto max-w-3xl" : ""}`}
		>
			{#each conversationItems() as message (message.id)}
				<div
					class={`max-w-[72%] rounded-lg bg-secondary px-4 py-3 text-sm leading-6 text-foreground ${message.role === "assistant" ? "" : "ms-auto"}`}
				>
					{message.text}
				</div>
			{/each}
			<div class="grid gap-3 md:grid-cols-3">
				<div class="rounded-md border border-border bg-card p-3 text-sm">
					<p class="font-medium">Thread context</p>
					<p class="mt-1 text-muted-foreground">Thread + session always visible</p>
				</div>
				<div class="rounded-md border border-border bg-card p-3 text-sm">
					<p class="font-medium">Primary canvas</p>
					<p class="mt-1 text-muted-foreground">Conversation + inline tool flow</p>
				</div>
				<div class="rounded-md border border-border bg-card p-3 text-sm">
					<p class="font-medium">Docked tools</p>
					<p class="mt-1 capitalize text-muted-foreground">{activeToolLabel()}</p>
				</div>
			</div>
		</div>
	</div>

	<div class="shrink-0 bg-background p-0 md:p-3">
		<div
			class={`w-full ${app.chatWidthMode === "constrained" ? "md:mx-auto md:max-w-3xl" : ""}`}
		>
			<input
				bind:this={fileInputRef}
				type="file"
				class="hidden"
				multiple
				onchange={handleFileInputChange}
			/>

			<ConversationQueuePanel expanded={queueExpanded} entries={planEntries()} />

			<ConversationHooksPanel
				expanded={hooksExpanded}
				hooksStatus={sessionHooks.status}
				outputById={sessionHooks.outputById}
				onRerunHook={(hookId) => sessionHooks.rerun(hookId)}
			/>

			<div class="relative">
				<ConversationFileMentionDropdown
					bind:this={fileMentionDropdownRef}
					files={sessionFiles}
					textareaRef={fileMentionTextareaRef}
					onDraftChange={(value) => threadUi.setComposerDraft(value)}
				/>

				<form
					onsubmit={(event) => {
						event.preventDefault();
						submitComposer();
					}}
				>
					<InputGroup class="overflow-hidden rounded-t-md rounded-b-none md:rounded-md">
					{#if attachmentFiles.length > 0}
						<InputGroupAddon
							align="block-start"
							class="w-full flex-wrap gap-1 border-b border-border px-3 pb-2 pt-3"
						>
							{#each attachmentFiles as file (file.id)}
								<div
									class="inline-flex max-w-[220px] items-center gap-1 rounded-md border border-border bg-background px-2 py-1 text-xs"
								>
									<span class="truncate">{file.filename}</span>
									<Button
										variant="ghost"
										size="icon-xs"
										onclick={() => removeAttachment(file.id)}
										class="size-4"
										aria-label={`Remove ${file.filename}`}
									>
										<XIcon class="size-3" />
									</Button>
								</div>
							{/each}
						</InputGroupAddon>
					{/if}

					<InputGroupTextarea
						bind:ref={fileMentionTextareaRef}
						rows={2}
						class="field-sizing-content max-h-48 min-h-16 transition-all"
						value={threadUi.composerDraft}
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

					<InputGroupAddon align="block-end" class="justify-between gap-1">
						<div class="tauri-no-drag flex flex-wrap items-center gap-1">
							<DropdownMenu>
								<DropdownMenuTrigger class="tauri-no-drag">
									<InputGroupButton
										size="icon-sm"
										variant="ghost"
										aria-label="Attachment actions"
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

							<ConversationEnvSetsControl
								sessionEnvSets={session.envSets}
								threadEnvSets={thread.envSets}
							/>

							{#each app.workflowActions as action, index (action + index)}
								<Button variant="ghost" size="xs" class="h-6 px-2 text-xs">
									{action}
								</Button>
							{/each}
						</div>

						<div class="tauri-no-drag flex items-center gap-2">
							{#if hooks().length > 0}
								<Button
									variant="ghost"
									size="xs"
									class="h-8 gap-1.5 px-2"
									onclick={() => {
										hooksExpanded = !hooksExpanded;
									}}
								>
									{#if hookHasRunning()}
										<Loader2Icon class="size-3.5 animate-spin text-blue-500" />
									{:else if hookHasFailures()}
										<AlertTriangleIcon class="size-3.5 text-yellow-500" />
									{:else}
										<ZapIcon class="size-3.5 text-green-500" />
									{/if}
									<span class="text-xs font-medium">{hookPassedCount()}</span>
								</Button>
							{/if}

							{#if queueTotalCount() > 0}
								<Button
									variant="ghost"
									size="xs"
									class="h-8 gap-1.5 px-2"
									onclick={() => {
										queueExpanded = !queueExpanded;
									}}
								>
									<CheckCircleIcon class="size-3.5" />
									<span class="text-xs font-medium"
										>{queueCompletedCount()}/{queueTotalCount()}</span
									>
								</Button>
							{/if}

							<InputGroupButton
								type={isGenerating() ? "button" : "submit"}
								variant="default"
								size="icon-sm"
								onclick={(event) => {
									event.preventDefault();
									submitComposer();
								}}
								onmouseenter={() => {
									submitHovered = true;
								}}
								onmouseleave={() => {
									submitHovered = false;
								}}
								aria-label={showPlusIcon() ? "New thread" : isGenerating() ? "Stop" : "Submit"}
							>
								{#if showPlusIcon()}
									<PlusIcon class="size-4" />
								{:else if submitStatus === "submitted"}
									<Loader2Icon class="size-4 animate-spin" />
								{:else if submitStatus === "streaming"}
									<SquareIcon class="size-4" />
								{:else if submitStatus === "error"}
									<XIcon class="size-4" />
								{:else}
									<CornerDownLeftIcon class="size-4" />
								{/if}
							</InputGroupButton>
						</div>
					</InputGroupAddon>
					</InputGroup>
				</form>
			</div>
		</div>
	</div>

</div>
