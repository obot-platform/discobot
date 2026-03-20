<script lang="ts">
	import { generateId } from "ai";
	import { InputGroup, InputGroupAddon } from "$lib/components/ui/input-group";
	import ConversationComposerAttachmentButton from "$lib/components/app/parts/ConversationComposerAttachmentButton.svelte";
	import ConversationComposerAttachments from "$lib/components/app/parts/ConversationComposerAttachments.svelte";
	import ConversationComposerHooksControl from "$lib/components/app/parts/ConversationComposerHooksControl.svelte";
	import ConversationComposerModelControl from "$lib/components/app/parts/ConversationComposerModelControl.svelte";
	import ConversationComposerModeControl from "$lib/components/app/parts/ConversationComposerModeControl.svelte";
	import ConversationComposerQueueControl from "$lib/components/app/parts/ConversationComposerQueueControl.svelte";
	import ConversationComposerSessionSetupStatus from "$lib/components/app/ConversationComposerSessionSetupStatus.svelte";
	import ConversationComposerSubmitButton from "$lib/components/app/parts/ConversationComposerSubmitButton.svelte";
	import ConversationComposerTextarea from "$lib/components/app/parts/ConversationComposerTextarea.svelte";
	import ConversationEnvSetsControl from "$lib/components/app/ConversationEnvSetsControl.svelte";
	import ConversationHooksPanel from "$lib/components/app/ConversationHooksPanel.svelte";
	import ConversationQueuePanel from "$lib/components/app/parts/ConversationQueuePanel.svelte";
	import ConversationWorkspaceSelector from "$lib/components/app/ConversationWorkspaceSelector.svelte";
	import type {
		ComposerAttachment,
		ComposerMode,
		ConversationComposerTextareaHandle,
		WorkspaceSelectionResult,
		WorkspaceSelectorHandle,
	} from "$lib/components/app/conversation-composer.types";
	import { useAppContext } from "$lib/context/app-context.svelte";
	import { useSessionContext } from "$lib/context/session-context.svelte";
	import { useThreadContext } from "$lib/context/thread-context.svelte";

	const app = useAppContext();
	const models = app.models;
	const preferences = app.preferences;
	const sessions = app.sessions;
	const session = useSessionContext();
	const thread = useThreadContext();
	const sessionView = session.ui;
	const sessionHooks = session.hooks;
	const sessionFiles = $derived.by(() => session.files.searchable);

	let attachmentFiles = $state<ComposerAttachment[]>([]);
	let modeOverride = $state<ComposerMode | undefined>(undefined);
	let modelIdOverride = $state<string | null | undefined>(undefined);
	let composerTextareaRef = $state<ConversationComposerTextareaHandle | null>(null);
	let sessionSetupRef = $state<WorkspaceSelectorHandle | null>(null);
	let pendingSubmitError = $state<string | null>(null);

	function normalizeComposerMode(mode: string | null | undefined): ComposerMode {
		if (!mode || mode === "" || mode === "build") {
			return "build";
		}
		return "plan";
	}

	function normalizeModelId(modelId: string | null): string | undefined {
		if (!modelId) return undefined;
		return modelId.endsWith(":thinking") ? modelId.slice(0, -":thinking".length) : modelId;
	}

	function composerModelUsesReasoning(modelId: string | null | undefined) {
		return modelId?.endsWith(":thinking") ?? false;
	}

	function clearComposerOverrides() {
		modeOverride = undefined;
		modelIdOverride = undefined;
	}

	// When pending, thread.thread is null so these fall back to defaults ("build", preferences.defaultModel).
	const sessionMode = $derived.by(() => normalizeComposerMode(thread.thread?.mode));

	const sessionModelId = $derived.by(() => {
		const t = thread.thread;
		if (!t?.model) {
			return session.isPending ? (preferences.defaultModel || null) : null;
		}

		const supportsReasoning = models.list.some(
			(model) => model.id === t.model && model.reasoning,
		);

		return t.reasoning === "enabled" && supportsReasoning
			? `${t.model}:thinking`
			: t.model;
	});

	const effectiveMode = $derived.by(() => modeOverride ?? sessionMode);
	const effectiveModelId = $derived.by(
		() => (modelIdOverride !== undefined ? modelIdOverride : sessionModelId),
	);
	const effectiveReasoning = $derived.by(() => composerModelUsesReasoning(effectiveModelId));
	const sessionSetupDisabled = $derived.by(
		() => sessionView.pendingWorkspaceRequiresSourceInput && !sessionView.pendingWorkspaceSourceIsValid,
	);

	function handleModeSelect(nextMode: ComposerMode) {
		modeOverride = nextMode === sessionMode ? undefined : nextMode;
	}

	function handleModelSelect(nextModelId: string | null) {
		modelIdOverride = nextModelId === sessionModelId ? undefined : nextModelId;
	}

	const submitStatus = $derived.by(() => {
		if (session.isPending) return "ready" as const;
		if (thread.status === "loading") return "submitted" as const;
		if (thread.status === "streaming") return "streaming" as const;
		if (thread.status === "error") return "error" as const;
		return "ready" as const;
	});

	function isGenerating() {
		return !session.isPending && (thread.status === "loading" || thread.status === "streaming");
	}

	function inputEmpty() {
		return sessionView.composerDraft.trim().length === 0;
	}

	function addFiles(files: File[] | FileList) {
		const incoming = Array.from(files);
		if (incoming.length === 0) {
			return;
		}

		attachmentFiles = attachmentFiles.concat(
			incoming.map((file) => ({
				id: `${Date.now()}-${Math.floor(Math.random() * 10_000)}`,
				file,
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

	function removeLastAttachment() {
		const lastAttachment = attachmentFiles.at(-1);
		if (lastAttachment) {
			removeAttachment(lastAttachment.id);
		}
	}

	function clearAttachments() {
		for (const file of attachmentFiles) {
			if (file.url) {
				URL.revokeObjectURL(file.url);
			}
		}
		attachmentFiles = [];
	}

	async function submitComposer() {
		if (isGenerating()) {
			await thread.cancel();
			composerTextareaRef?.closeMentionDropdown();
			return;
		}

		if (session.isPending) {
			await submitNewSession();
			return;
		}

		const emptyWithoutAttachments = inputEmpty() && attachmentFiles.length === 0;
		if (emptyWithoutAttachments) {
			return;
		}

		const nextMessageText = sessionView.composerDraft.trim();
		try {
			await thread.submit({
				text: nextMessageText,
				mode: effectiveMode,
				modelId: effectiveModelId,
				reasoning: effectiveReasoning,
			});
			sessionView.setComposerDraft("");
			clearComposerOverrides();
			composerTextareaRef?.closeMentionDropdown();
			clearAttachments();
		} catch {
			// Error is already surfaced via thread.error in ConversationPane
		}
	}

	async function submitNewSession() {
		pendingSubmitError = null;

		const workspaceSelection = await (
			sessionSetupRef?.getWorkspaceSelection() ??
			Promise.resolve<WorkspaceSelectionResult>({
				ready: false,
				workspaceId: null,
				workspaceType: null,
				workspacePath: null,
			})
		);
		if (!workspaceSelection.ready) {
			return;
		}

		const trimmedText = sessionView.composerDraft.trim();
		const model = normalizeModelId(effectiveModelId);

		try {
			const response = await app.chat({
				sessionId: session.sessionId,
				threadId: thread.threadId,
				messages: trimmedText
					? [{ id: generateId(), role: "user", parts: [{ type: "text", text: trimmedText }] }]
					: [],
				...(workspaceSelection.workspaceId ? { workspaceId: workspaceSelection.workspaceId } : {}),
				...(workspaceSelection.workspaceType && workspaceSelection.workspacePath
					? {
							workspaceType: workspaceSelection.workspaceType,
							workspacePath: workspaceSelection.workspacePath,
						}
					: {}),
				...(model ? { model } : {}),
				...(effectiveReasoning ? { reasoning: "enabled" } : {}),
				mode: effectiveMode === "plan" ? "plan" : "",
			});
			sessions.select(response.sessionId);

			sessionView.setComposerDraft("");
			clearComposerOverrides();
			composerTextareaRef?.closeMentionDropdown();
			clearAttachments();
			sessionView.resetPendingWorkspaceSetup();
		} catch (err) {
			pendingSubmitError = err instanceof Error ? err.message : "Failed to start session";
		}
	}
</script>

<div class="shrink-0 bg-background p-0 md:p-3">
	<div class={`w-full ${preferences.chatWidthMode === "constrained" ? "md:mx-auto md:max-w-3xl" : ""}`}>
		{#if !session.isPending}
			<ConversationQueuePanel expanded={sessionView.queueExpanded} entries={thread.planEntries} />

			<ConversationHooksPanel
				expanded={sessionView.hooksExpanded}
				hooksStatus={sessionHooks.status}
				outputById={sessionHooks.outputById}
				onRerunHook={(hookId) => sessionHooks.rerun(hookId)}
			/>
		{/if}

		{#if session.isPending}
			<ConversationComposerSessionSetupStatus />
			<div class="mb-2 flex w-full items-center gap-2 px-1 md:hidden">
				<ConversationWorkspaceSelector bind:this={sessionSetupRef} fullWidth={true} />
			</div>
		{/if}

		{#if pendingSubmitError}
			<div class="mb-2 text-sm text-destructive">{pendingSubmitError}</div>
		{/if}

		<div class="relative">
			<form
				onsubmit={(event) => {
					event.preventDefault();
					void submitComposer();
				}}
			>
				<InputGroup class="rounded-t-md rounded-b-none md:rounded-md">
					<ConversationComposerAttachments files={attachmentFiles} onRemove={removeAttachment} />

					<ConversationComposerTextarea
						bind:this={composerTextareaRef}
						draft={sessionView.composerDraft}
						onDraftChange={(v) => sessionView.setComposerDraft(v)}
						sessionFiles={session.isPending ? [] : sessionFiles}
						attachmentCount={attachmentFiles.length}
						onAddFiles={addFiles}
						onRemoveLastAttachment={removeLastAttachment}
						onSubmit={submitComposer}
					/>

					<InputGroupAddon align="block-end" class="justify-between gap-1">
						<div class="tauri-no-drag flex min-w-0 flex-1 flex-wrap items-center gap-1">
							<ConversationComposerAttachmentButton onFilesAdd={addFiles} />
							<ConversationComposerModeControl value={effectiveMode} onSelect={handleModeSelect} />
							{#if !session.isPending}
								<ConversationEnvSetsControl
									sessionEnvSets={session.envSets}
									threadEnvSets={thread.envSets}
								/>
							{/if}
							<ConversationComposerModelControl value={effectiveModelId} onSelect={handleModelSelect} models={models.list} />
						</div>

						<div class="tauri-no-drag flex items-center justify-end gap-2">
							{#if session.isPending}
								<div class="hidden md:contents">
									<ConversationWorkspaceSelector />
								</div>
							{:else}
								<ConversationComposerHooksControl bind:expanded={sessionView.hooksExpanded} hooksStatus={sessionHooks.status} />
								<ConversationComposerQueueControl bind:expanded={sessionView.queueExpanded} entries={thread.planEntries} />
							{/if}
							<ConversationComposerSubmitButton
								status={submitStatus}
								inputEmpty={inputEmpty()}
								disabled={session.isPending ? sessionSetupDisabled : false}
								onPress={submitComposer}
							/>
						</div>
					</InputGroupAddon>
				</InputGroup>
			</form>
		</div>
	</div>
</div>
