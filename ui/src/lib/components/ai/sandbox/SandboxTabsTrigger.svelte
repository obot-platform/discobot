<script lang="ts">
	import type { ComponentProps } from "svelte";
	import { TabsTrigger } from "$lib/components/ui/tabs";
	import { cn } from "$lib/utils";

	type Props = ComponentProps<typeof TabsTrigger> & {
		class?: string;
	};

	let { class: className, ...restProps }: Props = $props();
</script>

<TabsTrigger
	class={cn(
		"rounded-none border-0 border-transparent border-b-2 px-4 py-2 font-medium text-muted-foreground text-sm transition-colors data-[state=active]:border-primary data-[state=active]:bg-transparent data-[state=active]:text-foreground data-[state=active]:shadow-none",
		className,
	)}
	{...restProps}
/>
