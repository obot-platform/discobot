<script lang="ts">
	import type { SVGAttributes } from "svelte/elements";
	import { cn } from "$lib/utils";
	import { getBezierPath, type HandlePosition } from "./path";

	type Props = SVGAttributes<SVGPathElement> & {
		id?: string;
		sourceX: number;
		sourceY: number;
		targetX: number;
		targetY: number;
		sourcePosition?: HandlePosition;
		targetPosition?: HandlePosition;
		class?: string;
	};

	let {
		id,
		sourceX,
		sourceY,
		targetX,
		targetY,
		sourcePosition = "right",
		targetPosition = "left",
		class: className,
		...restProps
	}: Props = $props();

	const edgePath = $derived.by(() =>
		getBezierPath({
			sourceX,
			sourceY,
			targetX,
			targetY,
			sourcePosition,
			targetPosition,
		}),
	);
</script>

<path
	{id}
	class={cn("stroke-ring stroke-1 fill-none", className)}
	d={edgePath}
	stroke-dasharray="5, 5"
	{...restProps}
/>
