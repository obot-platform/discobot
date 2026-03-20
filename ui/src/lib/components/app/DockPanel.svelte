<script lang="ts">
	import DesktopPanel from "$lib/components/app/parts/DesktopPanel.svelte";
	import DiffReviewPanel from "$lib/components/app/parts/DiffReviewPanel.svelte";
	import FilesPanel from "$lib/components/app/parts/FilesPanel.svelte";
	import ServicePanel from "$lib/components/app/parts/ServicePanel.svelte";
	import TerminalPanel from "$lib/components/app/parts/TerminalPanel.svelte";
	import { useSessionContext } from "$lib/context/session-context.svelte";

	const session = useSessionContext();
	const sessionView = session.ui;
	const sessionFiles = $derived.by(() => session.files.list);
	const sessionFileContents = $derived.by(() => session.files.contents);
	const sessionFileDiff = $derived.by(() => session.files.diff);
	const sessionFileDiffStats = $derived.by(() => session.files.diffStats);
	const sessionActiveService = $derived.by(() => session.services.active);
</script>

<div class="h-full overflow-auto bg-background p-3">
	{#if sessionView.activeView.kind === "terminal"}
		<TerminalPanel onClose={sessionView.openChat} />
	{:else if sessionView.activeView.kind === "desktop"}
		<DesktopPanel onClose={sessionView.openChat} />
	{:else if sessionView.activeView.kind === "file"}
		<FilesPanel
			fileContents={sessionFileContents}
			files={sessionFiles}
			onClose={sessionView.openChat}
			onSelectFile={session.files.open}
			selectedFile={session.files.selected}
		/>
	{:else if sessionView.activeView.kind === "diff-review"}
		<DiffReviewPanel onClose={sessionView.openChat} diff={sessionFileDiff} fileContents={sessionFileContents} diffStats={sessionFileDiffStats} />
	{:else if sessionActiveService}
		<ServicePanel service={sessionActiveService} onClose={sessionView.openChat} />
	{/if}
</div>
