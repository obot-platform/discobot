<script lang="ts">
	import { getTheme, toggleTheme } from "$lib/theme";
	import AppHeader from "$lib/components/ide/app-header.svelte";
	import ConversationPane from "$lib/components/ide/conversation-pane.svelte";
	import DockPanel from "$lib/components/ide/dock-panel.svelte";
	import SessionToolbar from "$lib/components/ide/session-toolbar.svelte";
	import ThreadSidebar from "$lib/components/ide/thread-sidebar.svelte";
	import {
		PREFERRED_IDE_STORAGE_KEY,
		allThreads,
		baseBranch,
		baseCommit,
		fileContents,
		files,
		ideOptions,
		issueReference,
		pullRequestReference,
		recentThreads,
		sessionStatus,
		services,
		suggestedPrompts,
		windowControls,
		workflowActions,
		workspaceTarget,
	} from "$lib/mock-shell-data";
	import type { CenterPanel, PreferredIde, WindowControlsSide } from "$lib/shell-types";

	let theme = $state(getTheme());
	let centerPanel = $state<CenterPanel>("chat");
	let currentThread = $state(allThreads[0]);
	let ideMenuOpen = $state(false);
	let preferredIde = $state<PreferredIde>(loadPreferredIde());
	let selectedFile = $state(files[0]);
	let windowControlsSide = detectWindowControlsSide();

	function detectWindowControlsSide(): WindowControlsSide {
		if (typeof navigator === "undefined") {
			return "right";
		}

		const nav = navigator as Navigator & {
			userAgentData?: {
				platform?: string;
			};
		};
		const platform = nav.userAgentData?.platform || nav.platform || nav.userAgent;
		return /mac/i.test(platform) ? "left" : "right";
	}

	function loadPreferredIde(): PreferredIde {
		if (typeof window === "undefined") {
			return "cursor";
		}

		const stored = window.localStorage.getItem(PREFERRED_IDE_STORAGE_KEY);
		return ideOptions.some((option) => option.id === stored)
			? (stored as PreferredIde)
			: "cursor";
	}

	function savePreferredIde(ide: PreferredIde) {
		preferredIde = ide;
		ideMenuOpen = false;
		if (typeof window !== "undefined") {
			window.localStorage.setItem(PREFERRED_IDE_STORAGE_KEY, ide);
		}
	}

	function preferredIdeLabel() {
		return ideOptions.find((option) => option.id === preferredIde)?.label ?? "Cursor";
	}

	function handleThemeToggle() {
		theme = toggleTheme();
	}

	function openTerminal() {
		centerPanel = "terminal";
	}

	function openDesktop() {
		centerPanel = "desktop";
	}

	function openService(serviceId: string) {
		centerPanel = `service:${serviceId}`;
	}

	function openFiles(file = selectedFile) {
		selectedFile = file;
		centerPanel = "files";
	}

	function openChat() {
		centerPanel = "chat";
	}

	function selectThread(thread: string) {
		currentThread = thread;
		openChat();
	}
</script>

<svelte:head>
	<title>Discobot UI</title>
</svelte:head>

<div class="min-h-screen text-foreground">
	<div class="flex min-h-screen flex-col">
		<AppHeader
			{currentThread}
			onThemeToggle={handleThemeToggle}
			{sessionStatus}
			{theme}
			{windowControls}
			{windowControlsSide}
			{workflowActions}
			{workspaceTarget}
		/>

		<div class="grid flex-1 lg:grid-cols-[16rem_minmax(0,1fr)]">
			<ThreadSidebar
				{allThreads}
				{currentThread}
				onSelectThread={selectThread}
				{recentThreads}
			/>

			<main class="flex min-h-0 flex-col">
				<SessionToolbar
					{baseBranch}
					{baseCommit}
					{centerPanel}
					{currentThread}
					{ideMenuOpen}
					{ideOptions}
					{issueReference}
					onChooseIde={savePreferredIde}
					onOpenDesktop={openDesktop}
					onOpenFiles={() => openFiles()}
					onOpenService={openService}
					onOpenTerminal={openTerminal}
					onToggleIdeMenu={() => (ideMenuOpen = !ideMenuOpen)}
					preferredIde={preferredIde}
					preferredIdeLabel={preferredIdeLabel()}
					pullRequestReference={pullRequestReference}
					{services}
				/>

				<div class="flex-1 overflow-auto p-4">
					<div class={centerPanel === "chat" ? "grid min-h-[28rem] gap-4 lg:grid-rows-[minmax(0,1fr)_9rem]" : "grid min-h-[28rem] gap-4 xl:grid-cols-[1.15fr_0.85fr]"}>
						<ConversationPane {centerPanel} {currentThread} />

						{#if centerPanel !== "chat"}
							<DockPanel
								{centerPanel}
								{fileContents}
								{files}
								onClose={openChat}
								onSelectFile={openFiles}
								{selectedFile}
								{services}
								{suggestedPrompts}
							/>
						{/if}
					</div>
				</div>
			</main>
		</div>
	</div>
</div>
