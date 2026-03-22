<script lang="ts">
	type Props = {
		estimatedHeight?: number;
		root?: HTMLElement | null;
		rootMargin?: string;
		children?: () => any;
	};

	let {
		estimatedHeight = 240,
		root = null,
		rootMargin = "400px 0px",
		children,
	}: Props = $props();

	let target = $state<HTMLDivElement | null>(null);
	let hasMounted = $state(false);

	$effect(() => {
		const element = target;
		if (!element || !root || hasMounted) {
			return;
		}
		if (typeof IntersectionObserver === "undefined") {
			hasMounted = true;
			return;
		}

		const observer = new IntersectionObserver(
			(entries) => {
				if (entries.some((entry) => entry.isIntersecting)) {
					hasMounted = true;
					observer.disconnect();
				}
			},
			{
				root,
				rootMargin,
			},
		);

		observer.observe(element);

		return () => {
			observer.disconnect();
		};
	});
</script>

<div bind:this={target}>
	{#if hasMounted}
		{@render children?.()}
	{:else}
		<div aria-hidden="true" style={`height: ${estimatedHeight}px;`}></div>
	{/if}
</div>
