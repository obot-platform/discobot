<script lang="ts">
	import type { HTMLAttributes } from "svelte/elements";
	import { cn } from "$lib/utils";

	type Props = HTMLAttributes<HTMLDivElement> & {
		children?: string;
		class?: string;
	};

	let { children, class: className, ...restProps }: Props = $props();
</script>

<div class={cn("space-y-2", className)} {...restProps}>
	<span class="font-medium text-muted-foreground text-sm">Instructions</span>
	<div class="rounded-md bg-muted/50 p-3 text-muted-foreground text-sm">
		<p>{children}</p>
	</div>
</div>
