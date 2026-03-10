<script lang="ts">
	import type { SVGAttributes } from "svelte/elements";
	import { cn } from "$lib/utils";
	import { getBezierPath, type HandlePosition } from "./path";

	type Props = SVGAttributes<SVGGElement> & {
		id?: string;
		sourceX: number;
		sourceY: number;
		targetX: number;
		targetY: number;
		sourcePosition?: HandlePosition;
		targetPosition?: HandlePosition;
		markerEnd?: string;
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
		markerEnd,
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

<g class={cn(className)} data-slot="edge-animated" {...restProps}>
	<path
		{id}
		d={edgePath}
		fill="none"
		marker-end={markerEnd}
		stroke="currentColor"
		stroke-width="1"
	/>
	<circle fill="var(--color-primary)" r="4">
		<animateMotion dur="2s" path={edgePath} repeatCount="indefinite" />
	</circle>
</g>
