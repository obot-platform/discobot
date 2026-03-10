<script lang="ts">
	import ChevronDownIcon from "@lucide/svelte/icons/chevron-down";
	import { cn } from "$lib/utils";

	type Props = {
		count?: number;
		label: string;
		class?: string;
		children?: () => any;
	};

	let {
		count,
		label,
		class: className,
		children,
		...restProps
	}: Props = $props();
</script>

<span class={cn("flex items-center gap-2", className)} {...restProps}>
	<ChevronDownIcon class="size-4 transition-transform group-data-[state=closed]:-rotate-90" />
	{@render children?.()}
	<span>{count} {label}</span>
</span>
