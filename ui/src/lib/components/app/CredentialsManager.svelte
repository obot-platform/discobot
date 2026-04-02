<script lang="ts">
	import CircleHelpIcon from "@lucide/svelte/icons/circle-help";
	import CopyIcon from "@lucide/svelte/icons/copy";
	import ExternalLinkIcon from "@lucide/svelte/icons/external-link";
	import Loader2Icon from "@lucide/svelte/icons/loader-2";
	import LogInIcon from "@lucide/svelte/icons/log-in";
	import PencilIcon from "@lucide/svelte/icons/pencil";
	import PlusIcon from "@lucide/svelte/icons/plus";
	import Trash2Icon from "@lucide/svelte/icons/trash-2";
	import XIcon from "@lucide/svelte/icons/x";
	import type { CredentialAuthType, CredentialEnvVar } from "$lib/api-types";
	import { Button } from "$lib/components/ui/button";
	import { Input } from "$lib/components/ui/input";
	import {
		Item,
		ItemActions,
		ItemContent,
		ItemDescription,
		ItemGroup,
		ItemTitle,
	} from "$lib/components/ui/item";
	import { Label } from "$lib/components/ui/label";
	import { NativeSelect } from "$lib/components/ui/native-select";
	import * as Tooltip from "$lib/components/ui/tooltip";
	import { useAppContext } from "$lib/context/app-context.svelte";
	import { openUrl, writeClipboardText } from "$lib/tauri";

	type EditorMode = "list" | "create" | "edit";
	type EnvVarRow = {
		id: string;
		key: string;
		value: string;
		hasStoredValue: boolean;
		replaceValue: boolean;
	};

	const CUSTOM_PROVIDER = "__custom__";
	const app = useAppContext();
	const credentialsApi = app.credentials;

	let loading = $state(false);
	let errorMessage = $state<string | null>(null);
	let mode = $state<EditorMode>("list");
	let selectedProvider = $state("");
	let selectedAuthType = $state<CredentialAuthType>("api_key");
	let editingCredentialId = $state<string | null>(null);
	let nameDraft = $state("");
	let descriptionDraft = $state("");
	let showNameDraft = $state(false);
	let showDescriptionDraft = $state(false);
	let apiKeyDraft = $state("");
	let oauthDeviceIdDraft = $state("");
	let oauthUserCodeDraft = $state("");
	let oauthVerificationUrl = $state("");
	let oauthAuthUrl = $state("");
	let oauthInputDraft = $state("");
	let oauthPollIntervalSeconds = $state(5);
	let oauthPollDomainDraft = $state("");
	let oauthVerifierDraft = $state("");
	let startingOAuth = $state(false);
	let pollingOAuth = $state(false);
	let copiedOAuthCode = $state(false);
	let inactiveDraft = $state(false);
	let replaceSecretDraft = $state(false);
	let agentVisibleDraft = $state(false);
	let envVarRows = $state<EnvVarRow[]>([]);
	let submitting = $state(false);
	let deletingId = $state<string | null>(null);
	let oauthPollTimer: ReturnType<typeof setTimeout> | null = null;
	let oauthCopiedTimer: ReturnType<typeof setTimeout> | null = null;

	const providerOptions = $derived.by(() => {
		const options = credentialsApi.credentialTypes.map((type) => ({
			value: type.id,
			provider: type.provider,
			backendProvider: type.backendProvider,
			authType: type.authType,
			label:
				type.id === "openai:oauth"
					? "OpenAI Codex (OAuth)"
					: type.authType === "oauth"
						? `${type.name} (OAuth)`
						: `${type.name} (${type.authType === "id" ? "ID" : "API key"})`,
			description:
				type.authType === "oauth"
					? (type.oauth?.description ?? type.description ?? type.provider)
					: (type.description ?? type.provider),
			agentVisible: false,
		}));
		return [
			...options,
			{
				value: CUSTOM_PROVIDER,
				provider: CUSTOM_PROVIDER,
				backendProvider: CUSTOM_PROVIDER,
				authType: "api_key" as CredentialAuthType,
				label: "Custom env vars",
				description: "Create a reusable bundle of environment variables.",
				agentVisible: false,
			},
		];
	});
	const editingExistingSecret = $derived(
		mode === "edit" &&
			selectedProvider !== CUSTOM_PROVIDER &&
			selectedAuthType !== "oauth",
	);
	const hasSelectedProvider = $derived(selectedProvider !== "");
	const selectedCredentialType = $derived.by(() => {
		if (!hasSelectedProvider || selectedProvider === CUSTOM_PROVIDER) {
			return null;
		}
		return (
			credentialsApi.credentialTypes.find(
				(type) =>
					type.authType === selectedAuthType &&
					(type.backendProvider === selectedProvider ||
						type.provider === selectedProvider),
			) ?? null
		);
	});
	const selectedProviderValue = $derived.by(() => {
		if (!hasSelectedProvider) {
			return "";
		}
		if (selectedProvider === CUSTOM_PROVIDER) {
			return CUSTOM_PROVIDER;
		}
		return (
			selectedCredentialType?.id ?? `${selectedProvider}:${selectedAuthType}`
		);
	});
	const selectedEnvVarName = $derived.by(
		() => selectedCredentialType?.env?.[0] ?? "",
	);
	const selectedOAuthConfig = $derived.by(() =>
		selectedAuthType === "oauth"
			? (selectedCredentialType?.oauth ?? null)
			: null,
	);
	const selectedOAuthKind = $derived(selectedOAuthConfig?.kind ?? null);

	function makeEnvVarRow(
		key = "",
		value = "",
		hasStoredValue = false,
		replaceValue = true,
	): EnvVarRow {
		return {
			id: `env-var-${Date.now()}-${Math.floor(Math.random() * 10_000)}`,
			key,
			value,
			hasStoredValue,
			replaceValue,
		};
	}

	function resetEditor() {
		if (oauthPollTimer) {
			clearTimeout(oauthPollTimer);
			oauthPollTimer = null;
		}
		if (oauthCopiedTimer) {
			clearTimeout(oauthCopiedTimer);
			oauthCopiedTimer = null;
		}
		mode = "list";
		selectedProvider = "";
		selectedAuthType = "api_key";
		editingCredentialId = null;
		nameDraft = "";
		descriptionDraft = "";
		showNameDraft = false;
		showDescriptionDraft = false;
		apiKeyDraft = "";
		oauthDeviceIdDraft = "";
		oauthUserCodeDraft = "";
		oauthVerificationUrl = "";
		oauthAuthUrl = "";
		oauthInputDraft = "";
		oauthPollIntervalSeconds = 5;
		oauthPollDomainDraft = "";
		oauthVerifierDraft = "";
		startingOAuth = false;
		pollingOAuth = false;
		copiedOAuthCode = false;
		inactiveDraft = false;
		replaceSecretDraft = false;
		agentVisibleDraft = false;
		envVarRows = [makeEnvVarRow()];
		submitting = false;
	}

	function parseOAuthCode(value: string): string {
		const trimmed = value.trim();
		if (!trimmed) {
			return "";
		}
		if (!trimmed.includes("code=")) {
			return trimmed;
		}
		try {
			const url = new URL(trimmed);
			return url.searchParams.get("code")?.trim() ?? trimmed;
		} catch {
			return trimmed;
		}
	}

	function credentialDisplayName(credential: ConfiguredCredential) {
		const name = credential.name.trim();
		if (name.length > 0) {
			return name;
		}
		if (credential.provider.startsWith("custom:")) {
			return credential.envKeys?.join(", ") || "Custom env vars";
		}
		const matchedType = credentialsApi.credentialTypes.find(
			(type) =>
				type.backendProvider === credential.provider &&
				type.authType === credential.authType,
		);
		if (matchedType) {
			return matchedType.name;
		}
		return credential.provider;
	}

	function typeLabel(
		credentialId: string,
		provider: string,
		authType: CredentialAuthType,
	) {
		const matchedType = credentialsApi.credentialTypes.find(
			(type) => type.backendProvider === provider && type.authType === authType,
		);
		if (matchedType) {
			return matchedType.name;
		}
		if (provider.startsWith("custom:")) {
			return "Custom env vars";
		}
		return credentialId;
	}

	function envKeySummary(envKeys?: string[]) {
		if (!envKeys || envKeys.length === 0) {
			return "No environment variables";
		}
		return envKeys.slice(0, 3).join(" · ");
	}

	function credentialSummary(credential: ConfiguredCredential) {
		if (credential.authType === "oauth") {
			return "OAuth";
		}
		return envKeySummary(credential.envKeys);
	}

	async function load() {
		loading = true;
		errorMessage = null;
		try {
			await credentialsApi.refresh();
		} catch (error) {
			errorMessage =
				error instanceof Error ? error.message : "Failed to load credentials";
		} finally {
			loading = false;
		}
	}

	type ConfiguredCredential = (typeof credentialsApi.list)[number];

	function startCreate() {
		resetEditor();
		mode = "create";
		selectedProvider = "";
		selectedAuthType = "api_key";
		replaceSecretDraft = false;
	}

	function startEdit(credential: ConfiguredCredential) {
		resetEditor();
		mode = "edit";
		editingCredentialId = credential.id;
		selectedProvider = credential.provider.startsWith("custom:")
			? CUSTOM_PROVIDER
			: credential.provider;
		selectedAuthType = credential.authType;
		nameDraft = credential.name;
		descriptionDraft = credential.description ?? "";
		showNameDraft = credential.name.trim().length > 0;
		showDescriptionDraft = (credential.description ?? "").trim().length > 0;
		agentVisibleDraft = credential.agentVisible;
		inactiveDraft = credential.inactive;
		replaceSecretDraft = false;
		if (selectedProvider === CUSTOM_PROVIDER) {
			envVarRows = credential.envKeys?.map((envKey) =>
				makeEnvVarRow(envKey, "", true, false),
			) ?? [makeEnvVarRow()];
		}
	}

	async function startOAuthAuthorization() {
		if (!selectedOAuthConfig) {
			return;
		}
		startingOAuth = true;
		errorMessage = null;
		try {
			switch (selectedOAuthConfig.provider) {
				case "codex": {
					const response = await credentialsApi.codexDeviceCode();
					oauthDeviceIdDraft = response.deviceAuthId;
					oauthUserCodeDraft = response.userCode;
					oauthVerificationUrl = response.verificationUri;
					oauthPollIntervalSeconds = response.interval;
					break;
				}
				case "github-git": {
					const response = await credentialsApi.githubDeviceCode();
					oauthDeviceIdDraft = response.deviceCode;
					oauthUserCodeDraft = response.userCode;
					oauthVerificationUrl = response.verificationUri;
					oauthPollIntervalSeconds = response.interval;
					oauthPollDomainDraft = response.domain;
					break;
				}
				case "anthropic": {
					const response = await credentialsApi.anthropicAuthorize();
					oauthAuthUrl = response.url;
					oauthVerifierDraft = response.verifier;
					break;
				}
				default:
					throw new Error(
						`OAuth is not available for ${selectedCredentialType?.name ?? "this credential type"} yet.`,
					);
			}
		} catch (error) {
			errorMessage =
				error instanceof Error ? error.message : "Failed to start OAuth";
		} finally {
			startingOAuth = false;
		}
	}

	async function copyOAuthCode() {
		if (!oauthUserCodeDraft) {
			return;
		}
		await writeClipboardText(oauthUserCodeDraft);
		copiedOAuthCode = true;
		if (oauthCopiedTimer) {
			clearTimeout(oauthCopiedTimer);
		}
		oauthCopiedTimer = setTimeout(() => {
			copiedOAuthCode = false;
			oauthCopiedTimer = null;
		}, 2000);
	}

	async function startOAuthPolling() {
		if (!oauthDeviceIdDraft || !oauthUserCodeDraft || !selectedOAuthConfig) {
			return;
		}
		if (oauthPollTimer) {
			clearTimeout(oauthPollTimer);
			oauthPollTimer = null;
		}
		pollingOAuth = true;
		errorMessage = null;

		const poll = async () => {
			try {
				switch (selectedOAuthConfig.provider) {
					case "codex": {
						const response = await credentialsApi.codexPoll({
							deviceAuthId: oauthDeviceIdDraft,
							userCode: oauthUserCodeDraft,
						});
						if (response.status === "success") {
							resetEditor();
							await load();
							return;
						}
						if (response.status === "pending") {
							oauthPollTimer = setTimeout(
								() => void poll(),
								oauthPollIntervalSeconds * 1000,
							);
							return;
						}
						throw new Error(response.error || "Authorization failed");
					}
					case "github-git": {
						const response = await credentialsApi.githubPoll({
							deviceCode: oauthDeviceIdDraft,
							domain: oauthPollDomainDraft,
						});
						if (response.status === "success") {
							resetEditor();
							await load();
							return;
						}
						if (response.status === "pending") {
							oauthPollTimer = setTimeout(
								() => void poll(),
								oauthPollIntervalSeconds * 1000,
							);
							return;
						}
						throw new Error(response.error || "Authorization failed");
					}
					default:
						throw new Error(
							`OAuth is not available for ${selectedCredentialType?.name ?? "this credential type"} yet.`,
						);
				}
			} catch (error) {
				pollingOAuth = false;
				errorMessage =
					error instanceof Error ? error.message : "Authorization failed";
			}
		};

		await poll();
	}

	async function submitOAuthAuthorizationCode() {
		if (!selectedOAuthConfig) {
			return;
		}
		pollingOAuth = true;
		errorMessage = null;
		try {
			switch (selectedOAuthConfig.provider) {
				case "anthropic": {
					const trimmedInput = oauthInputDraft.trim();
					if (!trimmedInput) {
						throw new Error("Enter the authorization code or token.");
					}
					const isDirectToken =
						selectedOAuthConfig.allowsDirectToken === true &&
						trimmedInput.startsWith("sk-ant-oat0");
					if (!isDirectToken && !oauthVerifierDraft.trim()) {
						throw new Error("Start the OAuth flow before connecting.");
					}
					const response = await credentialsApi.anthropicExchange({
						code: isDirectToken ? trimmedInput : parseOAuthCode(trimmedInput),
						verifier: isDirectToken ? "" : oauthVerifierDraft.trim(),
					});
					if (!response.success) {
						throw new Error(response.error || "Authorization failed");
					}
					resetEditor();
					await load();
					return;
				}
				default:
					throw new Error(
						`OAuth is not available for ${selectedCredentialType?.name ?? "this credential type"} yet.`,
					);
			}
		} catch (error) {
			errorMessage =
				error instanceof Error ? error.message : "Authorization failed";
		} finally {
			pollingOAuth = false;
		}
	}

	function updateEnvVarRow(
		rowId: string,
		patch: Partial<Omit<EnvVarRow, "id">>,
	) {
		envVarRows = envVarRows.map((row) =>
			row.id === rowId ? { ...row, ...patch } : row,
		);
	}

	function addEnvVarRow() {
		envVarRows = [...envVarRows, makeEnvVarRow()];
	}

	function showEnvVarValueInput(rowId: string) {
		updateEnvVarRow(rowId, { replaceValue: true, value: "" });
	}

	function hideEnvVarValueInput(rowId: string) {
		updateEnvVarRow(rowId, { replaceValue: false, value: "" });
	}

	function removeEnvVarRow(rowId: string) {
		const nextRows = envVarRows.filter((row) => row.id !== rowId);
		envVarRows = nextRows.length > 0 ? nextRows : [makeEnvVarRow()];
	}

	function envVarsFromRows(): CredentialEnvVar[] {
		return envVarRows
			.map((row) => ({ key: row.key.trim(), value: row.value }))
			.filter((row) => row.key.length > 0);
	}

	async function save() {
		submitting = true;
		errorMessage = null;
		try {
			if (!hasSelectedProvider) {
				throw new Error("Select a credential type before saving.");
			}
			if (selectedProvider === CUSTOM_PROVIDER) {
				await credentialsApi.create({
					credentialId: editingCredentialId ?? undefined,
					name: nameDraft.trim() || "",
					description: descriptionDraft.trim() || undefined,
					authType: "api_key",
					envVars: envVarsFromRows(),
					agentVisible: agentVisibleDraft,
					inactive: inactiveDraft,
				});
			} else if (selectedAuthType === "oauth") {
				if (!editingCredentialId) {
					throw new Error("Use the OAuth flow to connect this credential.");
				}
				await credentialsApi.create({
					provider: selectedProvider,
					credentialId: editingCredentialId,
					name: nameDraft.trim() || "",
					description: descriptionDraft.trim() || undefined,
					authType: selectedAuthType,
					agentVisible: agentVisibleDraft,
					inactive: inactiveDraft,
				});
			} else {
				const trimmedKey = apiKeyDraft.trim();
				if (!replaceSecretDraft && editingCredentialId) {
					await credentialsApi.create({
						provider: selectedProvider,
						credentialId: editingCredentialId,
						name: nameDraft.trim() || "",
						description: descriptionDraft.trim() || undefined,
						authType: selectedAuthType,
						agentVisible: agentVisibleDraft,
						inactive: inactiveDraft,
					});
				} else {
					if (!trimmedKey) {
						throw new Error(
							editingCredentialId
								? "Enter a new value or keep the existing one."
								: "Enter a value before saving.",
						);
					}
					await credentialsApi.create({
						provider: selectedProvider,
						credentialId: editingCredentialId ?? undefined,
						name: nameDraft.trim() || "",
						description: descriptionDraft.trim() || undefined,
						authType: selectedAuthType,
						apiKey: trimmedKey,
						agentVisible: agentVisibleDraft,
						inactive: inactiveDraft,
					});
				}
			}
			resetEditor();
			await load();
		} catch (error) {
			errorMessage =
				error instanceof Error ? error.message : "Failed to save credential";
		} finally {
			submitting = false;
		}
	}

	async function removeCredential(id: string) {
		deletingId = id;
		try {
			await credentialsApi.remove(id);
		} finally {
			deletingId = null;
		}
	}

	$effect(() => {
		void load();
	});

	$effect(() => {
		return () => {
			if (oauthPollTimer) {
				clearTimeout(oauthPollTimer);
			}
			if (oauthCopiedTimer) {
				clearTimeout(oauthCopiedTimer);
			}
		};
	});
</script>

<div class="flex h-full min-h-0 flex-col gap-4">
	<Tooltip.Provider>
		{#if loading}
			<div class="text-sm text-muted-foreground">Loading credentials…</div>
		{:else if mode === "list"}
			<div class="flex items-center justify-between gap-2">
				<div class="text-sm text-muted-foreground">
					Manage built-in credentials and custom environment variable bundles.
				</div>
				<Button variant="outline" size="sm" onclick={startCreate}>
					<PlusIcon class="size-4" />
					New credential
				</Button>
			</div>

			{#if errorMessage}
				<div class="text-sm text-destructive">{errorMessage}</div>
			{/if}

			<div class="min-h-0 flex-1 overflow-auto">
				<ItemGroup>
					{#if credentialsApi.list.length === 0}
						<div
							class="rounded-md border border-dashed border-border p-6 text-sm text-muted-foreground"
						>
							No credentials configured.
						</div>
					{:else}
						{#each credentialsApi.list as credential (credential.id)}
							<Item>
								<ItemContent>
									<ItemTitle>{credentialDisplayName(credential)}</ItemTitle>
									<ItemDescription>
										{typeLabel(
											credential.id,
											credential.provider,
											credential.authType,
										)} ·
										{credential.inactive
											? "inactive"
											: credential.agentVisible
												? "visible to agent"
												: "internal only"} ·
										{credentialSummary(credential)}
									</ItemDescription>
								</ItemContent>
								<ItemActions>
									<Button
										variant="ghost"
										size="icon-sm"
										onclick={() => {
											void startEdit(credential);
										}}
									>
										<PencilIcon class="size-4" />
									</Button>
									<Button
										variant="ghost"
										size="icon-sm"
										disabled={deletingId === credential.id}
										onclick={() => {
											void removeCredential(credential.id);
										}}
									>
										<Trash2Icon class="size-4 text-destructive" />
									</Button>
								</ItemActions>
							</Item>
						{/each}
					{/if}
				</ItemGroup>
			</div>
		{:else}
			<div class="space-y-4">
				{#if errorMessage}
					<div class="text-sm text-destructive">{errorMessage}</div>
				{/if}

				<div class="space-y-1.5">
					<Label for="credential-provider">Credential type</Label>
					<NativeSelect
						id="credential-provider"
						value={selectedProviderValue}
						onchange={(event) => {
							const value = (event.currentTarget as HTMLSelectElement).value;
							if (value === "") {
								selectedProvider = "";
								selectedAuthType = "api_key";
								replaceSecretDraft = false;
								agentVisibleDraft = false;
								return;
							}
							const option = providerOptions.find(
								(candidate) => candidate.value === value,
							);
							if (!option) {
								return;
							}
							selectedProvider =
								option.provider === CUSTOM_PROVIDER
									? CUSTOM_PROVIDER
									: option.backendProvider;
							selectedAuthType = option.authType;
							replaceSecretDraft =
								option.authType === "oauth" ? false : mode === "create";
							agentVisibleDraft = false;
							apiKeyDraft = "";
							oauthDeviceIdDraft = "";
							oauthUserCodeDraft = "";
							oauthVerificationUrl = "";
							oauthAuthUrl = "";
							oauthInputDraft = "";
							oauthPollIntervalSeconds = 5;
							oauthPollDomainDraft = "";
							oauthVerifierDraft = "";
							pollingOAuth = false;
						}}
					>
						<option value="">Select a credential type</option>
						{#each providerOptions as option}
							<option value={option.value}>{option.label}</option>
						{/each}
					</NativeSelect>
				</div>

				{#if hasSelectedProvider}
					{#if selectedProvider === CUSTOM_PROVIDER}
						<div class="space-y-2">
							<div class="flex items-center justify-between">
								<Label>Environment variables</Label>
								<Button variant="outline" size="xs" onclick={addEnvVarRow}>
									<PlusIcon class="size-3" />
									Add row
								</Button>
							</div>
							{#each envVarRows as row (row.id)}
								<div
									class="grid gap-2 md:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] md:items-start"
								>
									<Input
										value={row.key}
										placeholder="KEY"
										class="min-w-0 font-mono"
										oninput={(event) =>
											updateEnvVarRow(row.id, {
												key: (event.currentTarget as HTMLInputElement).value,
											})}
									/>
									<div class="min-w-0 space-y-1">
										{#if row.hasStoredValue && !row.replaceValue}
											<div class="text-sm text-muted-foreground">
												A value is already stored.
											</div>
											<Button
												variant="ghost"
												size="xs"
												class="h-auto px-0"
												onclick={() => showEnvVarValueInput(row.id)}
											>
												Update value
											</Button>
										{:else}
											<Input
												type="password"
												value={row.value}
												placeholder={row.hasStoredValue
													? "Enter a new value"
													: "value"}
												class="font-mono"
												oninput={(event) =>
													updateEnvVarRow(row.id, {
														value: (event.currentTarget as HTMLInputElement)
															.value,
													})}
											/>
											<p class="text-sm text-muted-foreground">
												{row.hasStoredValue
													? "Saving will replace the stored value."
													: "This value will be stored securely."}
											</p>
											{#if row.hasStoredValue}
												<Button
													variant="ghost"
													size="xs"
													class="h-auto px-0"
													onclick={() => hideEnvVarValueInput(row.id)}
												>
													Keep existing value
												</Button>
											{/if}
										{/if}
									</div>
									{#if envVarRows.length > 1}
										<Button
											variant="ghost"
											size="icon-xs"
											class="md:self-start"
											onclick={() => removeEnvVarRow(row.id)}
										>
											<XIcon class="size-3" />
										</Button>
									{/if}
								</div>
							{/each}
						</div>
					{:else if selectedAuthType === "oauth"}
						<div class="space-y-3">
							<div class="space-y-1">
								<Label>
									{selectedOAuthKind === "device_code"
										? "Device code"
										: (selectedOAuthConfig?.inputLabel ?? "Authorization code")}
								</Label>
								<p class="text-sm text-muted-foreground">
									{selectedOAuthConfig?.description ??
										"Use ChatGPT device auth to connect this credential."}
								</p>
							</div>
							<div class="flex flex-wrap gap-2">
								<Button
									variant="outline"
									size="sm"
									class="gap-2"
									disabled={startingOAuth || pollingOAuth}
									onclick={() => void startOAuthAuthorization()}
								>
									{#if startingOAuth}
										<Loader2Icon class="size-4 animate-spin" />
										Starting…
									{:else}
										<LogInIcon class="size-4" />
										{selectedOAuthKind === "device_code"
											? "Get device code"
											: "Start sign-in"}
									{/if}
								</Button>
								{#if oauthVerificationUrl}
									<Button
										variant="ghost"
										size="sm"
										class="gap-2"
										disabled={pollingOAuth}
										onclick={() => void openUrl(oauthVerificationUrl)}
									>
										<ExternalLinkIcon class="size-4" />
										Open device page
									</Button>
								{/if}
								{#if oauthAuthUrl}
									<Button
										variant="ghost"
										size="sm"
										class="gap-2"
										disabled={pollingOAuth}
										onclick={() => void openUrl(oauthAuthUrl)}
									>
										<ExternalLinkIcon class="size-4" />
										Open auth page
									</Button>
								{/if}
							</div>
							{#if selectedOAuthKind === "device_code" && oauthUserCodeDraft}
								<div
									class="space-y-3 rounded-md border border-border bg-muted/40 p-3"
								>
									<div class="text-sm text-muted-foreground">
										Open
										<code class="mx-1 font-mono">{oauthVerificationUrl}</code>
										and enter this code:
									</div>
									<div class="flex items-center gap-2">
										<div
											class="flex-1 rounded-lg bg-background p-4 text-center"
										>
											<code class="text-xl font-bold tracking-[0.3em]">
												{oauthUserCodeDraft}
											</code>
										</div>
										<Button
											variant="outline"
											size="icon"
											class="h-14 w-14"
											disabled={pollingOAuth}
											onclick={() => void copyOAuthCode()}
										>
											<CopyIcon class="size-5" />
										</Button>
									</div>
									{#if copiedOAuthCode}
										<p class="text-xs text-center text-muted-foreground">
											Copied to clipboard
										</p>
									{/if}
									<div class="flex justify-end">
										<Button
											size="sm"
											disabled={pollingOAuth}
											onclick={() => void startOAuthPolling()}
										>
											{#if pollingOAuth}
												<Loader2Icon class="size-4 animate-spin" />
												Waiting for authorization…
											{:else}
												I've entered the code
											{/if}
										</Button>
									</div>
								</div>
							{:else if selectedOAuthKind === "authorization_code"}
								<div class="space-y-2">
									<Input
										id="credential-secret"
										value={oauthInputDraft}
										placeholder={selectedOAuthConfig?.inputPlaceholder ??
											"Paste authorization code"}
										oninput={(event) =>
											(oauthInputDraft = (
												event.currentTarget as HTMLInputElement
											).value)}
									/>
									{#if selectedOAuthConfig?.allowsDirectToken}
										<p class="text-sm text-muted-foreground">
											You can also paste a direct token starting with
											<code class="mx-1 font-mono">sk-ant-oat0</code>.
										</p>
									{/if}
									<div class="flex justify-end">
										<Button
											size="sm"
											disabled={pollingOAuth}
											onclick={() => void submitOAuthAuthorizationCode()}
										>
											{#if pollingOAuth}
												<Loader2Icon class="size-4 animate-spin" />
												Connecting…
											{:else}
												Connect
											{/if}
										</Button>
									</div>
								</div>
							{/if}
						</div>
					{:else}
						<div class="space-y-2">
							<Label for="credential-secret">Value</Label>
							{#if selectedEnvVarName}
								<p class="text-sm text-muted-foreground">
									Stored as
									<code class="font-mono">{selectedEnvVarName}</code>.
								</p>
							{/if}
							{#if editingExistingSecret && !replaceSecretDraft}
								<div class="text-sm text-muted-foreground">
									A value is already stored.
								</div>
								<div class="flex justify-start">
									<Button
										variant="ghost"
										size="xs"
										class="h-auto px-0"
										onclick={() => {
											replaceSecretDraft = true;
											apiKeyDraft = "";
										}}
									>
										Update value
									</Button>
								</div>
							{:else}
								<Input
									id="credential-secret"
									type="password"
									value={apiKeyDraft}
									placeholder={editingExistingSecret
										? "Enter a new value"
										: "Enter value"}
									oninput={(event) =>
										(apiKeyDraft = (event.currentTarget as HTMLInputElement)
											.value)}
								/>
								<p class="text-sm text-muted-foreground">
									{editingExistingSecret
										? "Saving will replace the currently stored value."
										: "This value will be stored securely."}
								</p>
								{#if editingExistingSecret}
									<div class="flex justify-start">
										<Button
											variant="ghost"
											size="xs"
											onclick={() => {
												replaceSecretDraft = false;
												apiKeyDraft = "";
											}}
										>
											Keep existing value
										</Button>
									</div>
								{/if}
							{/if}
						</div>
					{/if}

					{#if selectedAuthType !== "oauth"}
						<div class="space-y-2">
							<div class="flex items-center gap-2 text-sm">
								<label class="flex items-center gap-2 text-sm">
									<input
										type="checkbox"
										checked={agentVisibleDraft}
										onchange={(event) =>
											(agentVisibleDraft = (
												event.currentTarget as HTMLInputElement
											).checked)}
									/>
									Visible to the agent and tool environment
								</label>
								<Tooltip.Root>
									<Tooltip.Trigger>
										{#snippet child({ props })}
											<button
												type="button"
												class="text-muted-foreground hover:text-foreground inline-flex items-center"
												aria-label="Explain agent visibility"
												{...props}
											>
												<CircleHelpIcon class="size-4" />
											</button>
										{/snippet}
									</Tooltip.Trigger>
									<Tooltip.Content side="top" align="start" class="max-w-72">
										When enabled, this credential can be exposed inside the
										agent runtime and may be read or used by tools and
										model-driven actions. Only enable this when the agent truly
										needs direct access.
									</Tooltip.Content>
								</Tooltip.Root>
							</div>
							{#if agentVisibleDraft}
								<div
									class="rounded-md border border-amber-500/40 bg-amber-500/10 p-3 text-sm text-amber-950 dark:text-amber-100"
								>
									<div class="font-medium">
										Warning: agent-visible credentials increase exposure.
									</div>
									<div class="mt-1 text-current/90">
										The agent and its tools may be able to read or use this
										credential during a conversation.
									</div>
								</div>
							{/if}
						</div>

						<div class="space-y-2">
							<div class="flex flex-wrap gap-2">
								{#if !showNameDraft}
									<Button
										variant="ghost"
										size="xs"
										onclick={() => {
											showNameDraft = true;
										}}
									>
										Add name
									</Button>
								{/if}
								{#if !showDescriptionDraft}
									<Button
										variant="ghost"
										size="xs"
										onclick={() => {
											showDescriptionDraft = true;
										}}
									>
										Add description
									</Button>
								{/if}
							</div>

							{#if showNameDraft}
								<div class="space-y-1.5">
									<Label for="credential-name">Name</Label>
									<Input
										id="credential-name"
										value={nameDraft}
										placeholder="Optional"
										oninput={(event) =>
											(nameDraft = (event.currentTarget as HTMLInputElement)
												.value)}
									/>
								</div>
							{/if}

							{#if showDescriptionDraft}
								<div class="space-y-1.5">
									<Label for="credential-description">Description</Label>
									<Input
										id="credential-description"
										value={descriptionDraft}
										placeholder="Optional"
										oninput={(event) =>
											(descriptionDraft = (
												event.currentTarget as HTMLInputElement
											).value)}
									/>
								</div>
							{/if}
						</div>
					{/if}

					<div class="space-y-2">
						<div class="flex items-center gap-2 text-sm">
							<label class="flex items-center gap-2 text-sm">
								<input
									type="checkbox"
									checked={inactiveDraft}
									onchange={(event) =>
										(inactiveDraft = (event.currentTarget as HTMLInputElement)
											.checked)}
								/>
								Inactive
							</label>
						</div>
						{#if inactiveDraft}
							<div
								class="rounded-md border border-border bg-muted/40 p-3 text-sm text-muted-foreground"
							>
								Inactive credentials stay saved in the project but are skipped
								when credentials are prepared for the agent runtime.
							</div>
						{/if}
					</div>

					<div class="flex items-center justify-end gap-2">
						<Button variant="ghost" size="sm" onclick={resetEditor}
							>Cancel</Button
						>
						<Button
							variant="default"
							size="sm"
							disabled={submitting ||
								(selectedAuthType === "oauth" && !editingCredentialId)}
							onclick={() => void save()}
						>
							{submitting ? "Saving…" : "Save credential"}
						</Button>
					</div>
				{:else}
					<div
						class="rounded-md border border-dashed border-border p-4 text-sm text-muted-foreground"
					>
						Select a credential type to start configuring this credential.
					</div>
				{/if}
			</div>
		{/if}
	</Tooltip.Provider>
</div>
