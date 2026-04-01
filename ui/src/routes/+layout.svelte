<script lang="ts">
	import "../app.css";
	import "katex/dist/katex.min.css";

	import StartupGate from "$lib/components/app/StartupGate.svelte";
	import { Toaster } from "$lib/components/ui/sonner";
	import { ideOptions, windowControls } from "$lib/app/app-shell-config";
	import { setAppContext } from "$lib/context/app-context.svelte";

	type Props = {
		children?: () => any;
	};

	let { children }: Props = $props();

	const app = setAppContext({
		ideOptions,
		windowControls,
	});
</script>

<Toaster />

<StartupGate {app}>
	{@render children?.()}
</StartupGate>
