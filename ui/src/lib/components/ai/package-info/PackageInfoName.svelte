<script lang="ts">
	import PackageIcon from "@lucide/svelte/icons/package";
	import { cn } from "$lib/utils";
	import { usePackageInfoContext } from "./context";

	type Props = {
		class?: string;
		children?: () => any;
	};

	let { class: className, children, ...restProps }: Props = $props();
	const info = usePackageInfoContext();
</script>

<div class={cn("flex items-center gap-2", className)} {...restProps}>
	<PackageIcon class="size-4 text-muted-foreground" />
	<span class="font-medium font-mono text-sm">
		{#if children}
			{@render children()}
		{:else}
			{info.name}
		{/if}
	</span>
</div>
