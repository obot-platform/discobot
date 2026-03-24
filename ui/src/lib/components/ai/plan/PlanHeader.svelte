<script lang="ts">
	import { CardHeader } from "$lib/components/ui/card";
	import { cn } from "$lib/utils";

	type Props = {
		class?: string;
		children?: () => any;
	};

	let { class: className, children, ...restProps }: Props = $props();
</script>

<CardHeader
	class={cn("flex items-start justify-between", className)}
	data-slot="plan-header"
	{...restProps}
>
	{@render children?.()}
</CardHeader>
