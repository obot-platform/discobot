<script lang="ts">
	import ConversationWorkspaceSelector from "$lib/components/ide/ConversationWorkspaceSelector.svelte";
	import { useSessionContext } from "$lib/context/session-context.svelte";
	import type {
		WorkspaceReadyResult,
		WorkspaceSelectorHandle,
		WorkspaceSelectorState,
	} from "$lib/components/ide/conversation-composer.types";

	type Props = {
		selectorState?: WorkspaceSelectorState;
		disabled?: boolean;
	};

	const session = useSessionContext();

	const defaultState: WorkspaceSelectorState = {
		selectedWorkspaceOption: "new-workspace",
		selectedWorkspaceBranch: "",
		requiresSourceInput: false,
		workspaceSourceInput: "",
		workspaceSourceType: "local",
		workspaceValidation: null,
		workspaceSourceIsValid: true,
		workspaceValidationMessage: null,
		validatingWorkspaceSource: false,
		creatingSessionSetup: false,
		setupMessage: null,
	};

	let {
		selectorState = $bindable<WorkspaceSelectorState>(defaultState),
		disabled = $bindable(false),
	}: Props = $props();

	let workspaceSelectorRef = $state<WorkspaceSelectorHandle | null>(null);

	$effect(() => {
		disabled =
			selectorState.creatingSessionSetup ||
			(!session.current && selectorState.requiresSourceInput && !selectorState.workspaceSourceIsValid);
	});

	export function resetForNewSession() {
		workspaceSelectorRef?.resetForNewSession();
	}

	export async function ensureWorkspaceReady(): Promise<WorkspaceReadyResult> {
		return workspaceSelectorRef?.ensureWorkspaceReady() ?? { ready: false, workspaceId: null };
	}

	export async function ensureSessionReady(): Promise<boolean> {
		return workspaceSelectorRef?.ensureSessionReady() ?? false;
	}
</script>

{#if !session.current}
	<ConversationWorkspaceSelector
		bind:this={workspaceSelectorRef}
		onStateChange={(nextState) => {
			selectorState = nextState;
		}}
	/>
{/if}
