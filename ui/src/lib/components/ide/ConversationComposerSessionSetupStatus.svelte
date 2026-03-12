<script lang="ts">
	import { useAppContext } from "$lib/context/app-context.svelte";
	import { useSessionContext } from "$lib/context/session-context.svelte";
	import type { WorkspaceSelectorState } from "$lib/components/ide/conversation-composer.types";

	type Props = {
		state: WorkspaceSelectorState;
	};

	const app = useAppContext();
	const ui = app.ui;
	const workspaces = app.workspaces;
	const session = useSessionContext();

	let { state }: Props = $props();
</script>

{#if !session.current}
	<p class="mb-2 px-1 text-sm font-medium text-muted-foreground">Start a new session</p>
	{#if workspaces.status === "loading"}
		<p class="mb-2 px-1 text-xs text-muted-foreground">Loading workspaces...</p>
	{/if}
	{#if state.setupMessage}
		<p class="mb-2 truncate px-1 text-xs text-destructive" title={state.setupMessage}>
			{state.setupMessage}
		</p>
	{/if}
	{#if state.workspaceValidationMessage}
		<p
			class={`mb-2 truncate px-1 text-xs ${state.workspaceSourceIsValid ? "text-muted-foreground" : "text-destructive"}`}
			title={state.workspaceValidationMessage}
		>
			{state.workspaceValidationMessage}
		</p>
	{/if}
	{#if state.workspaceValidation?.authMessage}
		<p class="mb-2 px-1 text-xs text-muted-foreground">
			{state.workspaceValidation.authMessage}
		</p>
		{#if state.workspaceValidation.authRequired && state.workspaceValidation.authProvider === "github-git"}
			<button
				type="button"
				class="mb-2 px-1 text-xs text-primary underline underline-offset-2 hover:text-primary/80"
				onclick={ui.openGitHubCredentialFlow}
			>
				Connect GitHub credential
			</button>
		{/if}
	{/if}
{/if}
