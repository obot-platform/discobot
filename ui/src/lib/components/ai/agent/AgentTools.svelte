<script lang="ts">
	import type { HTMLAttributes } from "svelte/elements";
	import { Accordion } from "$lib/components/ui/accordion";
	import { cn } from "$lib/utils";

	type Props = Omit<HTMLAttributes<HTMLDivElement>, "value"> & {
		value?: string[];
		defaultValue?: string[];
		onValueChange?: (value: string[]) => void;
		class?: string;
		children?: () => any;
	};

	let {
		defaultValue = [],
		value = $bindable(defaultValue),
		onValueChange,
		class: className,
		children,
		...restProps
	}: Props = $props();

	$effect(() => {
		if (Array.isArray(value)) {
			onValueChange?.(value);
		}
	});
</script>

<div class={cn("space-y-2", className)} {...restProps}>
	<span class="font-medium text-muted-foreground text-sm">Tools</span>
	<Accordion
		bind:value={value as string[] | undefined}
		type="multiple"
		class="rounded-md border"
	>
		{@render children?.()}
	</Accordion>
</div>
