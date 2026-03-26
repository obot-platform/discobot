<script lang="ts">
	import BrainIcon from "@lucide/svelte/icons/brain";
	import CheckIcon from "@lucide/svelte/icons/check";
	import type { ModelInfo } from "$lib/api-types";
	import {
		DropdownMenu,
		DropdownMenuContent,
		DropdownMenuItem,
		DropdownMenuLabel,
		DropdownMenuSeparator,
		DropdownMenuTrigger,
	} from "$lib/components/ui/dropdown-menu";
	import { InputGroupButton } from "$lib/components/ui/input-group";

	type Props = {
		value?: string | null;
		onSelect?: (value: string | null) => void;
		models: ModelInfo[];
	};

	let { value = null, onSelect = () => {}, models }: Props = $props();

	const dedupedModels = $derived.by(() => {
		const modelByName: Record<string, ModelInfo> = {};

		for (const model of models) {
			const cleanName = model.name.replace(/\s*\(latest\)\s*/gi, "").trim();
			const isLatest = /\(latest\)/i.test(model.name);
			const existing = modelByName[cleanName];

			if (!existing || isLatest) {
				modelByName[cleanName] = {
					...model,
					name: cleanName,
				};
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
			if (!matches || matches.length === 0) {
				return 0;
			}
			return Number.parseFloat(matches[matches.length - 1]);
		};

		return Object.values(modelByName).sort((left, right) => {
			const baseLeft = getBaseName(left.name);
			const baseRight = getBaseName(right.name);
			const baseCompare = baseLeft.localeCompare(baseRight);
			if (baseCompare !== 0) {
				return baseCompare;
			}

			const versionLeft = extractVersion(left.name);
			const versionRight = extractVersion(right.name);
			if (versionLeft !== versionRight) {
				return versionRight - versionLeft;
			}

			return left.name.localeCompare(right.name);
		});
	});

	const modelProviderEntries = $derived.by(() => {
		const grouped: Record<string, ModelInfo[]> = {};
		for (const model of dedupedModels) {
			const provider = model.provider || "Other";
			if (!grouped[provider]) {
				grouped[provider] = [];
			}
			grouped[provider].push(model);
		}

		return Object.entries(grouped).sort(([left], [right]) =>
			left.localeCompare(right),
		);
	});

	const selectedModel = $derived.by(
		() => dedupedModels.find((model) => model.id === value) ?? null,
	);
</script>

<DropdownMenu>
	<DropdownMenuTrigger class="tauri-no-drag">
		<InputGroupButton
			size="xs"
			variant="ghost"
			class="h-6 max-w-[160px] gap-1.5 px-2 text-xs"
			title={selectedModel ? `Model: ${selectedModel.name}` : "Model"}
		>
			{#if selectedModel}
				<span class="truncate">{selectedModel.name}</span>
			{:else}
				<BrainIcon class="size-3.5 shrink-0" />
			{/if}
		</InputGroupButton>
	</DropdownMenuTrigger>
	<DropdownMenuContent align="start" class="max-h-[24rem] w-80 overflow-y-auto">
		<DropdownMenuItem
			onclick={() => {
				onSelect(null);
			}}
			class="justify-between"
		>
			<span>Default model</span>
			{#if value === null}
				<CheckIcon class="size-3.5 text-primary" />
			{/if}
		</DropdownMenuItem>

		{#if modelProviderEntries.length > 0}
			<DropdownMenuSeparator />
		{/if}

		{#each modelProviderEntries as [provider, providerModels], providerIndex (provider)}
			{#if providerIndex > 0}
				<DropdownMenuSeparator />
			{/if}
			<DropdownMenuLabel
				class="text-xs uppercase tracking-[0.16em] text-muted-foreground"
			>
				{provider}
			</DropdownMenuLabel>
			{#each providerModels as model (model.id)}
				<DropdownMenuItem
					onclick={() => {
						onSelect(model.id);
					}}
					class="justify-between gap-3"
				>
					<div class="min-w-0 flex-1 pl-3">
						<div class="truncate font-medium">{model.name}</div>
						{#if model.description}
							<div class="truncate text-xs text-muted-foreground">
								{model.description}
							</div>
						{/if}
					</div>
					{#if value === model.id}
						<CheckIcon class="size-3.5 text-primary" />
					{/if}
				</DropdownMenuItem>
			{/each}
		{/each}
	</DropdownMenuContent>
</DropdownMenu>
