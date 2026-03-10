<script lang="ts">
	import ArrowRightIcon from "@lucide/svelte/icons/arrow-right";
	import { cn } from "$lib/utils";
	import { usePackageInfoContext } from "./context";

	type Props = {
		class?: string;
		children?: () => any;
	};

	let { class: className, children, ...restProps }: Props = $props();
	const info = usePackageInfoContext();
</script>

{#if info.currentVersion || info.newVersion}
	<div
		class={cn("mt-2 flex items-center gap-2 font-mono text-muted-foreground text-sm", className)}
		{...restProps}
	>
		{#if children}
			{@render children()}
		{:else}
			{#if info.currentVersion}
				<span>{info.currentVersion}</span>
			{/if}
			{#if info.currentVersion && info.newVersion}
				<ArrowRightIcon class="size-3" />
			{/if}
			{#if info.newVersion}
				<span class="font-medium text-foreground">{info.newVersion}</span>
			{/if}
		{/if}
	</div>
{/if}
