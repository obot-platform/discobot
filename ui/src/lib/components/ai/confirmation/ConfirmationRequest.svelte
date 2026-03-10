<script lang="ts">
	import { useConfirmationContext } from "./context";

	type Props = { children?: () => any };
	let { children }: Props = $props();
	const confirmation = useConfirmationContext();
</script>

{#if confirmation.state === "approval-requested"}
	{@render children?.()}
{/if}
