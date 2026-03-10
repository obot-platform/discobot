<script lang="ts">
	import OpenInProviderItem from "./OpenInProviderItem.svelte";
	let { ...restProps } = $props();
</script>

<OpenInProviderItem provider="v0" {...restProps} />
