<script lang="ts">
	import { cn } from "$lib/utils";

	type Props = { class?: string; children?: () => any };
	let { class: className, children, ...restProps }: Props = $props();
</script>

<div
	class={cn("flex items-center gap-3 border-b px-4 py-3", className)}
	{...restProps}
>
	{@render children?.()}
</div>
