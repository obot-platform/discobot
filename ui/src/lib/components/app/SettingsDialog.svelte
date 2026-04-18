<script lang="ts">
	import InfoIcon from "@lucide/svelte/icons/info";
	import RefreshCwIcon from "@lucide/svelte/icons/refresh-cw";
	import { RECENT_THREADS_VISIBLE_LIMIT_PRESETS } from "$lib/store/ui-state.store.svelte";
	import { Button } from "$lib/components/ui/button";
	import {
		Card,
		CardAction,
		CardContent,
		CardDescription,
		CardHeader,
		CardTitle,
	} from "$lib/components/ui/card";
	import * as Dialog from "$lib/components/ui/dialog";
	import {
		Item,
		ItemActions,
		ItemContent,
		ItemDescription,
		ItemGroup,
		ItemSeparator,
		ItemTitle,
	} from "$lib/components/ui/item";
	import { Label } from "$lib/components/ui/label";
	import { NativeSelect } from "$lib/components/ui/native-select";
	import { Progress } from "$lib/components/ui/progress";
	import { Switch } from "$lib/components/ui/switch";
	import {
		ToggleGroup,
		ToggleGroupItem,
	} from "$lib/components/ui/toggle-group";
	import {
		Tabs,
		TabsContent,
		TabsList,
		TabsTrigger,
	} from "$lib/components/ui/tabs";
	import CredentialsManager from "$lib/components/app/CredentialsManager.svelte";
	import ProjectSettingsTabContent from "$lib/components/app/parts/ProjectSettingsTabContent.svelte";
	import SupportInfoDialog from "$lib/components/app/SupportInfoDialog.svelte";
	import { useAppContext } from "$lib/context/app-context.svelte";
	import type { ModelInfo, ThemeColorScheme } from "$lib/api-types";
	import type { ThemeMode } from "$lib/theme";

	const app = useAppContext();
	const models = app.models;
	const preferences = app.preferences;

	const dedupedModels = $derived.by(() => {
		const modelByProviderAndName: Record<string, ModelInfo> = {};

		for (const model of models.list) {
			const cleanName = model.name.replace(/\s*\(latest\)\s*/gi, "").trim();
			const isLatest = /\(latest\)/i.test(model.name);
			const dedupeKey = `${model.provider || "Other"}::${cleanName}`;
			const existing = modelByProviderAndName[dedupeKey];

			if (!existing || isLatest) {
				modelByProviderAndName[dedupeKey] = { ...model, name: cleanName };
			}
		}

		const getBaseName = (name: string) =>
			name
				.replace(/\s*\(latest\)\s*/gi, "")
				.replace(/\s+v\d+\s*/gi, "")
				.replace(/\s+[\d.]+\s*$/, "")
				.trim();

		const extractVersion = (name: string) => {
			const matches = name.match(/(\d+(?:\.\d+)?)/g);
			return matches?.length
				? Number.parseFloat(matches[matches.length - 1])
				: 0;
		};

		return Object.values(modelByProviderAndName).sort((a, b) => {
			const baseCompare = getBaseName(a.name).localeCompare(
				getBaseName(b.name),
			);
			if (baseCompare !== 0) return baseCompare;
			const versionDiff = extractVersion(b.name) - extractVersion(a.name);
			if (versionDiff !== 0) return versionDiff;
			return a.name.localeCompare(b.name);
		});
	});

	const selectedDefaultModel = $derived.by(() =>
		preferences.defaultModel
			? (models.list.find((model) => model.id === preferences.defaultModel) ??
				null)
			: null,
	);

	const modelProviderEntries = $derived.by(() => {
		const grouped: Record<string, ModelInfo[]> = {};
		for (const model of dedupedModels) {
			const provider = model.provider || "Other";
			if (!grouped[provider]) grouped[provider] = [];
			grouped[provider].push(model);
		}

		if (
			selectedDefaultModel &&
			!dedupedModels.some((model) => model.id === selectedDefaultModel.id)
		) {
			const provider = selectedDefaultModel.provider || "Other";
			if (!grouped[provider]) grouped[provider] = [];
			grouped[provider] = [selectedDefaultModel, ...grouped[provider]];
		}

		return Object.entries(grouped).sort(([a], [b]) => a.localeCompare(b));
	});
	const ui = app.ui;
	const updates = app.updates;
	const environment = app.environment;
	const showUpdateTab = $derived(environment.supportsAppUpdates);
	const themeModes: ThemeMode[] = ["light", "dark", "system"];

	function formatBytes(bytes: number): string {
		if (bytes < 1024 * 1024) {
			return `${(bytes / 1024).toFixed(1)} KB`;
		}
		return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
	}

	const updateProgress = $derived.by(() => {
		if (!updates.totalBytes || updates.totalBytes <= 0) {
			return 0;
		}
		return Math.min(
			100,
			Math.round((updates.downloadedBytes / updates.totalBytes) * 100),
		);
	});

	const activeThemeName = $derived.by(
		() =>
			preferences.availableThemes.find(
				(themeOption) => themeOption.id === preferences.colorScheme,
			)?.name ?? "Default",
	);

	function handleSettingsOpenChange(open: boolean) {
		if (!open) {
			ui.closeSettings();
			return;
		}

		if (!showUpdateTab && ui.settingsDialog.tab === "update") {
			ui.settingsDialog.tab = "appearance";
		}

		ui.settingsDialog.open = true;
	}

	function handleSettingsTabChange(value: string) {
		if (
			value !== "appearance" &&
			value !== "chat" &&
			value !== "project" &&
			value !== "credentials" &&
			(showUpdateTab || value !== "update")
		) {
			return;
		}

		ui.settingsDialog.tab = value;
	}

	function handleSettingsInteractOutside(event: Event) {
		event.preventDefault();
	}

	function handleSettingsEscapeKeydown(event: KeyboardEvent) {
		event.preventDefault();
	}
</script>

<Dialog.Root
	open={ui.settingsDialog.open}
	onOpenChange={handleSettingsOpenChange}
>
	<Dialog.Content
		class="sm:max-w-2xl"
		onInteractOutside={handleSettingsInteractOutside}
		onEscapeKeydown={handleSettingsEscapeKeydown}
	>
		<Dialog.Header>
			<Dialog.Title>Settings</Dialog.Title>
			<Dialog.Description>
				Configure appearance, chat defaults, system tools, {showUpdateTab
					? "updates, and support tools"
					: "and support tools"}.
			</Dialog.Description>
		</Dialog.Header>

		<Tabs
			value={ui.settingsDialog.tab}
			onValueChange={handleSettingsTabChange}
			class="mt-1"
		>
			<TabsList
				class={`grid w-full ${showUpdateTab ? "grid-cols-5" : "grid-cols-4"}`}
			>
				<TabsTrigger value="appearance">Appearance</TabsTrigger>
				<TabsTrigger value="chat">Chat</TabsTrigger>
				<TabsTrigger value="project">System</TabsTrigger>
				{#if showUpdateTab}
					<TabsTrigger value="update">
						<span class="relative inline-flex items-center px-2">
							Update
							{#if updates.showBadge}
								<span
									class="absolute -right-1 top-0 h-2 w-2 rounded-full bg-blue-500"
								></span>
							{/if}
						</span>
					</TabsTrigger>
				{/if}
				<TabsTrigger value="credentials">Credentials</TabsTrigger>
			</TabsList>

			<div class="mt-3 min-h-[28rem]">
				<TabsContent value="appearance" class="mt-0 h-full">
					<Card class="gap-4 py-4">
						<CardHeader class="gap-1 border-b pb-4">
							<CardTitle class="text-sm">Appearance</CardTitle>
							<CardDescription
								>Mode and color theme preferences.</CardDescription
							>
						</CardHeader>
						<CardContent>
							<ItemGroup class="rounded-md border border-border">
								<Item size="sm">
									<ItemContent>
										<ItemTitle>Mode</ItemTitle>
										<ItemDescription>
											Resolved mode: {preferences.resolvedTheme}
										</ItemDescription>
									</ItemContent>
									<ItemActions class="ml-auto justify-end">
										<ToggleGroup
											type="single"
											value={preferences.theme}
											onValueChange={(value) => {
												if (
													value === "light" ||
													value === "dark" ||
													value === "system"
												) {
													preferences.setTheme(value);
												}
											}}
											variant="outline"
											size="sm"
											spacing={1}
											class="rounded-full border border-border bg-muted p-1"
										>
											{#each themeModes as mode (mode)}
												<ToggleGroupItem
													value={mode}
													class="rounded-full border border-transparent px-3 capitalize data-[state=off]:bg-transparent data-[state=off]:text-muted-foreground data-[state=on]:border-primary data-[state=on]:bg-primary data-[state=on]:text-primary-foreground data-[state=on]:shadow-sm"
												>
													{mode}
												</ToggleGroupItem>
											{/each}
										</ToggleGroup>
									</ItemActions>
								</Item>
								<ItemSeparator />
								<Item size="sm">
									<ItemContent>
										<ItemTitle>Theme</ItemTitle>
										<ItemDescription
											>Current palette: {activeThemeName}</ItemDescription
										>
									</ItemContent>
									<ItemActions class="ml-auto w-56 justify-end">
										<Label for="settings-theme" class="sr-only">Theme</Label>
										<NativeSelect
											id="settings-theme"
											value={preferences.colorScheme}
											onchange={(event) => {
												preferences.setColorScheme(
													(event.currentTarget as HTMLSelectElement)
														.value as ThemeColorScheme,
												);
											}}
											class="w-full"
										>
											{#each preferences.availableThemes as themeOption (themeOption.mode + themeOption.id)}
												<option value={themeOption.id}
													>{themeOption.name}</option
												>
											{/each}
										</NativeSelect>
									</ItemActions>
								</Item>
								<ItemSeparator />
								<Item size="sm">
									<ItemContent>
										<ItemTitle>Recent list size</ItemTitle>
										<ItemDescription>
											Choose how many recent threads appear in the sidebar.
										</ItemDescription>
									</ItemContent>
									<ItemActions class="ml-auto justify-end">
										<ToggleGroup
											type="single"
											value={String(preferences.recentThreadsVisibleLimit)}
											onValueChange={(value) => {
												const nextValue = Number(value);
												if (
													RECENT_THREADS_VISIBLE_LIMIT_PRESETS.includes(
														nextValue as (typeof RECENT_THREADS_VISIBLE_LIMIT_PRESETS)[number],
													)
												) {
													preferences.setRecentThreadsVisibleLimit(nextValue);
												}
											}}
											variant="outline"
											size="sm"
											spacing={1}
											class="rounded-full border border-border bg-muted p-1"
										>
											{#each RECENT_THREADS_VISIBLE_LIMIT_PRESETS as limit (limit)}
												<ToggleGroupItem
													value={String(limit)}
													class="rounded-full border border-transparent px-3 data-[state=off]:bg-transparent data-[state=off]:text-muted-foreground data-[state=on]:border-primary data-[state=on]:bg-primary data-[state=on]:text-primary-foreground data-[state=on]:shadow-sm"
												>
													{limit === 1 ? "Off" : limit}
												</ToggleGroupItem>
											{/each}
										</ToggleGroup>
									</ItemActions>
								</Item>
								<ItemSeparator />
								<Item size="sm">
									<ItemContent>
										<ItemTitle>Show refresh button</ItemTitle>
										<ItemDescription>
											Display a refresh button in the header bar.
										</ItemDescription>
									</ItemContent>
									<ItemActions>
										<Switch
											id="settings-show-refresh-button"
											checked={preferences.showRefreshButton}
											onCheckedChange={(checked) => {
												preferences.setShowRefreshButton(checked === true);
											}}
										/>
									</ItemActions>
								</Item>
							</ItemGroup>
						</CardContent>
					</Card>
				</TabsContent>

				<TabsContent value="chat" class="mt-0 h-full">
					<Card class="gap-4 py-4">
						<CardHeader class="gap-1 border-b pb-4">
							<CardTitle class="text-sm">Chat</CardTitle>
							<CardDescription
								>Conversation defaults for new prompts.</CardDescription
							>
						</CardHeader>
						<CardContent>
							<ItemGroup class="rounded-md border border-border">
								<Item size="sm">
									<ItemContent>
										<ItemTitle>Default model</ItemTitle>
										<ItemDescription
											>Set a preferred model or keep auto-select.</ItemDescription
										>
									</ItemContent>
									<ItemActions class="ml-auto w-56 justify-end">
										<Label for="settings-default-model" class="sr-only"
											>Default model</Label
										>
										<NativeSelect
											id="settings-default-model"
											value={preferences.defaultModel || "__auto__"}
											onchange={(event) => {
												const next = (event.currentTarget as HTMLSelectElement)
													.value;
												preferences.setDefaultModel(
													next === "__auto__" ? "" : next,
												);
											}}
											class="w-full"
										>
											<option value="__auto__">Auto-select</option>
											{#each modelProviderEntries as [provider, providerModels] (provider)}
												<optgroup label={provider}>
													{#each providerModels as model (model.id)}
														<option value={model.id}>{model.name}</option>
													{/each}
												</optgroup>
											{/each}
										</NativeSelect>
									</ItemActions>
								</Item>
								<ItemSeparator />
								<Item size="sm">
									<ItemContent>
										<ItemTitle>Full width conversation</ItemTitle>
										<ItemDescription>
											Expand messages and composer to use full space.
										</ItemDescription>
									</ItemContent>
									<ItemActions>
										<Switch
											id="settings-chat-width"
											checked={preferences.chatWidthMode === "full"}
											onCheckedChange={(checked) => {
												preferences.setChatWidthMode(
													checked === true ? "full" : "constrained",
												);
											}}
										/>
									</ItemActions>
								</Item>
							</ItemGroup>
						</CardContent>
					</Card>
				</TabsContent>

				<TabsContent value="project" class="mt-0 h-full">
					<ProjectSettingsTabContent
						active={ui.settingsDialog.open &&
							ui.settingsDialog.tab === "project"}
					/>
				</TabsContent>

				{#if showUpdateTab}
					<TabsContent value="update" class="mt-0 h-full">
						<Card class="gap-4 py-4">
							<CardHeader class="gap-1 border-b pb-4">
								<CardTitle class="text-sm">Update</CardTitle>
								<CardDescription
									>Check, download, and install app updates.</CardDescription
								>
								<CardAction>
									<Button
										variant="ghost"
										size="xs"
										onclick={() => {
											void updates.check();
										}}
										disabled={updates.status === "checking" ||
											updates.status === "downloading" ||
											updates.status === "installing"}
									>
										<RefreshCwIcon
											class={`size-3.5 ${updates.status === "checking" ? "animate-spin" : ""}`}
										/>
										Check
									</Button>
								</CardAction>
							</CardHeader>
							<CardContent class="space-y-3">
								{#if updates.canTrackPrereleases}
									<div
										class="flex items-start justify-between gap-4 rounded-md border border-border bg-background p-3"
									>
										<div class="space-y-1">
											<Label class="text-sm font-medium"
												>Track pre-releases</Label
											>
											<p class="text-sm text-muted-foreground">
												Use the latest GitHub pre-release channel instead of
												stable releases.
											</p>
										</div>
										<Switch
											checked={updates.trackPrereleases}
											onCheckedChange={(checked) =>
												void updates.setTrackPrereleases(checked === true)}
										/>
									</div>
								{/if}
								{#if updates.status === "ready" && !updates.isIgnored}
									<div
										class="rounded-md border border-border bg-background p-3"
									>
										<p class="text-sm text-muted-foreground">
											Version {updates.availableVersion} is ready to install{#if updates.trackPrereleases}
												from the pre-release channel{/if}.
										</p>
										<div class="mt-3 flex items-center gap-2">
											<Button
												variant="default"
												size="xs"
												onclick={() => void updates.installAndRelaunch()}
											>
												Restart to update
											</Button>
											<Button
												variant="outline"
												size="xs"
												onclick={updates.ignore}>Ignore</Button
											>
										</div>
									</div>
								{:else if updates.status === "ready" && updates.isIgnored}
									<div
										class="rounded-md border border-border bg-background p-3 text-sm text-muted-foreground"
									>
										Version {updates.availableVersion} available (ignored).
									</div>
								{:else if updates.status === "checking"}
									<div
										class="rounded-md border border-border bg-background p-3 text-sm text-muted-foreground"
									>
										Checking for updates...
									</div>
								{:else if updates.status === "downloading"}
									<div
										class="space-y-2 rounded-md border border-border bg-background p-3"
									>
										<div
											class="flex items-center justify-between text-xs text-muted-foreground"
										>
											<span>Downloading update...</span>
											<span>
												{#if updates.totalBytes !== null}
													{formatBytes(updates.downloadedBytes)} / {formatBytes(
														updates.totalBytes,
													)}
												{:else}
													{formatBytes(updates.downloadedBytes)}
												{/if}
											</span>
										</div>
										<Progress value={updateProgress} />
									</div>
								{:else if updates.status === "installing"}
									<div
										class="rounded-md border border-border bg-background p-3 text-sm text-muted-foreground"
									>
										Installing update...
									</div>
								{:else if updates.status === "error"}
									<div
										class="rounded-md border border-destructive/30 bg-destructive/10 p-3 text-sm text-destructive"
									>
										Update failed: {updates.error}
									</div>
								{:else}
									<div
										class="rounded-md border border-border bg-background p-3 text-sm text-muted-foreground"
									>
										You're on the latest version.
									</div>
								{/if}
							</CardContent>
						</Card>
					</TabsContent>
				{/if}

				<TabsContent value="credentials" class="mt-0 h-full">
					<Card class="gap-4 py-4">
						<CardHeader class="gap-1 border-b pb-4">
							<CardTitle class="text-sm">API Credentials</CardTitle>
							<CardDescription>
								Create, update, or remove credentials for Anthropic, OpenAI,
								Tavily, and GitHub.
							</CardDescription>
						</CardHeader>
						<CardContent>
							{#if ui.settingsDialog.open && ui.settingsDialog.tab === "credentials"}
								<CredentialsManager />
							{/if}
						</CardContent>
					</Card>
				</TabsContent>
			</div>
		</Tabs>

		<Dialog.Footer class="mt-3">
			<div class="flex w-full items-center justify-between gap-2">
				<Button
					variant="outline"
					size="icon-sm"
					onclick={ui.openSupportInfo}
					title="Support information"
					aria-label="Support information"
				>
					<InfoIcon class="size-4" />
				</Button>
				<Button variant="default" size="sm" onclick={ui.closeSettings}
					>Done</Button
				>
			</div>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>

{#if ui.supportInfoDialogOpen}
	<SupportInfoDialog />
{/if}
