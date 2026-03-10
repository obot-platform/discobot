<script lang="ts">
	import { cn } from "$lib/utils";

	type Props = {
		class?: string;
		children?: () => any;
	};

	let { class: className, children, ...restProps }: Props = $props();

	function handleKeyDown(event: KeyboardEvent) {
		if (event.key === "Enter" || event.key === " ") {
			event.stopPropagation();
		}
	}
</script>

<div
	class={cn("flex shrink-0 items-center gap-1", className)}
	role="group"
	onclick={(event) => event.stopPropagation()}
	onkeydown={handleKeyDown}
	{...restProps}
>
	{@render children?.()}
</div>
