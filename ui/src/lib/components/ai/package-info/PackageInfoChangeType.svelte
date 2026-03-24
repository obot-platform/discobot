<script lang="ts">
	import ArrowRightIcon from "@lucide/svelte/icons/arrow-right";
	import MinusIcon from "@lucide/svelte/icons/minus";
	import PlusIcon from "@lucide/svelte/icons/plus";
	import { Badge } from "$lib/components/ui/badge";
	import { cn } from "$lib/utils";
	import { usePackageInfoContext, type ChangeType } from "./context";

	type Props = {
		class?: string;
		children?: () => any;
	};

	let { class: className, children, ...restProps }: Props = $props();
	const info = usePackageInfoContext();

	const styles: Record<ChangeType, string> = {
		major: "bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400",
		minor:
			"bg-yellow-100 text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-400",
		patch:
			"bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400",
		added: "bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400",
		removed: "bg-gray-100 text-gray-700 dark:bg-gray-900/30 dark:text-gray-400",
	};
</script>

{#if info.changeType}
	<Badge
		class={cn("gap-1 text-xs capitalize", styles[info.changeType], className)}
		variant="secondary"
		{...restProps}
	>
		{#if info.changeType === "added"}
			<PlusIcon class="size-3" />
		{:else if info.changeType === "removed"}
			<MinusIcon class="size-3" />
		{:else}
			<ArrowRightIcon class="size-3" />
		{/if}
		{#if children}
			{@render children()}
		{:else}
			{info.changeType}
		{/if}
	</Badge>
{/if}
