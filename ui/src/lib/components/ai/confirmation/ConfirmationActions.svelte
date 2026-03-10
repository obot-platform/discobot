<script lang="ts">
	import { cn } from "$lib/utils";
	import { useConfirmationContext } from "./context";

	type Props = {
		class?: string;
		children?: () => any;
	};

	let { class: className, children, ...restProps }: Props = $props();
	const confirmation = useConfirmationContext();
</script>

{#if confirmation.state === "approval-requested"}
	<div
		class={cn("flex items-center justify-end gap-2 self-end", className)}
		{...restProps}
	>
		{@render children?.()}
	</div>
{/if}
