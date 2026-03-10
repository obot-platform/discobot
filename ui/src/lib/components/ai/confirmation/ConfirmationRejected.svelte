<script lang="ts">
	import { useConfirmationContext } from "./context";

	type Props = { children?: () => any };
	let { children }: Props = $props();
	const confirmation = useConfirmationContext();

	const shouldRender = $derived.by(
		() =>
			confirmation.approval?.approved === false &&
			(confirmation.state === "approval-responded" ||
				confirmation.state === "output-denied" ||
				confirmation.state === "output-available"),
	);
</script>

{#if shouldRender}
	{@render children?.()}
{/if}
