<script lang="ts">
	import EyeIcon from "@lucide/svelte/icons/eye";
	import EyeOffIcon from "@lucide/svelte/icons/eye-off";
	import InfoIcon from "@lucide/svelte/icons/info";
	import PencilIcon from "@lucide/svelte/icons/pencil";
	import PlusIcon from "@lucide/svelte/icons/plus";
	import RefreshCwIcon from "@lucide/svelte/icons/refresh-cw";
	import Trash2Icon from "@lucide/svelte/icons/trash-2";
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
	import { Input } from "$lib/components/ui/input";
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
	import { ToggleGroup, ToggleGroupItem } from "$lib/components/ui/toggle-group";
	import { Tabs, TabsContent, TabsList, TabsTrigger } from "$lib/components/ui/tabs";
	import SupportInfoDialog from "$lib/components/ide/SupportInfoDialog.svelte";
	import { useAppContext } from "$lib/context/app-context.svelte";
	import type { ThemeColorScheme } from "$lib/api-types";
	import type { ThemeMode } from "$lib/theme";

	type CredentialEditorMode = "list" | "create" | "edit";

	const app = useAppContext();
	const themeModes: ThemeMode[] = ["light", "dark", "system"];
	let settingsTab = $state("appearance");
	let credentialsMode = $state<CredentialEditorMode>("list");
	let editingCredentialId = $state<string | null>(null);
	let selectedProviderId = $state("");
	let apiKeyDraft = $state("");
	let showApiKey = $state(false);

	function formatBytes(bytes: number): string {
		if (bytes < 1024 * 1024) {
			return `${(bytes / 1024).toFixed(1)} KB`;
		}
		return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
	}

	function providerName(providerId: string) {
		return app.authProviders.find((provider) => provider.id === providerId)?.name ?? providerId;
	}

	function resetCredentialEditor() {
		credentialsMode = "list";
		editingCredentialId = null;
		selectedProviderId = app.authProviders[0]?.id ?? "";
		apiKeyDraft = "";
		showApiKey = false;
	}

	function startCredentialCreate() {
		credentialsMode = "create";
		editingCredentialId = null;
		selectedProviderId = app.authProviders[0]?.id ?? "";
		apiKeyDraft = "";
		showApiKey = false;
	}

	function startCredentialEdit(credentialId: string) {
		const credential = app.credentials.find((item) => item.id === credentialId);
		if (!credential) {
			return;
		}

		credentialsMode = "edit";
		editingCredentialId = credential.id;
		selectedProviderId = credential.provider;
		apiKeyDraft = "";
		showApiKey = false;
	}

	function saveCredentialDraft() {
		if (!selectedProviderId || apiKeyDraft.trim().length === 0) {
			return;
		}

		app.saveCredential(selectedProviderId, apiKeyDraft, editingCredentialId ?? undefined);
		resetCredentialEditor();
	}

	const updateProgress = $derived.by(() => {
		if (!app.totalBytes || app.totalBytes <= 0) {
			return 0;
		}
		return Math.min(100, Math.round((app.downloadedBytes / app.totalBytes) * 100));
	});

	const activeThemeName = $derived.by(
		() => app.availableThemes.find((themeOption) => themeOption.id === app.colorScheme)?.name ?? "Default",
	);

	$effect(() => {
		if (!app.settingsDialogOpen) {
			settingsTab = "appearance";
			resetCredentialEditor();
		}
	});
</script>

<Dialog.Root bind:open={app.settingsDialogOpen}>
	<Dialog.Content class="sm:max-w-2xl">
		<Dialog.Header>
			<Dialog.Title>Settings</Dialog.Title>
			<Dialog.Description>
				Configure appearance, chat defaults, updates, and support tools.
			</Dialog.Description>
		</Dialog.Header>

		<Tabs bind:value={settingsTab} class="mt-1">
			<TabsList class="grid w-full grid-cols-4">
				<TabsTrigger value="appearance">Appearance</TabsTrigger>
				<TabsTrigger value="chat">Chat</TabsTrigger>
				<TabsTrigger value="update">Update</TabsTrigger>
				<TabsTrigger value="credentials">Credentials</TabsTrigger>
			</TabsList>

			<div class="mt-3 min-h-[28rem]">
				<TabsContent value="appearance" class="mt-0 h-full">
						<Card class="gap-4 py-4">
							<CardHeader class="gap-1 border-b pb-4">
								<CardTitle class="text-sm">Appearance</CardTitle>
								<CardDescription>Mode and color theme preferences.</CardDescription>
							</CardHeader>
							<CardContent>
								<ItemGroup class="rounded-md border border-border">
									<Item size="sm">
										<ItemContent>
											<ItemTitle>Mode</ItemTitle>
											<ItemDescription>
												Resolved mode: {app.resolvedTheme}
											</ItemDescription>
										</ItemContent>
										<ItemActions class="ml-auto justify-end">
											<ToggleGroup
												type="single"
												value={app.theme}
												onValueChange={(value) => {
													if (value === "light" || value === "dark" || value === "system") {
														app.setTheme(value);
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
											<ItemDescription>Current palette: {activeThemeName}</ItemDescription>
										</ItemContent>
										<ItemActions class="ml-auto w-56 justify-end">
											<Label for="settings-theme" class="sr-only">Theme</Label>
											<NativeSelect
												id="settings-theme"
												value={app.colorScheme}
												onchange={(event) => {
													app.setColorScheme(
														(event.currentTarget as HTMLSelectElement).value as ThemeColorScheme,
													);
												}}
												class="w-full"
											>
												{#each app.availableThemes as themeOption (themeOption.mode + themeOption.id)}
													<option value={themeOption.id}>{themeOption.name}</option>
												{/each}
											</NativeSelect>
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
								<CardDescription>Conversation defaults for new prompts.</CardDescription>
							</CardHeader>
							<CardContent>
								<ItemGroup class="rounded-md border border-border">
									<Item size="sm">
										<ItemContent>
											<ItemTitle>Default model</ItemTitle>
											<ItemDescription>Set a preferred model or keep auto-select.</ItemDescription>
										</ItemContent>
										<ItemActions class="ml-auto w-56 justify-end">
											<Label for="settings-default-model" class="sr-only">Default model</Label>
											<NativeSelect
												id="settings-default-model"
												value={app.defaultModel || "__auto__"}
												onchange={(event) => {
													const next = (event.currentTarget as HTMLSelectElement).value;
													app.setDefaultModel(next === "__auto__" ? "" : next);
												}}
												class="w-full"
											>
												<option value="__auto__">Auto-select</option>
												{#each app.models as model (model.id)}
													<option value={model.id}>{model.name}</option>
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
												checked={app.chatWidthMode === "full"}
												onCheckedChange={(checked) => {
													app.setChatWidthMode(checked === true ? "full" : "constrained");
												}}
											/>
										</ItemActions>
									</Item>
								</ItemGroup>
							</CardContent>
						</Card>
				</TabsContent>

				<TabsContent value="update" class="mt-0 h-full">
						<Card class="gap-4 py-4">
							<CardHeader class="gap-1 border-b pb-4">
								<CardTitle class="text-sm">Update</CardTitle>
								<CardDescription>Check, download, and install app updates.</CardDescription>
								<CardAction>
									<Button
										variant="ghost"
										size="xs"
										onclick={() => {
											void app.checkForUpdate();
										}}
										disabled={app.updateStatus === "checking" || app.updateStatus === "downloading" || app.updateStatus === "installing"}
									>
										<RefreshCwIcon class={`size-3.5 ${app.updateStatus === "checking" ? "animate-spin" : ""}`} />
										Check
									</Button>
								</CardAction>
							</CardHeader>
							<CardContent class="space-y-3">
								{#if app.updateStatus === "ready" && !app.isUpdateIgnored}
									<div class="rounded-md border border-border bg-background p-3">
										<p class="text-sm text-muted-foreground">
											Version {app.availableVersion} is ready to install.
										</p>
										<div class="mt-3 flex items-center gap-2">
											<Button variant="default" size="xs" onclick={() => void app.installAndRelaunch()}>
												Restart to update
											</Button>
											<Button variant="outline" size="xs" onclick={app.ignoreVersion}>Ignore</Button>
										</div>
									</div>
								{:else if app.updateStatus === "ready" && app.isUpdateIgnored}
									<div class="rounded-md border border-border bg-background p-3 text-sm text-muted-foreground">
										Version {app.availableVersion} available (ignored).
									</div>
								{:else if app.updateStatus === "checking"}
									<div class="rounded-md border border-border bg-background p-3 text-sm text-muted-foreground">
										Checking for updates...
									</div>
								{:else if app.updateStatus === "downloading"}
									<div class="space-y-2 rounded-md border border-border bg-background p-3">
										<div class="flex items-center justify-between text-xs text-muted-foreground">
											<span>Downloading update...</span>
											<span>
												{#if app.totalBytes !== null}
													{formatBytes(app.downloadedBytes)} / {formatBytes(app.totalBytes)}
												{:else}
													{formatBytes(app.downloadedBytes)}
												{/if}
											</span>
										</div>
										<Progress value={updateProgress} />
									</div>
								{:else if app.updateStatus === "installing"}
									<div class="rounded-md border border-border bg-background p-3 text-sm text-muted-foreground">
										Installing update...
									</div>
								{:else if app.updateStatus === "error"}
									<div class="rounded-md border border-destructive/30 bg-destructive/10 p-3 text-sm text-destructive">
										Update failed: {app.updateError}
									</div>
								{:else}
									<div class="rounded-md border border-border bg-background p-3 text-sm text-muted-foreground">
										You're on the latest version.
									</div>
								{/if}
							</CardContent>
						</Card>
				</TabsContent>

				<TabsContent value="credentials" class="mt-0 h-full">
						<Card class="gap-4 py-4">
							<CardHeader class="gap-1 border-b pb-4">
								<CardTitle class="text-sm">API Credentials</CardTitle>
								<CardDescription>Manage provider credentials for local testing.</CardDescription>
								<CardAction>
									{#if credentialsMode === "list"}
										<Button variant="outline" size="xs" onclick={startCredentialCreate}>
											<PlusIcon class="size-3" />
											Add credential
										</Button>
									{/if}
								</CardAction>
							</CardHeader>
							<CardContent>
								{#if credentialsMode === "list"}
									<ItemGroup class="rounded-md border border-border">
										{#if app.credentials.length === 0}
											<div class="flex min-h-32 items-center justify-center px-3 text-sm text-muted-foreground">
												No credentials configured.
											</div>
										{:else}
											{#each app.credentials as credential, index (credential.id)}
												<Item size="sm">
													<ItemContent>
														<ItemTitle>{providerName(credential.provider)}</ItemTitle>
														<ItemDescription>
															{credential.authType.replace("_", " ")}
															{#if credential.updatedAt}
																 · updated {new Date(credential.updatedAt).toLocaleString()}
															{/if}
														</ItemDescription>
													</ItemContent>
													<ItemActions>
														<Button
															variant="ghost"
															size="icon-xs"
															onclick={() => startCredentialEdit(credential.id)}
															title="Edit credential"
														>
															<PencilIcon class="size-3" />
														</Button>
														<Button
															variant="ghost"
															size="icon-xs"
															onclick={() => app.removeCredential(credential.id)}
															title="Delete credential"
														>
															<Trash2Icon class="size-3 text-destructive" />
														</Button>
													</ItemActions>
												</Item>
												{#if index < app.credentials.length - 1}
													<ItemSeparator />
												{/if}
											{/each}
										{/if}
									</ItemGroup>
								{:else}
									<div class="space-y-3">
										<ItemGroup class="rounded-md border border-border">
											<Item size="sm">
												<ItemContent>
													<ItemTitle>Provider</ItemTitle>
												</ItemContent>
												<ItemActions class="ml-auto w-56 justify-end">
													<Label for="credential-provider" class="sr-only">Provider</Label>
													<NativeSelect
														id="credential-provider"
														value={selectedProviderId}
														onchange={(event) => {
															selectedProviderId = (event.currentTarget as HTMLSelectElement).value;
														}}
														class="w-full"
													>
														{#each app.authProviders as provider (provider.id)}
															<option value={provider.id}>{provider.name}</option>
														{/each}
													</NativeSelect>
												</ItemActions>
											</Item>
											<ItemSeparator />
											<Item size="sm">
												<ItemContent>
													<ItemTitle>API key</ItemTitle>
													<ItemDescription>
														Paste a new key to {credentialsMode === "create" ? "save" : "update"}.
													</ItemDescription>
												</ItemContent>
												<ItemActions class="ml-auto w-80 justify-end">
													<div class="flex w-full items-center gap-2">
														<Input
															id="credential-api-key"
															type={showApiKey ? "text" : "password"}
															value={apiKeyDraft}
															oninput={(event) => {
																apiKeyDraft = (event.currentTarget as HTMLInputElement).value;
															}}
															placeholder="sk-..."
															class="font-mono"
														/>
														<Button
															variant="ghost"
															size="icon-xs"
															onclick={() => {
																showApiKey = !showApiKey;
															}}
															title={showApiKey ? "Hide API key" : "Show API key"}
														>
															{#if showApiKey}
																<EyeOffIcon class="size-3.5" />
															{:else}
																<EyeIcon class="size-3.5" />
															{/if}
														</Button>
													</div>
												</ItemActions>
											</Item>
										</ItemGroup>

										<div class="flex justify-end gap-2">
											<Button variant="ghost" size="sm" onclick={resetCredentialEditor}>Cancel</Button>
											<Button
												variant="default"
												size="sm"
												onclick={saveCredentialDraft}
												disabled={apiKeyDraft.trim().length === 0 || !selectedProviderId}
											>
												{credentialsMode === "create" ? "Save credential" : "Update credential"}
											</Button>
										</div>
									</div>
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
					onclick={app.openSupportInfoDialog}
					title="Support information"
					aria-label="Support information"
				>
					<InfoIcon class="size-4" />
				</Button>
				<Button variant="default" size="sm" onclick={app.closeSettingsDialog}>Done</Button>
			</div>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>

<SupportInfoDialog />
