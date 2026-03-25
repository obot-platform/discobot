<script lang="ts">
	import FileTextIcon from "@lucide/svelte/icons/file-text";
	import GlobeIcon from "@lucide/svelte/icons/globe";
	import ImageIcon from "@lucide/svelte/icons/image";
	import Music2Icon from "@lucide/svelte/icons/music-2";
	import PaperclipIcon from "@lucide/svelte/icons/paperclip";
	import VideoIcon from "@lucide/svelte/icons/video";
	import type { Component } from "svelte";
	import * as Dialog from "$lib/components/ui/dialog";
	import { cn } from "$lib/utils";
	import { useAttachmentContext } from "./context";
	import { canOpenAttachmentFullscreen, getAttachmentLabel } from "./utils";

	type IconComponent = Component<{ class?: string }>;

	type Props = {
		fallbackIcon?: IconComponent;
		class?: string;
	};

	let { fallbackIcon, class: className, ...restProps }: Props = $props();
	const attachment = useAttachmentContext();

	const iconSize = $derived.by(() =>
		attachment.variant === "inline" ? "size-3" : "size-4",
	);
	const attachmentLabel = $derived.by(() =>
		getAttachmentLabel(attachment.data),
	);
	const canPreviewFullscreen = $derived.by(() =>
		canOpenAttachmentFullscreen(attachment.data),
	);

	const Icon = $derived.by(() => {
		switch (attachment.mediaCategory) {
			case "image":
				return ImageIcon;
			case "video":
				return VideoIcon;
			case "audio":
				return Music2Icon;
			case "source":
				return GlobeIcon;
			case "document":
				return FileTextIcon;
			default:
				return PaperclipIcon;
		}
	});

	const FallbackIcon = $derived.by(() => fallbackIcon);
</script>

<div
	class={cn(
		"flex shrink-0 items-center justify-center overflow-hidden",
		attachment.variant === "grid" ? "size-full bg-muted" : "",
		attachment.variant === "inline" ? "size-5 rounded bg-background" : "",
		attachment.variant === "list" ? "size-12 rounded bg-muted" : "",
		className,
	)}
	{...restProps}
>
	{#if canPreviewFullscreen && attachment.data.type === "file" && attachment.data.url}
		<Dialog.Root>
			<Dialog.Trigger
				class="focus-visible:ring-ring flex size-full cursor-zoom-in items-center justify-center overflow-hidden rounded-[inherit] focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-hidden"
				aria-label={`Open ${attachmentLabel} full screen`}
			>
				{#if attachment.variant === "grid"}
					<img
						src={attachment.data.url}
						alt={attachmentLabel}
						width="96"
						height="96"
						class="size-full object-cover"
					/>
				{:else}
					<img
						src={attachment.data.url}
						alt={attachmentLabel}
						width="20"
						height="20"
						class="size-full rounded object-cover"
					/>
				{/if}
			</Dialog.Trigger>
			<Dialog.Content
				class="flex max-h-[calc(100vh-2rem)] max-w-[calc(100vw-2rem)] items-center justify-center border-none bg-transparent p-0 shadow-none sm:max-w-[calc(100vw-2rem)]"
				showCloseButton={false}
			>
				<Dialog.Title class="sr-only">{attachmentLabel}</Dialog.Title>
				<img
					src={attachment.data.url}
					alt={attachmentLabel}
					class="max-h-[calc(100vh-2rem)] max-w-[calc(100vw-2rem)] rounded-lg object-contain"
				/>
			</Dialog.Content>
		</Dialog.Root>
	{:else if attachment.mediaCategory === "image" && attachment.data.type === "file" && attachment.data.url}
		{#if attachment.variant === "grid"}
			<img
				src={attachment.data.url}
				alt={attachmentLabel}
				width="96"
				height="96"
				class="size-full object-cover"
			/>
		{:else}
			<img
				src={attachment.data.url}
				alt={attachmentLabel}
				width="20"
				height="20"
				class="size-full rounded object-cover"
			/>
		{/if}
	{:else if attachment.mediaCategory === "video" && attachment.data.type === "file" && attachment.data.url}
		<video class="size-full object-cover" muted src={attachment.data.url}
		></video>
	{:else if FallbackIcon}
		<FallbackIcon class={cn(iconSize, "text-muted-foreground")} />
	{:else}
		<Icon class={cn(iconSize, "text-muted-foreground")} />
	{/if}
</div>
