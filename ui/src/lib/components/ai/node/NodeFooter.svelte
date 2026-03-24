<script lang="ts">
	import { CardFooter } from "$lib/components/ui/card";
	import { cn } from "$lib/utils";

	type Props = {
		class?: string;
		children?: () => any;
	};

	let { class: className, children, ...restProps }: Props = $props();
</script>

<CardFooter
	class={cn("rounded-b-md border-t bg-secondary p-3!", className)}
	{...restProps}
>
	{@render children?.()}
</CardFooter>
