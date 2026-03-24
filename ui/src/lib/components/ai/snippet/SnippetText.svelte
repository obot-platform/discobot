<script lang="ts">
	import type { ComponentProps } from "svelte";
	import { InputGroupText } from "$lib/components/ui/input-group";
	import { cn } from "$lib/utils";

	let {
		class: className,
		children,
		...restProps
	}: ComponentProps<typeof InputGroupText> = $props();
</script>

<InputGroupText
	class={cn("pl-2 font-normal text-muted-foreground", className)}
	{...restProps}
>
	{@render children?.()}
</InputGroupText>
