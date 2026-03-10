<script lang="ts">
	import CheckCircle2Icon from "@lucide/svelte/icons/check-circle-2";
	import CircleDotIcon from "@lucide/svelte/icons/circle-dot";
	import CircleIcon from "@lucide/svelte/icons/circle";
	import XCircleIcon from "@lucide/svelte/icons/x-circle";
	import { cn } from "$lib/utils";
	import { useTestContext } from "./context";

	type Props = { class?: string; children?: () => any };
	let { class: className, children, ...restProps }: Props = $props();
	const test = useTestContext();
</script>

<span
	class={cn(
		"shrink-0",
		test.status === "passed" && "text-green-600 dark:text-green-400",
		test.status === "failed" && "text-red-600 dark:text-red-400",
		test.status === "skipped" && "text-yellow-600 dark:text-yellow-400",
		test.status === "running" && "text-blue-600 dark:text-blue-400",
		className,
	)}
	{...restProps}
>
	{#if children}
		{@render children()}
	{:else if test.status === "passed"}
		<CheckCircle2Icon class="size-4" />
	{:else if test.status === "failed"}
		<XCircleIcon class="size-4" />
	{:else if test.status === "skipped"}
		<CircleIcon class="size-4" />
	{:else}
		<CircleDotIcon class="size-4 animate-pulse" />
	{/if}
</span>
