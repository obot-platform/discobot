<script lang="ts">
	import type { ComponentProps } from "svelte";
	import { Tabs } from "$lib/components/ui/tabs";
	import { cn } from "$lib/utils";

	type Props = ComponentProps<typeof Tabs> & {
		class?: string;
		children?: () => any;
	};

	let { class: className, children, ...restProps }: Props = $props();
</script>

<Tabs class={cn("w-full gap-0", className)} {...restProps}>
	{@render children?.()}
</Tabs>
