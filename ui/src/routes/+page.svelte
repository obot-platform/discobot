<script lang="ts">
	import SessionWorkspace from "$lib/components/ide/SessionWorkspace.svelte";
	import { useAppContext } from "$lib/context/app-context.svelte";

	const app = useAppContext();
	const sessions = app.sessions;
</script>

<svelte:head>
	<title>Discobot UI</title>
</svelte:head>

{#key sessions.selectedId ?? sessions.pendingId}
	<SessionWorkspace />
{/key}
