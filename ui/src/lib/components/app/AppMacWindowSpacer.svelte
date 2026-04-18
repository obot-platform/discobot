<script lang="ts">
	import { withCurrentDesktopWindow } from "$lib/shell";
	import { onMount } from "svelte";
	import LeftWindowControls from "$lib/components/app/parts/LeftWindowControls.svelte";
	import { useAppContext } from "$lib/context/app-context.svelte";

	const environment = useAppContext().environment;
	let isMacFullscreen = $state(false);

	onMount(() => {
		if (
			!environment.supportsNativeWindowControls ||
			environment.windowControlsSide !== "left"
		) {
			return;
		}

		let unlisten: (() => void) | undefined;

		void withCurrentDesktopWindow(async (window) => {
			const syncFullscreen = async () => {
				isMacFullscreen = await window.isFullscreen();
			};

			await syncFullscreen();
			unlisten = await window.onResized(() => {
				void syncFullscreen();
			});
		});

		return () => {
			unlisten?.();
		};
	});

	const showMacSpacer = $derived(
		environment.supportsNativeWindowControls &&
			environment.windowControlsSide === "left" &&
			!isMacFullscreen,
	);
</script>

{#if showMacSpacer}
	<LeftWindowControls />
{/if}
