<script lang="ts">
	import type { HTMLAttributes } from "svelte/elements";
	import { cn } from "$lib/utils";

	type Props = HTMLAttributes<HTMLDivElement> & {
		class?: string;
		children?: () => any;
	};

	let { class: className, children, ...restProps }: Props = $props();
</script>

<div
	class={cn(
		"relative w-full overflow-hidden rounded-md border bg-sidebar",
		className,
	)}
	data-slot="canvas"
	{...restProps}
>
	<div
		aria-hidden="true"
		class="pointer-events-none absolute inset-0 opacity-35 [background-image:radial-gradient(var(--color-border)_1px,transparent_1px)] [background-size:20px_20px]"
	></div>
	<div class="relative h-full min-h-72 w-full">
		{@render children?.()}
	</div>
</div>
