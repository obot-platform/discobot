<script lang="ts">
	import { cn } from "$lib/utils";
	import { useAttachmentContext } from "./context";
	import { getAttachmentLabel } from "./utils";

	type Props = {
		showMediaType?: boolean;
		class?: string;
	};

	let {
		showMediaType = false,
		class: className,
		...restProps
	}: Props = $props();
	const attachment = useAttachmentContext();
	const label = $derived.by(() => getAttachmentLabel(attachment.data));
</script>

{#if attachment.variant !== "grid"}
	<div class={cn("min-w-0 flex-1", className)} {...restProps}>
		<span class="block truncate">{label}</span>
		{#if showMediaType && attachment.data.mediaType}
			<span class="block truncate text-muted-foreground text-xs"
				>{attachment.data.mediaType}</span
			>
		{/if}
	</div>
{/if}
