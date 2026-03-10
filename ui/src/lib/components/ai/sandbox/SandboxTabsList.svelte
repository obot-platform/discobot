<script lang="ts">
	import type { ComponentProps } from "svelte";
	import { TabsList } from "$lib/components/ui/tabs";
	import { cn } from "$lib/utils";

	type Props = ComponentProps<typeof TabsList> & {
		class?: string;
	};

	let { class: className, ...restProps }: Props = $props();
</script>

<TabsList
	class={cn("h-auto rounded-none border-0 bg-transparent p-0", className)}
	{...restProps}
/>
