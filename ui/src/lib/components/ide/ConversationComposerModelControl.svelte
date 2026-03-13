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
	import { useAppContext } from "$lib/context/app-context.svelte";

	type ModelVariant = {
		id: string;
		displayName: string;
		model: ModelInfo;
		reasoning: boolean;
	};

type Props = {
	value?: string | null;
	onSelect?: (value: string | null) => void;
};

	const app = useAppContext();
	const models = app.models;

let { value = null, onSelect = () => {} }: Props = $props();

	const modelVariants = $derived.by(() => {
		const modelByName: Record<string, ModelInfo> = {};

		for (const model of models.list) {
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

		const variants: ModelVariant[] = [];
		for (const model of Object.values(modelByName)) {
			if (model.reasoning) {
				variants.push({
					id: `${model.id}:thinking`,
					displayName: `${model.name} (thinking)`,
					model,
					reasoning: true,
				});
			}

			variants.push({
				id: model.id,
				displayName: model.name,
				model,
				reasoning: false,
			});
		}

		const getBaseName = (name: string) =>
			name
				.replace(/\s*\(latest\)\s*/gi, "")
				.replace(/\s*\(thinking\)\s*/gi, "")
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

		return [...variants].sort((left, right) => {
			const baseLeft = getBaseName(left.displayName);
			const baseRight = getBaseName(right.displayName);
			const baseCompare = baseLeft.localeCompare(baseRight);
			if (baseCompare !== 0) {
				return baseCompare;
			}

			const versionLeft = extractVersion(left.displayName);
			const versionRight = extractVersion(right.displayName);
			if (versionLeft !== versionRight) {
				return versionRight - versionLeft;
			}

			if (left.reasoning && !right.reasoning) {
				return -1;
			}
			if (!left.reasoning && right.reasoning) {
				return 1;
			}

			return left.displayName.localeCompare(right.displayName);
		});
	});

	const modelProviderEntries = $derived.by(() => {
		const grouped: Record<string, ModelVariant[]> = {};
		for (const variant of modelVariants) {
			const provider = variant.model.provider || "Other";
			if (!grouped[provider]) {
				grouped[provider] = [];
			}
			grouped[provider].push(variant);
		}

		return Object.entries(grouped).sort(([left], [right]) => left.localeCompare(right));
	});

	const selectedModelVariant = $derived.by(
		() => modelVariants.find((variant) => variant.id === value) ?? null,
	);

</script>

<DropdownMenu>
	<DropdownMenuTrigger class="tauri-no-drag">
		<InputGroupButton
			size="xs"
			variant="ghost"
			class="h-6 max-w-[160px] gap-1.5 px-2 text-xs"
			title={selectedModelVariant ? `Model: ${selectedModelVariant.displayName}` : "Model"}
		>
			{#if selectedModelVariant}
				<span class="truncate">
					{selectedModelVariant.displayName.replace(/\s*\(thinking\)\s*/i, "")}
				</span>
			{:else}
				<BrainIcon class="size-3.5 shrink-0" />
			{/if}
			{#if selectedModelVariant?.reasoning}
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

		{#each modelProviderEntries as [provider, variants], providerIndex (provider)}
			{#if providerIndex > 0}
				<DropdownMenuSeparator />
			{/if}
			<DropdownMenuLabel class="text-xs uppercase tracking-[0.16em] text-muted-foreground">
				{provider}
			</DropdownMenuLabel>
			{#each variants as variant (variant.id)}
				<DropdownMenuItem
					onclick={() => {
						onSelect(variant.id);
					}}
					class="justify-between gap-3"
				>
					<div class="min-w-0 flex-1 pl-3">
						<div class="truncate font-medium">{variant.displayName}</div>
						{#if variant.model.description && !variant.reasoning}
							<div class="truncate text-xs text-muted-foreground">{variant.model.description}</div>
						{/if}
					</div>
					{#if value === variant.id}
						<CheckIcon class="size-3.5 text-primary" />
					{/if}
				</DropdownMenuItem>
			{/each}
		{/each}
	</DropdownMenuContent>
</DropdownMenu>
