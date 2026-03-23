<script lang="ts">
	import DownloadIcon from "@lucide/svelte/icons/download";
	import Maximize2Icon from "@lucide/svelte/icons/maximize-2";
	import XIcon from "@lucide/svelte/icons/x";
	import ZoomInIcon from "@lucide/svelte/icons/zoom-in";
	import ZoomOutIcon from "@lucide/svelte/icons/zoom-out";
	import { Button } from "$lib/components/ui/button";
	import { downloadFile } from "$lib/tauri";
	import { cn } from "$lib/utils";

	type Props = {
		src: string;
		filename?: string;
		class?: string;
	};

	let { src, filename = "image", class: className }: Props = $props();

	let isOpen = $state(false);
	let zoom = $state(1);
	let naturalWidth = $state(0);
	let naturalHeight = $state(0);
	let innerWidth = $state(0);
	let innerHeight = $state(0);

	function open() {
		isOpen = true;
	}

	function close() {
		isOpen = false;
		zoom = 1;
	}

	function decodeBase64(content: string): Uint8Array {
		const decoded = globalThis.atob(content);
		const bytes = new Uint8Array(decoded.length);
		for (let index = 0; index < decoded.length; index += 1) {
			bytes[index] = decoded.charCodeAt(index);
		}
		return bytes;
	}

	async function handleDownload() {
		const [metadata, content] = src.split(",", 2);
		if (!metadata || !content) {
			return;
		}

		const mimeType = metadata.match(/^data:([^;]+);base64$/)?.[1] ?? "application/octet-stream";
		await downloadFile({
			filename,
			content: decodeBase64(content),
			mimeType,
		});
	}

	function zoomIn() {
		zoom = Math.min(zoom + 0.25, 4);
	}

	function zoomOut() {
		zoom = Math.max(zoom - 0.25, 0.25);
	}

	function resetZoom() {
		zoom = 1;
	}

	function handleWheel(event: WheelEvent) {
		event.preventDefault();
		const delta = event.deltaY > 0 ? -0.1 : 0.1;
		zoom = Math.min(Math.max(zoom + delta, 0.25), 4);
	}

	function handleImageLoad(target: HTMLImageElement) {
		naturalWidth = target.naturalWidth;
		naturalHeight = target.naturalHeight;
	}

	function handleWindowKeydown(event: KeyboardEvent) {
		if (isOpen && event.key === "Escape") {
			close();
		}
	}

	const displaySize = $derived.by(() => {
		if (!naturalWidth || !naturalHeight || !innerWidth || !innerHeight) {
			return {
				width: "auto",
				height: "auto",
				minHeight: "100vh",
				minWidth: "100vw",
			};
		}

		const maxWidth = innerWidth * 0.85;
		const maxHeight = innerHeight * 0.8;

		const widthRatio = maxWidth / naturalWidth;
		const heightRatio = maxHeight / naturalHeight;
		const baseRatio = Math.min(widthRatio, heightRatio, 1);

		const baseWidth = naturalWidth * baseRatio;
		const baseHeight = naturalHeight * baseRatio;

		const width = baseWidth * zoom;
		const height = baseHeight * zoom;

		return {
			width: `${width}px`,
			height: `${height}px`,
			minHeight: `max(100vh, calc(${height}px + 8rem))`,
			minWidth: `max(100vw, calc(${width}px + 8rem))`,
		};
	});

	$effect(() => {
		if (typeof document === "undefined" || !isOpen) {
			return;
		}

		const previousOverflow = document.body.style.overflow;
		document.body.style.overflow = "hidden";

		return () => {
			document.body.style.overflow = previousOverflow;
		};
	});
</script>

<svelte:window bind:innerWidth bind:innerHeight onkeydown={handleWindowKeydown} />

<button
	class={cn(
		"group relative max-w-xs cursor-pointer overflow-hidden rounded-lg border border-border transition-all hover:border-primary/50 hover:shadow-md",
		className,
	)}
	onclick={open}
	type="button"
>
	<img alt={filename} class="h-auto max-w-full" src={src} title={filename} />
	<div class="absolute inset-0 flex items-center justify-center bg-black/0 transition-colors group-hover:bg-black/20">
		<Maximize2Icon class="size-6 opacity-0 transition-opacity group-hover:opacity-100 text-white drop-shadow-lg" />
	</div>
</button>

{#if isOpen}
	<div
		aria-label={`Image: ${filename}`}
		aria-modal="true"
		class="fixed inset-0 z-[100] overflow-auto"
		onwheel={handleWheel}
		role="dialog"
	>
		<button
			aria-label="Close image preview"
			class="fixed inset-0 m-0 border-0 bg-black/90 p-0"
			onclick={close}
			type="button"
		></button>

		<div class="fixed right-4 top-4 z-[102] flex items-center gap-2">
			<Button
				class="size-9 border-0 bg-white/10 text-white backdrop-blur-sm hover:bg-white/20"
				disabled={zoom <= 0.25}
				onclick={zoomOut}
				size="icon"
				title="Zoom out"
				variant="secondary"
			>
				<ZoomOutIcon class="size-4" />
			</Button>
			<Button
				class="size-9 min-w-[4rem] border-0 bg-white/10 text-white font-mono text-xs backdrop-blur-sm hover:bg-white/20"
				onclick={resetZoom}
				size="icon"
				title="Reset zoom"
				variant="secondary"
			>
				{Math.round(zoom * 100)}%
			</Button>
			<Button
				class="size-9 border-0 bg-white/10 text-white backdrop-blur-sm hover:bg-white/20"
				disabled={zoom >= 4}
				onclick={zoomIn}
				size="icon"
				title="Zoom in"
				variant="secondary"
			>
				<ZoomInIcon class="size-4" />
			</Button>
			<Button
				class="size-9 border-0 bg-white/10 text-white backdrop-blur-sm hover:bg-white/20"
				onclick={handleDownload}
				size="icon"
				title="Download"
				variant="secondary"
			>
				<DownloadIcon class="size-4" />
			</Button>
			<Button
				class="size-9 border-0 bg-white/10 text-white backdrop-blur-sm hover:bg-white/20"
				onclick={close}
				size="icon"
				title="Close (Esc)"
				variant="secondary"
			>
				<XIcon class="size-4" />
			</Button>
		</div>

		<div class="fixed bottom-4 left-4 z-[102] flex items-center gap-3">
			<div class="rounded-md bg-white/10 px-3 py-1.5 text-sm text-white/70 backdrop-blur-sm">
				{filename}
			</div>
			<div class="rounded-md bg-white/10 px-3 py-1.5 text-white/50 text-xs backdrop-blur-sm">
				Scroll to zoom
			</div>
		</div>

		<div
			class="relative z-[101] flex min-h-full min-w-full items-center justify-center p-16"
			style={`min-height: ${displaySize.minHeight}; min-width: ${displaySize.minWidth};`}
		>
			<img
				alt={filename}
				class="object-contain"
				draggable="false"
				onload={(event) =>
					handleImageLoad(event.currentTarget as EventTarget & HTMLImageElement)}
				src={src}
				style={`width: ${displaySize.width}; height: ${displaySize.height};`}
				title={filename}
			/>
		</div>
	</div>
{/if}
