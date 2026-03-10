<script lang="ts">
	import { cn } from "$lib/utils";
	import { setMessageBranchContext } from "./context";

	type Props = {
		branch?: number;
		totalBranches?: number;
		onBranchChange?: (branchIndex: number) => void;
		class?: string;
		children?: () => any;
	};

	let {
		branch = $bindable(0),
		totalBranches = 1,
		onBranchChange,
		class: className,
		children,
		...restProps
	}: Props = $props();

	const messageBranch = $state({
		currentBranch: 0,
		totalBranches: 1,
		goToPrevious: () => {
			const previous =
				messageBranch.currentBranch > 0
					? messageBranch.currentBranch - 1
					: messageBranch.totalBranches - 1;
			branch = previous;
			onBranchChange?.(previous);
		},
		goToNext: () => {
			const next =
				messageBranch.currentBranch < messageBranch.totalBranches - 1
					? messageBranch.currentBranch + 1
					: 0;
			branch = next;
			onBranchChange?.(next);
		},
	});

	$effect(() => {
		messageBranch.currentBranch = branch;
		messageBranch.totalBranches = Math.max(totalBranches, 1);
	});

	setMessageBranchContext(messageBranch);
</script>

<div class={cn("grid w-full gap-2 [&>div]:pb-0", className)} {...restProps}>
	{@render children?.()}
</div>
