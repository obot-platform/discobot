<script lang="ts">
	import { CommandInput } from "$lib/components/ui/command";
	import { cn } from "$lib/utils";

	let { class: className = undefined, ...restProps } = $props();
</script>

<CommandInput class={cn("h-auto py-3.5", className)} {...restProps} />
