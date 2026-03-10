<script lang="ts">
	import { cn } from "$lib/utils";

	type Props = {
		class?: string;
		children?: () => any;
	};

	let { class: className, children, ...restProps }: Props = $props();

	function stopPropagation(event: Event) {
		event.stopPropagation();
	}
</script>

<div
	class={cn("ml-auto flex items-center gap-1", className)}
	role="group"
	onclick={stopPropagation}
	onkeydown={stopPropagation}
	{...restProps}
>
	{@render children?.()}
</div>
