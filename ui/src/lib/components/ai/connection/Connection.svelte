<script lang="ts">
	const HALF = 0.5;

	type Props = {
		fromX: number;
		fromY: number;
		toX: number;
		toY: number;
	};

	let { fromX, fromY, toX, toY, ...restProps }: Props = $props();

	const path = $derived.by(
		() =>
			`M${fromX},${fromY} C ${fromX + (toX - fromX) * HALF},${fromY} ${fromX + (toX - fromX) * HALF},${toY} ${toX},${toY}`,
	);
</script>

<g {...restProps}>
	<path class="animated" d={path} fill="none" stroke="var(--color-ring)" stroke-width={1}></path>
	<circle cx={toX} cy={toY} fill="#fff" r={3} stroke="var(--color-ring)" stroke-width={1}></circle>
</g>
