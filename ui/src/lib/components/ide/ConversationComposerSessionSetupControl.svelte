<script lang="ts">
	import ConversationComposerSessionSetupStatus from "$lib/components/ide/ConversationComposerSessionSetupStatus.svelte";
	import ConversationWorkspaceSelector from "$lib/components/ide/ConversationWorkspaceSelector.svelte";
	import type {
		WorkspaceSelectionResult,
		WorkspaceSelectorHandle,
		WorkspaceSelectorState,
	} from "$lib/components/ide/conversation-composer.types";

	type Props = {
		disabled?: boolean;
	};

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
		setupMessage: null,
	};

	let { disabled = $bindable(false) }: Props = $props();

	let selectorState = $state<WorkspaceSelectorState>(defaultState);
	let workspaceSelectorRef = $state<WorkspaceSelectorHandle | null>(null);

	$effect(() => {
		disabled = selectorState.requiresSourceInput && !selectorState.workspaceSourceIsValid;
	});

	export function resetForNewSession() {
		workspaceSelectorRef?.resetForNewSession();
	}

	export async function getWorkspaceSelection(): Promise<WorkspaceSelectionResult> {
		return workspaceSelectorRef?.getWorkspaceSelection() ?? {
			ready: false,
			workspaceId: null,
			workspaceType: null,
			workspacePath: null,
		};
	}
</script>

<ConversationComposerSessionSetupStatus state={selectorState} />
<div class="mb-2 flex items-center gap-1.5">
	<ConversationWorkspaceSelector
		bind:this={workspaceSelectorRef}
		onStateChange={(nextState) => {
			selectorState = nextState;
		}}
	/>
</div>
