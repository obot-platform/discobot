<script lang="ts">
	import { Card } from "$lib/components/ui/card";
	import { Collapsible } from "$lib/components/ui/collapsible";
	import { cn } from "$lib/utils";
	import { setPlanContext } from "./context";

	type Props = {
		isStreaming?: boolean;
		open?: boolean;
		class?: string;
		children?: () => any;
	};

	let {
		isStreaming = false,
		open = $bindable(false),
		class: className,
		children,
		...restProps
	}: Props = $props();

	const planContext = $state({ isStreaming: false });
	$effect(() => {
		planContext.isStreaming = isStreaming;
	});
	setPlanContext(planContext);
</script>

<Collapsible bind:open {...restProps}>
	<Card class={cn("shadow-none", className)} data-slot="plan">
		{@render children?.()}
	</Card>
</Collapsible>
