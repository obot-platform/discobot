<script lang="ts">
	import { Alert } from "$lib/components/ui/alert";
	import { cn } from "$lib/utils";
	import type { ToolApproval, ToolState } from "$lib/components/ai/types";
	import { setConfirmationContext } from "./context";

	type Props = {
		approval?: ToolApproval;
		state: ToolState;
		class?: string;
		children?: () => any;
	};

	let {
		class: className,
		approval,
		state: toolState,
		children,
		...restProps
	}: Props = $props();

	const shouldRender = $derived.by(
		() =>
			!!approval &&
			toolState !== "input-streaming" &&
			toolState !== "input-available",
	);

	const confirmation = $state({
		approval: undefined as ToolApproval,
		state: "input-streaming" as ToolState,
	});
	$effect(() => {
		confirmation.approval = approval;
		confirmation.state = toolState;
	});
	setConfirmationContext(confirmation);
</script>

{#if shouldRender}
	<Alert class={cn("flex flex-col gap-2", className)} {...restProps}>
		{@render children?.()}
	</Alert>
{/if}
