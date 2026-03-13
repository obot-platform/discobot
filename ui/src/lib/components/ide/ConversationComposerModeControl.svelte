<script lang="ts">
	import CheckIcon from "@lucide/svelte/icons/check";
	import HammerIcon from "@lucide/svelte/icons/hammer";
	import MapIcon from "@lucide/svelte/icons/map";
	import {
		DropdownMenu,
		DropdownMenuContent,
		DropdownMenuItem,
		DropdownMenuTrigger,
	} from "$lib/components/ui/dropdown-menu";
	import { InputGroupButton } from "$lib/components/ui/input-group";
	import type { ComposerMode } from "./conversation-composer.types";

	type ModeOption = {
		id: ComposerMode;
		label: string;
		description: string;
	};

type Props = {
	value?: ComposerMode;
	onSelect?: (value: ComposerMode) => void;
};

	const modeOptions: ModeOption[] = [
		{
			id: "build",
			label: "Build",
			description: "Execute code, edit files, run tools",
		},
		{
			id: "plan",
			label: "Plan",
			description: "Plan only, no tool execution",
		},
	];

let { value = "build", onSelect = () => {} }: Props = $props();

	const selectedModeOption = $derived.by(
		() => modeOptions.find((modeOption) => modeOption.id === value) ?? modeOptions[0],
	);
</script>

<DropdownMenu>
	<DropdownMenuTrigger class="tauri-no-drag">
		<InputGroupButton
			size="icon-sm"
			variant="ghost"
			aria-label={`Mode: ${selectedModeOption.label}`}
			title={`Mode: ${selectedModeOption.label}`}
		>
			{#if selectedModeOption.id === "plan"}
				<MapIcon class="size-4" />
			{:else}
				<HammerIcon class="size-4" />
			{/if}
		</InputGroupButton>
	</DropdownMenuTrigger>
	<DropdownMenuContent align="start" class="w-64">
		{#each modeOptions as modeOption (modeOption.id)}
			<DropdownMenuItem
				onclick={() => {
					onSelect(modeOption.id);
				}}
				class="justify-between gap-3"
			>
				<div class="flex min-w-0 items-start gap-2">
					{#if modeOption.id === "plan"}
						<MapIcon class="mt-0.5 size-3.5 shrink-0" />
					{:else}
						<HammerIcon class="mt-0.5 size-3.5 shrink-0" />
					{/if}
					<div class="min-w-0">
						<div class="font-medium">{modeOption.label}</div>
						<div class="text-xs text-muted-foreground">{modeOption.description}</div>
					</div>
				</div>
				{#if value === modeOption.id}
					<CheckIcon class="size-3.5 text-primary" />
				{/if}
			</DropdownMenuItem>
		{/each}
	</DropdownMenuContent>
</DropdownMenu>
