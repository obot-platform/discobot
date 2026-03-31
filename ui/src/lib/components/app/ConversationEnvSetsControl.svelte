<script lang="ts">
	import CheckIcon from "@lucide/svelte/icons/check";
	import EyeIcon from "@lucide/svelte/icons/eye";
	import EyeOffIcon from "@lucide/svelte/icons/eye-off";
	import LayersIcon from "@lucide/svelte/icons/layers";
	import PencilIcon from "@lucide/svelte/icons/pencil";
	import PlusIcon from "@lucide/svelte/icons/plus";
	import SettingsIcon from "@lucide/svelte/icons/settings";
	import Trash2Icon from "@lucide/svelte/icons/trash-2";
	import XIcon from "@lucide/svelte/icons/x";
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
	import { Input } from "$lib/components/ui/input";
	import { Label } from "$lib/components/ui/label";
	import { useSessionContext } from "$lib/context/session-context.svelte";
	import type {
		SessionEnvSetsService,
		ThreadEnvSetsService,
	} from "$lib/session/session-context.types";

	type EnvVarRow = {
		id: string;
		key: string;
		value: string;
	};

	type Props = {
		sessionEnvSets: SessionEnvSetsService;
		threadEnvSets: ThreadEnvSetsService;
	};

	let { sessionEnvSets, threadEnvSets }: Props = $props();

	const session = useSessionContext();
	const sessionView = session.ui;

	let envSetNameDraft = $state("");
	let envVarRows = $state<EnvVarRow[]>([]);
	let showEnvVarValues = $state(false);

	function envSetVariableCount(envSet: SessionEnvSetsService["list"][number]) {
		return Object.keys(envSet.envVars).length;
	}

	function envSetPreview(envSet: SessionEnvSetsService["list"][number]) {
		const keys = Object.keys(envSet.envVars).slice(0, 2);
		if (keys.length === 0) {
			return "No variables";
		}
		return keys.join(" · ");
	}

	function activeEnvSetCount() {
		return threadEnvSets.activeIds.length;
	}

	function totalEnvSetCount() {
		return sessionEnvSets.list.length;
	}

	function isEnvSetActive(envSetId: string) {
		return threadEnvSets.activeIds.includes(envSetId);
	}

	function makeEnvVarRow(key = "", value = ""): EnvVarRow {
		return {
			id: `env-var-${Date.now()}-${Math.floor(Math.random() * 10_000)}`,
			key,
			value,
		};
	}

	function resetEnvSetEditor() {
		envSetNameDraft = "";
		envVarRows = [];
		showEnvVarValues = false;
	}

	function openEnvSetManager() {
		sessionView.openEnvSetManager();
	}

	function closeEnvSetManager() {
		resetEnvSetEditor();
		sessionView.closeEnvSetManager();
	}

	function startEnvSetCreate() {
		sessionView.startEnvSetCreate();
		envSetNameDraft = "";
		envVarRows = [makeEnvVarRow()];
		showEnvVarValues = false;
	}

	function startEnvSetEdit(envSetId: string) {
		const envSet = sessionEnvSets.list.find((item) => item.id === envSetId);
		if (!envSet) {
			return;
		}

		sessionView.startEnvSetEdit(envSet.id);
		envSetNameDraft = envSet.name;
		envVarRows = Object.entries(envSet.envVars).map(([key, value]) =>
			makeEnvVarRow(key, value),
		);
		showEnvVarValues = false;
		if (envVarRows.length === 0) {
			envVarRows = [makeEnvVarRow()];
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

	function removeEnvVarRow(rowId: string) {
		const nextRows = envVarRows.filter((row) => row.id !== rowId);
		envVarRows = nextRows.length > 0 ? nextRows : [makeEnvVarRow()];
	}

	function envVarsFromRows() {
		return Object.fromEntries(
			envVarRows
				.map((row) => [row.key.trim(), row.value] as const)
				.filter(([key]) => key.length > 0),
		);
	}

	function saveEnvSetEditor() {
		const trimmedName = envSetNameDraft.trim();
		if (!trimmedName) {
			return;
		}

		const envVars = envVarsFromRows();
		if (sessionView.envSetEditorMode === "create") {
			sessionEnvSets.create(trimmedName, envVars);
			closeEnvSetManager();
			return;
		}

		if (
			sessionView.envSetEditorMode === "edit" &&
			sessionView.editingEnvSetId
		) {
			sessionEnvSets.update(sessionView.editingEnvSetId, trimmedName, envVars);
			closeEnvSetManager();
		}
	}

	function removeEnvSet(envSetId: string) {
		sessionEnvSets.remove(envSetId);
		if (sessionView.editingEnvSetId === envSetId) {
			closeEnvSetManager();
		}
	}

	function formatRelativeTime(isoString?: string) {
		if (!isoString) {
			return "never";
		}
		const date = new Date(isoString);
		const diffMs = Date.now() - date.getTime();
		const diffSec = Math.floor(diffMs / 1000);
		if (diffSec < 5) {
			return "just now";
		}
		if (diffSec < 60) {
			return `${diffSec}s ago`;
		}
		const diffMin = Math.floor(diffSec / 60);
		if (diffMin < 60) {
			return `${diffMin}m ago`;
		}
		const diffHour = Math.floor(diffMin / 60);
		if (diffHour < 24) {
			return `${diffHour}h ago`;
		}
		return date.toLocaleDateString();
	}

	function handleEnvSetDialogOpenChange(open: boolean) {
		if (open) {
			sessionView.envSetDialogOpen = true;
			return;
		}
		closeEnvSetManager();
	}
</script>

<DropdownMenu>
	<DropdownMenuTrigger class="tauri-no-drag">
		<Button
			variant="ghost"
			size="xs"
			class="h-6 gap-1.5 px-2 text-xs"
			aria-label="Select env sets"
		>
			<LayersIcon
				class={`size-3.5 ${activeEnvSetCount() > 0 ? "text-yellow-500" : "text-muted-foreground"}`}
			/>
			{#if activeEnvSetCount() > 1}
				<span>{activeEnvSetCount()}</span>
			{/if}
		</Button>
	</DropdownMenuTrigger>
	<DropdownMenuContent align="start" class="w-72">
		<DropdownMenuLabel
			class="text-xs uppercase tracking-[0.16em] text-muted-foreground"
		>
			Env sets
		</DropdownMenuLabel>
		{#if sessionEnvSets.list.length === 0}
			<DropdownMenuItem disabled class="text-muted-foreground"
				>No env sets</DropdownMenuItem
			>
		{:else}
			{#each sessionEnvSets.list as envSet (envSet.id)}
				<DropdownMenuItem
					onclick={() => threadEnvSets.toggle(envSet.id)}
					class="justify-between gap-3"
				>
					<div class="min-w-0 flex-1">
						<div class="truncate">{envSet.name}</div>
						<div class="truncate text-[11px] text-muted-foreground">
							{envSetVariableCount(envSet)} vars
						</div>
					</div>
					{#if isEnvSetActive(envSet.id)}
						<CheckIcon class="size-3.5 text-primary" />
					{/if}
				</DropdownMenuItem>
			{/each}
		{/if}
		<DropdownMenuSeparator />
		<DropdownMenuItem onclick={openEnvSetManager} class="gap-2">
			<SettingsIcon class="size-3.5" />
			Manage env sets
		</DropdownMenuItem>
	</DropdownMenuContent>
</DropdownMenu>

<Dialog.Root
	open={sessionView.envSetDialogOpen}
	onOpenChange={handleEnvSetDialogOpenChange}
>
	<Dialog.Content
		class="sm:max-w-4xl max-h-[85vh] flex flex-col overflow-hidden"
	>
		<Dialog.Header>
			<Dialog.Title class="flex items-center gap-2">
				<LayersIcon class="size-4" />
				{#if sessionView.envSetEditorMode === "create"}
					Create env set
				{:else if sessionView.envSetEditorMode === "edit"}
					Edit env set
				{:else}
					Manage env sets
				{/if}
			</Dialog.Title>
			<Dialog.Description>
				{#if sessionView.envSetEditorMode === "list"}
					Choose env sets for this session and manage reusable variable groups.
				{:else}
					Names and variables are mock-backed through session context.
				{/if}
			</Dialog.Description>
		</Dialog.Header>

		{#if sessionView.envSetEditorMode === "list"}
			<div class="flex items-center justify-between gap-2">
				<div class="text-sm text-muted-foreground">
					Active in this session: {activeEnvSetCount()} / {totalEnvSetCount()}
				</div>
				<Button variant="outline" size="xs" onclick={startEnvSetCreate}>
					<PlusIcon class="size-3" />
					New env set
				</Button>
			</div>

			<div
				class="mt-2 min-h-0 flex-1 overflow-auto rounded-md border border-border bg-muted/30 p-2"
			>
				{#if sessionEnvSets.list.length === 0}
					<div
						class="flex h-full items-center justify-center text-sm text-muted-foreground"
					>
						No env sets yet.
					</div>
				{:else}
					<div class="space-y-2">
						{#each sessionEnvSets.list as envSet (envSet.id)}
							<div class="rounded-md border border-border bg-background p-3">
								<div class="flex items-start gap-3">
									<div class="min-w-0 flex-1">
										<div class="flex items-center gap-2">
											<p class="truncate text-sm font-medium">{envSet.name}</p>
											{#if isEnvSetActive(envSet.id)}
												<span
													class="inline-flex items-center rounded-sm border border-border bg-muted px-1.5 py-0.5 text-[10px] uppercase tracking-wide text-muted-foreground"
												>
													Active
												</span>
											{/if}
										</div>
										<p class="mt-1 text-xs text-muted-foreground">
											{envSetVariableCount(envSet)} vars · updated {formatRelativeTime(
												envSet.updatedAt,
											)}
										</p>
										<p class="mt-1 truncate text-xs text-muted-foreground">
											{envSetPreview(envSet)}
										</p>
									</div>
									<div class="flex items-center gap-1">
										<Button
											variant={isEnvSetActive(envSet.id)
												? "secondary"
												: "outline"}
											size="xs"
											onclick={() => threadEnvSets.toggle(envSet.id)}
										>
											{isEnvSetActive(envSet.id) ? "Enabled" : "Enable"}
										</Button>
										<Button
											variant="ghost"
											size="icon-xs"
											onclick={() => startEnvSetEdit(envSet.id)}
											title="Edit env set"
										>
											<PencilIcon class="size-3" />
										</Button>
										<Button
											variant="ghost"
											size="icon-xs"
											onclick={() => removeEnvSet(envSet.id)}
											title="Delete env set"
										>
											<Trash2Icon class="size-3 text-destructive" />
										</Button>
									</div>
								</div>
							</div>
						{/each}
					</div>
				{/if}
			</div>
		{:else}
			<div
				class="mt-1 min-h-0 flex-1 overflow-auto rounded-md border border-border bg-muted/30 p-3"
			>
				<div class="space-y-4">
					<div class="space-y-1.5">
						<Label for="env-set-name">Name</Label>
						<Input
							id="env-set-name"
							value={envSetNameDraft}
							oninput={(event) =>
								(envSetNameDraft = (event.currentTarget as HTMLInputElement)
									.value)}
							placeholder="Preview environment"
						/>
					</div>

					<div class="space-y-2">
						<div class="flex items-center justify-between">
							<Label>Variables</Label>
							<div class="flex items-center gap-1">
								<Button
									variant="ghost"
									size="xs"
									onclick={() => {
										showEnvVarValues = !showEnvVarValues;
									}}
								>
									{#if showEnvVarValues}
										<EyeOffIcon class="size-3.5" />
										Hide values
									{:else}
										<EyeIcon class="size-3.5" />
										Show values
									{/if}
								</Button>
								<Button variant="outline" size="xs" onclick={addEnvVarRow}>
									<PlusIcon class="size-3" />
									Add row
								</Button>
							</div>
						</div>
						<div class="space-y-2">
							{#each envVarRows as row (row.id)}
								<div class="flex items-center gap-2">
									<Input
										value={row.key}
										oninput={(event) =>
											updateEnvVarRow(row.id, {
												key: (event.currentTarget as HTMLInputElement).value,
											})}
										placeholder="KEY"
										class="font-mono"
									/>
									<Input
										type={showEnvVarValues ? "text" : "password"}
										value={row.value}
										oninput={(event) =>
											updateEnvVarRow(row.id, {
												value: (event.currentTarget as HTMLInputElement).value,
											})}
										placeholder="value"
										class="font-mono"
									/>
									<Button
										variant="ghost"
										size="icon-xs"
										onclick={() => removeEnvVarRow(row.id)}
										title="Remove row"
									>
										<XIcon class="size-3" />
									</Button>
								</div>
							{/each}
						</div>
					</div>
				</div>
			</div>

			<Dialog.Footer class="mt-3">
				<Button variant="ghost" size="sm" onclick={closeEnvSetManager}>
					Cancel
				</Button>
				<Button
					variant="default"
					size="sm"
					onclick={saveEnvSetEditor}
					disabled={envSetNameDraft.trim().length === 0}
				>
					Save env set
				</Button>
			</Dialog.Footer>
		{/if}
	</Dialog.Content>
</Dialog.Root>
