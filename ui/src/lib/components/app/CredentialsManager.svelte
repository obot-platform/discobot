<script lang="ts">
	import PencilIcon from "@lucide/svelte/icons/pencil";
	import PlusIcon from "@lucide/svelte/icons/plus";
	import CircleHelpIcon from "@lucide/svelte/icons/circle-help";
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
	let replaceSecretDraft = $state(false);
	let agentVisibleDraft = $state(false);
	let envVarRows = $state<EnvVarRow[]>([]);
	let submitting = $state(false);
	let deletingId = $state<string | null>(null);

	const providerOptions = $derived.by(() => {
		const options = credentialsApi.credentialTypes
			.filter((type) => type.authType !== "oauth")
			.map((type) => ({
				value: `${type.provider}:${type.authType}`,
				provider: type.provider,
				authType: type.authType,
				label: `${type.name} (${type.authType === "id" ? "ID" : "API key"})`,
				description: type.description ?? type.provider,
				agentVisible: false,
			}));
		return [
			...options,
			{
				value: CUSTOM_PROVIDER,
				provider: CUSTOM_PROVIDER,
				authType: "api_key" as CredentialAuthType,
				label: "Custom env vars",
				description: "Create a reusable bundle of environment variables.",
				agentVisible: false,
			},
		];
	});
	const editingExistingSecret = $derived(
		mode === "edit" && selectedProvider !== CUSTOM_PROVIDER,
	);
	const hasSelectedProvider = $derived(selectedProvider !== "");
	const selectedCredentialType = $derived.by(() => {
		if (!hasSelectedProvider || selectedProvider === CUSTOM_PROVIDER) {
			return null;
		}
		return (
			credentialsApi.credentialTypes.find(
				(type) =>
					type.provider === selectedProvider &&
					type.authType === selectedAuthType,
			) ?? null
		);
	});
	const selectedEnvVarName = $derived.by(
		() => selectedCredentialType?.env?.[0] ?? "",
	);

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
		mode = "list";
		selectedProvider = "";
		selectedAuthType = "api_key";
		editingCredentialId = null;
		nameDraft = "";
		descriptionDraft = "";
		showNameDraft = false;
		showDescriptionDraft = false;
		apiKeyDraft = "";
		replaceSecretDraft = false;
		agentVisibleDraft = false;
		envVarRows = [makeEnvVarRow()];
		submitting = false;
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
		replaceSecretDraft = false;
		if (selectedProvider === CUSTOM_PROVIDER) {
			envVarRows = credential.envKeys?.map((envKey) =>
				makeEnvVarRow(envKey, "", true, false),
			) ?? [makeEnvVarRow()];
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
										{credential.agentVisible
											? "visible to agent"
											: "internal only"} ·
										{envKeySummary(credential.envKeys)}
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
						value={selectedProvider === ""
							? ""
							: selectedProvider === CUSTOM_PROVIDER
								? CUSTOM_PROVIDER
								: `${selectedProvider}:${selectedAuthType}`}
						onchange={(event) => {
							const value = (event.currentTarget as HTMLSelectElement).value;
							if (value === "") {
								selectedProvider = "";
								selectedAuthType = "api_key";
								replaceSecretDraft = false;
								agentVisibleDraft = false;
								return;
							}
							if (value === CUSTOM_PROVIDER) {
								selectedProvider = CUSTOM_PROVIDER;
								selectedAuthType = "api_key";
								replaceSecretDraft = true;
								agentVisibleDraft = false;
								return;
							}
							const [provider, authType] = value.split(":") as [
								string,
								CredentialAuthType,
							];
							selectedProvider = provider;
							selectedAuthType = authType;
							replaceSecretDraft = mode === "create";
							agentVisibleDraft = false;
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
									When enabled, this credential can be exposed inside the agent
									runtime and may be read or used by tools and model-driven
									actions. Only enable this when the agent truly needs direct
									access.
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

					<div class="flex items-center justify-end gap-2">
						<Button variant="ghost" size="sm" onclick={resetEditor}
							>Cancel</Button
						>
						<Button
							variant="default"
							size="sm"
							disabled={submitting}
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
