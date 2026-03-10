<script lang="ts">
	import { DropdownMenu } from "$lib/components/ui/dropdown-menu";
	import { setOpenInChatContext } from "./context";

	type Props = {
		query: string;
		children?: () => any;
	};

	let { query, children, ...restProps }: Props = $props();
	const openIn = $state({ query: "" });
	$effect(() => {
		openIn.query = query;
	});
	setOpenInChatContext(openIn);
</script>

<DropdownMenu {...restProps}>
	{@render children?.()}
</DropdownMenu>
