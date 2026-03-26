<script lang="ts">
	import BrainIcon from "@lucide/svelte/icons/brain";
	import CheckIcon from "@lucide/svelte/icons/check";
	import {
		DropdownMenu,
		DropdownMenuContent,
		DropdownMenuItem,
		DropdownMenuTrigger,
	} from "$lib/components/ui/dropdown-menu";
	import { InputGroupButton } from "$lib/components/ui/input-group";

	type Props = {
		value?: string | undefined;
		defaultValue?: string | undefined;
		levels: string[];
		onSelect?: (value: string | undefined) => void;
	};

	let {
		value = undefined,
		defaultValue = undefined,
		levels,
		onSelect = () => {},
	}: Props = $props();

	function formatReasoningLabel(level: string | undefined) {
		if (!level || level === "default") {
			return "Default";
		}
		if (level === "xhigh") {
			return "X-High";
		}
		return level.charAt(0).toUpperCase() + level.slice(1);
	}

	const resolvedDefaultValue = $derived.by(() => defaultValue ?? undefined);
	const resolvedValue = $derived.by(() =>
		value === undefined || value === "default" ? resolvedDefaultValue : value,
	);
	const displayLevels = $derived.by(() =>
		resolvedDefaultValue
			? levels.filter((level) => level !== resolvedDefaultValue)
			: levels,
	);
	const buttonLabel = $derived.by(() => formatReasoningLabel(resolvedValue));
	const buttonSuffix = $derived.by(() =>
		value === undefined || value === "default" ? " (default)" : "",
	);
	const defaultLabel = $derived.by(() =>
		resolvedDefaultValue
			? `${formatReasoningLabel(resolvedDefaultValue)} (default)`
			: "Default",
	);
	const defaultDescription = $derived.by(() =>
		resolvedDefaultValue
			? `Use the model default (${formatReasoningLabel(resolvedDefaultValue)})`
			: "Use the model default",
	);
</script>

<DropdownMenu>
	<DropdownMenuTrigger class="tauri-no-drag">
		<InputGroupButton
			size="xs"
			variant="ghost"
			class="h-6 gap-1.5 px-2 text-xs"
			title={`Reasoning: ${buttonLabel}${buttonSuffix}`}
		>
			<BrainIcon class="size-3.5 shrink-0" />
			<span>{buttonLabel}{buttonSuffix}</span>
		</InputGroupButton>
	</DropdownMenuTrigger>
	<DropdownMenuContent align="start" class="w-64">
		<DropdownMenuItem
			onclick={() => {
				onSelect("default");
			}}
			class="justify-between gap-3"
		>
			<div class="min-w-0 flex-1">
				<div class="font-medium">{defaultLabel}</div>
				<div class="text-xs text-muted-foreground">{defaultDescription}</div>
			</div>
			{#if value === undefined || value === "default"}
				<CheckIcon class="size-3.5 text-primary" />
			{/if}
		</DropdownMenuItem>

		{#each displayLevels as level (level)}
			<DropdownMenuItem
				onclick={() => {
					onSelect(level);
				}}
				class="justify-between gap-3"
			>
				<div class="min-w-0 flex-1">
					<div class="font-medium">{formatReasoningLabel(level)}</div>
					<div class="text-xs text-muted-foreground">
						Use {level === "none" ? "no" : level} reasoning effort
					</div>
				</div>
				{#if value === level}
					<CheckIcon class="size-3.5 text-primary" />
				{/if}
			</DropdownMenuItem>
		{/each}
	</DropdownMenuContent>
</DropdownMenu>
