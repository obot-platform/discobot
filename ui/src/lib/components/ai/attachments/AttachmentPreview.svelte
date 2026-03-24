<script lang="ts">
	import FileTextIcon from "@lucide/svelte/icons/file-text";
	import GlobeIcon from "@lucide/svelte/icons/globe";
	import ImageIcon from "@lucide/svelte/icons/image";
	import Music2Icon from "@lucide/svelte/icons/music-2";
	import PaperclipIcon from "@lucide/svelte/icons/paperclip";
	import VideoIcon from "@lucide/svelte/icons/video";
	import type { Component } from "svelte";
	import { cn } from "$lib/utils";
	import { useAttachmentContext } from "./context";

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
	{#if attachment.mediaCategory === "image" && attachment.data.type === "file" && attachment.data.url}
		{#if attachment.variant === "grid"}
			<img
				src={attachment.data.url}
				alt={attachment.data.filename || "Image"}
				width="96"
				height="96"
				class="size-full object-cover"
			/>
		{:else}
			<img
				src={attachment.data.url}
				alt={attachment.data.filename || "Image"}
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
