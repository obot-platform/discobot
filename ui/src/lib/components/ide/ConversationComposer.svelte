<script lang="ts">
	import { InputGroup, InputGroupAddon } from "$lib/components/ui/input-group";
	import ConversationComposerAttachmentButton from "$lib/components/ide/ConversationComposerAttachmentButton.svelte";
	import ConversationComposerAttachments from "$lib/components/ide/ConversationComposerAttachments.svelte";
	import ConversationComposerHooksControl from "$lib/components/ide/ConversationComposerHooksControl.svelte";
	import ConversationComposerModelControl from "$lib/components/ide/ConversationComposerModelControl.svelte";
	import ConversationComposerModeControl from "$lib/components/ide/ConversationComposerModeControl.svelte";
	import ConversationComposerQueueControl from "$lib/components/ide/ConversationComposerQueueControl.svelte";
	import ConversationComposerSessionSetupControl from "$lib/components/ide/ConversationComposerSessionSetupControl.svelte";
	import ConversationComposerSessionSetupStatus from "$lib/components/ide/ConversationComposerSessionSetupStatus.svelte";
	import ConversationComposerSubmitButton from "$lib/components/ide/ConversationComposerSubmitButton.svelte";
	import ConversationComposerTextarea from "$lib/components/ide/ConversationComposerTextarea.svelte";
	import ConversationEnvSetsControl from "$lib/components/ide/ConversationEnvSetsControl.svelte";
	import ConversationHooksPanel from "$lib/components/ide/ConversationHooksPanel.svelte";
	import ConversationQueuePanel from "$lib/components/ide/ConversationQueuePanel.svelte";
	import { useAppContext } from "$lib/context/app-context.svelte";
	import { useSessionContext } from "$lib/context/session-context.svelte";
	import { useThreadContext } from "$lib/context/thread-context.svelte";
	import type {
		ComposerAttachment,
		ComposerMode,
		ConversationComposerTextareaHandle,
		WorkspaceSelectorHandle,
		WorkspaceSelectorState,
	} from "$lib/components/ide/conversation-composer.types";

	const app = useAppContext();
	const models = app.models;
	const preferences = app.preferences;
	const sessions = app.sessions;
	const workspaces = app.workspaces;
	const session = useSessionContext();
	const thread = useThreadContext();
	const conversation = session.conversation;
	const sessionView = session.ui;
	const sessionHooks = session.hooks;
	const sessionFiles = $derived.by(() => session.files.searchable);

	let attachmentFiles = $state<ComposerAttachment[]>([]);
	let selectedMode = $state<ComposerMode>("build");
	let selectedModelId = $state<string | null>(null);
	let selectedReasoning = $state(false);
	let selectorSessionId = $state<string | null>(null);
	let composerTextareaRef = $state<ConversationComposerTextareaHandle | null>(null);
	let sessionSetupRef = $state<WorkspaceSelectorHandle | null>(null);
	let sessionSetupDisabled = $state(false);
	let workspaceSelectorState = $state<WorkspaceSelectorState>({
		selectedWorkspaceOption: "new-workspace",
		selectedWorkspaceBranch: "",
		requiresSourceInput: false,
		workspaceSourceInput: "",
		workspaceSourceType: "local",
		workspaceValidation: null,
		workspaceSourceIsValid: true,
		workspaceValidationMessage: null,
		validatingWorkspaceSource: false,
		creatingSessionSetup: false,
		setupMessage: null,
	});

	function normalizeComposerMode(mode: string | null | undefined): ComposerMode {
		if (!mode || mode === "" || mode === "build") {
			return "build";
		}
		return "plan";
	}

	$effect(() => {
		const currentSessionId = session.current?.id ?? "new-session";
		if (selectorSessionId === currentSessionId) {
			return;
		}

		selectorSessionId = currentSessionId;
		selectedMode = normalizeComposerMode(session.current?.mode);

		if (session.current?.model) {
			const supportsReasoning = models.list.some(
				(model) => model.id === session.current?.model && model.reasoning,
			);
			selectedModelId = session.current.reasoning && supportsReasoning
				? `${session.current.model}:thinking`
				: session.current.model;
		} else {
			selectedModelId = preferences.defaultModel || null;
		}

		if (currentSessionId === "new-session") {
			sessionSetupRef?.resetForNewSession();
			void workspaces.refresh();
			void models.refresh();
		}
	});

	const submitStatus = $derived.by(() => {
		if (conversation.status === "loading") {
			return "submitted" as const;
		}
		if (conversation.status === "streaming") {
			return "streaming" as const;
		}
		if (conversation.status === "error") {
			return "error" as const;
		}
		return "ready" as const;
	});

	function isGenerating() {
		return conversation.status === "loading" || conversation.status === "streaming";
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
			await conversation.cancel();
			composerTextareaRef?.closeMentionDropdown();
			return;
		}

		if (!session.current) {
			const ready = await (sessionSetupRef?.ensureSessionReady() ?? Promise.resolve(false));
			if (!ready) {
				return;
			}
		}

		if (inputEmpty() && attachmentFiles.length === 0) {
			if (!session.current) {
				return;
			}
			await sessions.create(session.current.workspaceId);
			return;
		}

		const nextMessageText = sessionView.composerDraft.trim();
		await conversation.submit({
			text: nextMessageText,
			mode: selectedMode,
			modelId: selectedModelId,
			reasoning: selectedReasoning,
		});
		sessionView.setComposerDraft("");
		composerTextareaRef?.closeMentionDropdown();
		clearAttachments();
	}
</script>

<div class="shrink-0 bg-background p-0 md:p-3">
	<div class={`w-full ${preferences.chatWidthMode === "constrained" ? "md:mx-auto md:max-w-3xl" : ""}`}>
		<ConversationQueuePanel expanded={sessionView.queueExpanded} entries={thread.planEntries} />

		<ConversationHooksPanel
			expanded={sessionView.hooksExpanded}
			hooksStatus={sessionHooks.status}
			outputById={sessionHooks.outputById}
			onRerunHook={(hookId) => sessionHooks.rerun(hookId)}
		/>

		<ConversationComposerSessionSetupStatus state={workspaceSelectorState} />

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
						sessionFiles={sessionFiles}
						attachmentCount={attachmentFiles.length}
						onAddFiles={addFiles}
						onRemoveLastAttachment={removeLastAttachment}
						onSubmit={submitComposer}
					/>

					<InputGroupAddon align="block-end" class="justify-between gap-1">
						<div class="tauri-no-drag flex min-w-0 flex-1 flex-wrap items-center gap-1">
							<ConversationComposerAttachmentButton onFilesAdd={addFiles} />
							<ConversationComposerModeControl bind:value={selectedMode} />
							<ConversationEnvSetsControl
								sessionEnvSets={session.envSets}
								threadEnvSets={thread.envSets}
							/>
							<ConversationComposerModelControl
								bind:value={selectedModelId}
								bind:reasoning={selectedReasoning}
							/>
						</div>

						<div class="tauri-no-drag flex items-center justify-end gap-2">
							<ConversationComposerHooksControl bind:expanded={sessionView.hooksExpanded} />
							<ConversationComposerQueueControl bind:expanded={sessionView.queueExpanded} />
							<ConversationComposerSessionSetupControl
								bind:this={sessionSetupRef}
								bind:selectorState={workspaceSelectorState}
								bind:disabled={sessionSetupDisabled}
							/>
							<ConversationComposerSubmitButton
								status={submitStatus}
								inputEmpty={inputEmpty()}
								disabled={sessionSetupDisabled}
								onPress={submitComposer}
							/>
						</div>
					</InputGroupAddon>
				</InputGroup>
			</form>
		</div>
	</div>
</div>
