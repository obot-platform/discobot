<script lang="ts">
	import type { ComponentProps } from "svelte";
	import { TabsContent } from "$lib/components/ui/tabs";
	import { cn } from "$lib/utils";

	type Props = ComponentProps<typeof TabsContent> & {
		class?: string;
	};

	let { class: className, ...restProps }: Props = $props();
</script>

<TabsContent class={cn("mt-0 text-sm", className)} {...restProps} />
