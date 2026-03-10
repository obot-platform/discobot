<script lang="ts">
	import EyeIcon from "@lucide/svelte/icons/eye";
	import EyeOffIcon from "@lucide/svelte/icons/eye-off";
	import { Switch } from "$lib/components/ui/switch";
	import { cn } from "$lib/utils";
	import { useEnvironmentVariablesContext } from "./context";

	type Props = {
		class?: string;
	};

	let { class: className, ...restProps }: Props = $props();
	const environment = useEnvironmentVariablesContext();
</script>

<div class={cn("flex items-center gap-2", className)}>
	<span class="text-muted-foreground text-xs">
		{#if environment.showValues}
			<EyeIcon size={14} />
		{:else}
			<EyeOffIcon size={14} />
		{/if}
	</span>
	<Switch
		aria-label="Toggle value visibility"
		checked={environment.showValues}
		onCheckedChange={environment.setShowValues}
		{...restProps}
	/>
</div>
