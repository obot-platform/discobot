<script lang="ts">
	import DesktopPanel from "$lib/components/ide/DesktopPanel.svelte";
	import DiffReviewPanel from "$lib/components/ide/DiffReviewPanel.svelte";
	import FilesPanel from "$lib/components/ide/FilesPanel.svelte";
	import ServicePanel from "$lib/components/ide/ServicePanel.svelte";
	import TerminalPanel from "$lib/components/ide/TerminalPanel.svelte";
	import { useSessionContext } from "$lib/context/session-context.svelte";
	import { useThreadContext } from "$lib/context/thread-context.svelte";

	const session = useSessionContext();
	const thread = useThreadContext();
	const threadUi = thread.ui;
	const sessionFiles = $derived.by(() => session.files);
	const sessionFileContents = $derived.by(() => session.fileContents);
	const sessionSuggestedPrompts = $derived.by(() => session.suggestedPrompts);
	const sessionActiveService = $derived.by(() => session.activeService);
</script>

<div class="h-full overflow-auto bg-background p-3">
	{#if threadUi.centerPanel === "terminal"}
		<TerminalPanel onClose={threadUi.openChat} />
	{:else if threadUi.centerPanel === "desktop"}
		<DesktopPanel onClose={threadUi.openChat} suggestedPrompts={sessionSuggestedPrompts} />
	{:else if threadUi.centerPanel === "files"}
		<FilesPanel
			fileContents={sessionFileContents}
			files={sessionFiles}
			onClose={threadUi.openChat}
			onSelectFile={threadUi.openFiles}
			selectedFile={threadUi.selectedFile}
		/>
	{:else if threadUi.centerPanel === "diff-review"}
		<DiffReviewPanel onClose={threadUi.openChat} />
	{:else if sessionActiveService}
		<ServicePanel service={sessionActiveService} onClose={threadUi.openChat} />
	{/if}
</div>
