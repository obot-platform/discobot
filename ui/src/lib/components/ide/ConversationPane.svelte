<script lang="ts">
	import AlertTriangleIcon from "@lucide/svelte/icons/alert-triangle";
	import BrainIcon from "@lucide/svelte/icons/brain";
	import CheckIcon from "@lucide/svelte/icons/check";
	import CheckCircleIcon from "@lucide/svelte/icons/check-circle";
	import CornerDownLeftIcon from "@lucide/svelte/icons/corner-down-left";
	import FolderIcon from "@lucide/svelte/icons/folder";
	import GithubIcon from "@lucide/svelte/icons/github";
	import HammerIcon from "@lucide/svelte/icons/hammer";
	import Loader2Icon from "@lucide/svelte/icons/loader-2";
	import MapIcon from "@lucide/svelte/icons/map";
	import PaperclipIcon from "@lucide/svelte/icons/paperclip";
	import PlusIcon from "@lucide/svelte/icons/plus";
	import SquareIcon from "@lucide/svelte/icons/square";
	import XIcon from "@lucide/svelte/icons/x";
	import ZapIcon from "@lucide/svelte/icons/zap";
	import GitCommitIcon from "@lucide/svelte/icons/git-commit";
	import GitBranchIcon from "@lucide/svelte/icons/git-branch";
	import { onDestroy } from "svelte";
	import { api } from "$lib/api-client";
	import type { AgentModel, Workspace } from "$lib/api-types";
	import { Button } from "$lib/components/ui/button";
	import ConversationEnvSetsControl from "$lib/components/ide/ConversationEnvSetsControl.svelte";
	import ConversationFileMentionDropdown from "$lib/components/ide/ConversationFileMentionDropdown.svelte";
	import ConversationHooksPanel from "$lib/components/ide/ConversationHooksPanel.svelte";
	import ConversationQueuePanel from "$lib/components/ide/ConversationQueuePanel.svelte";
	import {
		DropdownMenu,
		DropdownMenuContent,
		DropdownMenuItem,
		DropdownMenuLabel,
		DropdownMenuSeparator,
		DropdownMenuTrigger,
	} from "$lib/components/ui/dropdown-menu";
	import {
		InputGroup,
		InputGroupAddon,
		InputGroupButton,
		InputGroupTextarea,
	} from "$lib/components/ui/input-group";
	import { Input } from "$lib/components/ui/input";
	import { NativeSelect } from "$lib/components/ui/native-select";
	import { useAppContext } from "$lib/context/app-context.svelte";
	import { useSessionContext } from "$lib/context/session-context.svelte";
	import { useThreadContext } from "$lib/context/thread-context.svelte";
	import type { SessionConversationMessage } from "$lib/shell-types";

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
	type ComposerMode = "build" | "plan";
	type ModeOption = {
		id: ComposerMode;
		label: string;
		description: string;
		icon: typeof HammerIcon;
	};
	type ModelVariant = {
		id: string;
		displayName: string;
		model: AgentModel;
		reasoning: boolean;
	};

	const modeOptions: ModeOption[] = [
		{
			id: "build",
			label: "Build",
			description: "Execute code, edit files, run tools",
			icon: HammerIcon,
		},
		{
			id: "plan",
			label: "Plan",
			description: "Plan only, no tool execution",
			icon: MapIcon,
		},
	];

	const app = useAppContext();
	const session = useSessionContext();
	const thread = useThreadContext();
	const threadUi = thread.ui;
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
	let selectedMode = $state<ComposerMode>("build");
	let selectedModelId = $state<string | null>(null);
	let selectorSessionId = $state<string | null>(null);
	let selectedWorkspaceOption = $state("new-workspace");
	let selectedWorkspaceBranch = $state("");
	let workspaceSourceInput = $state("");
	let showWorkspaceSourceInput = $state(false);
	let creatingSessionSetup = $state(false);
	let availableWorkspaces = $state<Workspace[]>([]);
	let loadingWorkspaces = $state(false);
	let loadedWorkspaces = $state(false);
	let sessionSetupMessage = $state<string | null>(null);
	let optimisticConversation = $state<SessionConversationMessage[]>([]);
	let optimisticMessageCounter = $state(0);

	function shortenHomePath(path: string): string {
		const homeMatch = path.match(/^(\/home\/[^/]+|\/Users\/[^/]+)(\/.*)?$/);
		if (homeMatch) {
			const rest = homeMatch[2] || "";
			return `~${rest}`;
		}
		return path;
	}

	function getWorkspaceOptionLabel(workspace: Workspace): string {
		return workspace.displayName || shortenHomePath(workspace.path);
	}

	function normalizeGitPath(value: string): string {
		if (/^[^/\s]+\/[^/\s]+$/.test(value)) {
			return `https://github.com/${value}`;
		}
		return value;
	}

	function buildNewWorkspacePath(): string {
		return `~/workspace-${Date.now()}`;
	}

	function isGithubWorkspace(workspace: Workspace): boolean {
		if (workspace.sourceType !== "git") {
			return false;
		}

		const value = `${workspace.path} ${workspace.displayName || ""}`.toLowerCase();
		return value.includes("github.com") || value.includes("github");
	}

	const requiresSourceInput = $derived.by(
		() => selectedWorkspaceOption === "local-directory" || selectedWorkspaceOption === "git-repo",
	);
	const selectedExistingWorkspace = $derived.by(() => {
		if (!selectedWorkspaceOption.startsWith("existing:")) {
			return null;
		}

		const selectedWorkspaceId = selectedWorkspaceOption.slice("existing:".length);
		return availableWorkspaces.find((workspace) => workspace.id === selectedWorkspaceId) ?? null;
	});
	const existingWorkspaceIsGithub = $derived.by(
		() => selectedExistingWorkspace !== null && isGithubWorkspace(selectedExistingWorkspace),
	);
	const showBranchSelector = $derived.by(() => selectedWorkspaceOption !== "new-workspace");
	const availableWorkspaceBranches = $derived.by(() => {
		if (selectedWorkspaceOption === "local-directory") {
			return ["main", "discobot-session", "feature/local-workspace"];
		}

		if (selectedWorkspaceOption === "git-repo") {
			return ["main", "develop", "release"];
		}

		if (selectedExistingWorkspace?.sourceType === "git") {
			return ["main", "develop", "feature/workspace-sync"];
		}

		return ["main", "discobot-session", "feature/workspace-setup"];
	});
	const conversationMessages = $derived.by(() =>
		thread.conversation.length > 0 ? thread.conversation : optimisticConversation,
	);
	const hasMessages = $derived.by(() => conversationMessages.length > 0);

	const modelVariants = $derived.by(() => {
		const modelByName: Record<string, AgentModel> = {};

		for (const model of app.models) {
			const cleanName = model.name.replace(/\s*\(latest\)\s*/gi, "").trim();
			const isLatest = /\(latest\)/i.test(model.name);
			const existing = modelByName[cleanName];

			if (!existing || isLatest) {
				modelByName[cleanName] = {
					...model,
					name: cleanName,
				};
			}
		}

		const variants: ModelVariant[] = [];
		for (const model of Object.values(modelByName)) {
			if (model.reasoning) {
				variants.push({
					id: `${model.id}:thinking`,
					displayName: `${model.name} (thinking)`,
					model,
					reasoning: true,
				});
			}

			variants.push({
				id: model.id,
				displayName: model.name,
				model,
				reasoning: false,
			});
		}

		const getBaseName = (name: string) =>
			name
				.replace(/\s*\(latest\)\s*/gi, "")
				.replace(/\s*\(thinking\)\s*/gi, "")
				.replace(/\s+v\d+\s*/gi, "")
				.replace(/\s+[\d.]+\s*$/, "")
				.trim();

		const extractVersion = (name: string) => {
			const matches = name.match(/(\d+(?:\.\d+)?)/g);
			if (!matches || matches.length === 0) {
				return 0;
			}
			return Number.parseFloat(matches[matches.length - 1]);
		};

		return [...variants].sort((a, b) => {
			const baseA = getBaseName(a.displayName);
			const baseB = getBaseName(b.displayName);
			const baseCompare = baseA.localeCompare(baseB);
			if (baseCompare !== 0) {
				return baseCompare;
			}

			const versionA = extractVersion(a.displayName);
			const versionB = extractVersion(b.displayName);
			if (versionA !== versionB) {
				return versionB - versionA;
			}

			if (a.reasoning && !b.reasoning) {
				return -1;
			}
			if (!a.reasoning && b.reasoning) {
				return 1;
			}

			return a.displayName.localeCompare(b.displayName);
		});
	});

	const modelProviderEntries = $derived.by(() => {
		const grouped: Record<string, ModelVariant[]> = {};
		for (const variant of modelVariants) {
			const provider = variant.model.provider || "Other";
			if (!grouped[provider]) {
				grouped[provider] = [];
			}
			grouped[provider].push(variant);
		}

		return Object.entries(grouped).sort(([a], [b]) => a.localeCompare(b));
	});

	const selectedModeOption = $derived.by(
		() => modeOptions.find((modeOption) => modeOption.id === selectedMode) ?? modeOptions[0],
	);

	const selectedModelVariant = $derived.by(
		() => modelVariants.find((variant) => variant.id === selectedModelId) ?? null,
	);

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
		optimisticConversation = [];
		optimisticMessageCounter = 0;
		selectedMode = normalizeComposerMode(session.current?.mode);

		if (session.current?.model) {
			const thinkingVariantId = `${session.current.model}:thinking`;
			if (
				session.current.reasoning &&
				modelVariants.some((variant) => variant.id === thinkingVariantId)
			) {
				selectedModelId = thinkingVariantId;
				return;
			}

			selectedModelId = session.current.model;
			return;
		}

		selectedModelId = app.defaultModel || null;

		if (currentSessionId === "new-session") {
			sessionSetupMessage = null;
			selectedWorkspaceBranch = "";
			if (!loadingWorkspaces) {
				void loadWorkspaces();
			}
			return;
		}

		sessionSetupMessage = null;
	});

	$effect(() => {
		if (thread.conversation.length > 0 && optimisticConversation.length > 0) {
			optimisticConversation = [];
		}
	});

	$effect(() => {
		if (!showBranchSelector) {
			selectedWorkspaceBranch = "";
			return;
		}

		if (
			selectedWorkspaceBranch.length > 0 &&
			!availableWorkspaceBranches.includes(selectedWorkspaceBranch)
		) {
			selectedWorkspaceBranch = "";
		}
	});

	async function loadWorkspaces() {
		if (loadingWorkspaces) {
			return;
		}

		loadingWorkspaces = true;
		const initialLoad = !loadedWorkspaces;
		try {
			const { workspaces } = await api.getWorkspaces();
			availableWorkspaces = workspaces;
			sessionSetupMessage = null;
			const preferredWorkspace = workspaces.find((workspace) => workspace.status === "ready") ?? workspaces[0];

			if (selectedWorkspaceOption.startsWith("existing:")) {
				const selectedWorkspaceId = selectedWorkspaceOption.slice("existing:".length);
				if (!workspaces.some((workspace) => workspace.id === selectedWorkspaceId)) {
					selectedWorkspaceOption = preferredWorkspace
						? `existing:${preferredWorkspace.id}`
						: "new-workspace";
				}
			} else if (initialLoad && preferredWorkspace) {
				selectedWorkspaceOption = `existing:${preferredWorkspace.id}`;
			}
		} catch (error) {
			sessionSetupMessage = error instanceof Error
				? `Failed to load workspaces: ${error.message}`
				: "Failed to load workspaces.";
		} finally {
			loadingWorkspaces = false;
			loadedWorkspaces = true;
		}
	}

	async function ensureSessionFromSelection(): Promise<boolean> {
		if (session.current) {
			return true;
		}
		if (creatingSessionSetup) {
			return false;
		}

		creatingSessionSetup = true;
		try {
			let workspaceId: string | null = null;

			if (selectedWorkspaceOption.startsWith("existing:")) {
				workspaceId = selectedWorkspaceOption.slice("existing:".length);
				if (!availableWorkspaces.some((workspace) => workspace.id === workspaceId)) {
					sessionSetupMessage = "Select an existing workspace.";
					return false;
				}
			} else if (selectedWorkspaceOption === "new-workspace") {
				const workspace = await api.createWorkspace({
					path: buildNewWorkspacePath(),
					sourceType: "local",
				});
				workspaceId = workspace.id;
				selectedWorkspaceOption = `existing:${workspace.id}`;
				selectedWorkspaceBranch = "";
				showWorkspaceSourceInput = false;
			} else if (selectedWorkspaceOption === "local-directory") {
				const path = workspaceSourceInput.trim();
				if (path.length === 0) {
					sessionSetupMessage = "Enter a local directory path.";
					return false;
				}

				const workspace = await api.createWorkspace({
					path,
					sourceType: "local",
				});
				workspaceId = workspace.id;
				selectedWorkspaceOption = `existing:${workspace.id}`;
				selectedWorkspaceBranch = "";
				showWorkspaceSourceInput = false;
			} else if (selectedWorkspaceOption === "git-repo") {
				const rawPath = workspaceSourceInput.trim();
				if (rawPath.length === 0) {
					sessionSetupMessage = "Enter a GitHub repository.";
					return false;
				}

				const workspace = await api.createWorkspace({
					path: normalizeGitPath(rawPath),
					sourceType: "git",
				});
				workspaceId = workspace.id;
				selectedWorkspaceOption = `existing:${workspace.id}`;
				selectedWorkspaceBranch = "";
				showWorkspaceSourceInput = false;
			}

			if (!workspaceId) {
				sessionSetupMessage = "Unable to determine workspace for this session.";
				return false;
			}

			const sessionId = await app.createSessionForWorkspace(workspaceId);
			if (!sessionId) {
				sessionSetupMessage = app.errorMessage || "Failed to create session.";
				return false;
			}

			await loadWorkspaces();
			sessionSetupMessage = null;
			return true;
		} catch (error) {
			sessionSetupMessage = error instanceof Error
				? `Failed to set up session: ${error.message}`
				: "Failed to set up session.";
			return false;
		} finally {
			creatingSessionSetup = false;
		}
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

	async function submitComposer() {
		if (isGenerating()) {
			stopSubmitCycle();
			return;
		}

		if (!session.current) {
			const ready = await ensureSessionFromSelection();
			if (!ready) {
				return;
			}
		}

		if (inputEmpty() && attachmentFiles.length === 0) {
			session.threads.create();
			return;
		}

		const nextMessageText = threadUi.composerDraft.trim();
		if (nextMessageText.length > 0) {
			optimisticMessageCounter += 1;
			optimisticConversation = [
				...optimisticConversation,
				{
					id: `optimistic-user-${optimisticMessageCounter}`,
					role: "user",
					text: nextMessageText,
				},
			];
		}

		threadUi.setComposerDraft("");
		fileMentionDropdownRef?.closeDropdown();
		clearAttachments();
		runSubmitCycle();
	}

	function handleSessionSourceKeydown(event: KeyboardEvent) {
		if (event.key === "Escape") {
			event.preventDefault();
			selectedWorkspaceOption = "new-workspace";
			selectedWorkspaceBranch = "";
			workspaceSourceInput = "";
			showWorkspaceSourceInput = false;
			return;
		}

		if (event.key === "Enter") {
			event.preventDefault();
			fileMentionTextareaRef?.focus();
		}
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
			void submitComposer();
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
	<div
		class={`flex min-h-0 flex-1 flex-col transition-all duration-300 ease-out ${hasMessages ? "" : "justify-center"}`}
	>
		{#if hasMessages}
			<div class="min-h-0 flex-1 overflow-auto p-4">
				<div
					class={`w-full space-y-4 ${app.chatWidthMode === "constrained" ? "mx-auto max-w-3xl" : ""}`}
				>
					{#each conversationMessages as message (message.id)}
						<div
							class={`max-w-[72%] rounded-lg bg-secondary px-4 py-3 text-sm leading-6 text-foreground ${message.role === "assistant" ? "" : "ms-auto"}`}
						>
							{message.text}
						</div>
					{/each}
				</div>
			</div>
		{/if}

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

			{#if !session.current}
				<p class="mb-2 px-1 text-sm font-medium text-muted-foreground">Start a new session</p>
				{#if loadingWorkspaces}
					<p class="mb-2 px-1 text-xs text-muted-foreground">Loading workspaces...</p>
				{/if}
				{#if sessionSetupMessage}
					<p class="mb-2 px-1 text-xs text-destructive">{sessionSetupMessage}</p>
				{/if}
			{/if}

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
						void submitComposer();
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
						<div class="tauri-no-drag flex min-w-0 flex-1 flex-wrap items-center gap-1">
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
									<DropdownMenuItem onclick={openFileDialog}>Add photos or files</DropdownMenuItem>
								</DropdownMenuContent>
							</DropdownMenu>

							<DropdownMenu>
								<DropdownMenuTrigger class="tauri-no-drag">
									<InputGroupButton
										size="icon-sm"
										variant="ghost"
										aria-label={`Mode: ${selectedModeOption.label}`}
										title={`Mode: ${selectedModeOption.label}`}
									>
										{#if selectedModeOption.id === "plan"}
											<MapIcon class="size-4" />
										{:else}
											<HammerIcon class="size-4" />
										{/if}
									</InputGroupButton>
								</DropdownMenuTrigger>
								<DropdownMenuContent align="start" class="w-64">
									{#each modeOptions as modeOption (modeOption.id)}
										<DropdownMenuItem
											onclick={() => {
												selectedMode = modeOption.id;
											}}
											class="justify-between gap-3"
										>
											<div class="flex min-w-0 items-start gap-2">
												{#if modeOption.id === "plan"}
													<MapIcon class="mt-0.5 size-3.5 shrink-0" />
												{:else}
													<HammerIcon class="mt-0.5 size-3.5 shrink-0" />
												{/if}
												<div class="min-w-0">
													<div class="font-medium">{modeOption.label}</div>
													<div class="text-xs text-muted-foreground">{modeOption.description}</div>
												</div>
											</div>
											{#if selectedMode === modeOption.id}
												<CheckIcon class="size-3.5 text-primary" />
											{/if}
										</DropdownMenuItem>
									{/each}
								</DropdownMenuContent>
							</DropdownMenu>

							<ConversationEnvSetsControl
								sessionEnvSets={session.envSets}
								threadEnvSets={thread.envSets}
							/>

							<DropdownMenu>
								<DropdownMenuTrigger class="tauri-no-drag">
									<InputGroupButton
										size="xs"
										variant="ghost"
										class="h-6 max-w-[160px] gap-1.5 px-2 text-xs"
										title={
											selectedModelVariant
												? `Model: ${selectedModelVariant.displayName}`
												: "Model: Default model"
										}
									>
										<span class="truncate">
											{#if selectedModelVariant}
												{selectedModelVariant.displayName.replace(/\s*\(thinking\)\s*/i, "")}
											{:else}
												Default model
											{/if}
										</span>
										{#if selectedModelVariant?.reasoning}
											<BrainIcon class="size-3.5 shrink-0" />
										{/if}
									</InputGroupButton>
								</DropdownMenuTrigger>
								<DropdownMenuContent align="start" class="max-h-[24rem] w-80 overflow-y-auto">
									<DropdownMenuItem
										onclick={() => {
											selectedModelId = null;
										}}
										class="justify-between"
									>
										<span>Default model</span>
										{#if selectedModelId === null}
											<CheckIcon class="size-3.5 text-primary" />
										{/if}
									</DropdownMenuItem>

									{#if modelProviderEntries.length > 0}
										<DropdownMenuSeparator />
									{/if}

									{#each modelProviderEntries as [provider, variants], providerIndex (provider)}
										{#if providerIndex > 0}
											<DropdownMenuSeparator />
										{/if}
										<DropdownMenuLabel class="text-xs uppercase tracking-[0.16em] text-muted-foreground">
											{provider}
										</DropdownMenuLabel>
										{#each variants as variant (variant.id)}
											<DropdownMenuItem
												onclick={() => {
													selectedModelId = variant.id;
												}}
												class="justify-between gap-3"
											>
												<div class="min-w-0 flex-1 pl-3">
													<div class="truncate font-medium">{variant.displayName}</div>
													{#if variant.model.description && !variant.reasoning}
														<div class="truncate text-xs text-muted-foreground">{variant.model.description}</div>
													{/if}
												</div>
												{#if selectedModelId === variant.id}
													<CheckIcon class="size-3.5 text-primary" />
												{/if}
											</DropdownMenuItem>
										{/each}
									{/each}
								</DropdownMenuContent>
							</DropdownMenu>

						</div>

						<div class="tauri-no-drag flex items-center justify-end gap-2">
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
									<span class="text-xs font-medium">{queueCompletedCount()}/{queueTotalCount()}</span>
								</Button>
							{/if}

							{#if !session.current}
								<div class="flex items-center gap-1.5">
									{#if selectedWorkspaceOption === "local-directory"}
										<FolderIcon class="size-4 text-muted-foreground" />
									{:else if selectedWorkspaceOption === "git-repo"}
										<GithubIcon class="size-4 text-muted-foreground" />
									{:else if selectedWorkspaceOption.startsWith("existing:")}
										{#if selectedExistingWorkspace?.sourceType === "local"}
											<FolderIcon class="size-4 text-muted-foreground" />
										{:else if existingWorkspaceIsGithub}
											<GithubIcon class="size-4 text-muted-foreground" />
										{:else}
											<GitCommitIcon class="size-4 text-muted-foreground" />
										{/if}
									{:else}
										<FolderIcon class="size-4 text-muted-foreground" />
									{/if}

									{#if showWorkspaceSourceInput && requiresSourceInput}
										<Input
											id="session-setup-source-inline"
											class="h-8 w-[240px] text-xs"
											value={workspaceSourceInput}
											placeholder={
												selectedWorkspaceOption === "local-directory"
													? "~/projects/my-app"
													: "https://github.com/org/repo or org/repo"
											}
											oninput={(event) => {
												workspaceSourceInput = (event.currentTarget as HTMLInputElement).value;
											}}
											onkeydown={handleSessionSourceKeydown}
										/>
									{:else}
										<NativeSelect
											id="session-setup-workspace-inline"
											class="h-8 w-[240px] text-xs"
											value={selectedWorkspaceOption}
											disabled={loadingWorkspaces}
											onchange={(event) => {
												const nextOption = (event.currentTarget as HTMLSelectElement).value;
												selectedWorkspaceOption = nextOption;
												selectedWorkspaceBranch = "";
												if (nextOption === "local-directory" || nextOption === "git-repo") {
													showWorkspaceSourceInput = true;
													return;
												}
												showWorkspaceSourceInput = false;
												workspaceSourceInput = "";
											}}
										>
											{#if availableWorkspaces.length > 0}
												<optgroup label="Existing workspaces">
													{#each availableWorkspaces as workspace (workspace.id)}
														<option value={`existing:${workspace.id}`}>
															{getWorkspaceOptionLabel(workspace)}
														</option>
													{/each}
												</optgroup>
											{/if}
											<optgroup label="Create new">
												<option value="new-workspace">New Workspace</option>
												<option value="local-directory">Local Directory</option>
												<option value="git-repo">GitHub Repo</option>
											</optgroup>
										</NativeSelect>
									{/if}
								</div>

								{#if showBranchSelector}
									<DropdownMenu>
										<DropdownMenuTrigger class="tauri-no-drag">
											<InputGroupButton
												type="button"
												size="icon-sm"
												variant="ghost"
												aria-label="Select branch"
												title={selectedWorkspaceBranch || "No branch selected"}
											>
												<GitBranchIcon
													class={`size-4 ${selectedWorkspaceBranch ? "text-foreground" : "text-muted-foreground"}`}
												/>
											</InputGroupButton>
										</DropdownMenuTrigger>
										<DropdownMenuContent align="end" class="w-56">
											<DropdownMenuItem
												onclick={() => {
													selectedWorkspaceBranch = "";
												}}
												class="justify-between"
											>
												<span>No branch</span>
												{#if selectedWorkspaceBranch === ""}
													<CheckIcon class="size-3.5 text-primary" />
												{/if}
											</DropdownMenuItem>
											<DropdownMenuSeparator />
											{#each availableWorkspaceBranches as branch (branch)}
												<DropdownMenuItem
													onclick={() => {
														selectedWorkspaceBranch = branch;
													}}
													class="justify-between"
												>
													<span class="truncate">{branch}</span>
													{#if selectedWorkspaceBranch === branch}
														<CheckIcon class="size-3.5 text-primary" />
													{/if}
												</DropdownMenuItem>
											{/each}
										</DropdownMenuContent>
									</DropdownMenu>
								{/if}
							{/if}

							<InputGroupButton
								type={isGenerating() ? "button" : "submit"}
								variant="default"
								size="icon-sm"
								onclick={(event) => {
									event.preventDefault();
									void submitComposer();
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

</div>
