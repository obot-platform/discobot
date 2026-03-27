<script lang="ts">
	import {
		FileDiff as PierreFileDiff,
		VirtualizedFileDiff,
		Virtualizer,
		type FileDiff as PierreFileDiffInstance,
	} from "@pierre/diffs";
	import { onMount } from "svelte";

	import {
		getDiffRendererOptions,
		getDiffWorkerPool,
		type DiffRendererParams,
	} from "$lib/pierre-diff";

	const VIRTUALIZER_CONFIG = {
		overscrollSize: 1000,
		intersectionObserverMargin: 4000,
		resizeDebugging: false,
	};

	type Props = {
		params: DiffRendererParams;
	};

	let { params }: Props = $props();

	let standardHost = $state<HTMLDivElement | null>(null);
	let virtualScrollRoot = $state<HTMLDivElement | null>(null);
	let virtualScrollContent = $state<HTMLDivElement | null>(null);

	let instance = $state<PierreFileDiffInstance | null>(null);
	let virtualizer = $state<Virtualizer | null>(null);
	let usingVirtualizedRenderer = $state<boolean | null>(null);
	let rendererIdentity = $state<string | null>(null);
	let renderRequestId = 0;

	function cleanupRenderer() {
		renderRequestId += 1;
		instance?.cleanUp();
		instance = null;
		virtualizer?.cleanUp();
		virtualizer = null;
		usingVirtualizedRenderer = null;
		rendererIdentity = null;
	}

	function getRendererIdentity(nextParams: DiffRendererParams): string {
		return [
			nextParams.virtualized ? "virtualized" : "standard",
			nextParams.diffStyle,
			nextParams.resolvedTheme,
			nextParams.oldFile.name,
			nextParams.oldFile.lang ?? "text",
			nextParams.oldFile.cacheKey,
			nextParams.newFile.name,
			nextParams.newFile.lang ?? "text",
			nextParams.newFile.cacheKey,
		].join("|");
	}

	function createRenderer(nextParams: DiffRendererParams) {
		const workerPool = getDiffWorkerPool();
		if (nextParams.virtualized) {
			if (!virtualScrollRoot || !virtualScrollContent) {
				return null;
			}
			const nextVirtualizer = new Virtualizer(VIRTUALIZER_CONFIG);
			nextVirtualizer.setup(virtualScrollRoot, virtualScrollContent);
			virtualizer = nextVirtualizer;
			usingVirtualizedRenderer = true;
			return new VirtualizedFileDiff(
				getDiffRendererOptions(nextParams.diffStyle, nextParams.resolvedTheme),
				nextVirtualizer,
				undefined,
				workerPool,
			);
		}

		usingVirtualizedRenderer = false;
		return new PierreFileDiff(
			getDiffRendererOptions(nextParams.diffStyle, nextParams.resolvedTheme),
			workerPool,
		);
	}

	function getContainerWrapper(nextParams: DiffRendererParams) {
		return nextParams.virtualized
			? (virtualScrollContent ?? undefined)
			: (standardHost ?? undefined);
	}

	async function renderDiff(
		currentInstance: PierreFileDiffInstance,
		nextParams: DiffRendererParams,
		requestId: number,
	) {
		try {
			await Promise.resolve(
				currentInstance.render({
					oldFile: nextParams.oldFile,
					newFile: nextParams.newFile,
					containerWrapper: getContainerWrapper(nextParams),
				}),
			);
		} catch (error) {
			if (requestId !== renderRequestId) {
				return;
			}

			console.error("[DiffReviewFileRenderer] Failed to render diff", {
				error,
				oldFile: nextParams.oldFile.name,
				newFile: nextParams.newFile.name,
				virtualized: nextParams.virtualized,
			});
		}
	}

	$effect(() => {
		const nextParams = params;
		const host = standardHost;
		const scrollRoot = virtualScrollRoot;
		const scrollContent = virtualScrollContent;
		void host;
		void scrollRoot;
		void scrollContent;

		if (!nextParams) {
			cleanupRenderer();
			return;
		}

		if (nextParams.virtualized) {
			if (!virtualScrollRoot || !virtualScrollContent) {
				return;
			}
		} else if (!standardHost) {
			return;
		}

		const nextRendererIdentity = getRendererIdentity(nextParams);
		if (
			!instance ||
			usingVirtualizedRenderer !== nextParams.virtualized ||
			rendererIdentity !== nextRendererIdentity
		) {
			cleanupRenderer();
			instance = createRenderer(nextParams);
			if (!instance) {
				return;
			}
			rendererIdentity = nextRendererIdentity;
		}

		const currentInstance = instance;
		if (!currentInstance) {
			return;
		}

		currentInstance.setOptions(
			getDiffRendererOptions(nextParams.diffStyle, nextParams.resolvedTheme),
		);
		const requestId = ++renderRequestId;
		void renderDiff(currentInstance, nextParams, requestId);
	});

	onMount(() => cleanupRenderer);
</script>

{#if params.virtualized}
	<div bind:this={virtualScrollRoot} class="max-h-[70vh] overflow-auto">
		<div bind:this={virtualScrollContent}></div>
	</div>
{:else}
	<div bind:this={standardHost}></div>
{/if}
