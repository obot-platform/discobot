<script lang="ts">
	import { cn } from "$lib/utils";
	import { useMessageBranchContext } from "./context";

	type Props = {
		branch: number;
		class?: string;
		children?: () => any;
	};

	let { branch, class: className, children, ...restProps }: Props = $props();
	const messageBranch = useMessageBranchContext();
</script>

{#if branch === messageBranch.currentBranch}
	<div class={cn("grid gap-2 overflow-hidden [&>div]:pb-0", className)} {...restProps}>
		{@render children?.()}
	</div>
{/if}
