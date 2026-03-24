<script lang="ts">
	import { cn } from "$lib/utils";

	type Props = {
		date: Date;
		class?: string;
		children?: () => any;
	};

	let { date, class: className, children, ...restProps }: Props = $props();

	const formatted = $derived.by(() =>
		new Intl.RelativeTimeFormat("en", {
			numeric: "auto",
		}).format(
			Math.round((date.getTime() - Date.now()) / (1000 * 60 * 60 * 24)),
			"day",
		),
	);
</script>

<time
	class={cn("text-xs", className)}
	dateTime={date.toISOString()}
	{...restProps}
>
	{#if children}
		{@render children()}
	{:else}
		{formatted}
	{/if}
</time>
