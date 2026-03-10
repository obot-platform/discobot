<script lang="ts">
	import GitCommitIcon from "@lucide/svelte/icons/git-commit";
	import { cn } from "$lib/utils";

	type Props = {
		class?: string;
		children?: () => any;
	};

	let { class: className, children, ...restProps }: Props = $props();
</script>

<span class={cn("font-mono text-xs", className)} {...restProps}>
	<GitCommitIcon class="mr-1 inline-block size-3" />
	{@render children?.()}
</span>
