<script lang="ts">
	import { cn } from "$lib/utils";

	type Props = {
		completed?: boolean;
		class?: string;
	};

	let { completed = false, class: className, ...restProps }: Props = $props();
</script>

<span
	class={cn(
		"mt-0.5 inline-block size-2.5 rounded-full border",
		completed
			? "border-muted-foreground/20 bg-muted-foreground/10"
			: "border-muted-foreground/50",
		className,
	)}
	{...restProps}
></span>
