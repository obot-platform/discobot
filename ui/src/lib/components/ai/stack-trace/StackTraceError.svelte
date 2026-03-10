<script lang="ts">
	import AlertTriangleIcon from "@lucide/svelte/icons/alert-triangle";
	import { cn } from "$lib/utils";

	type Props = { class?: string; children?: () => any };
	let { class: className, children, ...restProps }: Props = $props();
</script>

<div class={cn("flex flex-1 items-center gap-2 overflow-hidden", className)} {...restProps}>
	<AlertTriangleIcon class="size-4 shrink-0 text-destructive" />
	{@render children?.()}
</div>
