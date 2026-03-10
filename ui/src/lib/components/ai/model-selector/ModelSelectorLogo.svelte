<script lang="ts">
	import { cn } from "$lib/utils";

	type Props = {
		provider: string;
		class?: string;
	};

	let { provider, class: className, ...restProps }: Props = $props();
</script>

<img
	alt={`${provider} logo`}
	class={cn("size-3 dark:invert", className)}
	height={12}
	src={`https://models.dev/logos/${provider}.svg`}
	width={12}
	{...restProps}
/>
