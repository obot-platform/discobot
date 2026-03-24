<script lang="ts">
	import ChevronDownIcon from "@lucide/svelte/icons/chevron-down";
	import ClockIcon from "@lucide/svelte/icons/clock";
	import GitBranchIcon from "@lucide/svelte/icons/git-branch";
	import GitCommitIcon from "@lucide/svelte/icons/git-commit";
	import Loader2Icon from "@lucide/svelte/icons/loader-2";
	import {
		DropdownMenu,
		DropdownMenuContent,
		DropdownMenuItem,
		DropdownMenuLabel,
		DropdownMenuSub,
		DropdownMenuSubContent,
		DropdownMenuSubTrigger,
		DropdownMenuTrigger,
	} from "$lib/components/ui/dropdown-menu";
	import { Button } from "$lib/components/ui/button";
	import { SplitDropdownButton } from "$lib/components/ui/split-dropdown-button";
	import { getSSHPort } from "$lib/api-config";
	import { api } from "$lib/api-client";
	import type { CommitOperation } from "$lib/api-types";
	import { useAppContext } from "$lib/context/app-context.svelte";
	import { useSessionContext } from "$lib/context/session-context.svelte";
	import { getSessionToolbarOperationState } from "$lib/components/app/session-toolbar-actions";
	import { openUrl } from "$lib/tauri";
	import {
		DESKTOP_SERVICE_ID,
		type IdeOption,
		type JetBrainsIdeOption,
	} from "$lib/shell-types";

	const app = useAppContext();
	const preferences = app.preferences;
	const session = useSessionContext();
	const sessionView = session.ui;
	const sessionServices = $derived.by(() =>
		session.services.list.filter(
			(service) => service.id !== DESKTOP_SERVICE_ID,
		),
	);

	let startingOperation = $state<CommitOperation | null>(null);
	let waitingForOperationEvent = $state(false);

	function isJetBrainsIdeOption(
		option: IdeOption,
	): option is JetBrainsIdeOption {
		return option.family === "jetbrains";
	}

	function preferredIdeOption() {
		return (
			preferences.ideOptions.find(
				(option) => option.id === preferences.preferredIde,
			) ??
			preferences.ideOptions[0] ??
			null
		);
	}

	const WORKSPACE_PATH = "/home/discobot/workspace";

	const standardIdeOptions = $derived.by(() =>
		preferences.ideOptions.filter((option) => option.family === "standard"),
	);

	const jetbrainsIdeOptions = $derived.by(() =>
		preferences.ideOptions.filter(isJetBrainsIdeOption),
	);

	function getSSHHost() {
		if (typeof window === "undefined") {
			return "localhost";
		}

		const { hostname } = window.location;
		if (hostname === "127.0.0.1" || hostname === "::1") {
			return "localhost";
		}

		return hostname;
	}

	function buildJetBrainsIdeUrl(option: JetBrainsIdeOption, sessionId: string) {
		const host = getSSHHost();
		const port = getSSHPort();
		const params = new URLSearchParams({
			h: host,
			u: sessionId,
			p: String(port),
			launchIde: "true",
			ideHint: option.productCode,
			projectHint: WORKSPACE_PATH,
		});
		return `jetbrains://gateway/ssh/environment?${params.toString()}`;
	}

	function buildIdeUrl(option: IdeOption, sessionId: string) {
		const host = getSSHHost();
		const port = getSSHPort();
		if (option.family === "jetbrains") {
			return buildJetBrainsIdeUrl(option, sessionId);
		}
		if (option.id === "zed") {
			return `zed://ssh/${sessionId}@${host}:${port}${WORKSPACE_PATH}`;
		}
		if (option.id === "cursor") {
			return `cursor://vscode-remote/ssh-remote+${sessionId}@${host}:${port}${WORKSPACE_PATH}`;
		}
		return `vscode://vscode-remote/ssh-remote+${sessionId}@${host}:${port}${WORKSPACE_PATH}`;
	}

	const selectedIdeOption = $derived.by(() => preferredIdeOption());

	const ideLaunchUrl = $derived.by(() => {
		const activeSessionId = session.current?.id;
		if (!activeSessionId || !selectedIdeOption) {
			return null;
		}
		return buildIdeUrl(selectedIdeOption, activeSessionId);
	});

	const preferredIdeActionDisabled = $derived.by(() => ideLaunchUrl === null);

	async function openPreferredIde() {
		if (!ideLaunchUrl) {
			return;
		}

		await openUrl(ideLaunchUrl);
	}

	function toggleTerminal() {
		if (sessionView.activeView.kind === "terminal") {
			sessionView.openChat();
			return;
		}

		sessionView.openTerminal();
	}

	function toggleDesktop() {
		if (sessionView.activeView.kind === "desktop") {
			sessionView.openChat();
			return;
		}

		sessionView.openDesktop();
	}

	function toggleFiles() {
		if (sessionView.activeView.kind === "file") {
			sessionView.openChat();
			return;
		}

		void session.files.open();
	}

	function toggleDiffReview() {
		if (sessionView.activeView.kind === "diff-review") {
			sessionView.openChat();
			return;
		}

		sessionView.openDiffReview();
	}

	function toggleServices() {
		if (sessionView.activeView.kind === "services") {
			sessionView.openChat();
			return;
		}

		sessionView.openServices();
	}

	const diffStats = $derived.by(() => {
		const stats = session.files.diffStats;
		const additions = stats.additions;
		const filesChanged = stats.filesChanged;
		const deletions = stats.deletions;
		return { additions, deletions, filesChanged };
	});

	const operationState = $derived.by(() =>
		getSessionToolbarOperationState({
			filesChanged: diffStats.filesChanged,
			session: session.current,
			startingOperation,
		}),
	);
	const operationDisabled = $derived.by(
		() => !session.current || operationState.showBusy,
	);
	const sessionStatus = $derived.by(() => session.current?.status);

	$effect(() => {
		const currentSessionStatus = sessionStatus;
		if (currentSessionStatus === undefined || !waitingForOperationEvent) {
			return;
		}

		waitingForOperationEvent = false;
		startingOperation = null;
	});

	async function ensureActiveThreadStream() {
		const activeThreadId = session.threads.selectedId ?? session.sessionId;
		await session.threadContexts.get(activeThreadId)?.load();
	}

	async function startOperation(operation: CommitOperation) {
		if (!session.current || startingOperation || operationState.showBusy) {
			return;
		}

		startingOperation = operation;
		try {
			if (operation === "commit") {
				await api.commitSession(session.sessionId);
			} else {
				await api.rebaseSession(session.sessionId);
			}

			waitingForOperationEvent = true;
			void ensureActiveThreadStream().catch((error) => {
				console.error("Failed to sync active thread stream:", error);
			});
		} catch (error) {
			console.error(`Failed to start ${operation}:`, error);
			waitingForOperationEvent = false;
			startingOperation = null;
		}
	}

	function handleCommit() {
		void startOperation("commit");
	}

	function handleRebase() {
		void startOperation("rebase");
	}
</script>

<div
	class="flex h-10 w-full min-w-0 items-center justify-end gap-2 bg-background px-2"
>
	<div class="inline-flex rounded-md border border-border bg-background p-0.5">
		<Button
			variant={sessionView.activeView.kind === "terminal"
				? "secondary"
				: "ghost"}
			size="xs"
			onclick={toggleTerminal}
		>
			Terminal
		</Button>
		<Button
			variant={sessionView.activeView.kind === "desktop"
				? "secondary"
				: "ghost"}
			size="xs"
			onclick={toggleDesktop}
		>
			Desktop
		</Button>
		<Button
			variant={sessionView.activeView.kind === "file" ? "secondary" : "ghost"}
			size="xs"
			onclick={toggleFiles}
		>
			Files
		</Button>
		{#if diffStats.filesChanged > 0}
			<Button
				variant={sessionView.activeView.kind === "diff-review"
					? "secondary"
					: "ghost"}
				size="xs"
				onclick={toggleDiffReview}
				class="gap-1"
			>
				<span class="text-green-500">+{diffStats.additions}</span>
				<span class="text-red-500">-{diffStats.deletions}</span>
				<span class="text-muted-foreground">{diffStats.filesChanged}</span>
			</Button>
		{/if}
		<Button
			variant={sessionView.activeView.kind === "services"
				? "secondary"
				: "ghost"}
			size="xs"
			onclick={toggleServices}
			disabled={sessionServices.length === 0}
		>
			Services
		</Button>
	</div>

	{#if session.current}
		{#if operationState.showSplitButton}
			<DropdownMenu>
				<div
					class="inline-flex items-center overflow-hidden rounded-md border border-border bg-background p-0.5 shadow-xs"
				>
					<Button
						variant="outline"
						size="xs"
						onclick={handleCommit}
						disabled={operationDisabled}
						class="gap-1.5 rounded-l-[calc(var(--radius)-1px)] rounded-r-none border-0 bg-transparent shadow-none dark:bg-transparent"
						title="Commit changes"
					>
						{#if operationState.showPending}
							<ClockIcon class="size-3.5" />
						{:else if operationState.showBusy}
							<Loader2Icon class="size-3.5 animate-spin" />
						{:else}
							<GitCommitIcon class="size-3.5" />
						{/if}
						{operationState.buttonLabel}
					</Button>
					<DropdownMenuTrigger>
						<Button
							variant="outline"
							size="xs"
							disabled={operationDisabled}
							class="rounded-r-[calc(var(--radius)-1px)] rounded-l-none border-0 border-l border-border bg-transparent px-2 shadow-none dark:bg-transparent"
							aria-label="More git actions"
							title="More git actions"
						>
							<ChevronDownIcon class="size-3.5" />
						</Button>
					</DropdownMenuTrigger>
				</div>
				<DropdownMenuContent align="end" sideOffset={8} class="min-w-[8rem]">
					<DropdownMenuLabel
						class="text-xs uppercase tracking-[0.16em] text-muted-foreground"
					>
						Git actions
					</DropdownMenuLabel>
					<DropdownMenuItem onclick={handleRebase} class="gap-2">
						<GitBranchIcon class="size-3.5" />
						Rebase
					</DropdownMenuItem>
				</DropdownMenuContent>
			</DropdownMenu>
		{:else}
			<Button
				variant="outline"
				size="xs"
				onclick={handleRebase}
				disabled={operationDisabled}
				class="gap-1.5"
				title="Rebase branch"
			>
				{#if operationState.showPending}
					<ClockIcon class="size-3.5" />
				{:else if operationState.showBusy}
					<Loader2Icon class="size-3.5 animate-spin" />
				{:else}
					<GitBranchIcon class="size-3.5" />
				{/if}
				{operationState.buttonLabel}
			</Button>
		{/if}
	{/if}

	<SplitDropdownButton
		class="tauri-no-drag py-0.5"
		label={`Open ${selectedIdeOption?.label ?? "Cursor"}`}
		menuAriaLabel="Select preferred IDE"
		onclick={openPreferredIde}
		primaryDisabled={preferredIdeActionDisabled}
		variant="outline"
		size="xs"
		contentClass="min-w-[11rem]"
	>
		<DropdownMenuLabel
			class="text-xs uppercase tracking-[0.16em] text-muted-foreground"
		>
			Preferred IDE
		</DropdownMenuLabel>
		{#each standardIdeOptions as option}
			<DropdownMenuItem
				onclick={() => preferences.setPreferredIde(option.id)}
				class="justify-between gap-3"
			>
				<span>{option.label}</span>
				{#if preferences.preferredIde === option.id}
					<span class="text-xs font-medium">Default</span>
				{/if}
			</DropdownMenuItem>
		{/each}
		<DropdownMenuSub>
			<DropdownMenuSubTrigger class="gap-3">
				<span>JetBrains</span>
				{#if selectedIdeOption?.family === "jetbrains"}
					<span class="text-xs font-medium">Default</span>
				{/if}
			</DropdownMenuSubTrigger>
			<DropdownMenuSubContent class="min-w-[13rem]">
				{#each jetbrainsIdeOptions as option}
					<DropdownMenuItem
						onclick={() => preferences.setPreferredIde(option.id)}
						class="justify-between gap-3"
					>
						<span>{option.label}</span>
						{#if preferences.preferredIde === option.id}
							<span class="text-xs font-medium">Default</span>
						{/if}
					</DropdownMenuItem>
				{/each}
			</DropdownMenuSubContent>
		</DropdownMenuSub>
	</SplitDropdownButton>
</div>
