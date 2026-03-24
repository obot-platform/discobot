<script lang="ts">
	import type { AttachmentVariant } from "$lib/components/ai/types";
	import { cn } from "$lib/utils";
	import { setAttachmentsContext } from "./context";

	type Props = {
		variant?: AttachmentVariant;
		class?: string;
		children?: () => any;
	};

	let {
		variant = "grid",
		class: className,
		children,
		...restProps
	}: Props = $props();

	const attachments = $state({ variant: "grid" as AttachmentVariant });
	$effect(() => {
		attachments.variant = variant;
	});
	setAttachmentsContext(attachments);
</script>

<div
	class={cn(
		"flex items-start",
		variant === "list" ? "flex-col gap-2" : "flex-wrap gap-2",
		variant === "grid" ? "ml-auto w-fit" : "",
		className,
	)}
	{...restProps}
>
	{@render children?.()}
</div>
