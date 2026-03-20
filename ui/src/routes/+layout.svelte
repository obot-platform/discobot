<script lang="ts">
	import "../app.css";
	import "katex/dist/katex.min.css";
	import "streamdown/styles.css";

	import { onMount } from "svelte";
	import { ideOptions, windowControls, workflowActions } from "$lib/app/app-shell-config";
	import { setAppContext } from "$lib/context/app-context.svelte";

	const app = setAppContext({
		ideOptions,
		windowControls,
		workflowActions,
	});

	onMount(() => {
		void app.refresh();
		return app.connectProjectEvents();
	});
</script>

<slot />
