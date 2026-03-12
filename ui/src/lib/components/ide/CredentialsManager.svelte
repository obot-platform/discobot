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
	import { onDestroy } from "svelte";
	import type {
		AuthProvider,
		CredentialAuthType,
		CredentialInfo,
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
	type OAuthFlow = "none" | "anthropic" | "openai" | "github-git";
	const SUPPORTED_PROVIDERS = ["anthropic", "openai", "tavily", "github-git"] as const;
	type SupportedProvider = (typeof SUPPORTED_PROVIDERS)[number];

	type SupportedCredential = {
		id: string;
		provider: SupportedProvider;
		backendProvider: string;
		authType: CredentialAuthType;
		updatedAt?: string;
		expiresAt?: string;
	};

	const FALLBACK_PROVIDER_NAMES: Record<SupportedProvider, string> = {
		anthropic: "Anthropic",
		openai: "OpenAI",
		tavily: "Tavily",
		"github-git": "GitHub",
	};

	const PROVIDER_AUTH_TYPES: Record<SupportedProvider, CredentialAuthType[]> = {
		anthropic: ["api_key", "oauth"],
		openai: ["api_key", "oauth"],
		tavily: ["api_key"],
		"github-git": ["oauth"],
	};

	type ProviderGroup = {
		id: "model-providers" | "git-version-control" | "tools";
		title: string;
		providers: SupportedProvider[];
	};

	const PROVIDER_GROUPS: ProviderGroup[] = [
		{
			id: "model-providers",
			title: "Model Providers",
			providers: ["anthropic", "openai"],
		},
		{
			id: "git-version-control",
			title: "Git / Version Control",
			providers: ["github-git"],
		},
		{
			id: "tools",
			title: "Tools",
			providers: ["tavily"],
		},
	];

	const app = useAppContext();
	const credentialsApi = app.credentials;
	const ui = app.ui;
	let providersById = $state<Record<string, AuthProvider>>({});
	let credentials = $state<SupportedCredential[]>([]);
	let loading = $state(false);
	let errorMessage = $state<string | null>(null);
	let mode = $state<EditorMode>("list");
	let editingCredentialId = $state<string | null>(null);
	let selectedProvider = $state<SupportedProvider>("anthropic");
	let selectedAuthType = $state<CredentialAuthType>("api_key");
	let apiKeyDraft = $state("");
	let showApiKey = $state(false);
	let submitting = $state(false);
	let deletingProvider = $state<string | null>(null);

	let oauthFlow = $state<OAuthFlow>("none");
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
	const selectedAuthTypeOptions = $derived.by(() => PROVIDER_AUTH_TYPES[selectedProvider]);
	const credentialsByGroup = $derived.by(() =>
		Object.fromEntries(
			PROVIDER_GROUPS.map((group) => [
				group.id,
				credentials.filter((credential) => group.providers.includes(credential.provider)),
			]),
		) as Record<ProviderGroup["id"], SupportedCredential[]>,
	);
	const providerEnvHint = $derived.by(() => {
		if (selectedProvider === "openai" && selectedAuthType === "oauth") {
			return "CODEX_API_KEY";
		}
		if (selectedProvider === "github-git") {
			return "GITHUB_TOKEN";
		}
		return providersById[selectedProvider]?.env?.[0] ?? "";
	});

	function providerName(provider: SupportedProvider): string {
		return providersById[provider]?.name ?? FALLBACK_PROVIDER_NAMES[provider];
	}

	function credentialLabel(credential: SupportedCredential): string {
		if (credential.provider === "openai" && credential.backendProvider === "codex") {
			return `${providerName("openai")} (OAuth)`;
		}
		return providerName(credential.provider);
	}

	function authLabel(authType: CredentialAuthType): string {
		return authType === "oauth" ? "OAuth" : "API key";
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
		oauthFlow = "none";
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
		selectedProvider = "anthropic";
		selectedAuthType = "api_key";
		apiKeyDraft = "";
		showApiKey = false;
		submitting = false;
		resetOAuthState();
	}

	function normalizeCredentials(rawCredentials: CredentialInfo[]): SupportedCredential[] {
		const mapped: SupportedCredential[] = [];
		for (const credential of rawCredentials) {
			if (credential.provider === "codex") {
				mapped.push({
					id: credential.id,
					provider: "openai",
					backendProvider: "codex",
					authType: "oauth",
					updatedAt: credential.updatedAt,
					expiresAt: credential.expiresAt,
				});
				continue;
			}

			if (!SUPPORTED_PROVIDERS.includes(credential.provider as SupportedProvider)) {
				continue;
			}

			mapped.push({
				id: credential.id,
				provider: credential.provider as SupportedProvider,
				backendProvider: credential.provider,
				authType: credential.authType,
				updatedAt: credential.updatedAt,
				expiresAt: credential.expiresAt,
			});
		}

		mapped.sort((left, right) => {
			const providerOrder =
				SUPPORTED_PROVIDERS.indexOf(left.provider) - SUPPORTED_PROVIDERS.indexOf(right.provider);
			if (providerOrder !== 0) {
				return providerOrder;
			}
			if (left.authType === right.authType) {
				return 0;
			}
			return left.authType === "api_key" ? -1 : 1;
		});

		return mapped;
	}

	async function loadCredentialsData() {
		loading = true;
		errorMessage = null;
		try {
			await credentialsApi.refresh();
			providersById = Object.fromEntries(
				credentialsApi.providers.map((provider) => [provider.id, provider]),
			);
			credentials = normalizeCredentials(credentialsApi.list);
		} catch (error) {
			errorMessage = error instanceof Error ? error.message : "Failed to load credentials";
		} finally {
			loading = false;
		}
	}

	function startCreate() {
		mode = "create";
		editingCredentialId = null;
		selectedProvider = "anthropic";
		selectedAuthType = PROVIDER_AUTH_TYPES.anthropic[0];
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
		if (!SUPPORTED_PROVIDERS.includes(value as SupportedProvider)) {
			return;
		}
		selectedProvider = value as SupportedProvider;
		const nextAuthTypes = PROVIDER_AUTH_TYPES[selectedProvider];
		if (!nextAuthTypes.includes(selectedAuthType)) {
			selectedAuthType = nextAuthTypes[0];
		}
		apiKeyDraft = "";
		resetOAuthState();
		errorMessage = null;
	}

	function handleAuthTypeChange(value: string) {
		if (value !== "api_key" && value !== "oauth") {
			return;
		}
		selectedAuthType = value;
		apiKeyDraft = "";
		resetOAuthState();
		errorMessage = null;
	}

	async function saveApiKeyCredential() {
		const trimmedKey = apiKeyDraft.trim();
		if (!trimmedKey) {
			return;
		}

		submitting = true;
		errorMessage = null;
		try {
			await credentialsApi.create(selectedProvider, trimmedKey);
			await loadCredentialsData();
			resetEditor();
		} catch (error) {
			errorMessage = error instanceof Error ? error.message : "Failed to save credential";
		} finally {
			submitting = false;
		}
	}

	async function deleteCredential(credential: SupportedCredential) {
		deletingProvider = credential.backendProvider;
		errorMessage = null;
		try {
			await credentialsApi.remove(credential.backendProvider);
			await loadCredentialsData();
			if (editingCredentialId === credential.id) {
				resetEditor();
			}
		} catch (error) {
			errorMessage = error instanceof Error ? error.message : "Failed to delete credential";
		} finally {
			deletingProvider = null;
		}
	}

	async function startAnthropicOAuth() {
		oauthBusy = true;
		oauthError = null;
		try {
			const response = await credentialsApi.anthropicAuthorize();
			oauthFlow = "anthropic";
			oauthAuthUrl = response.url;
			oauthVerifier = response.verifier;
			oauthCode = "";
			await openUrl(response.url);
		} catch (error) {
			oauthError = error instanceof Error ? error.message : "Failed to start Anthropic OAuth";
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
			oauthError = error instanceof Error ? error.message : "Anthropic OAuth failed";
		} finally {
			oauthBusy = false;
		}
	}

	async function startOpenAIOAuth() {
		oauthBusy = true;
		oauthError = null;
		try {
			const response = await credentialsApi.codexAuthorize();
			oauthFlow = "openai";
			oauthAuthUrl = response.url;
			oauthVerifier = response.verifier;
			oauthCode = "";
			await openUrl(response.url);
		} catch (error) {
			oauthError = error instanceof Error ? error.message : "Failed to start OpenAI OAuth";
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

	async function completeOpenAIOAuth() {
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
			const result = await credentialsApi.codexExchange({ code, verifier: oauthVerifier });
			if (!result.success) {
				oauthError = result.error ?? "OpenAI OAuth failed";
				return;
			}
			await loadCredentialsData();
			resetEditor();
		} catch (error) {
			oauthError = error instanceof Error ? error.message : "OpenAI OAuth failed";
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
			oauthError = error instanceof Error ? error.message : "GitHub authorization failed";
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
			oauthFlow = "github-git";
			githubDeviceInfo = response;
			await openUrl(response.verificationUri);
		} catch (error) {
			oauthError = error instanceof Error ? error.message : "Failed to start GitHub OAuth";
		} finally {
			oauthBusy = false;
		}
	}

	async function copyGitHubCode() {
		if (!githubDeviceInfo?.userCode || typeof navigator === "undefined" || !navigator.clipboard?.writeText) {
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
		if (selectedProvider === "anthropic") {
			await startAnthropicOAuth();
			return;
		}
		if (selectedProvider === "openai") {
			await startOpenAIOAuth();
			return;
		}
		if (selectedProvider === "github-git") {
			await startGithubOAuth();
		}
	}

	$effect(() => {
		if (!ui.settingsDialog.open) {
			resetEditor();
			return;
		}
		void loadCredentialsData();
	});

	$effect(() => {
		if (!ui.settingsDialog.open || ui.credentialFlowIntent !== "github-git") {
			return;
		}

		ui.credentialFlowIntent = null;
		mode = "create";
		editingCredentialId = null;
		selectedProvider = "github-git";
		selectedAuthType = "oauth";
		apiKeyDraft = "";
		showApiKey = false;
		resetOAuthState();
		errorMessage = null;
		void startGithubOAuth();
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
					<ItemDescription>Choose where this credential will be used.</ItemDescription>
				</ItemContent>
				<ItemActions class="ml-auto w-56 justify-end">
					<Label for="credential-provider" class="sr-only">Provider</Label>
					<NativeSelect
						id="credential-provider"
						value={selectedProvider}
						onchange={(event) => {
							handleProviderChange((event.currentTarget as HTMLSelectElement).value);
						}}
						class="w-full"
						disabled={isEditing}
					>
						{#each PROVIDER_GROUPS as group (group.id)}
							<optgroup label={group.title}>
								{#each group.providers as providerId (providerId)}
									<option value={providerId}>{providerName(providerId)}</option>
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
							Select API key or OAuth.
						{/if}
					</ItemDescription>
				</ItemContent>
				<ItemActions class="ml-auto w-56 justify-end">
					<Label for="credential-auth-type" class="sr-only">Auth type</Label>
					<NativeSelect
						id="credential-auth-type"
						value={selectedAuthType}
						onchange={(event) => {
							handleAuthTypeChange((event.currentTarget as HTMLSelectElement).value);
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

		{#if selectedAuthType === "api_key"}
			<ItemGroup class="rounded-md border border-border">
				<Item size="sm">
					<ItemContent>
						<ItemTitle>API key</ItemTitle>
						<ItemDescription>
							Paste a key to {isEditing ? "update" : "save"} this credential.
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
					</ItemActions>
				</Item>
			</ItemGroup>
		{:else}
			<div class="rounded-md border border-border bg-muted/20 p-3 space-y-3">
				{#if oauthFlow === "none"}
					<p class="text-sm text-muted-foreground">
						{#if selectedProvider === "anthropic"}
							Use Claude login (or direct token) to connect Anthropic.
						{:else if selectedProvider === "openai"}
							Use ChatGPT login (Codex OAuth) to connect OpenAI.
						{:else}
							Use GitHub device flow to connect GitHub for git operations.
						{/if}
					</p>
					<Button variant="outline" size="sm" onclick={startOAuthFlow} disabled={oauthBusy}>
						{#if oauthBusy}
							<Loader2Icon class="size-3.5 animate-spin" />
							Starting...
						{:else}
							<LogInIcon class="size-3.5" />
							Start OAuth
						{/if}
					</Button>
				{:else if oauthFlow === "github-git"}
					{#if githubDeviceInfo}
						<div class="space-y-3">
							<p class="text-sm text-muted-foreground">
								Enter this code on GitHub to authorize access.
							</p>
							<div class="flex items-center gap-2">
								<div class="flex-1 rounded-md border border-border bg-background px-3 py-2 text-center">
									<code class="font-mono text-lg tracking-[0.2em]">{githubDeviceInfo.userCode}</code>
								</div>
								<Button variant="outline" size="icon-sm" onclick={copyGitHubCode}>
									<CopyIcon class="size-3.5" />
								</Button>
							</div>
							{#if copiedCode}
								<p class="text-xs text-muted-foreground">Copied to clipboard.</p>
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
								<div class="flex items-center gap-2 text-sm text-muted-foreground">
									<Loader2Icon class="size-4 animate-spin" />
									Waiting for GitHub authorization...
								</div>
							{:else}
								<Button size="sm" onclick={startGithubPolling}>I've entered the code</Button>
							{/if}
						</div>
					{/if}
				{:else}
					<div class="space-y-3">
						{#if oauthAuthUrl}
							<Button variant="outline" size="sm" onclick={() => oauthAuthUrl && openUrl(oauthAuthUrl)}>
								<ExternalLinkIcon class="size-3.5" />
								Open auth page again
							</Button>
						{/if}
						<Label for="oauth-code" class="text-sm">
							{oauthFlow === "anthropic"
								? "Authorization code or token"
								: "Authorization code or callback URL"}
						</Label>
						<Input
							id="oauth-code"
							value={oauthCode}
							oninput={(event) => {
								oauthCode = (event.currentTarget as HTMLInputElement).value;
							}}
							placeholder={oauthFlow === "anthropic" ? "Paste code or sk-ant-oat0..." : "Paste code or callback URL"}
							class="font-mono text-sm"
							disabled={oauthBusy}
						/>
						<Button
							size="sm"
							onclick={oauthFlow === "anthropic" ? completeAnthropicOAuth : completeOpenAIOAuth}
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
			<div class="rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive">
				{errorMessage}
			</div>
		{/if}

		<div class="flex justify-end gap-2">
			<Button variant="ghost" size="sm" onclick={resetEditor}>Cancel</Button>
			{#if selectedAuthType === "api_key"}
				<Button
					variant="default"
					size="sm"
					onclick={saveApiKeyCredential}
					disabled={apiKeyDraft.trim().length === 0 || submitting}
				>
					{#if submitting}
						<Loader2Icon class="size-3.5 animate-spin" />
						Saving...
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
			<Button variant="outline" size="xs" onclick={startCreate} disabled={loading}>
				<PlusIcon class="size-3" />
				Add credential
			</Button>
		</div>

		{#if loading}
			<div class="flex min-h-28 items-center justify-center gap-2 rounded-md border border-border text-sm text-muted-foreground">
				<Loader2Icon class="size-4 animate-spin" />
				Loading credentials...
			</div>
		{:else}
			<div class="space-y-4">
				{#each PROVIDER_GROUPS as group (group.id)}
					<div class="space-y-2">
						<p class="text-xs font-medium uppercase tracking-[0.14em] text-muted-foreground">
							{group.title}
						</p>
						<ItemGroup class="rounded-md border border-border">
							{#if credentialsByGroup[group.id].length === 0}
								<div class="flex min-h-16 items-center justify-center px-3 text-sm text-muted-foreground">
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
													 · expires {new Date(credential.expiresAt).toLocaleString()}
												{/if}
												{#if credential.updatedAt}
													 · updated {new Date(credential.updatedAt).toLocaleString()}
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
												disabled={deletingProvider === credential.backendProvider}
												title="Delete credential"
											>
												{#if deletingProvider === credential.backendProvider}
													<Loader2Icon class="size-3 animate-spin text-destructive" />
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
			<div class="rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive">
				{errorMessage}
			</div>
		{/if}
	</div>
{/if}
