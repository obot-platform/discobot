<script lang="ts">
	import type { HTMLAttributes } from "svelte/elements";
	import { cn } from "$lib/utils";

	type Props = HTMLAttributes<HTMLDivElement> & {
		language: string;
		class?: string;
		children?: () => any;
	};

	let {
		language,
		class: className,
		children,
		style,
		...restProps
	}: Props = $props();
</script>

<div
	class={cn(
		"group relative w-full overflow-hidden rounded-md border bg-background text-foreground",
		className,
	)}
	data-language={language}
	style={`content-visibility: auto; contain-intrinsic-size: auto 200px; ${style ?? ""}`}
	{...restProps}
>
	{@render children?.()}
</div>
