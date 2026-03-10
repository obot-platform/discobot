<script lang="ts">
	import OpenInProviderItem from "./OpenInProviderItem.svelte";
	let { ...restProps } = $props();
</script>

<OpenInProviderItem provider="scira" {...restProps} />
