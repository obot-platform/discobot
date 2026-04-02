<script lang="ts">
	import CheckIcon from "@lucide/svelte/icons/check";
	import EyeIcon from "@lucide/svelte/icons/eye";
	import EyeOffIcon from "@lucide/svelte/icons/eye-off";
	import KeyRoundIcon from "@lucide/svelte/icons/key-round";
	import SettingsIcon from "@lucide/svelte/icons/settings";
	import { api } from "$lib/api-client";
	import type { SessionCredentialAssignment } from "$lib/api-types";
	import { Button } from "$lib/components/ui/button";
	import * as Dialog from "$lib/components/ui/dialog";
	import {
		DropdownMenu,
		DropdownMenuContent,
		DropdownMenuItem,
		DropdownMenuLabel,
		DropdownMenuSeparator,
		DropdownMenuTrigger,
	} from "$lib/components/ui/dropdown-menu";
	import { useAppContext } from "$lib/context/app-context.svelte";
	import { useSessionContext } from "$lib/context/session-context.svelte";

	const app = useAppContext();
	const session = useSessionContext();

	let assignments = $state<SessionCredentialAssignment[]>([]);
	let loading = $state(false);
	let dialogOpen = $state(false);
	let loadedSessionId = $state<string | null>(null);

	const visibleCount = $derived.by(
		() =>
			assignments.filter(
				(assignment) =>
					assignment.agentVisible && !assignment.credential.inactive,
			).length,
	);

	async function loadAssignments() {
		if (session.isPending) {
			assignments = [];
			return;
		}
		loading = true;
		try {
			const response = await api.getSessionCredentials(session.sessionId);
			assignments = response.credentials;
			await app.credentials.refresh();
		} finally {
			loading = false;
		}
	}

	async function saveAssignments(
		nextAssignments: SessionCredentialAssignment[],
	) {
		const response = await api.setSessionCredentials(
			session.sessionId,
			nextAssignments.map((assignment) => ({
				credentialId: assignment.credentialId,
				agentVisible: assignment.agentVisible,
			})),
		);
		assignments = response.credentials;
	}

	function credentialPreview(assignment: SessionCredentialAssignment) {
		if (
			assignment.credential.envKeys &&
			assignment.credential.envKeys.length > 0
		) {
			return assignment.credential.envKeys.slice(0, 2).join(" · ");
		}
		if (assignment.credential.description) {
			return assignment.credential.description;
		}
		return assignment.credential.provider;
	}

	function credentialDisplayName(assignment: SessionCredentialAssignment) {
		const { credential } = assignment;
		const name = credential.name.trim();
		if (name.length > 0) {
			return name;
		}
		if (credential.provider.startsWith("custom:")) {
			return credential.envKeys?.join(", ") || "Custom env vars";
		}
		const matchedType = app.credentials.credentialTypes.find(
			(type) =>
				type.backendProvider === credential.provider &&
				type.authType === credential.authType,
		);
		if (matchedType) {
			return matchedType.name;
		}
		return credential.provider;
	}

	async function toggleVisibility(credentialId: string) {
		const nextAssignments = assignments.map((assignment) =>
			assignment.credentialId === credentialId
				? assignment.credential.inactive
					? assignment
					: { ...assignment, agentVisible: !assignment.agentVisible }
				: assignment,
		);
		await saveAssignments(nextAssignments);
	}

	$effect(() => {
		const sessionId = session.sessionId;
		const isPending = session.isPending;
		if (isPending) {
			assignments = [];
			loadedSessionId = null;
			return;
		}
		if (loadedSessionId === sessionId) {
			return;
		}
		loadedSessionId = sessionId;
		void loadAssignments();
	});
</script>

<DropdownMenu>
	<DropdownMenuTrigger class="tauri-no-drag">
		<Button
			variant="ghost"
			size="xs"
			class="h-6 gap-1.5 px-2 text-xs"
			aria-label="Select session credentials"
		>
			<KeyRoundIcon
				class={`size-3.5 ${visibleCount > 0 ? "text-yellow-500" : "text-muted-foreground"}`}
			/>
			{#if visibleCount > 1}
				<span>{visibleCount}</span>
			{/if}
		</Button>
	</DropdownMenuTrigger>
	<DropdownMenuContent align="start" class="w-80">
		<DropdownMenuLabel
			class="text-xs uppercase tracking-[0.16em] text-muted-foreground"
		>
			Session credentials
		</DropdownMenuLabel>
		{#if loading}
			<DropdownMenuItem disabled class="text-muted-foreground"
				>Loading…</DropdownMenuItem
			>
		{:else if assignments.length === 0}
			<DropdownMenuItem disabled class="text-muted-foreground"
				>No credentials</DropdownMenuItem
			>
		{:else}
			{#each assignments as assignment (assignment.credentialId)}
				{@const credential = assignment.credential}
				<DropdownMenuItem
					onclick={() => {
						void toggleVisibility(credential.id);
					}}
					class="justify-between gap-3"
				>
					<div class="min-w-0 flex-1">
						<div class="truncate">{credentialDisplayName(assignment)}</div>
						<div class="truncate text-[11px] text-muted-foreground">
							{credential.inactive
								? "Inactive"
								: assignment.agentVisible
									? "Visible to LLM"
									: "Internal only"}
						</div>
					</div>
					{#if assignment.agentVisible && !credential.inactive}
						<CheckIcon class="size-3.5 text-primary" />
					{/if}
				</DropdownMenuItem>
			{/each}
		{/if}
		<DropdownMenuSeparator />
		<DropdownMenuItem onclick={() => (dialogOpen = true)} class="gap-2">
			<SettingsIcon class="size-3.5" />
			Manage session credentials
		</DropdownMenuItem>
	</DropdownMenuContent>
</DropdownMenu>

<Dialog.Root bind:open={dialogOpen}>
	<Dialog.Content
		class="sm:max-w-3xl max-h-[85vh] flex flex-col overflow-hidden"
	>
		<Dialog.Header>
			<Dialog.Title>Session credentials</Dialog.Title>
			<Dialog.Description>
				Choose which credentials are visible to the LLM in this session.
			</Dialog.Description>
		</Dialog.Header>

		<div
			class="min-h-0 flex-1 overflow-auto rounded-md border border-border bg-muted/30 p-2"
		>
			<div class="space-y-2">
				{#each assignments as assignment (assignment.credentialId)}
					{@const credential = assignment.credential}
					<div class="rounded-md border border-border bg-background p-3">
						<div class="flex items-start justify-between gap-3">
							<div class="min-w-0 flex-1">
								<p class="truncate text-sm font-medium">
									{credentialDisplayName(assignment)}
								</p>
								<p class="mt-1 text-xs text-muted-foreground">
									{credentialPreview(assignment)}
								</p>
							</div>
							<div class="flex items-center gap-2">
								<Button
									variant="ghost"
									size="xs"
									onclick={() => {
										void toggleVisibility(credential.id);
									}}
									disabled={credential.inactive}
								>
									{#if credential.inactive}
										<EyeOffIcon class="size-3.5" />
										Inactive
									{:else if assignment.agentVisible}
										<EyeIcon class="size-3.5" />
										Visible
									{:else}
										<EyeOffIcon class="size-3.5" />
										Internal
									{/if}
								</Button>
							</div>
						</div>
					</div>
				{/each}
			</div>
		</div>
	</Dialog.Content>
</Dialog.Root>
