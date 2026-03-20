<script lang="ts">
	import { cn } from "$lib/utils";

	type Props = {
		class?: string;
		size?: number;
	};

	let { class: className, size = 24 }: Props = $props();
</script>

<svg
	viewBox="0 0 48 48"
	fill="none"
	xmlns="http://www.w3.org/2000/svg"
	class={cn("text-foreground", className)}
	width={size}
	height={size}
	role="img"
	aria-label="Discobot logo"
>
	<title>Discobot logo</title>

	<circle
		cx="24"
		cy="26"
		r="18"
		stroke="currentColor"
		stroke-width="1.5"
		fill="none"
		opacity="0.6"
	/>

	<path d="M6 26 H42" stroke="currentColor" stroke-width="1" opacity="0.5" />
	<path d="M8 18 H40" stroke="currentColor" stroke-width="1" opacity="0.5" />
	<path d="M8 34 H40" stroke="currentColor" stroke-width="1" opacity="0.5" />

	<path
		d="M14 8.5 Q14 26 14 43.5"
		stroke="currentColor"
		stroke-width="1"
		opacity="0.5"
		fill="none"
	/>
	<path d="M24 8 V44" stroke="currentColor" stroke-width="1" opacity="0.5" />
	<path
		d="M34 8.5 Q34 26 34 43.5"
		stroke="currentColor"
		stroke-width="1"
		opacity="0.5"
		fill="none"
	/>

	<circle cx="30" cy="18" r="2" fill="currentColor" opacity="0.8" />
	<circle cx="16" cy="30" r="1.5" fill="currentColor" opacity="0.7" />
	<circle cx="34" cy="32" r="1.2" fill="currentColor" opacity="0.6" />

	<path
		d="M40 12 L41 14 L43 15 L41 16 L40 18 L39 16 L37 15 L39 14 Z"
		fill="currentColor"
		opacity="0.7"
	/>
	<path
		d="M4 26 L5 27.5 L6.5 28.5 L5 29.5 L4 31 L3 29.5 L1.5 28.5 L3 27.5 Z"
		fill="currentColor"
		opacity="0.6"
	/>
	<path
		d="M42 38 L42.8 39.2 L44 40 L42.8 40.8 L42 42 L41.2 40.8 L40 40 L41.2 39.2 Z"
		fill="currentColor"
		opacity="0.65"
	/>
	<path
		d="M8 10 L8.6 11 L9.5 11.5 L8.6 12 L8 13 L7.4 12 L6.5 11.5 L7.4 11 Z"
		fill="currentColor"
		opacity="0.5"
	/>

	<line
		x1="24"
		y1="8"
		x2="24"
		y2="4"
		stroke="currentColor"
		stroke-width="2"
		stroke-linecap="round"
	/>
	<rect x="22" y="2" width="4" height="3" rx="1" fill="currentColor" />

	<rect
		x="17"
		y="24"
		width="14"
		height="4"
		rx="2"
		fill="currentColor"
		opacity="0.3"
	/>
	<circle cx="20" cy="26" r="1.5" fill="currentColor" />
	<circle cx="28" cy="26" r="1.5" fill="currentColor" />
</svg>
