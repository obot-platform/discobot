<script lang="ts">
	import type { HTMLAttributes } from "svelte/elements";
	import { ButtonGroup } from "$lib/components/ui/button-group";
	import { cn } from "$lib/utils";

	type Props = HTMLAttributes<HTMLDivElement> & {
		class?: string;
		children?: () => any;
	};

	let { class: className, children, ...restProps }: Props = $props();
</script>

<div class={cn("flex items-center", className)} data-slot="audio-player-control-bar" {...restProps}>
	<ButtonGroup orientation="horizontal">
		{@render children?.()}
	</ButtonGroup>
</div>
