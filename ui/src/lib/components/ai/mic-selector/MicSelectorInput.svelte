<script lang="ts">
	import { CommandInput } from "$lib/components/ui/command";

	let { ...restProps } = $props();
</script>

<CommandInput placeholder="Search microphones..." {...restProps} />
