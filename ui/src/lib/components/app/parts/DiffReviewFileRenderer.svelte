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

	function cleanupRenderer() {
		instance?.cleanUp();
		instance = null;
		virtualizer?.cleanUp();
		virtualizer = null;
		usingVirtualizedRenderer = null;
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

		if (!instance || usingVirtualizedRenderer !== nextParams.virtualized) {
			cleanupRenderer();
			instance = createRenderer(nextParams);
			if (!instance) {
				return;
			}
		}

		instance.setOptions(
			getDiffRendererOptions(nextParams.diffStyle, nextParams.resolvedTheme),
		);
		instance.render({
			oldFile: nextParams.oldFile,
			newFile: nextParams.newFile,
			containerWrapper: nextParams.virtualized
				? (virtualScrollContent ?? undefined)
				: (standardHost ?? undefined),
		});
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
