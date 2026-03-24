<script lang="ts">
	import CheckCircle2Icon from "@lucide/svelte/icons/check-circle-2";
	import ChevronRightIcon from "@lucide/svelte/icons/chevron-right";
	import CircleDotIcon from "@lucide/svelte/icons/circle-dot";
	import CircleIcon from "@lucide/svelte/icons/circle";
	import XCircleIcon from "@lucide/svelte/icons/x-circle";
	import { CollapsibleTrigger } from "$lib/components/ui/collapsible";
	import { cn } from "$lib/utils";
	import { useTestSuiteContext } from "./context";

	type Props = { class?: string; children?: () => any };
	let { class: className, children, ...restProps }: Props = $props();
	const testSuite = useTestSuiteContext();
</script>

<CollapsibleTrigger
	class={cn(
		"group flex w-full items-center gap-2 px-4 py-3 text-left transition-colors hover:bg-muted/50",
		className,
	)}
	{...restProps}
>
	<ChevronRightIcon
		class="size-4 shrink-0 text-muted-foreground transition-transform group-data-[state=open]:rotate-90"
	/>
	{#if testSuite.status === "passed"}
		<CheckCircle2Icon
			class="size-4 shrink-0 text-green-600 dark:text-green-400"
		/>
	{:else if testSuite.status === "failed"}
		<XCircleIcon class="size-4 shrink-0 text-red-600 dark:text-red-400" />
	{:else if testSuite.status === "skipped"}
		<CircleIcon class="size-4 shrink-0 text-yellow-600 dark:text-yellow-400" />
	{:else}
		<CircleDotIcon
			class="size-4 shrink-0 animate-pulse text-blue-600 dark:text-blue-400"
		/>
	{/if}
	<span class="font-medium text-sm">
		{#if children}
			{@render children()}
		{:else}
			{testSuite.name}
		{/if}
	</span>
</CollapsibleTrigger>
