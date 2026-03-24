<script lang="ts">
	import { HoverCard } from "$lib/components/ui/hover-card";
	import type { LanguageModelUsage } from "$lib/components/ai/types";
	import { setContextUsageContext } from "./context";

	type Props = {
		usedTokens: number;
		maxTokens: number;
		usage?: LanguageModelUsage;
		modelId?: string;
		children?: () => any;
	};

	let { usedTokens, maxTokens, usage, modelId, children, ...restProps }: Props =
		$props();
	const usageContext = $state({
		usedTokens: 0,
		maxTokens: 1,
		usage: undefined as LanguageModelUsage | undefined,
		modelId: undefined as string | undefined,
	});
	$effect(() => {
		usageContext.usedTokens = usedTokens;
		usageContext.maxTokens = maxTokens;
		usageContext.usage = usage;
		usageContext.modelId = modelId;
	});
	setContextUsageContext(usageContext);
</script>

<HoverCard closeDelay={0} openDelay={0} {...restProps}>
	{@render children?.()}
</HoverCard>
