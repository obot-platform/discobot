<script lang="ts">
	import { cn } from "$lib/utils";

	type Props = {
		class?: string;
		src?: string;
		alt?: string;
	};

	let { class: className, alt = "", ...restProps }: Props = $props();
</script>

<img
	alt={alt}
	class={cn("h-8 w-8 rounded border object-cover", className)}
	height={32}
	width={32}
	{...restProps}
/>
