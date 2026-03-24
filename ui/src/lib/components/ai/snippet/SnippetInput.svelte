<script lang="ts">
	import type { ComponentProps } from "svelte";
	import { InputGroupInput } from "$lib/components/ui/input-group";
	import { cn } from "$lib/utils";
	import { useSnippetContext } from "./context";

	let {
		class: className,
		...restProps
	}: ComponentProps<typeof InputGroupInput> = $props();
	const snippet = useSnippetContext();
</script>

<InputGroupInput
	class={cn("text-foreground", className)}
	readonly
	value={snippet.code}
	{...restProps}
/>
