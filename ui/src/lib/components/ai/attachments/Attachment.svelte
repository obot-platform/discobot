<script lang="ts">
	import type {
		AttachmentData,
		AttachmentMediaCategory,
		AttachmentVariant,
	} from "$lib/components/ai/types";
	import { cn } from "$lib/utils";
	import { useAttachmentsContext, setAttachmentContext } from "./context";
	import { getMediaCategory } from "./utils";

	type Props = {
		data: AttachmentData;
		onRemove?: () => void;
		class?: string;
		children?: () => any;
	};

	let { data, onRemove, class: className, children, ...restProps }: Props =
		$props();

	const attachments = useAttachmentsContext();
	const mediaCategory = $derived.by(() => getMediaCategory(data));

	const attachment = $state({
		data: { id: "", type: "file" } as AttachmentData,
		mediaCategory: "unknown" as AttachmentMediaCategory,
		onRemove: undefined as (() => void) | undefined,
		variant: "grid" as AttachmentVariant,
	});

	$effect(() => {
		attachment.data = data;
		attachment.mediaCategory = mediaCategory;
		attachment.onRemove = onRemove;
		attachment.variant = attachments.variant;
	});

	setAttachmentContext(attachment);
</script>

<div
	class={cn(
		"group relative",
		attachment.variant === "grid" ? "size-24 overflow-hidden rounded-lg" : "",
		attachment.variant === "inline"
			? [
					"flex h-8 cursor-pointer select-none items-center gap-1.5",
					"rounded-md border border-border px-1.5",
					"font-medium text-sm transition-all",
					"hover:bg-accent hover:text-accent-foreground dark:hover:bg-accent/50",
				]
			: "",
		attachment.variant === "list"
			? [
					"flex w-full items-center gap-3 rounded-lg border p-3",
					"hover:bg-accent/50",
				]
			: "",
		className,
	)}
	{...restProps}
>
	{@render children?.()}
</div>
