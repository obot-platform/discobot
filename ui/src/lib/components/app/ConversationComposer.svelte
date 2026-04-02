<script lang="ts">
	import { onDestroy, onMount, tick } from "svelte";
	import { InputGroup, InputGroupAddon } from "$lib/components/ui/input-group";
	import { Button } from "$lib/components/ui/button";
	import ConversationComposerAttachmentButton from "$lib/components/app/parts/ConversationComposerAttachmentButton.svelte";
	import ConversationComposerAttachments from "$lib/components/app/parts/ConversationComposerAttachments.svelte";
	import ConversationComposerHooksControl from "$lib/components/app/parts/ConversationComposerHooksControl.svelte";
	import ConversationComposerModelControl from "$lib/components/app/parts/ConversationComposerModelControl.svelte";
	import ConversationComposerModeControl from "$lib/components/app/parts/ConversationComposerModeControl.svelte";
	import ConversationComposerPlanControl from "$lib/components/app/parts/ConversationComposerPlanControl.svelte";
	import ConversationComposerQueueControl from "$lib/components/app/parts/ConversationComposerQueueControl.svelte";
	import ConversationComposerReasoningControl from "$lib/components/app/parts/ConversationComposerReasoningControl.svelte";
	import ConversationPromptQueuePanel from "$lib/components/app/parts/ConversationPromptQueuePanel.svelte";
	import ConversationComposerSessionSetupStatus from "$lib/components/app/ConversationComposerSessionSetupStatus.svelte";
	import ConversationComposerSubmitButton from "$lib/components/app/parts/ConversationComposerSubmitButton.svelte";
	import ConversationComposerTextarea from "$lib/components/app/parts/ConversationComposerTextarea.svelte";
	import ConversationCredentialsControl from "$lib/components/app/ConversationCredentialsControl.svelte";
	import ConversationHooksPanel from "$lib/components/app/ConversationHooksPanel.svelte";
	import ConversationQueuePanel from "$lib/components/app/parts/ConversationQueuePanel.svelte";
	import ConversationWorkspaceSelector from "$lib/components/app/ConversationWorkspaceSelector.svelte";
	import {
		moveComposerDraft,
		resolveComposerDraftStorageKey,
	} from "$lib/composer-draft-storage";
	import type {
		ComposerAttachment,
		ComposerMode,
		ConversationComposerTextareaHandle,
		WorkspaceSelectionResult,
		WorkspaceSelectorHandle,
	} from "$lib/components/app/conversation-composer.types";
	import type { ModelInfo } from "$lib/api-types";
	import { useAppContext } from "$lib/context/app-context.svelte";
	import { useSessionContext } from "$lib/context/session-context.svelte";
	import {
		normalizeThreadComposerReasoning,
		parseComposerModelSelection,
		useThreadContext,
	} from "$lib/context/thread-context.svelte";
	import {
		buildUserMessageParts,
		createUserMessageAttachment,
		getLatestPlanState,
	} from "$lib/session/domains/session-domain.helpers";

	const app = useAppContext();
	const models = app.models;
	const preferences = app.preferences;
	const sessions = app.sessions;
	const ui = app.ui;
	const session = useSessionContext();
	const thread = useThreadContext();
	const sessionView = session.ui;
	const sessionHooks = session.hooks;

	let attachmentFiles = $state<ComposerAttachment[]>([]);
	let composerTextareaRef = $state<ConversationComposerTextareaHandle | null>(
		null,
	);
	let sessionSetupRef = $state<WorkspaceSelectorHandle | null>(null);
	let pendingSubmitError = $state<string | null>(null);
	let pendingMentionSessionCreation = $state<Promise<boolean> | null>(null);
	let mounted = true;

	function findModelById(modelId: string | null): ModelInfo | null {
		if (!modelId) {
			return null;
		}
		return models.list.find((model) => model.id === modelId) ?? null;
	}

	function normalizeReasoningForModel(
		model: ModelInfo | null,
		reasoning: string | undefined,
	): string | undefined {
		if (!model?.reasoning) {
			return undefined;
		}
		const normalizedReasoning = normalizeThreadComposerReasoning(reasoning);
		if (!normalizedReasoning) {
			return undefined;
		}
		if (normalizedReasoning === "default") {
			return "default";
		}
		const levels = model.reasoningLevels ?? [];
		if (levels.length === 0 || levels.includes(normalizedReasoning)) {
			return normalizedReasoning;
		}
		return undefined;
	}

	function getReasoningForModel(
		model: ModelInfo | null,
		preferredReasoning: string | undefined,
		fallbackReasoning: string | undefined,
	): string | undefined {
		return (
			normalizeReasoningForModel(model, preferredReasoning) ??
			normalizeReasoningForModel(model, fallbackReasoning)
		);
	}

	function getNextReasoningForModel(
		model: ModelInfo | null,
		preferredReasoning: string | undefined,
		fallbackReasoning: string | undefined,
	): string | undefined {
		if (!model?.reasoning) {
			return undefined;
		}
		return (
			getReasoningForModel(model, preferredReasoning, fallbackReasoning) ??
			"default"
		);
	}

	function isSameReasoningSelection(
		left: string | undefined,
		right: string | undefined,
	): boolean {
		return (left ?? "default") === (right ?? "default");
	}

	const effectiveMode = $derived.by(() => thread.nextMode ?? thread.mode);
	const effectiveModelId = $derived.by(
		() => thread.nextModelId ?? thread.modelId,
	);
	const selectedModelId = $derived.by(() =>
		thread.nextModelId !== undefined
			? (thread.nextModelId ?? preferences.defaultModel) || null
			: effectiveModelId,
	);
	const selectedModel = $derived.by(() => findModelById(selectedModelId));
	const effectiveReasoning = $derived.by(() =>
		getReasoningForModel(selectedModel, thread.nextReasoning, thread.reasoning),
	);
	const reasoningLevels = $derived.by(
		() => selectedModel?.reasoningLevels ?? [],
	);
	const latestPlan = $derived.by(() => getLatestPlanState(thread.messages));
	const hasAvailableModels = $derived.by(() => models.list.length > 0);
	const sessionSetupDisabled = $derived.by(
		() =>
			sessionView.pendingWorkspaceRequiresSourceInput &&
			!sessionView.pendingWorkspaceSourceIsValid,
	);

	function handleModeSelect(nextMode: ComposerMode) {
		thread.setNextMode(nextMode === thread.mode ? undefined : nextMode);
	}

	function handleModelSelect(nextSelection: string | null) {
		const parsedSelection = parseComposerModelSelection(nextSelection);
		const nextModel = findModelById(
			(parsedSelection.modelId ?? preferences.defaultModel) || null,
		);
		const nextReasoning = getNextReasoningForModel(
			nextModel,
			thread.nextReasoning,
			thread.reasoning,
		);

		if (parsedSelection.modelId === thread.modelId) {
			thread.setNextModelId(undefined);
			thread.setNextReasoning(
				isSameReasoningSelection(nextReasoning, thread.reasoning)
					? undefined
					: nextReasoning,
			);
			return;
		}

		thread.setNextModelId(parsedSelection.modelId);
		thread.setNextReasoning(nextReasoning);
	}

	function handleReasoningSelect(nextReasoning: string | undefined) {
		if (nextReasoning === "default") {
			const modelDefaultReasoning = selectedModel?.defaultReasoning;
			if (effectiveReasoning === undefined) {
				thread.setNextReasoning(
					thread.nextModelId === undefined &&
						(thread.reasoning === undefined || thread.reasoning === "default")
						? undefined
						: "default",
				);
				return;
			}
			thread.setNextReasoning(modelDefaultReasoning ?? "default");
			return;
		}

		thread.setNextReasoning(
			thread.nextModelId === undefined &&
				isSameReasoningSelection(nextReasoning, thread.reasoning)
				? undefined
				: nextReasoning,
		);
	}

	const submitStatus = $derived.by(() => {
		if (session.isPending) return "ready" as const;
		if (thread.status === "loading") return "submitted" as const;
		if (thread.status === "streaming") return "streaming" as const;
		if (thread.status === "error") return "error" as const;
		return "ready" as const;
	});
	const composerDisabledMessage = $derived.by(() => {
		if (!hasAvailableModels) {
			return "Please add a valid LLM provider credential";
		}
		if (thread.hasPendingQuestion) {
			return "Answer the agent's pending question before sending a new message.";
		}
		return null;
	});
	const composerDisabled = $derived.by(() => composerDisabledMessage !== null);

	function isGenerating() {
		return (
			!session.isPending &&
			(thread.status === "loading" || thread.status === "streaming")
		);
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

	async function createMessageParts(text: string) {
		const attachments = await Promise.all(
			attachmentFiles.map(({ file }) => createUserMessageAttachment(file)),
		);
		return buildUserMessageParts(text, attachments);
	}

	async function focusComposerTextarea() {
		await tick();
		composerTextareaRef?.focus();
	}

	onMount(() => {
		void focusComposerTextarea();
	});

	onDestroy(() => {
		mounted = false;
	});

	async function getPendingWorkspaceSelection() {
		return (
			sessionSetupRef?.getWorkspaceSelection() ??
			Promise.resolve<WorkspaceSelectionResult>({
				ready: false,
				workspaceId: null,
				workspaceType: null,
				workspacePath: null,
			})
		);
	}

	async function getPendingSubmitOptions() {
		const workspaceSelection = await getPendingWorkspaceSelection();
		if (!workspaceSelection.ready) {
			return null;
		}

		return {
			...(workspaceSelection.workspaceId
				? { workspaceId: workspaceSelection.workspaceId }
				: {}),
			...(workspaceSelection.workspaceType && workspaceSelection.workspacePath
				? {
						workspaceType: workspaceSelection.workspaceType,
						workspacePath: workspaceSelection.workspacePath,
					}
				: {}),
		};
	}

	function finalizePendingSessionStart(sessionId: string, threadId: string) {
		if (mounted) {
			sessions.openThread(sessionId, threadId);
		}
		thread.clearNextComposerValues();
		sessionView.resetPendingWorkspaceSetup();
	}

	function movePendingDraftToThread(threadId: string, draft: string) {
		moveComposerDraft({
			fromStorageKey: resolveComposerDraftStorageKey({
				isPending: true,
				threadId: thread.threadId,
			}),
			toStorageKey: resolveComposerDraftStorageKey({
				isPending: false,
				threadId,
			}),
			value: draft,
		});
	}

	function clearCurrentDraft() {
		thread.clearComposerDraft();
	}

	async function handleDeleteQueuedPrompt(queueId: string) {
		await thread.deleteQueuedPrompt(queueId);
	}

	async function createSessionForFileMentions(): Promise<boolean> {
		if (!session.isPending) {
			return true;
		}

		if (pendingMentionSessionCreation) {
			return pendingMentionSessionCreation;
		}

		const creation = submitComposer({
			forceEmptyPendingMessage: true,
			preserveDraft: true,
		});
		pendingMentionSessionCreation = creation;
		return creation.finally(() => {
			if (pendingMentionSessionCreation === creation) {
				pendingMentionSessionCreation = null;
			}
		});
	}

	async function submitComposer({
		forceEmptyPendingMessage = false,
		preserveDraft = false,
	}: {
		forceEmptyPendingMessage?: boolean;
		preserveDraft?: boolean;
	} = {}) {
		if (composerDisabled && !forceEmptyPendingMessage) {
			return false;
		}
		const emptyWithoutAttachments =
			inputEmpty() && attachmentFiles.length === 0;
		if (isGenerating() && emptyWithoutAttachments) {
			await thread.cancel();
			composerTextareaRef?.closeMentionDropdown();
			composerTextareaRef?.closePromptHistoryDropdown();
			return false;
		}
		if (!session.isPending && emptyWithoutAttachments) {
			return false;
		}

		pendingSubmitError = null;
		const wasPending = session.isPending;
		const currentDraft = sessionView.composerDraft;
		const nextMessageText = forceEmptyPendingMessage ? "" : currentDraft.trim();
		const shouldAllowEmptyPendingMessage =
			wasPending &&
			(forceEmptyPendingMessage ||
				(attachmentFiles.length === 0 && nextMessageText.length === 0));
		const nextMessageParts = forceEmptyPendingMessage
			? []
			: shouldAllowEmptyPendingMessage
				? []
				: await createMessageParts(nextMessageText);
		const pendingSubmitOptions = wasPending
			? await getPendingSubmitOptions()
			: null;
		if (wasPending && !pendingSubmitOptions) {
			return false;
		}

		if (!preserveDraft) {
			if (nextMessageText) {
				preferences.addPromptToHistory(nextMessageText);
			}
			clearCurrentDraft();
		}

		try {
			if (wasPending) {
				sessions.setAwaitingInitialStatus(session.sessionId);
			}
			const result = await thread.submit({
				parts: nextMessageParts,
				allowEmptyPendingMessage: shouldAllowEmptyPendingMessage,
				...pendingSubmitOptions,
			});
			if (wasPending && result?.materialized) {
				sessions.setAwaitingInitialStatus(result.sessionId);
				if (preserveDraft) {
					movePendingDraftToThread(result.threadId, currentDraft);
				}
				finalizePendingSessionStart(result.sessionId, result.threadId);
			}
			if (!preserveDraft) {
				thread.clearNextComposerValues();
				composerTextareaRef?.closeMentionDropdown();
				composerTextareaRef?.closePromptHistoryDropdown();
				clearAttachments();
				await focusComposerTextarea();
			}
			return true;
		} catch (err) {
			if (wasPending) {
				sessions.setAwaitingInitialStatus(null);
				pendingSubmitError =
					err instanceof Error ? err.message : "Failed to start chat";
			}
			await focusComposerTextarea();
			return false;
		}
	}

	async function handleComposerSubmit() {
		await submitComposer();
	}
</script>

<div class="shrink-0 bg-background p-0 md:p-3">
	<div
		class={`w-full ${preferences.chatWidthMode === "constrained" ? "md:mx-auto md:max-w-3xl" : ""}`}
	>
		{#if !session.isPending}
			<ConversationPromptQueuePanel
				entries={thread.promptQueue}
				onDelete={handleDeleteQueuedPrompt}
			/>

			<ConversationQueuePanel
				expanded={sessionView.queueExpanded}
				entries={thread.planEntries}
			/>

			<ConversationHooksPanel
				expanded={sessionView.hooksExpanded}
				hooksStatus={sessionHooks.status}
				outputById={sessionHooks.outputById}
				onRerunHook={(hookId) => sessionHooks.rerun(hookId)}
			/>
		{/if}

		{#if session.isPending || session.current?.status !== "ready"}
			<ConversationComposerSessionSetupStatus />
			{#if session.isPending}
				<div class="mb-2 flex w-full items-center gap-2 px-1 md:hidden">
					<ConversationWorkspaceSelector
						bind:this={sessionSetupRef}
						fullWidth={true}
					/>
				</div>
			{/if}
		{/if}

		{#if pendingSubmitError}
			<div class="mb-2 text-sm text-destructive">{pendingSubmitError}</div>
		{/if}
		{#if composerDisabledMessage}
			<div
				class="mb-2 flex flex-wrap items-center gap-2 text-sm text-muted-foreground"
			>
				<span>{composerDisabledMessage}</span>
				{#if !hasAvailableModels}
					<Button
						variant="link"
						size="xs"
						class="h-auto px-0"
						onclick={ui.openCredentialsDialog}
					>
						Open credentials
					</Button>
				{/if}
			</div>
		{/if}

		<div class="relative">
			<form
				class={composerDisabled ? "pointer-events-none opacity-60" : undefined}
				onsubmit={(event) => {
					event.preventDefault();
					void submitComposer();
				}}
			>
				<InputGroup class="rounded-t-md rounded-b-none md:rounded-md">
					<ConversationComposerAttachments
						files={attachmentFiles}
						onRemove={removeAttachment}
					/>

					<ConversationComposerTextarea
						bind:this={composerTextareaRef}
						draft={sessionView.composerDraft}
						disabled={composerDisabled}
						onDraftChange={(v) => sessionView.setComposerDraft(v)}
						sessionId={session.isPending ? null : session.sessionId}
						attachmentCount={attachmentFiles.length}
						onAddFiles={addFiles}
						onRemoveLastAttachment={removeLastAttachment}
						onRequestMentionSession={createSessionForFileMentions}
						onSubmit={handleComposerSubmit}
					/>

					<InputGroupAddon align="block-end" class="justify-between gap-1">
						<div
							class="tauri-no-drag flex min-w-0 flex-1 flex-wrap items-center gap-1"
						>
							<ConversationComposerAttachmentButton
								onFilesAdd={addFiles}
								disabled={composerDisabled}
							/>
							<ConversationComposerModeControl
								value={effectiveMode}
								onSelect={handleModeSelect}
							/>
							{#if !session.isPending}
								<ConversationCredentialsControl />
							{/if}
							<ConversationComposerModelControl
								value={thread.nextModelId !== undefined
									? thread.nextModelId
									: thread.modelId}
								onSelect={handleModelSelect}
								models={models.list}
							/>
							{#if selectedModel?.reasoning}
								<ConversationComposerReasoningControl
									value={effectiveReasoning}
									defaultValue={selectedModel.defaultReasoning}
									levels={reasoningLevels}
									onSelect={handleReasoningSelect}
								/>
							{/if}
						</div>

						<div class="tauri-no-drag flex items-center justify-end gap-2">
							{#if session.isPending}
								<div class="hidden md:contents">
									<ConversationWorkspaceSelector />
								</div>
							{:else}
								<ConversationComposerHooksControl
									bind:expanded={sessionView.hooksExpanded}
									hooksStatus={sessionHooks.status}
								/>
								<ConversationComposerQueueControl
									bind:expanded={sessionView.queueExpanded}
									entries={thread.planEntries}
								/>
								<ConversationComposerPlanControl
									{latestPlan}
									sessionId={session.sessionId}
									threadId={thread.threadId}
								/>
							{/if}
							<ConversationComposerSubmitButton
								status={submitStatus}
								inputEmpty={inputEmpty()}
								isPending={session.isPending}
								disabled={composerDisabled ||
									(session.isPending ? sessionSetupDisabled : false)}
								onPress={handleComposerSubmit}
							/>
						</div>
					</InputGroupAddon>
				</InputGroup>
			</form>
		</div>
	</div>
</div>
