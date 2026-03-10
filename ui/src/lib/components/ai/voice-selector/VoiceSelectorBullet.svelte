<script lang="ts">
	import { cn } from "$lib/utils";
	type Props = { class?: string };
	let { class: className, ...restProps }: Props = $props();
</script>

<span
	aria-hidden="true"
	class={cn("select-none text-border", className)}
	{...restProps}
>
	&bull;
</span>
