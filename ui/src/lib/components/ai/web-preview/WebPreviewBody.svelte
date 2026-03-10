<script lang="ts">
	import { cn } from "$lib/utils";
	import { type WebPreviewViewport, useWebPreviewContext } from "./context";

	type Props = {
		src?: string;
		class?: string;
		loading?: () => any;
	};

	let { src, class: className, loading, ...restProps }: Props = $props();
	const webPreview = useWebPreviewContext();

	const viewportWidths: Record<WebPreviewViewport, string | null> = {
		desktop: null,
		tablet: "768px",
		mobile: "390px",
	};

	const constrainedWidth = $derived.by(
		() => viewportWidths[webPreview.viewport],
	);
</script>

<div
	class={cn(
		"flex-1 overflow-auto",
		constrainedWidth ? "flex justify-center bg-muted/30" : "",
	)}
>
	<div
		class="h-full shrink-0"
		style={constrainedWidth
			? `width: ${constrainedWidth};`
			: "width: 100%;"}
	>
		<iframe
			class={cn("size-full", className)}
			sandbox="allow-scripts allow-same-origin allow-forms allow-popups allow-presentation"
			src={(src ?? webPreview.url) || undefined}
			title="Preview"
			{...restProps}
		></iframe>
		{@render loading?.()}
	</div>
</div>
