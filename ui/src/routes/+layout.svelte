<script lang="ts">
	import "../app.css";
	import "katex/dist/katex.min.css";
	import "streamdown/styles.css";

	import { setQueryClientContext } from "@tanstack/svelte-query";
	import { ideOptions, windowControls, workflowActions } from "$lib/app/app-shell-config";
	import { setAppContext } from "$lib/context/app-context.svelte";
	import { getQueryClient } from "$lib/query/query-client";

	const queryClient = getQueryClient();
	setQueryClientContext(queryClient);

	setAppContext({
		ideOptions,
		windowControls,
		workflowActions,
	});
</script>

<slot />
