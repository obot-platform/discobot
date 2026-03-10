<script lang="ts">
	type Props = {
		children?: () => any;
	};

	let { children, ...restProps }: Props = $props();
</script>

<span {...restProps}>
	{#if children}
		{@render children()}
	{:else}
		•
	{/if}
</span>
