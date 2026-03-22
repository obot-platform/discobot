<script lang="ts">
	import "../app.css";
	import "katex/dist/katex.min.css";
	import "streamdown/styles.css";

	import { onMount } from "svelte";
	import { ideOptions, windowControls } from "$lib/app/app-shell-config";
	import { setAppContext } from "$lib/context/app-context.svelte";

	const app = setAppContext({
		ideOptions,
		windowControls,
	});

	onMount(() => {
		void app.refresh();
		return app.connectProjectEvents();
	});
</script>

<slot />
