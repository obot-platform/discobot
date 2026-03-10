<script lang="ts">
	import { Avatar, AvatarFallback } from "$lib/components/ui/avatar";
	import { cn } from "$lib/utils";

	type Props = {
		initials: string;
		class?: string;
	};

	let { initials, class: className, ...restProps }: Props = $props();
</script>

<Avatar class={cn("size-8", className)} {...restProps}>
	<AvatarFallback class="text-xs">{initials}</AvatarFallback>
</Avatar>
