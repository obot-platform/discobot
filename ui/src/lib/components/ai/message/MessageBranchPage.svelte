<script lang="ts">
	import { ButtonGroupText } from "$lib/components/ui/button-group";
	import { cn } from "$lib/utils";
	import { useMessageBranchContext } from "./context";

	type Props = {
		class?: string;
	};

	let { class: className, ...restProps }: Props = $props();
	const messageBranch = useMessageBranchContext();
</script>

<ButtonGroupText
	class={cn(
		"border-none bg-transparent text-muted-foreground shadow-none",
		className,
	)}
	{...restProps}
>
	{messageBranch.currentBranch + 1} of {messageBranch.totalBranches}
</ButtonGroupText>
