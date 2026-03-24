<script lang="ts">
	import { useAppContext } from "$lib/context/app-context.svelte";
	import { useSessionContext } from "$lib/context/session-context.svelte";

	const app = useAppContext();
	const ui = app.ui;
	const workspaces = app.workspaces;
	const session = useSessionContext();
</script>

{#if session.isPending}
	<p class="mb-2 px-1 text-sm font-medium text-muted-foreground">
		Start a new session
	</p>
	{#if workspaces.status === "loading"}
		<p class="mb-2 px-1 text-xs text-muted-foreground">Loading workspaces...</p>
	{/if}
	{#if session.ui.pendingWorkspaceSetupMessage}
		<p
			class="mb-2 truncate px-1 text-xs text-destructive"
			title={session.ui.pendingWorkspaceSetupMessage}
		>
			{session.ui.pendingWorkspaceSetupMessage}
		</p>
	{/if}
	{#if session.ui.pendingWorkspaceValidationMessage}
		<p
			class={`mb-2 truncate px-1 text-xs ${session.ui.pendingWorkspaceSourceIsValid ? "text-muted-foreground" : "text-destructive"}`}
			title={session.ui.pendingWorkspaceValidationMessage}
		>
			{session.ui.pendingWorkspaceValidationMessage}
		</p>
	{/if}
	{#if session.ui.pendingWorkspaceValidation?.authMessage}
		<p class="mb-2 px-1 text-xs text-muted-foreground">
			{session.ui.pendingWorkspaceValidation.authMessage}
		</p>
		{#if session.ui.pendingWorkspaceValidation.authRequired && session.ui.pendingWorkspaceValidation.authProvider === "github-git"}
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
