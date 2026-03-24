<script lang="ts">
	import { Button } from "$lib/components/ui/button";
	import {
		Tooltip,
		TooltipContent,
		TooltipProvider,
		TooltipTrigger,
	} from "$lib/components/ui/tooltip";

	type Props = {
		tooltip?: string;
		label?: string;
		variant?:
			| "default"
			| "destructive"
			| "outline"
			| "secondary"
			| "ghost"
			| "link";
		size?:
			| "default"
			| "xs"
			| "sm"
			| "lg"
			| "icon"
			| "icon-xs"
			| "icon-sm"
			| "icon-lg";
		children?: () => any;
		[key: string]: unknown;
	};

	let {
		tooltip,
		label,
		variant = "ghost",
		size = "icon-sm",
		children,
		...restProps
	}: Props = $props();
</script>

{#if tooltip}
	<TooltipProvider>
		<Tooltip>
			<TooltipTrigger>
				<Button {size} type="button" {variant} {...restProps}>
					{@render children?.()}
					<span class="sr-only">{label || tooltip}</span>
				</Button>
			</TooltipTrigger>
			<TooltipContent>
				<p>{tooltip}</p>
			</TooltipContent>
		</Tooltip>
	</TooltipProvider>
{:else}
	<Button {size} type="button" {variant} {...restProps}>
		{@render children?.()}
		{#if label}
			<span class="sr-only">{label}</span>
		{/if}
	</Button>
{/if}
