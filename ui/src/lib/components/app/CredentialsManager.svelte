<script lang="ts">
	import CircleHelpIcon from "@lucide/svelte/icons/circle-help";
	import CopyIcon from "@lucide/svelte/icons/copy";
	import ExternalLinkIcon from "@lucide/svelte/icons/external-link";
	import Loader2Icon from "@lucide/svelte/icons/loader-2";
	import LogInIcon from "@lucide/svelte/icons/log-in";
	import PlusIcon from "@lucide/svelte/icons/plus";
	import type {
		CredentialAuthType,
		CredentialEnvVar,
		CredentialOAuthKind,
		CredentialVisibility,
		Icon,
	} from "$lib/api-types";
	import {
		parseBulkEnvVarPaste,
		type BulkEnvVarPaste,
	} from "$lib/components/app/credentials-manager-env-vars";
	import { Button } from "$lib/components/ui/button";
	import CredentialEnvVarEditor from "$lib/components/app/parts/CredentialEnvVarEditor.svelte";
	import CredentialListItem from "$lib/components/app/parts/CredentialListItem.svelte";
	import CredentialOAuthScopePicker from "$lib/components/app/parts/CredentialOAuthScopePicker.svelte";
	import CredentialOAuthWizardDialog from "$lib/components/app/parts/CredentialOAuthWizardDialog.svelte";
	import CredentialTypePicker from "$lib/components/app/parts/CredentialTypePicker.svelte";
	import * as Dialog from "$lib/components/ui/dialog";
	import { Input } from "$lib/components/ui/input";
	import { ItemGroup } from "$lib/components/ui/item";
	import { Label } from "$lib/components/ui/label";
	import { NativeSelect } from "$lib/components/ui/native-select";
	import * as Tooltip from "$lib/components/ui/tooltip";
	import { useAppContext } from "$lib/context/app-context.svelte";
	import { openUrl, writeClipboardText } from "$lib/shell";

	type EditorMode = "list" | "create" | "edit";
	type EnvVarRow = {
		id: string;
		key: string;
		value: string;
		hasStoredValue: boolean;
		replaceValue: boolean;
		valueFocused: boolean;
	};

	type PendingBulkEnvVarPaste = {
		field: "key" | "value";
		originalText: string;
		rowId: string;
		entries: BulkEnvVarPaste[];
	};
	type ScopePickerMode = "simple" | "advanced";
	type ProviderOption = {
		value: string;
		provider: string;
		backendProvider: string;
		authType: CredentialAuthType;
		label: string;
		description: string;
		icons: Icon[];
		group: string;
		groupName: string;
		agentVisible: boolean;
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
	let apiKeyDraft = $state("");
	let oauthDeviceIdDraft = $state("");
	let oauthUserCodeDraft = $state("");
	let oauthVerificationUrl = $state("");
	let oauthAuthUrl = $state("");
	let oauthAuthStateDraft = $state("");
	let oauthRedirectUriDraft = $state("");
	let oauthInputDraft = $state("");
	let oauthPollIntervalSeconds = $state(5);
	let oauthPollDomainDraft = $state("");
	let oauthVerifierDraft = $state("");
	let oauthScopesDraft = $state<string[]>([]);
	let oauthScopePickerMode = $state<ScopePickerMode>("simple");
	let oauthCallbackListening = $state(false);
	let oauthCallbackPolling = $state(false);
	let oauthKindDraft = $state<CredentialOAuthKind | null>(null);
	let githubOAuthWizardOpen = $state(false);
	let openAIOAuthWizardOpen = $state(false);
	let startingOAuth = $state(false);
	let pollingOAuth = $state(false);
	let copiedOAuthCode = $state(false);
	let copiedOAuthAuthUrl = $state(false);
	let inactiveDraft = $state(false);
	let replaceSecretDraft = $state(false);
	let visibilityDraft = $state<CredentialVisibility>({
		tools: false,
		console: false,
		services: false,
		hooks: false,
	});
	let envVarRows = $state<EnvVarRow[]>([]);
	let pendingBulkEnvVarPaste = $state<PendingBulkEnvVarPaste | null>(null);
	let submitting = $state(false);
	let deletingId = $state<string | null>(null);
	let togglingInactiveId = $state<string | null>(null);
	let oauthPollTimer: ReturnType<typeof setTimeout> | null = null;
	let oauthCallbackPollTimer: ReturnType<typeof setTimeout> | null = null;
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
			icons: [...(type.icons ?? [])],
			group: type.group,
			groupName: type.groupName,
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
				icons: [],
				group: "tools",
				groupName: "Tools",
				agentVisible: false,
			},
		];
	});
	const credentialPickerGroups = $derived.by(() => {
		const groups: Array<{
			group: string;
			name: string;
			options: ProviderOption[];
		}> = [];
		for (const option of providerOptions) {
			const existing = groups.find((entry) => entry.group === option.group);
			if (existing) {
				existing.options.push(option);
				continue;
			}
			groups.push({
				group: option.group,
				name: option.groupName,
				options: [option],
			});
		}
		return groups;
	});
	const credentialPickerCardGroups = $derived.by(() =>
		credentialPickerGroups.map((group) => ({
			...group,
			options: group.options.map((option) => {
				const optionImage = providerOptionImage(option);
				return {
					value: option.value,
					label: option.label,
					description: option.description,
					image: optionImage,
					imageClass: optionImage ? providerOptionImageClass(optionImage) : "",
					monogram: providerOptionMonogram(option),
				};
			}),
		})),
	);
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
	const selectedProviderOption = $derived.by(
		() =>
			providerOptions.find(
				(option) => option.value === selectedProviderValue,
			) ?? null,
	);
	const selectedEnvVarName = $derived.by(
		() => selectedCredentialType?.env?.[0] ?? "",
	);
	const selectedOAuthConfig = $derived.by(() =>
		selectedAuthType === "oauth"
			? (selectedCredentialType?.oauth ?? null)
			: null,
	);
	const availableOAuthKinds = $derived.by(
		() =>
			(selectedOAuthConfig?.provider === "github-git"
				? (["authorization_code", "device_code"] as CredentialOAuthKind[])
				: null) ??
			selectedOAuthConfig?.supportedKinds ??
			(selectedOAuthConfig ? [selectedOAuthConfig.kind] : []),
	);
	const selectedOAuthKind = $derived.by(() => {
		if (!selectedOAuthConfig) {
			return null;
		}
		if (oauthKindDraft && availableOAuthKinds.includes(oauthKindDraft)) {
			return oauthKindDraft;
		}
		return selectedOAuthConfig.kind;
	});
	const selectedOAuthScopeOptions = $derived.by(
		() => selectedOAuthConfig?.scopeOptions ?? [],
	);
	const isGitHubOAuthCredential = $derived(
		selectedProvider === "github-git" && selectedAuthType === "oauth",
	);
	const isOpenAIOAuthCredential = $derived(
		selectedProvider === "codex" && selectedAuthType === "oauth",
	);
	const simpleOAuthScopeOptions = $derived.by(() =>
		selectedOAuthScopeOptions.filter((scope) => scope.includeInSimple),
	);
	const defaultOAuthScopeOptions = $derived.by(() => {
		const defaultScopes = selectedOAuthConfig?.defaultScopes ?? [];
		return selectedOAuthScopeOptions.filter((scope) =>
			defaultScopes.includes(scope.value),
		);
	});
	const advancedOAuthScopeGroups = $derived.by(() => {
		const groups: Array<{
			group: string;
			scopes: (typeof selectedOAuthScopeOptions)[number][];
		}> = [];
		for (const scope of selectedOAuthScopeOptions) {
			const group = scope.group ?? "Other";
			const existingGroup = groups.find((entry) => entry.group === group);
			if (existingGroup) {
				existingGroup.scopes.push(scope);
				continue;
			}
			groups.push({
				group,
				scopes: [scope],
			});
		}
		return groups;
	});

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
			valueFocused: false,
		};
	}

	function applyBulkEnvVarPaste(rowId: string, entries: BulkEnvVarPaste[]) {
		const rowIndex = envVarRows.findIndex((row) => row.id === rowId);
		if (rowIndex === -1) {
			return;
		}
		const replacementRows = entries.map((entry) =>
			makeEnvVarRow(entry.key, entry.value, false, true),
		);
		envVarRows = [
			...envVarRows.slice(0, rowIndex),
			...replacementRows,
			...envVarRows.slice(rowIndex + 1),
		];
	}

	function insertTextAtCursor(input: HTMLInputElement, text: string) {
		const start = input.selectionStart ?? input.value.length;
		const end = input.selectionEnd ?? input.value.length;
		const nextValue = `${input.value.slice(0, start)}${text}${input.value.slice(end)}`;
		input.value = nextValue;
		input.setSelectionRange(start + text.length, start + text.length);
		input.dispatchEvent(new Event("input", { bubbles: true }));
	}

	function handleEnvVarPaste(
		rowId: string,
		field: "key" | "value",
		event: ClipboardEvent,
	) {
		const text = event.clipboardData?.getData("text") ?? "";
		const entries = parseBulkEnvVarPaste(text);
		if (entries.length === 0) {
			return;
		}
		event.preventDefault();
		pendingBulkEnvVarPaste = {
			field,
			originalText: text,
			rowId,
			entries,
		};
	}

	function confirmBulkEnvVarPaste() {
		if (!pendingBulkEnvVarPaste) {
			return;
		}
		applyBulkEnvVarPaste(
			pendingBulkEnvVarPaste.rowId,
			pendingBulkEnvVarPaste.entries,
		);
		pendingBulkEnvVarPaste = null;
	}

	function pasteOriginalBulkEnvVarContent() {
		if (!pendingBulkEnvVarPaste) {
			return;
		}
		const { field, originalText, rowId } = pendingBulkEnvVarPaste;
		pendingBulkEnvVarPaste = null;
		queueMicrotask(() => {
			const input = document.querySelector<HTMLInputElement>(
				`[data-env-var-row-id="${rowId}"][data-env-var-field="${field}"]`,
			);
			if (!input) {
				return;
			}
			input.focus();
			insertTextAtCursor(input, originalText);
		});
	}

	function notifyCredentialsChanged() {
		if (typeof window === "undefined") {
			return;
		}
		window.dispatchEvent(new CustomEvent("discobot:credentials-changed"));
	}

	function clearOAuthProgress() {
		if (oauthPollTimer) {
			clearTimeout(oauthPollTimer);
			oauthPollTimer = null;
		}
		if (oauthCallbackPollTimer) {
			clearTimeout(oauthCallbackPollTimer);
			oauthCallbackPollTimer = null;
		}
		oauthDeviceIdDraft = "";
		oauthUserCodeDraft = "";
		oauthVerificationUrl = "";
		oauthAuthUrl = "";
		oauthAuthStateDraft = "";
		oauthRedirectUriDraft = "";
		oauthInputDraft = "";
		oauthPollIntervalSeconds = 5;
		oauthPollDomainDraft = "";
		oauthVerifierDraft = "";
		oauthCallbackListening = false;
		oauthCallbackPolling = false;
		startingOAuth = false;
		pollingOAuth = false;
		copiedOAuthCode = false;
		copiedOAuthAuthUrl = false;
	}

	function resetOAuthSelection(option?: ProviderOption | null) {
		clearOAuthProgress();
		oauthScopesDraft =
			option?.authType === "oauth"
				? [
						...(credentialsApi.credentialTypes.find(
							(type) => type.id === option.value,
						)?.oauth?.defaultScopes ?? []),
					]
				: [];
		oauthScopePickerMode = "simple";
		oauthKindDraft =
			option?.backendProvider === "github-git" && option.authType === "oauth"
				? "authorization_code"
				: option?.authType === "oauth"
					? ((credentialsApi.credentialTypes.find(
							(type) => type.id === option.value,
						)?.oauth?.kind ?? null) as CredentialOAuthKind | null)
					: null;
	}

	function selectCredentialType(option?: ProviderOption | null) {
		if (!option) {
			selectedProvider = "";
			selectedAuthType = "api_key";
			replaceSecretDraft = false;
			visibilityDraft = {
				tools: false,
				console: false,
				services: false,
				hooks: false,
			};
			resetOAuthSelection(null);
			return;
		}
		selectedProvider =
			option.provider === CUSTOM_PROVIDER
				? CUSTOM_PROVIDER
				: option.backendProvider;
		selectedAuthType = option.authType;
		replaceSecretDraft =
			option.authType === "oauth" ? false : mode === "create";
		visibilityDraft = {
			tools: false,
			console: false,
			services: false,
			hooks: false,
		};
		apiKeyDraft = "";
		resetOAuthSelection(option);
	}

	function resetEditor() {
		if (oauthCopiedTimer) {
			clearTimeout(oauthCopiedTimer);
			oauthCopiedTimer = null;
		}
		clearOAuthProgress();
		mode = "list";
		selectedProvider = "";
		selectedAuthType = "api_key";
		editingCredentialId = null;
		nameDraft = "";
		descriptionDraft = "";
		apiKeyDraft = "";
		oauthScopesDraft = [];
		oauthScopePickerMode = "simple";
		oauthKindDraft = null;
		githubOAuthWizardOpen = false;
		openAIOAuthWizardOpen = false;
		inactiveDraft = false;
		replaceSecretDraft = false;
		visibilityDraft = {
			tools: false,
			console: false,
			services: false,
			hooks: false,
		};
		envVarRows = [makeEnvVarRow()];
		pendingBulkEnvVarPaste = null;
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

	function providerOptionImage(option: ProviderOption): Icon | null {
		return option.icons.find((icon) => icon.src.trim().length > 0) ?? null;
	}

	function providerOptionImageClass(icon: Icon): string {
		return [
			"size-full object-contain",
			icon.invertDark ? "dark:[filter:brightness(0)_invert(1)]" : "",
		].join(" ");
	}

	function providerOptionMonogram(option: ProviderOption): string {
		const baseLabel = option.label.replace(/\s*\(.+\)$/, "").trim();
		const words = baseLabel.split(/\s+/).filter(Boolean);
		return (words[0]?.[0] ?? option.label[0] ?? "?").toUpperCase();
	}

	function credentialMonogram(credential: ConfiguredCredential): string {
		const option = credentialProviderOption(credential);
		if (option) {
			return providerOptionMonogram(option);
		}
		const label = credentialDisplayName(credential);
		return (label[0] ?? "?").toUpperCase();
	}

	function chooseCredentialType(option: ProviderOption) {
		selectCredentialType(option);
	}

	function editorDialogTitle() {
		if (mode === "edit") {
			return "Edit credential";
		}
		return hasSelectedProvider
			? `New ${selectedProviderOption?.label ?? "credential"}`
			: "New credential";
	}

	function editorDialogDescription() {
		if (mode === "create" && !hasSelectedProvider) {
			return "Choose a credential type to start configuring it.";
		}
		return "Configure how this credential should be stored and used in this project.";
	}

	function credentialProviderOption(
		credential: ConfiguredCredential,
	): ProviderOption | null {
		if (credential.provider.startsWith("custom:")) {
			return (
				providerOptions.find((option) => option.value === CUSTOM_PROVIDER) ??
				null
			);
		}
		return (
			providerOptions.find(
				(option) =>
					option.backendProvider === credential.provider &&
					option.authType === credential.authType,
			) ?? null
		);
	}

	function credentialDisplayName(credential: ConfiguredCredential) {
		const name = credential.name.trim();
		if (name.length > 0) {
			return name;
		}
		const matchedOption = credentialProviderOption(credential);
		if (matchedOption && matchedOption.provider === CUSTOM_PROVIDER) {
			return credential.envKeys?.join(", ") || "Custom env vars";
		}
		if (matchedOption) {
			return matchedOption.label.replace(/\s*\(.+\)$/, "").trim();
		}
		if (credential.provider.startsWith("custom:")) {
			return credential.envKeys?.join(", ") || "Custom env vars";
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
			return credential.scopes && credential.scopes.length > 0
				? `OAuth · ${credential.scopes.join(", ")}`
				: "OAuth";
		}
		return envKeySummary(credential.envKeys);
	}

	function setOAuthScopeEnabled(scope: string, enabled: boolean) {
		oauthScopesDraft = enabled
			? Array.from(new Set([...oauthScopesDraft, scope]))
			: oauthScopesDraft.filter((value) => value !== scope);
	}

	function isOAuthScopeEnabled(scope: string) {
		return oauthScopesDraft.includes(scope);
	}

	function selectOAuthKind(kind: CredentialOAuthKind) {
		oauthKindDraft = kind;
		clearOAuthProgress();
		oauthScopesDraft = [...(selectedOAuthConfig?.defaultScopes ?? [])];
	}

	function visibilitySummary(visibility: CredentialVisibility) {
		const contexts = [
			visibility.tools ? "tools" : null,
			visibility.console ? "console" : null,
			visibility.services ? "services" : null,
			visibility.hooks ? "hooks" : null,
		].filter((value): value is string => value !== null);
		if (contexts.length === 0) {
			return "internal only";
		}
		return contexts.join(", ");
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
	const credentialListEntries = $derived.by(() =>
		credentialsApi.list.map((credential) => {
			const option = credentialProviderOption(credential);
			const image = option ? providerOptionImage(option) : null;
			return {
				credential,
				title: credentialDisplayName(credential),
				subtitle: `${typeLabel(credential.id, credential.provider, credential.authType)} · ${
					credential.inactive
						? "inactive"
						: visibilitySummary(credential.visibility)
				} · ${credentialSummary(credential)}`,
				image,
				imageClass: image ? providerOptionImageClass(image) : "",
				monogram: credentialMonogram(credential),
			};
		}),
	);

	function startCreate() {
		resetEditor();
		mode = "create";
		selectedProvider = "";
		selectedAuthType = "api_key";
		replaceSecretDraft = false;
	}

	function openGitHubOAuthWizard() {
		if (!isGitHubOAuthCredential) {
			return;
		}
		githubOAuthWizardOpen = true;
	}

	function openOpenAIOAuthWizard() {
		if (!isOpenAIOAuthCredential) {
			return;
		}
		openAIOAuthWizardOpen = true;
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
		visibilityDraft = { ...credential.visibility };
		inactiveDraft = credential.inactive;
		oauthScopesDraft = [
			...(credential.scopes ?? selectedOAuthConfig?.defaultScopes ?? []),
		];
		oauthScopePickerMode = credential.scopes?.some(
			(scope) =>
				!(selectedCredentialType?.oauth?.scopeOptions ?? []).some(
					(option) => option.value === scope && option.includeInSimple,
				),
		)
			? "advanced"
			: "simple";
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
					if (selectedOAuthKind === "authorization_code") {
						const response = await credentialsApi.codexAuthorize();
						oauthAuthUrl = response.url;
						oauthVerifierDraft = response.verifier;
						oauthAuthStateDraft = response.state;
						oauthRedirectUriDraft = response.redirectUri;
						oauthCallbackListening = response.callbackListening;
						startCodexCallbackPolling();
						break;
					}
					const response = await credentialsApi.codexDeviceCode();
					oauthDeviceIdDraft = response.deviceAuthId;
					oauthUserCodeDraft = response.userCode;
					oauthVerificationUrl = response.verificationUri;
					oauthPollIntervalSeconds = response.interval;
					break;
				}
				case "github-git": {
					if (selectedOAuthKind === "authorization_code") {
						const response = await credentialsApi.githubAuthorize({
							scopes: oauthScopesDraft,
							credentialId: editingCredentialId ?? undefined,
							name: nameDraft.trim() || undefined,
							description: descriptionDraft.trim() || undefined,
							visibility: visibilityDraft,
							inactive: inactiveDraft,
						});
						oauthAuthUrl = response.url;
						oauthVerifierDraft = response.verifier;
						oauthAuthStateDraft = response.state;
						oauthRedirectUriDraft = response.redirectUri;
						oauthCallbackListening = response.callbackListening;
						startGitHubCallbackPolling();
						break;
					}
					const response = await credentialsApi.githubDeviceCode({
						scopes: oauthScopesDraft,
					});
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

	async function copyOAuthAuthUrl() {
		if (!oauthAuthUrl) {
			return;
		}
		await writeClipboardText(oauthAuthUrl);
		copiedOAuthAuthUrl = true;
		if (oauthCopiedTimer) {
			clearTimeout(oauthCopiedTimer);
		}
		oauthCopiedTimer = setTimeout(() => {
			copiedOAuthAuthUrl = false;
			copiedOAuthCode = false;
			oauthCopiedTimer = null;
		}, 2000);
	}

	async function openOAuthAuthUrl() {
		if (!oauthAuthUrl) {
			await startOAuthAuthorization();
		}
		if (!oauthAuthUrl) {
			return;
		}
		await openUrl(oauthAuthUrl);
	}

	function startCodexCallbackPolling() {
		if (!oauthAuthStateDraft) {
			return;
		}
		if (oauthCallbackPollTimer) {
			clearTimeout(oauthCallbackPollTimer);
			oauthCallbackPollTimer = null;
		}
		oauthCallbackPolling = true;

		const poll = async () => {
			try {
				const response = await credentialsApi.codexCallbackStatus({
					state: oauthAuthStateDraft,
				});
				if (response.status === "success") {
					resetEditor();
					await load();
					notifyCredentialsChanged();
					return;
				}
				if (response.status === "error") {
					oauthCallbackPolling = false;
					errorMessage = response.error || "Authorization failed";
					return;
				}
				oauthCallbackPollTimer = setTimeout(() => void poll(), 2000);
			} catch {
				oauthCallbackPollTimer = setTimeout(() => void poll(), 2000);
			}
		};

		void poll();
	}

	function startGitHubCallbackPolling() {
		if (!oauthAuthStateDraft) {
			return;
		}
		if (oauthCallbackPollTimer) {
			clearTimeout(oauthCallbackPollTimer);
			oauthCallbackPollTimer = null;
		}
		oauthCallbackPolling = true;

		const poll = async () => {
			try {
				const response = await credentialsApi.githubCallbackStatus({
					state: oauthAuthStateDraft,
				});
				if (response.status === "success") {
					resetEditor();
					await load();
					notifyCredentialsChanged();
					return;
				}
				if (response.status === "error") {
					oauthCallbackPolling = false;
					errorMessage = response.error || "Authorization failed";
					return;
				}
				oauthCallbackPollTimer = setTimeout(() => void poll(), 2000);
			} catch {
				oauthCallbackPollTimer = setTimeout(() => void poll(), 2000);
			}
		};

		void poll();
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
							credentialId: editingCredentialId ?? undefined,
							name: nameDraft.trim() || undefined,
							description: descriptionDraft.trim() || undefined,
							visibility: visibilityDraft,
							inactive: inactiveDraft,
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
				case "codex": {
					const trimmedInput = oauthInputDraft.trim();
					if (!trimmedInput) {
						throw new Error(
							"Enter the authorization code or full redirect URL.",
						);
					}
					if (!oauthVerifierDraft.trim()) {
						throw new Error("Start the OAuth flow before connecting.");
					}
					const response = await credentialsApi.codexExchange({
						code: parseOAuthCode(trimmedInput),
						redirectUri: oauthRedirectUriDraft.trim() || undefined,
						verifier: oauthVerifierDraft.trim(),
					});
					if (!response.success) {
						throw new Error(response.error || "Authorization failed");
					}
					resetEditor();
					await load();
					return;
				}
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
				case "github-git": {
					const trimmedInput = oauthInputDraft.trim();
					if (!trimmedInput) {
						throw new Error(
							"Enter the authorization code or full redirect URL.",
						);
					}
					if (!oauthVerifierDraft.trim()) {
						throw new Error("Start the OAuth flow before connecting.");
					}
					const response = await credentialsApi.githubExchange({
						code: parseOAuthCode(trimmedInput),
						redirectUri: oauthRedirectUriDraft.trim() || undefined,
						verifier: oauthVerifierDraft.trim(),
						credentialId: editingCredentialId ?? undefined,
						name: nameDraft.trim() || undefined,
						description: descriptionDraft.trim() || undefined,
						visibility: visibilityDraft,
						inactive: inactiveDraft,
					});
					if (!response.success) {
						throw new Error(response.error || "Authorization failed");
					}
					resetEditor();
					await load();
					notifyCredentialsChanged();
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
		updateEnvVarRow(rowId, {
			replaceValue: true,
			value: "",
			valueFocused: false,
		});
	}

	function hideEnvVarValueInput(rowId: string) {
		updateEnvVarRow(rowId, {
			replaceValue: false,
			value: "",
			valueFocused: false,
		});
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
					visibility: visibilityDraft,
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
					visibility: visibilityDraft,
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
						visibility: visibilityDraft,
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
						visibility: visibilityDraft,
						inactive: inactiveDraft,
					});
				}
			}
			resetEditor();
			await load();
			notifyCredentialsChanged();
		} catch (error) {
			errorMessage =
				error instanceof Error ? error.message : "Failed to save credential";
		} finally {
			submitting = false;
		}
	}

	async function toggleCredentialInactive(credential: ConfiguredCredential) {
		togglingInactiveId = credential.id;
		errorMessage = null;
		try {
			await credentialsApi.create({
				provider: credential.provider.startsWith("custom:")
					? "custom"
					: credential.provider,
				credentialId: credential.id,
				name: credential.name,
				description: credential.description,
				authType: credential.authType,
				visibility: credential.visibility,
				inactive: !credential.inactive,
			});
			notifyCredentialsChanged();
		} catch (error) {
			errorMessage =
				error instanceof Error
					? error.message
					: `Failed to ${credential.inactive ? "enable" : "disable"} credential`;
		} finally {
			togglingInactiveId = null;
		}
	}

	async function removeCredential(id: string) {
		deletingId = id;
		try {
			await credentialsApi.remove(id);
			notifyCredentialsChanged();
		} finally {
			deletingId = null;
		}
	}

	$effect(() => {
		void load();
	});

	$effect(() => {
		if (loading || !app.ui.credentialFlowIntent) {
			return;
		}
		startCreate();
		const oauthOption =
			providerOptions.find(
				(option) =>
					option.authType === "oauth" &&
					option.backendProvider === app.ui.credentialFlowIntent,
			) ?? null;
		if (oauthOption) {
			selectCredentialType(oauthOption);
			if (oauthOption.backendProvider === "github-git") {
				githubOAuthWizardOpen = true;
			} else if (oauthOption.backendProvider === "codex") {
				openAIOAuthWizardOpen = true;
			}
		}
		app.ui.credentialFlowIntent = null;
	});

	$effect(() => {
		const targetId = app.ui.credentialsDialogTargetId;
		if (!targetId || loading) {
			return;
		}
		const credential = credentialsApi.peek(targetId);
		if (!credential) {
			return;
		}
		startEdit(credential);
		app.ui.credentialsDialogTargetId = null;
	});

	$effect(() => {
		return () => {
			if (oauthPollTimer) {
				clearTimeout(oauthPollTimer);
			}
			if (oauthCallbackPollTimer) {
				clearTimeout(oauthCallbackPollTimer);
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
		{:else}
			<div class="flex items-center justify-between gap-2">
				<div class="text-sm text-muted-foreground">
					Manage built-in credentials and custom environment variable bundles.
				</div>
				<Button variant="outline" size="sm" onclick={startCreate}>
					<PlusIcon class="size-4" />
					New credential
				</Button>
			</div>

			{#if errorMessage && mode === "list"}
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
						{#each credentialListEntries as entry (entry.credential.id)}
							<CredentialListItem
								credential={entry.credential}
								title={entry.title}
								subtitle={entry.subtitle}
								image={entry.image}
								imageClass={entry.imageClass}
								monogram={entry.monogram}
								togglingInactive={togglingInactiveId === entry.credential.id}
								deleting={deletingId === entry.credential.id}
								onToggleInactive={toggleCredentialInactive}
								onEdit={startEdit}
								onDelete={(credential) => removeCredential(credential.id)}
							/>
						{/each}
					{/if}
				</ItemGroup>
			</div>

			<Dialog.Root
				open={mode !== "list" &&
					!githubOAuthWizardOpen &&
					!openAIOAuthWizardOpen}
				onOpenChange={(open) => {
					if (!open && !githubOAuthWizardOpen && !openAIOAuthWizardOpen) {
						resetEditor();
					}
				}}
			>
				<Dialog.Content class="max-h-[85vh] overflow-hidden sm:max-w-3xl">
					<Dialog.Header>
						<Dialog.Title>{editorDialogTitle()}</Dialog.Title>
						<Dialog.Description>{editorDialogDescription()}</Dialog.Description>
					</Dialog.Header>
					<div class="max-h-[min(68vh,44rem)] space-y-4 overflow-y-auto pr-1">
						{#if errorMessage}
							<div class="text-sm text-destructive">{errorMessage}</div>
						{/if}

						{#if mode === "create" && !hasSelectedProvider}
							<CredentialTypePicker
								groups={credentialPickerCardGroups}
								onChoose={(optionValue) => {
									const option =
										providerOptions.find(
											(candidate) => candidate.value === optionValue,
										) ?? null;
									if (option) {
										chooseCredentialType(option);
									}
								}}
							/>
						{:else if hasSelectedProvider}
							<div
								class="space-y-3 rounded-lg border border-border bg-muted/20 p-3"
							>
								<div class="text-sm font-medium">Optional details</div>
								<div class="grid gap-3 md:grid-cols-2">
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
								</div>
							</div>
							{#if selectedProvider === CUSTOM_PROVIDER}
								<CredentialEnvVarEditor
									rows={envVarRows}
									onAddRow={addEnvVarRow}
									onRemoveRow={removeEnvVarRow}
									onUpdateRow={updateEnvVarRow}
									onShowValueInput={showEnvVarValueInput}
									onHideValueInput={hideEnvVarValueInput}
									onPaste={handleEnvVarPaste}
								/>
							{:else if selectedAuthType === "oauth"}
								<div class="space-y-3">
									{#if isGitHubOAuthCredential}
										<div
											class="space-y-3 rounded-md border border-border bg-muted/40 p-4"
										>
											<div class="space-y-1">
												<div class="font-medium">GitHub sign-in wizard</div>
												<p class="text-sm text-muted-foreground">
													Choose how you want to sign in, review the GitHub
													access, and finish the connection here.
												</p>
											</div>
											<div class="flex flex-wrap gap-2">
												<Button
													variant="default"
													size="sm"
													class="gap-2"
													onclick={openGitHubOAuthWizard}
												>
													<LogInIcon class="size-4" />
													Open GitHub sign-in wizard
												</Button>
												{#if editingCredentialId}
													<div
														class="flex items-center text-sm text-muted-foreground"
													>
														GitHub is connected. Reopen the wizard if you want
														to reconnect it.
													</div>
												{/if}
											</div>
										</div>
									{:else if isOpenAIOAuthCredential}
										<div
											class="space-y-3 rounded-md border border-border bg-muted/40 p-4"
										>
											<div class="space-y-1">
												<div class="font-medium">OpenAI sign-in wizard</div>
												<p class="text-sm text-muted-foreground">
													Choose how you want to sign in and finish the OpenAI
													connection here.
												</p>
											</div>
											<div class="flex flex-wrap gap-2">
												<Button
													variant="default"
													size="sm"
													class="gap-2"
													onclick={openOpenAIOAuthWizard}
												>
													<LogInIcon class="size-4" />
													Open OpenAI sign-in wizard
												</Button>
												{#if editingCredentialId}
													<div
														class="flex items-center text-sm text-muted-foreground"
													>
														OpenAI is connected. Reopen the wizard if you want
														to reconnect it.
													</div>
												{/if}
											</div>
										</div>
									{:else}
										<div class="space-y-1">
											<Label>
												{selectedOAuthKind === "device_code"
													? "Device code"
													: (selectedOAuthConfig?.inputLabel ??
														"Authorization code")}
											</Label>
											<p class="text-sm text-muted-foreground">
												{selectedOAuthConfig?.description ??
													"Use ChatGPT device auth to connect this credential."}
											</p>
										</div>
										{#if selectedOAuthConfig?.scopeOptions?.length}
											<CredentialOAuthScopePicker
												mode={oauthScopePickerMode}
												simpleOptions={simpleOAuthScopeOptions}
												advancedGroups={advancedOAuthScopeGroups}
												onModeChange={(mode) => {
													oauthScopePickerMode = mode;
												}}
												isEnabled={isOAuthScopeEnabled}
												onSetEnabled={setOAuthScopeEnabled}
											/>
										{/if}
										{#if availableOAuthKinds.length > 1}
											<div class="flex flex-wrap gap-2">
												{#each availableOAuthKinds as oauthKind}
													<Button
														variant={selectedOAuthKind === oauthKind
															? "default"
															: "outline"}
														size="sm"
														onclick={() => selectOAuthKind(oauthKind)}
													>
														{oauthKind === "authorization_code"
															? "Redirect Sign-In"
															: "Device code"}
													</Button>
												{/each}
											</div>
										{/if}
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
													onclick={() => void openOAuthAuthUrl()}
												>
													<ExternalLinkIcon class="size-4" />
													{copiedOAuthAuthUrl
														? "Opened and copied"
														: "Open auth page"}
												</Button>
											{/if}
										</div>
										{#if selectedOAuthKind === "device_code" && oauthUserCodeDraft}
											<div
												class="space-y-3 rounded-md border border-border bg-muted/40 p-3"
											>
												<div class="text-sm text-muted-foreground">
													Open
													<code class="mx-1 font-mono"
														>{oauthVerificationUrl}</code
													>
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
												{#if oauthAuthUrl}
													<div
														class="rounded-md border border-border bg-muted/40 p-3 text-sm text-muted-foreground"
													>
														<p>
															Sign in with ChatGPT. OpenAI redirects to
															<code class="mx-1 font-mono">localhost:1455</code
															>, and Discobot will try to catch that redirect
															automatically.
														</p>
														<p class="mt-2">
															{#if oauthCallbackListening}
																Waiting for the local callback. If it does not
																land, paste the full redirect URL or just the <code
																	class="font-mono">code</code
																> below.
															{:else}
																Could not bind
																<code class="mx-1 font-mono"
																	>localhost:1455</code
																>. Paste the full redirect URL or just the
																<code class="font-mono">code</code> below after signing
																in.
															{/if}
														</p>
														{#if oauthCallbackPolling}
															<p
																class="mt-2 flex items-center gap-2 text-foreground"
															>
																<Loader2Icon class="size-4 animate-spin" />
																Waiting for the redirect…
															</p>
														{/if}
													</div>
												{/if}
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

							<div class="space-y-2">
								<div class="flex items-center gap-2 text-sm">
									<div class="font-medium">Runtime visibility</div>
									<Tooltip.Root>
										<Tooltip.Trigger>
											{#snippet child({ props })}
												<button
													type="button"
													class="text-muted-foreground hover:text-foreground inline-flex items-center"
													aria-label="Explain credential visibility"
													{...props}
												>
													<CircleHelpIcon class="size-4" />
												</button>
											{/snippet}
										</Tooltip.Trigger>
										<Tooltip.Content side="top" align="start" class="max-w-72">
											Choose which runtime contexts receive this credential.
											Tools allows the agent and developer tools to use it
											directly. Console / SSH / IDE applies to SSH and terminal
											sessions. Services and hooks apply to workspace automation
											under <code>.discobot/</code>.
										</Tooltip.Content>
									</Tooltip.Root>
								</div>
								<div class="grid gap-2 sm:grid-cols-2">
									<label class="flex items-center gap-2 text-sm">
										<input
											type="checkbox"
											checked={visibilityDraft.tools}
											onchange={(event) =>
												(visibilityDraft = {
													...visibilityDraft,
													tools: (event.currentTarget as HTMLInputElement)
														.checked,
												})}
										/>
										Tools
									</label>
									<label class="flex items-center gap-2 text-sm">
										<input
											type="checkbox"
											checked={visibilityDraft.console}
											onchange={(event) =>
												(visibilityDraft = {
													...visibilityDraft,
													console: (event.currentTarget as HTMLInputElement)
														.checked,
												})}
										/>
										Console
									</label>
									<label class="flex items-center gap-2 text-sm">
										<input
											type="checkbox"
											checked={visibilityDraft.services}
											onchange={(event) =>
												(visibilityDraft = {
													...visibilityDraft,
													services: (event.currentTarget as HTMLInputElement)
														.checked,
												})}
										/>
										Services
									</label>
									<label class="flex items-center gap-2 text-sm">
										<input
											type="checkbox"
											checked={visibilityDraft.hooks}
											onchange={(event) =>
												(visibilityDraft = {
													...visibilityDraft,
													hooks: (event.currentTarget as HTMLInputElement)
														.checked,
												})}
										/>
										Hooks
									</label>
								</div>
								{#if visibilityDraft.tools}
									<div
										class="rounded-md border border-amber-500/40 bg-amber-500/10 p-3 text-sm text-amber-950 dark:text-amber-100"
									>
										<div class="font-medium">
											Warning: tool visibility increases exposure.
										</div>
										<div class="mt-1 text-current/90">
											The agent and its tools may be able to read or use this
											credential during a conversation.
										</div>
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
						{/if}
					</div>
				</Dialog.Content>
			</Dialog.Root>
		{/if}

		<CredentialOAuthWizardDialog
			open={githubOAuthWizardOpen}
			title="Connect GitHub"
			providerName="GitHub"
			openVerificationLabel="Open GitHub page"
			waitingForProviderLabel="Waiting for GitHub…"
			deviceIntroLine1="Ask Discobot for a GitHub device code."
			deviceIntroLine2="Open the GitHub verification page and enter the code."
			deviceReturnText="Come back here and wait while Discobot finishes the connection."
			{selectedOAuthKind}
			hasScopeOptions={selectedOAuthScopeOptions.length > 0}
			{oauthScopePickerMode}
			{defaultOAuthScopeOptions}
			{advancedOAuthScopeGroups}
			{startingOAuth}
			{pollingOAuth}
			{copiedOAuthCode}
			{copiedOAuthAuthUrl}
			{oauthAuthUrl}
			{oauthInputDraft}
			{oauthVerifierDraft}
			{oauthVerificationUrl}
			{oauthUserCodeDraft}
			{errorMessage}
			onOpenChange={(open) => {
				githubOAuthWizardOpen = open;
			}}
			onSelectOAuthKind={selectOAuthKind}
			onSetScopePickerMode={(mode) => {
				oauthScopePickerMode = mode;
			}}
			onResetScopesToDefaults={() => {
				oauthScopesDraft = [...(selectedOAuthConfig?.defaultScopes ?? [])];
				oauthScopePickerMode = "simple";
			}}
			{isOAuthScopeEnabled}
			onSetOAuthScopeEnabled={setOAuthScopeEnabled}
			onOpenOAuthAuthUrl={openOAuthAuthUrl}
			onCopyOAuthAuthUrl={copyOAuthAuthUrl}
			onSetOAuthInputDraft={(value) => {
				oauthInputDraft = value;
			}}
			onSubmitOAuthAuthorizationCode={submitOAuthAuthorizationCode}
			onStartOAuthAuthorization={startOAuthAuthorization}
			onOpenVerificationUrl={() => openUrl(oauthVerificationUrl)}
			onCopyOAuthCode={copyOAuthCode}
			onStartOAuthPolling={startOAuthPolling}
		/>

		<CredentialOAuthWizardDialog
			open={openAIOAuthWizardOpen}
			title="Connect OpenAI"
			providerName="OpenAI"
			openVerificationLabel="Open verification page"
			waitingForProviderLabel="Waiting for OpenAI…"
			deviceIntroLine1="Ask Discobot for an OpenAI device code."
			deviceIntroLine2="Open the verification page and enter the code."
			deviceReturnText="Come back here and wait while Discobot finishes the connection."
			{selectedOAuthKind}
			hasScopeOptions={false}
			{oauthScopePickerMode}
			{defaultOAuthScopeOptions}
			{advancedOAuthScopeGroups}
			{startingOAuth}
			{pollingOAuth}
			{copiedOAuthCode}
			{copiedOAuthAuthUrl}
			{oauthAuthUrl}
			{oauthInputDraft}
			{oauthVerifierDraft}
			{oauthVerificationUrl}
			{oauthUserCodeDraft}
			{errorMessage}
			onOpenChange={(open) => {
				openAIOAuthWizardOpen = open;
			}}
			onSelectOAuthKind={selectOAuthKind}
			onSetScopePickerMode={(mode) => {
				oauthScopePickerMode = mode;
			}}
			onResetScopesToDefaults={() => {
				oauthScopesDraft = [...(selectedOAuthConfig?.defaultScopes ?? [])];
				oauthScopePickerMode = "simple";
			}}
			{isOAuthScopeEnabled}
			onSetOAuthScopeEnabled={setOAuthScopeEnabled}
			onOpenOAuthAuthUrl={openOAuthAuthUrl}
			onCopyOAuthAuthUrl={copyOAuthAuthUrl}
			onSetOAuthInputDraft={(value) => {
				oauthInputDraft = value;
			}}
			onSubmitOAuthAuthorizationCode={submitOAuthAuthorizationCode}
			onStartOAuthAuthorization={startOAuthAuthorization}
			onOpenVerificationUrl={() => openUrl(oauthVerificationUrl)}
			onCopyOAuthCode={copyOAuthCode}
			onStartOAuthPolling={startOAuthPolling}
		/>

		<Dialog.Root
			open={pendingBulkEnvVarPaste !== null}
			onOpenChange={(open) => {
				if (!open) {
					pendingBulkEnvVarPaste = null;
				}
			}}
		>
			<Dialog.Content class="sm:max-w-lg">
				<Dialog.Header>
					<Dialog.Title
						>Create environment variables from pasted text?</Dialog.Title
					>
					<Dialog.Description>
						Detected {pendingBulkEnvVarPaste?.entries.length ?? 0} newline-separated
						assignments. You can create those environment variables now, or paste
						the original text into the field instead.
					</Dialog.Description>
				</Dialog.Header>
				{#if pendingBulkEnvVarPaste}
					<div class="min-w-0 space-y-3">
						<div
							class="min-w-0 max-w-full overflow-x-auto rounded-md border border-border bg-muted/40 p-3"
						>
							<div class="mb-2 text-sm font-medium">Summary</div>
							<ul class="w-max min-w-full space-y-1 text-sm">
								{#each pendingBulkEnvVarPaste.entries as entry}
									<li class="font-mono">{entry.key}={entry.value}</li>
								{/each}
							</ul>
						</div>
						<p class="text-sm text-muted-foreground">
							Leading <code class="font-mono">export</code> prefixes were trimmed
							and quoted values were unwrapped.
						</p>
					</div>
				{/if}
				<Dialog.Footer>
					<Button
						variant="ghost"
						size="sm"
						onclick={pasteOriginalBulkEnvVarContent}
					>
						Paste original text
					</Button>
					<Button variant="default" size="sm" onclick={confirmBulkEnvVarPaste}>
						Create all env vars
					</Button>
				</Dialog.Footer>
			</Dialog.Content>
		</Dialog.Root>
	</Tooltip.Provider>
</div>
