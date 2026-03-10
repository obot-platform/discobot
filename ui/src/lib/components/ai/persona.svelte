<script lang="ts">
	import BotIcon from "@lucide/svelte/icons/bot";
	import EarIcon from "@lucide/svelte/icons/ear";
	import MoonIcon from "@lucide/svelte/icons/moon";
	import SparklesIcon from "@lucide/svelte/icons/sparkles";
	import Volume2Icon from "@lucide/svelte/icons/volume-2";
	import { cn } from "$lib/utils";

	export type PersonaState = "idle" | "listening" | "thinking" | "speaking" | "asleep";
	type PersonaVariant = "obsidian" | "mana" | "opal" | "halo" | "glint" | "command";

	type Props = {
		state: PersonaState;
		variant?: PersonaVariant;
		class?: string;
	};

	let { state = "idle", variant = "obsidian", class: className }: Props = $props();

	const toneClass = $derived.by(() => {
		switch (variant) {
			case "mana":
				return "from-cyan-500/25 to-blue-500/25";
			case "opal":
				return "from-zinc-500/25 to-slate-500/25";
			case "halo":
				return "from-violet-500/25 to-indigo-500/25";
			case "glint":
				return "from-emerald-500/25 to-teal-500/25";
			case "command":
				return "from-amber-500/25 to-orange-500/25";
			default:
				return "from-primary/25 to-primary/10";
		}
	});

	const StateIcon = $derived.by(() => {
		switch (state) {
			case "listening":
				return EarIcon;
			case "thinking":
				return SparklesIcon;
			case "speaking":
				return Volume2Icon;
			case "asleep":
				return MoonIcon;
			default:
				return BotIcon;
		}
	});
</script>

<div
	class={cn(
		"relative inline-flex size-16 items-center justify-center rounded-full border bg-gradient-to-br",
		toneClass,
		state === "thinking" ? "animate-pulse" : "",
		state === "speaking" ? "discobot-ai-persona-speaking" : "",
		className,
	)}
	aria-label={`Persona ${state}`}
>
	<div class="absolute inset-1 rounded-full bg-background/70"></div>
	<div class="relative z-10">
		<StateIcon class="size-6 text-foreground" />
	</div>
</div>

<style>
	.discobot-ai-persona-speaking {
		animation: discobot-ai-speaking 1.2s ease-in-out infinite;
	}

	@keyframes discobot-ai-speaking {
		0%,
		100% {
			transform: scale(1);
		}
		50% {
			transform: scale(1.06);
		}
	}
</style>
