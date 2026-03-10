<script lang="ts">
	import { cn } from "$lib/utils";

	type Props = {
		class?: string;
		children?: () => any;
	};

	let { class: className, children, ...restProps }: Props = $props();

	function handleKeyDown(event: KeyboardEvent) {
		event.stopPropagation();
	}
</script>

<div
	class={cn("flex items-center gap-1", className)}
	role="group"
	onclick={(event) => event.stopPropagation()}
	onkeydown={handleKeyDown}
	{...restProps}
>
	{@render children?.()}
</div>
