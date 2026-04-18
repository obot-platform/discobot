<script lang="ts">
	import ChevronDownIcon from "@lucide/svelte/icons/chevron-down";
	import GitBranchIcon from "@lucide/svelte/icons/git-branch";
	import GitCommitIcon from "@lucide/svelte/icons/git-commit";
	import { untrack } from "svelte";
	import ClockIcon from "@lucide/svelte/icons/clock";
	import Loader2Icon from "@lucide/svelte/icons/loader-2";
	import type { Component } from "svelte";
	import { SvelteSet } from "svelte/reactivity";
	import {
		DropdownMenu,
		DropdownMenuContent,
		DropdownMenuGroup,
		DropdownMenuItem,
		DropdownMenuLabel,
		DropdownMenuSeparator,
		DropdownMenuSub,
		DropdownMenuSubContent,
		DropdownMenuSubTrigger,
		DropdownMenuTrigger,
	} from "$lib/components/ui/dropdown-menu";
	import { Button } from "$lib/components/ui/button";
	import { SplitDropdownButton } from "$lib/components/ui/split-dropdown-button";
	import SessionCommandCredentialsDialog from "$lib/components/app/parts/SessionCommandCredentialsDialog.svelte";
	import { getSSHPort } from "$lib/api-config";
	import type { AgentCommand } from "$lib/api-types";
	import { useAppContext } from "$lib/context/app-context.svelte";
	import { useSessionContext } from "$lib/context/session-context.svelte";
	import { IsMobile } from "$lib/hooks/is-mobile.svelte.js";
	import { openUrl } from "$lib/shell";
	import {
		DESKTOP_SERVICE_ID,
		type IdeOption,
		type JetBrainsIdeOption,
	} from "$lib/shell-types";

	type Props = {
		sessionId: string;
	};

	type LucideIcon = Component<{ class?: string }>;
	type LucideIconModule = { default: LucideIcon };

	let { sessionId }: Props = $props();
	const app = useAppContext();
	const isMobile = new IsMobile(1024);
	const lucideIconModules = import.meta.glob<LucideIconModule>(
		"../../../../node_modules/@lucide/svelte/dist/icons/*.js",
	);
	const staticCommandIcons: Record<string, LucideIcon> = {
		"git-branch": GitBranchIcon,
		"git-commit": GitCommitIcon,
	};
	const attemptedCommandIcons = new SvelteSet<string>();
	const preferences = app.preferences;
	const session = useSessionContext(untrack(() => sessionId));
	const sessionView = session.ui;
	let loadedCommandIcons = $state<Record<string, LucideIcon>>({});
	const sessionServices = $derived.by(() =>
		session.services.list.filter(
			(service) => service.id !== DESKTOP_SERVICE_ID,
		),
	);

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

	const uiCommands = $derived.by(() => session.commands.uiVisible);
	const primaryCommand = $derived.by(() => uiCommands[0] ?? null);
	const secondaryCommands = $derived.by(() => uiCommands.slice(1));
	const commandGroups = $derived.by(() => {
		const groups: Array<{
			key: string;
			label: string | null;
			commands: AgentCommand[];
		}> = [];
		for (const command of uiCommands) {
			const label = command.discobot?.group?.trim() || null;
			const key = label ?? "__ungrouped__";
			const existing = groups.find((group) => group.key === key);
			if (existing) {
				existing.commands.push(command);
				continue;
			}
			groups.push({ key, label, commands: [command] });
		}
		return groups;
	});
	const groupedSecondaryCommands = $derived.by(() =>
		commandGroups
			.map((group) => ({
				...group,
				commands: group.commands.filter(
					(command) => command !== primaryCommand,
				),
			}))
			.filter((group) => group.commands.length > 0),
	);

	$effect(() => {
		for (const iconName of uiCommands
			.map((command) => normalizeLucideIconName(command.discobot?.icon))
			.filter((iconName): iconName is string => iconName !== null)) {
			if (attemptedCommandIcons.has(iconName)) {
				continue;
			}
			attemptedCommandIcons.add(iconName);
			if (staticCommandIcons[iconName]) {
				continue;
			}
			const loader =
				lucideIconModules[
					`../../../../node_modules/@lucide/svelte/dist/icons/${iconName}.js`
				];
			if (!loader) {
				continue;
			}
			void loader().then((module) => {
				loadedCommandIcons = {
					...loadedCommandIcons,
					[iconName]: module.default,
				};
			});
		}
	});
	const operationState = $derived.by(() => {
		const isPending = session.current?.status === "pending";
		const activeCommandName = normalizeActiveCommandName(
			session.threads.selected?.activeCommand,
		);
		const showBusy = activeCommandName !== null || isPending;
		const activeCommand =
			uiCommands.find((command) => command.name === activeCommandName) ?? null;
		const primaryLabel =
			primaryCommand?.discobot?.label || primaryCommand?.name || "Run";
		return {
			activeCommandName,
			showSplitButton: secondaryCommands.length > 0,
			showPending: isPending,
			showBusy,
			buttonLabel: isPending
				? "Pending..."
				: activeCommand
					? activeCommand.discobot?.activeLabel?.trim() ||
						`${activeCommand.discobot?.label || activeCommand.name}...`
					: activeCommandName
						? `${activeCommandName}...`
						: primaryLabel,
		};
	});
	const operationDisabled = $derived.by(
		() =>
			!session.current ||
			!primaryCommand ||
			operationState.showBusy ||
			session.commands.isSubmitting ||
			session.commands.credentialDialog.open,
	);

	function commandLabel(command: AgentCommand): string {
		return command.discobot?.label?.trim() || command.name;
	}

	function normalizeLucideIconName(
		name: string | null | undefined,
	): string | null {
		const trimmed = name?.trim() ?? "";
		if (trimmed.length === 0) {
			return null;
		}
		return trimmed
			.replace(/([a-z0-9])([A-Z])/g, "$1-$2")
			.replace(/[\s_]+/g, "-")
			.toLowerCase();
	}

	function commandIcon(command: AgentCommand): LucideIcon | null {
		const iconName = normalizeLucideIconName(command.discobot?.icon);
		if (!iconName) {
			return null;
		}
		return staticCommandIcons[iconName] ?? loadedCommandIcons[iconName] ?? null;
	}

	function normalizeActiveCommandName(
		name: string | null | undefined,
	): string | null {
		const trimmed = name?.trim() ?? "";
		return trimmed.length > 0 ? trimmed : null;
	}

	function commandBusy(command: AgentCommand) {
		return operationState.activeCommandName === command.name;
	}

	function handlePrimaryCommand() {
		if (!primaryCommand) {
			return;
		}
		void session.commands.run(primaryCommand).catch((error) => {
			console.error(`Failed to start ${primaryCommand.name}:`, error);
		});
	}

	function handleCommand(command: AgentCommand) {
		void session.commands.run(command).catch((error) => {
			console.error(`Failed to start ${command.name}:`, error);
		});
	}
</script>

<div
	class="flex h-full w-full min-w-0 items-center justify-end gap-2 bg-background px-2"
	data-tauri-drag-region
>
	{#if !isMobile.current}
		<div
			class="tauri-no-drag inline-flex items-center overflow-hidden rounded-md border border-border bg-background p-0.5 shadow-xs"
		>
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
	{:else if diffStats.filesChanged > 0}
		<div
			class="tauri-no-drag inline-flex items-center overflow-hidden rounded-md border border-border bg-background p-0.5 shadow-xs"
		>
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
		</div>
	{/if}

	{#if session.current && primaryCommand}
		{#if operationState.showSplitButton}
			<DropdownMenu>
				<div
					class="tauri-no-drag inline-flex items-center overflow-hidden rounded-md border border-border bg-background p-0.5 shadow-xs"
				>
					<Button
						variant="outline"
						size="xs"
						onclick={handlePrimaryCommand}
						disabled={operationDisabled}
						class="gap-1.5 rounded-l-[calc(var(--radius)-1px)] rounded-r-none border-0 bg-transparent shadow-none dark:bg-transparent"
						title={commandLabel(primaryCommand)}
					>
						{#if operationState.showPending}
							<ClockIcon class="size-3.5" />
						{:else if operationState.showBusy}
							<Loader2Icon class="size-3.5 animate-spin" />
						{:else}
							{@const PrimaryIcon = commandIcon(primaryCommand)}
							{#if PrimaryIcon}
								<PrimaryIcon class="size-3.5" />
							{/if}
						{/if}
						{operationState.buttonLabel}
					</Button>
					<DropdownMenuTrigger>
						{#snippet child({ props })}
							<Button
								{...props}
								variant="outline"
								size="xs"
								disabled={operationDisabled}
								class="rounded-r-[calc(var(--radius)-1px)] rounded-l-none border-0 border-l border-border bg-transparent px-2 shadow-none dark:bg-transparent"
								aria-label="More actions"
								title="More actions"
							>
								<ChevronDownIcon class="size-3.5" />
							</Button>
						{/snippet}
					</DropdownMenuTrigger>
				</div>
				<DropdownMenuContent align="end" sideOffset={8} class="min-w-[8rem]">
					{#each groupedSecondaryCommands as group, index}
						{#if index > 0}
							<DropdownMenuSeparator />
						{/if}
						<DropdownMenuGroup>
							{#if group.label}
								<DropdownMenuLabel
									class="text-xs uppercase tracking-[0.16em] text-muted-foreground"
								>
									{group.label}
								</DropdownMenuLabel>
							{/if}
							{#each group.commands as command}
								<DropdownMenuItem
									onclick={() => handleCommand(command)}
									class="gap-2"
								>
									{#if commandBusy(command)}
										<Loader2Icon class="size-3.5 animate-spin" />
									{:else}
										{@const Icon = commandIcon(command)}
										{#if Icon}
											<Icon class="size-3.5" />
										{/if}
									{/if}
									{commandLabel(command)}
								</DropdownMenuItem>
							{/each}
						</DropdownMenuGroup>
					{/each}
				</DropdownMenuContent>
			</DropdownMenu>
		{:else}
			<Button
				variant="outline"
				size="xs"
				onclick={handlePrimaryCommand}
				disabled={operationDisabled}
				class="tauri-no-drag gap-1.5"
				title={commandLabel(primaryCommand)}
			>
				{#if operationState.showPending}
					<ClockIcon class="size-3.5" />
				{:else if operationState.showBusy}
					<Loader2Icon class="size-3.5 animate-spin" />
				{:else}
					{@const PrimaryIcon = commandIcon(primaryCommand)}
					<PrimaryIcon class="size-3.5" />
				{/if}
				{operationState.buttonLabel}
			</Button>
		{/if}
	{/if}

	{#if !isMobile.current}
		<SplitDropdownButton
			class="tauri-no-drag"
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
	{/if}

	<SessionCommandCredentialsDialog dialog={session.commands.credentialDialog} />
</div>
