<script lang="ts">
	import "../app.css";
	import "katex/dist/katex.min.css";
	import "streamdown/styles.css";

	import { setQueryClientContext } from "@tanstack/svelte-query";
	import { setAppContext } from "$lib/context/app-context.svelte";
	import { getQueryClient } from "$lib/query/query-client";
	import {
		authProviders,
		credentialFixtures,
		settingsModels,
		ideOptions,
		sessionSummaries,
		supportInfoFixture,
		windowControls,
		workflowActions,
	} from "$lib/mock-shell-data";

	const queryClient = getQueryClient();
	setQueryClientContext(queryClient);

	const appContext = setAppContext({
		ideOptions,
		sessions: sessionSummaries,
		windowControls,
		workflowActions,
		models: settingsModels,
		authProviders,
		credentials: credentialFixtures,
		supportInfo: supportInfoFixture,
	});

	appContext.initialize();
</script>

<slot />
