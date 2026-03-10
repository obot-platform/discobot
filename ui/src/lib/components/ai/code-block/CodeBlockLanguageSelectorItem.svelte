<script lang="ts">
	import type { ComponentProps } from "svelte";
	import { SelectItem } from "$lib/components/ui/select";

	type Props = ComponentProps<typeof SelectItem>;

	let { ...restProps }: Props = $props();
</script>

<SelectItem {...restProps} />
