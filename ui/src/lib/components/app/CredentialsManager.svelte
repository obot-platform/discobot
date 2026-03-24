<script lang="ts">
	import CopyIcon from "@lucide/svelte/icons/copy";
	import EyeIcon from "@lucide/svelte/icons/eye";
	import EyeOffIcon from "@lucide/svelte/icons/eye-off";
	import ExternalLinkIcon from "@lucide/svelte/icons/external-link";
	import Loader2Icon from "@lucide/svelte/icons/loader-2";
	import LogInIcon from "@lucide/svelte/icons/log-in";
	import PencilIcon from "@lucide/svelte/icons/pencil";
	import PlusIcon from "@lucide/svelte/icons/plus";
	import Trash2Icon from "@lucide/svelte/icons/trash-2";
	import { onDestroy, onMount } from "svelte";
	import { SvelteSet } from "svelte/reactivity";
	import type {
		CredentialAuthType,
		CredentialInfo,
		CredentialType,
		CredentialTypeOAuthConfig,
		GitHubDeviceCodeResponse,
	} from "$lib/api-types";
	import { openUrl } from "$lib/tauri";
	import { Button } from "$lib/components/ui/button";
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
	import { useAppContext } from "$lib/context/app-context.svelte";

	type EditorMode = "list" | "create" | "edit";

	type DisplayCredential = {
		id: string;
		provider: string;
		backendProvider: string;
		authType: CredentialAuthType;
		updatedAt?: string;
		expiresAt?: string;
		type: CredentialType;
	};

	type ProviderOption = {
		provider: string;
		name: string;
		group: CredentialType["group"];
		groupName: string;
	};

	const app = useAppContext();
	const credentialsApi = app.credentials;
	let credentialTypes = $state<CredentialType[]>([]);
	let credentials = $state<DisplayCredential[]>([]);
	let loading = $state(false);
	let errorMessage = $state<string | null>(null);
	let mode = $state<EditorMode>("list");
	let editingCredentialId = $state<string | null>(null);
	let selectedProvider = $state("");
	let selectedAuthType = $state<CredentialAuthType>("api_key");
	let apiKeyDraft = $state("");
	let showApiKey = $state(false);
	let submitting = $state(false);
	let deletingProvider = $state<string | null>(null);

	let activeOAuth = $state<CredentialTypeOAuthConfig | null>(null);
	let oauthError = $state<string | null>(null);
	let oauthBusy = $state(false);
	let oauthAuthUrl = $state<string | null>(null);
	let oauthVerifier = $state<string | null>(null);
	let oauthCode = $state("");
	let githubDeviceInfo = $state<GitHubDeviceCodeResponse | null>(null);
	let githubPolling = $state(false);
	let githubPollTimer = $state<ReturnType<typeof setTimeout> | null>(null);
	let copiedCode = $state(false);
	let copiedCodeTimer = $state<ReturnType<typeof setTimeout> | null>(null);

	const hasEditor = $derived.by(() => mode === "create" || mode === "edit");
	const isEditing = $derived.by(() => mode === "edit");
	const providerOptions = $derived.by(() => {
		const seen = new SvelteSet<string>();
		const options: ProviderOption[] = [];
		for (const credentialType of credentialTypes) {
			if (seen.has(credentialType.provider)) {
				continue;
			}
			seen.add(credentialType.provider);
			options.push({
				provider: credentialType.provider,
				name: credentialType.name,
				group: credentialType.group,
				groupName: credentialType.groupName,
			});
		}
		return options;
	});
	const providerGroups = $derived.by(() => {
		const groups: {
			id: CredentialType["group"];
			title: string;
			providers: ProviderOption[];
		}[] = [];
		for (const option of providerOptions) {
			const existing = groups.find((group) => group.id === option.group);
			if (existing) {
				existing.providers.push(option);
				continue;
			}
			groups.push({
				id: option.group,
				title: option.groupName,
				providers: [option],
			});
		}
		return groups;
	});
	const selectedAuthTypeOptions = $derived.by(() =>
		getAuthTypesForProvider(selectedProvider),
	);
	const selectedCredentialType = $derived.by(() =>
		getCredentialType(selectedProvider, selectedAuthType),
	);
	const credentialsByGroup = $derived.by(
		() =>
			Object.fromEntries(
				providerGroups.map((group) => [
					group.id,
					credentials.filter(
						(credential) => credential.type.group === group.id,
					),
				]),
			) as Record<CredentialType["group"], DisplayCredential[]>,
	);
	const providerEnvHint = $derived.by(
		() => selectedCredentialType?.env?.[0] ?? "",
	);

	function getAuthTypesForProvider(provider: string): CredentialAuthType[] {
		const authTypes = credentialTypes
			.filter((credentialType) => credentialType.provider === provider)
			.map((credentialType) => credentialType.authType);
		return Array.from(new Set(authTypes));
	}

	function getCredentialType(
		provider: string,
		authType: CredentialAuthType,
	): CredentialType | undefined {
		return credentialTypes.find(
			(credentialType) =>
				credentialType.provider === provider &&
				credentialType.authType === authType,
		);
	}

	function firstProvider(): string {
		return providerOptions[0]?.provider ?? "";
	}

	function ensureSelection() {
		const nextProvider =
			selectedProvider &&
			providerOptions.some((option) => option.provider === selectedProvider)
				? selectedProvider
				: firstProvider();
		selectedProvider = nextProvider;
		const authTypes = getAuthTypesForProvider(nextProvider);
		if (!authTypes.includes(selectedAuthType)) {
			selectedAuthType = authTypes[0] ?? "api_key";
		}
	}

	function secretLabel(provider: string, authType: CredentialAuthType): string {
		return (
			getCredentialType(provider, authType)?.secretLabel ??
			(authType === "id" ? "ID" : "API key")
		);
	}

	function secretDescription(
		provider: string,
		authType: CredentialAuthType,
		isEditing: boolean,
	): string {
		return (
			getCredentialType(provider, authType)?.secretDescription ??
			`Paste a key to ${isEditing ? "update" : "save"} this credential.`
		);
	}

	function autoGenerateSecret(
		provider: string,
		authType: CredentialAuthType,
	): boolean {
		return getCredentialType(provider, authType)?.autoGenerateSecret ?? false;
	}

	function autoGeneratePrefix(
		provider: string,
		authType: CredentialAuthType,
	): string {
		return getCredentialType(provider, authType)?.autoGeneratePrefix ?? "";
	}

	function autoGenerateDescription(
		provider: string,
		authType: CredentialAuthType,
	): string {
		return (
			getCredentialType(provider, authType)?.autoGenerateDescription ??
			"A random value will be generated automatically when you save."
		);
	}

	function credentialLabel(credential: DisplayCredential): string {
		const providerCredentialTypes = credentialTypes.filter(
			(credentialType) => credentialType.provider === credential.provider,
		);
		if (providerCredentialTypes.length > 1) {
			return `${credential.type.name} (${authLabel(credential.authType)})`;
		}
		return credential.type.configuredName ?? credential.type.name;
	}

	function authLabel(authType: CredentialAuthType): string {
		if (authType === "oauth") {
			return "OAuth";
		}
		if (authType === "id") {
			return "ID";
		}
		return "API key";
	}

	function clearCopiedTimer() {
		if (!copiedCodeTimer) {
			return;
		}
		clearTimeout(copiedCodeTimer);
		copiedCodeTimer = null;
	}

	function stopGithubPolling() {
		githubPolling = false;
		if (!githubPollTimer) {
			return;
		}
		clearTimeout(githubPollTimer);
		githubPollTimer = null;
	}

	function resetOAuthState() {
		stopGithubPolling();
		activeOAuth = null;
		oauthError = null;
		oauthBusy = false;
		oauthAuthUrl = null;
		oauthVerifier = null;
		oauthCode = "";
		githubDeviceInfo = null;
		copiedCode = false;
		clearCopiedTimer();
	}

	function resetEditor() {
		mode = "list";
		editingCredentialId = null;
		apiKeyDraft = "";
		showApiKey = false;
		submitting = false;
		resetOAuthState();
		ensureSelection();
	}

	function normalizeCredentials(
		rawCredentials: CredentialInfo[],
	): DisplayCredential[] {
		const mapped: DisplayCredential[] = [];
		for (const credential of rawCredentials) {
			const credentialType = credentialTypes.find(
				(type) =>
					type.backendProvider === credential.provider &&
					type.authType === credential.authType,
			);
			if (!credentialType) {
				continue;
			}

			mapped.push({
				id: credential.id,
				provider: credentialType.provider,
				backendProvider: credential.provider,
				authType: credential.authType,
				updatedAt: credential.updatedAt,
				expiresAt: credential.expiresAt,
				type: credentialType,
			});
		}

		mapped.sort((left, right) => {
			const leftIndex = credentialTypes.findIndex(
				(type) => type.id === left.type.id,
			);
			const rightIndex = credentialTypes.findIndex(
				(type) => type.id === right.type.id,
			);
			return leftIndex - rightIndex;
		});

		return mapped;
	}

	async function loadCredentialsData() {
		loading = true;
		errorMessage = null;
		try {
			await credentialsApi.refresh();
			credentialTypes = credentialsApi.credentialTypes;
			ensureSelection();
			credentials = normalizeCredentials(credentialsApi.list);
		} catch (error) {
			errorMessage =
				error instanceof Error ? error.message : "Failed to load credentials";
		} finally {
			loading = false;
		}
	}

	async function startGitHubCredentialFlow() {
		mode = "create";
		editingCredentialId = null;
		selectedProvider = "github-git";
		selectedAuthType = "oauth";
		apiKeyDraft = "";
		showApiKey = false;
		resetOAuthState();
		errorMessage = null;
		await loadCredentialsData();
		await startGithubOAuth();
	}

	function startCreate() {
		mode = "create";
		editingCredentialId = null;
		ensureSelection();
		apiKeyDraft = "";
		showApiKey = false;
		resetOAuthState();
		errorMessage = null;
	}

	function startEdit(credentialId: string) {
		const credential = credentials.find((entry) => entry.id === credentialId);
		if (!credential) {
			return;
		}

		mode = "edit";
		editingCredentialId = credential.id;
		selectedProvider = credential.provider;
		selectedAuthType = credential.authType;
		apiKeyDraft = "";
		showApiKey = false;
		resetOAuthState();
		errorMessage = null;
	}

	function handleProviderChange(value: string) {
		if (!providerOptions.some((option) => option.provider === value)) {
			return;
		}
		selectedProvider = value;
		const nextAuthTypes = getAuthTypesForProvider(selectedProvider);
		if (!nextAuthTypes.includes(selectedAuthType)) {
			selectedAuthType = nextAuthTypes[0] ?? "api_key";
		}
		apiKeyDraft = "";
		resetOAuthState();
		errorMessage = null;
	}

	function handleAuthTypeChange(value: string) {
		if (value !== "api_key" && value !== "id" && value !== "oauth") {
			return;
		}
		selectedAuthType = value;
		apiKeyDraft = "";
		resetOAuthState();
		errorMessage = null;
	}

	function generateAutoSecret(prefix: string): string {
		if (typeof crypto === "undefined" || !crypto.getRandomValues) {
			throw new Error(
				"Secure token generation is not available in this environment",
			);
		}
		const bytes = crypto.getRandomValues(new Uint8Array(16));
		return `${prefix}${Array.from(bytes, (byte) => byte.toString(16).padStart(2, "0")).join("")}`;
	}

	async function saveApiKeyCredential() {
		const credentialType = selectedCredentialType;
		if (!credentialType) {
			return;
		}

		submitting = true;
		errorMessage = null;
		try {
			const trimmedKey = autoGenerateSecret(selectedProvider, selectedAuthType)
				? generateAutoSecret(
						autoGeneratePrefix(selectedProvider, selectedAuthType),
					)
				: apiKeyDraft.trim();
			if (!trimmedKey) {
				return;
			}
			await credentialsApi.create(
				credentialType.backendProvider,
				credentialType.authType,
				trimmedKey,
			);
			await loadCredentialsData();
			resetEditor();
		} catch (error) {
			errorMessage =
				error instanceof Error ? error.message : "Failed to save credential";
		} finally {
			submitting = false;
		}
	}

	async function deleteCredential(credential: DisplayCredential) {
		deletingProvider = credential.backendProvider;
		errorMessage = null;
		try {
			await credentialsApi.remove(credential.backendProvider);
			await loadCredentialsData();
			if (editingCredentialId === credential.id) {
				resetEditor();
			}
		} catch (error) {
			errorMessage =
				error instanceof Error ? error.message : "Failed to delete credential";
		} finally {
			deletingProvider = null;
		}
	}

	async function startAnthropicOAuth() {
		oauthBusy = true;
		oauthError = null;
		try {
			const response = await credentialsApi.anthropicAuthorize();
			activeOAuth = selectedCredentialType?.oauth ?? null;
			oauthAuthUrl = response.url;
			oauthVerifier = response.verifier;
			oauthCode = "";
			await openUrl(response.url);
		} catch (error) {
			oauthError =
				error instanceof Error
					? error.message
					: "Failed to start Anthropic OAuth";
		} finally {
			oauthBusy = false;
		}
	}

	async function completeAnthropicOAuth() {
		const trimmedCode = oauthCode.trim();
		if (!trimmedCode) {
			return;
		}

		const isDirectToken = trimmedCode.startsWith("sk-ant-oat0");
		if (!isDirectToken && !oauthVerifier) {
			oauthError = "Missing verifier. Start the OAuth flow again.";
			return;
		}

		oauthBusy = true;
		oauthError = null;
		try {
			const result = await credentialsApi.anthropicExchange({
				code: trimmedCode,
				verifier: isDirectToken ? "" : (oauthVerifier ?? ""),
			});
			if (!result.success) {
				oauthError = result.error ?? "Anthropic OAuth failed";
				return;
			}
			await loadCredentialsData();
			resetEditor();
		} catch (error) {
			oauthError =
				error instanceof Error ? error.message : "Anthropic OAuth failed";
		} finally {
			oauthBusy = false;
		}
	}

	async function startCodexOAuth() {
		oauthBusy = true;
		oauthError = null;
		try {
			const response = await credentialsApi.codexAuthorize();
			activeOAuth = selectedCredentialType?.oauth ?? null;
			oauthAuthUrl = response.url;
			oauthVerifier = response.verifier;
			oauthCode = "";
			await openUrl(response.url);
		} catch (error) {
			oauthError =
				error instanceof Error ? error.message : "Failed to start OpenAI OAuth";
		} finally {
			oauthBusy = false;
		}
	}

	function extractAuthorizationCode(value: string): string {
		const trimmed = value.trim();
		if (!trimmed.includes("code=")) {
			return trimmed;
		}

		try {
			const parsed = new URL(trimmed);
			const param = parsed.searchParams.get("code");
			if (param) {
				return param;
			}
		} catch {
			const match = trimmed.match(/[?&]code=([^&]+)/);
			if (match?.[1]) {
				try {
					return decodeURIComponent(match[1]);
				} catch {
					return match[1];
				}
			}
		}

		return trimmed;
	}

	async function completeCodexOAuth() {
		if (!oauthVerifier) {
			oauthError = "Missing verifier. Start the OAuth flow again.";
			return;
		}

		const code = extractAuthorizationCode(oauthCode);
		if (!code) {
			return;
		}

		oauthBusy = true;
		oauthError = null;
		try {
			const result = await credentialsApi.codexExchange({
				code,
				verifier: oauthVerifier,
			});
			if (!result.success) {
				oauthError = result.error ?? "OpenAI OAuth failed";
				return;
			}
			await loadCredentialsData();
			resetEditor();
		} catch (error) {
			oauthError =
				error instanceof Error ? error.message : "OpenAI OAuth failed";
		} finally {
			oauthBusy = false;
		}
	}

	function scheduleGithubPoll() {
		if (!githubPolling || !githubDeviceInfo) {
			return;
		}
		githubPollTimer = setTimeout(
			() => {
				void runGithubPoll();
			},
			Math.max(1, githubDeviceInfo.interval) * 1000,
		);
	}

	async function runGithubPoll() {
		if (!githubPolling || !githubDeviceInfo) {
			return;
		}

		try {
			const result = await credentialsApi.githubPoll({
				deviceCode: githubDeviceInfo.deviceCode,
				domain: githubDeviceInfo.domain,
			});

			if (result.status === "success") {
				await loadCredentialsData();
				resetEditor();
				return;
			}
			if (result.status === "pending") {
				scheduleGithubPoll();
				return;
			}

			oauthError = result.error ?? "GitHub authorization failed";
			stopGithubPolling();
		} catch (error) {
			oauthError =
				error instanceof Error ? error.message : "GitHub authorization failed";
			stopGithubPolling();
		}
	}

	function startGithubPolling() {
		if (!githubDeviceInfo) {
			return;
		}
		stopGithubPolling();
		githubPolling = true;
		oauthError = null;
		void runGithubPoll();
	}

	async function startGithubOAuth() {
		oauthBusy = true;
		oauthError = null;
		try {
			const response = await credentialsApi.githubDeviceCode();
			activeOAuth = selectedCredentialType?.oauth ?? null;
			githubDeviceInfo = response;
			await openUrl(response.verificationUri);
		} catch (error) {
			oauthError =
				error instanceof Error ? error.message : "Failed to start GitHub OAuth";
		} finally {
			oauthBusy = false;
		}
	}

	async function copyGitHubCode() {
		if (
			!githubDeviceInfo?.userCode ||
			typeof navigator === "undefined" ||
			!navigator.clipboard?.writeText
		) {
			return;
		}
		await navigator.clipboard.writeText(githubDeviceInfo.userCode);
		copiedCode = true;
		clearCopiedTimer();
		copiedCodeTimer = setTimeout(() => {
			copiedCode = false;
			copiedCodeTimer = null;
		}, 1600);
	}

	async function startOAuthFlow() {
		const oauthConfig = selectedCredentialType?.oauth;
		if (!oauthConfig) {
			return;
		}
		if (oauthConfig.provider === "anthropic") {
			await startAnthropicOAuth();
			return;
		}
		if (oauthConfig.provider === "codex") {
			await startCodexOAuth();
			return;
		}
		if (oauthConfig.provider === "github-git") {
			await startGithubOAuth();
		}
	}

	async function completeOAuthFlow() {
		const oauthConfig = activeOAuth;
		if (!oauthConfig) {
			return;
		}
		if (oauthConfig.provider === "anthropic") {
			await completeAnthropicOAuth();
			return;
		}
		if (oauthConfig.provider === "codex") {
			await completeCodexOAuth();
		}
	}

	onMount(() => {
		if (app.ui.credentialFlowIntent === "github-git") {
			app.ui.credentialFlowIntent = null;
			void startGitHubCredentialFlow();
			return;
		}
		void loadCredentialsData();
	});

	onDestroy(() => {
		stopGithubPolling();
		clearCopiedTimer();
	});
</script>

{#if hasEditor}
	<div class="space-y-3">
		<ItemGroup class="rounded-md border border-border">
			<Item size="sm">
				<ItemContent>
					<ItemTitle>Provider</ItemTitle>
					<ItemDescription
						>Choose where this credential will be used.</ItemDescription
					>
				</ItemContent>
				<ItemActions class="ml-auto w-56 justify-end">
					<Label for="credential-provider" class="sr-only">Provider</Label>
					<NativeSelect
						id="credential-provider"
						value={selectedProvider}
						onchange={(event) => {
							handleProviderChange(
								(event.currentTarget as HTMLSelectElement).value,
							);
						}}
						class="w-full"
						disabled={isEditing}
					>
						{#each providerGroups as group (group.id)}
							<optgroup label={group.title}>
								{#each group.providers as option (option.provider)}
									<option value={option.provider}>{option.name}</option>
								{/each}
							</optgroup>
						{/each}
					</NativeSelect>
				</ItemActions>
			</Item>
			<ItemSeparator />
			<Item size="sm">
				<ItemContent>
					<ItemTitle>Authentication</ItemTitle>
					<ItemDescription>
						{#if providerEnvHint}
							Primary env var: {providerEnvHint}
						{:else}
							Select credential type.
						{/if}
					</ItemDescription>
				</ItemContent>
				<ItemActions class="ml-auto w-56 justify-end">
					<Label for="credential-auth-type" class="sr-only">Auth type</Label>
					<NativeSelect
						id="credential-auth-type"
						value={selectedAuthType}
						onchange={(event) => {
							handleAuthTypeChange(
								(event.currentTarget as HTMLSelectElement).value,
							);
						}}
						class="w-full"
					>
						{#each selectedAuthTypeOptions as authType (authType)}
							<option value={authType}>{authLabel(authType)}</option>
						{/each}
					</NativeSelect>
				</ItemActions>
			</Item>
		</ItemGroup>

		{#if selectedAuthType !== "oauth"}
			<ItemGroup class="rounded-md border border-border">
				<Item size="sm">
					<ItemContent>
						<ItemTitle
							>{secretLabel(selectedProvider, selectedAuthType)}</ItemTitle
						>
						<ItemDescription
							>{secretDescription(
								selectedProvider,
								selectedAuthType,
								isEditing,
							)}</ItemDescription
						>
					</ItemContent>
					<ItemActions class="ml-auto w-80 justify-end">
						{#if autoGenerateSecret(selectedProvider, selectedAuthType)}
							<div
								class="w-full rounded-md border border-border bg-muted/40 px-3 py-2 text-sm text-muted-foreground"
							>
								{autoGenerateDescription(selectedProvider, selectedAuthType)}
							</div>
						{:else}
							<div class="flex w-full items-center gap-2">
								<Input
									id="credential-api-key"
									type={showApiKey ? "text" : "password"}
									value={apiKeyDraft}
									oninput={(event) => {
										apiKeyDraft = (event.currentTarget as HTMLInputElement)
											.value;
									}}
									placeholder={providerEnvHint || "sk-..."}
									class="font-mono"
									disabled={submitting}
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
						{/if}
					</ItemActions>
				</Item>
			</ItemGroup>
		{:else}
			<div class="rounded-md border border-border bg-muted/20 p-3 space-y-3">
				{#if activeOAuth === null}
					<p class="text-sm text-muted-foreground">
						{selectedCredentialType?.oauth?.description ??
							"Use OAuth to connect this provider."}
					</p>
					<Button
						variant="outline"
						size="sm"
						onclick={startOAuthFlow}
						disabled={oauthBusy}
					>
						{#if oauthBusy}
							<Loader2Icon class="size-3.5 animate-spin" />
							Starting...
						{:else}
							<LogInIcon class="size-3.5" />
							Start OAuth
						{/if}
					</Button>
				{:else if activeOAuth.kind === "device_code"}
					{#if githubDeviceInfo}
						<div class="space-y-3">
							<p class="text-sm text-muted-foreground">
								Enter this code on GitHub to authorize access.
							</p>
							<div class="flex items-center gap-2">
								<div
									class="flex-1 rounded-md border border-border bg-background px-3 py-2 text-center"
								>
									<code class="font-mono text-lg tracking-[0.2em]"
										>{githubDeviceInfo.userCode}</code
									>
								</div>
								<Button
									variant="outline"
									size="icon-sm"
									onclick={copyGitHubCode}
								>
									<CopyIcon class="size-3.5" />
								</Button>
							</div>
							{#if copiedCode}
								<p class="text-xs text-muted-foreground">
									Copied to clipboard.
								</p>
							{/if}
							<Button
								variant="outline"
								size="sm"
								onclick={() => {
									if (githubDeviceInfo) {
										void openUrl(githubDeviceInfo.verificationUri);
									}
								}}
							>
								<ExternalLinkIcon class="size-3.5" />
								Open verification page
							</Button>
							{#if githubPolling}
								<div
									class="flex items-center gap-2 text-sm text-muted-foreground"
								>
									<Loader2Icon class="size-4 animate-spin" />
									Waiting for GitHub authorization...
								</div>
							{:else}
								<Button size="sm" onclick={startGithubPolling}
									>I've entered the code</Button
								>
							{/if}
						</div>
					{/if}
				{:else}
					<div class="space-y-3">
						{#if oauthAuthUrl}
							<Button
								variant="outline"
								size="sm"
								onclick={() => oauthAuthUrl && openUrl(oauthAuthUrl)}
							>
								<ExternalLinkIcon class="size-3.5" />
								Open auth page again
							</Button>
						{/if}
						<Label for="oauth-code" class="text-sm">
							{activeOAuth.inputLabel ?? "Authorization code"}
						</Label>
						<Input
							id="oauth-code"
							value={oauthCode}
							oninput={(event) => {
								oauthCode = (event.currentTarget as HTMLInputElement).value;
							}}
							placeholder={activeOAuth.inputPlaceholder ??
								"Paste authorization code"}
							class="font-mono text-sm"
							disabled={oauthBusy}
						/>
						<Button
							size="sm"
							onclick={completeOAuthFlow}
							disabled={!oauthCode.trim() || oauthBusy}
						>
							{#if oauthBusy}
								<Loader2Icon class="size-3.5 animate-spin" />
								Completing...
							{:else}
								Complete OAuth
							{/if}
						</Button>
					</div>
				{/if}

				{#if oauthError}
					<p class="text-sm text-destructive">{oauthError}</p>
				{/if}
			</div>
		{/if}

		{#if errorMessage}
			<div
				class="rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive"
			>
				{errorMessage}
			</div>
		{/if}

		<div class="flex justify-end gap-2">
			<Button variant="ghost" size="sm" onclick={resetEditor}>Cancel</Button>
			{#if selectedAuthType !== "oauth"}
				<Button
					variant="default"
					size="sm"
					onclick={saveApiKeyCredential}
					disabled={(!autoGenerateSecret(selectedProvider, selectedAuthType) &&
						apiKeyDraft.trim().length === 0) ||
						submitting}
				>
					{#if submitting}
						<Loader2Icon class="size-3.5 animate-spin" />
						Saving...
					{:else if autoGenerateSecret(selectedProvider, selectedAuthType)}
						{isEditing
							? `Regenerate ${secretLabel(selectedProvider, selectedAuthType)}`
							: `Add ${secretLabel(selectedProvider, selectedAuthType)}`}
					{:else}
						{isEditing ? "Update credential" : "Save credential"}
					{/if}
				</Button>
			{/if}
		</div>
	</div>
{:else}
	<div class="space-y-3">
		<div class="flex justify-end">
			<Button
				variant="outline"
				size="xs"
				onclick={startCreate}
				disabled={loading}
			>
				<PlusIcon class="size-3" />
				Add credential
			</Button>
		</div>

		{#if loading}
			<div
				class="flex min-h-28 items-center justify-center gap-2 rounded-md border border-border text-sm text-muted-foreground"
			>
				<Loader2Icon class="size-4 animate-spin" />
				Loading credentials...
			</div>
		{:else}
			<div class="space-y-4">
				{#each providerGroups as group (group.id)}
					<div class="space-y-2">
						<p
							class="text-xs font-medium uppercase tracking-[0.14em] text-muted-foreground"
						>
							{group.title}
						</p>
						<ItemGroup class="rounded-md border border-border">
							{#if credentialsByGroup[group.id].length === 0}
								<div
									class="flex min-h-16 items-center justify-center px-3 text-sm text-muted-foreground"
								>
									No credentials configured.
								</div>
							{:else}
								{#each credentialsByGroup[group.id] as credential, index (credential.id)}
									<Item size="sm">
										<ItemContent>
											<ItemTitle>{credentialLabel(credential)}</ItemTitle>
											<ItemDescription>
												{authLabel(credential.authType)}
												{#if credential.expiresAt}
													· expires {new Date(
														credential.expiresAt,
													).toLocaleString()}
												{/if}
												{#if credential.updatedAt}
													· updated {new Date(
														credential.updatedAt,
													).toLocaleString()}
												{/if}
											</ItemDescription>
										</ItemContent>
										<ItemActions>
											<Button
												variant="ghost"
												size="icon-xs"
												onclick={() => startEdit(credential.id)}
												title="Edit credential"
											>
												<PencilIcon class="size-3" />
											</Button>
											<Button
												variant="ghost"
												size="icon-xs"
												onclick={() => deleteCredential(credential)}
												disabled={deletingProvider ===
													credential.backendProvider}
												title="Delete credential"
											>
												{#if deletingProvider === credential.backendProvider}
													<Loader2Icon
														class="size-3 animate-spin text-destructive"
													/>
												{:else}
													<Trash2Icon class="size-3 text-destructive" />
												{/if}
											</Button>
										</ItemActions>
									</Item>
									{#if index < credentialsByGroup[group.id].length - 1}
										<ItemSeparator />
									{/if}
								{/each}
							{/if}
						</ItemGroup>
					</div>
				{/each}
			</div>
		{/if}

		{#if errorMessage}
			<div
				class="rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive"
			>
				{errorMessage}
			</div>
		{/if}
	</div>
{/if}
