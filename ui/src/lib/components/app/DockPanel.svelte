<script lang="ts">
	import DesktopPanel from "$lib/components/app/parts/DesktopPanel.svelte";
	import DiffReviewPanel from "$lib/components/app/parts/DiffReviewPanel.svelte";
	import FilesPanel from "$lib/components/app/parts/FilesPanel.svelte";
	import ServicePanel from "$lib/components/app/parts/ServicePanel.svelte";
	import TerminalPanel from "$lib/components/app/parts/TerminalPanel.svelte";
	import { useAppContext } from "$lib/context/app-context.svelte";
	import { useSessionContext } from "$lib/context/session-context.svelte";
	import { DESKTOP_SERVICE_ID } from "$lib/shell-types";

	const app = useAppContext();
	const session = useSessionContext();
	const sessionView = session.ui;
	const visibleServices = $derived.by(() =>
		session.services.list.filter((service) => service.id !== DESKTOP_SERVICE_ID),
	);
	const desktopAvailable = $derived.by(() =>
		session.services.list.some((service) => service.id === DESKTOP_SERVICE_ID),
	);
	const sessionFileContents = $derived.by(() => session.files.contents);
	const sessionFileDiff = $derived.by(() => session.files.diff);
	const sessionFileDiffStats = $derived.by(() => session.files.diffStats);
</script>

<div class="h-full overflow-auto bg-background p-3">
	{#if sessionView.activeView.kind === "terminal"}
		<TerminalPanel
			onClose={sessionView.openChat}
			sessionId={session.sessionId}
			rootEnabled={sessionView.terminalRootEnabled}
			onRootEnabledChange={sessionView.setTerminalRootEnabled}
			dockMaximized={sessionView.dockMaximized}
			onToggleDockMaximized={sessionView.toggleDockMaximized}
		/>
	{:else if sessionView.activeView.kind === "desktop"}
		<DesktopPanel
			sessionId={session.sessionId}
			desktopAvailable={desktopAvailable}
			onClose={sessionView.openChat}
			dockMaximized={sessionView.dockMaximized}
			onToggleDockMaximized={sessionView.toggleDockMaximized}
		/>
	{:else if sessionView.activeView.kind === "file"}
		<FilesPanel
			files={session.files}
			onClose={sessionView.openChat}
			onToggleDockMaximized={sessionView.toggleDockMaximized}
			dockMaximized={sessionView.dockMaximized}
			colorScheme={app.preferences.colorScheme}
			resolvedTheme={app.preferences.resolvedTheme}
		/>
	{:else if sessionView.activeView.kind === "diff-review"}
		<DiffReviewPanel
			dockMaximized={sessionView.dockMaximized}
			onClose={sessionView.openChat}
			onOpenFile={(path) => session.files.open(path)}
			onRefresh={() => session.files.refresh()}
			onToggleDockMaximized={sessionView.toggleDockMaximized}
			sessionId={session.sessionId}
			diff={sessionFileDiff}
			fileContents={sessionFileContents}
			diffStats={sessionFileDiffStats}
			resolvedTheme={app.preferences.resolvedTheme}
		/>
	{:else if sessionView.activeView.kind === "services"}
		<ServicePanel
			dockMaximized={sessionView.dockMaximized}
			sessionId={session.sessionId}
			services={visibleServices}
			activeServiceId={sessionView.activeServiceId}
			onSelectService={session.services.open}
			onClose={sessionView.openChat}
			onStart={session.services.start}
			onStop={session.services.stop}
			onToggleDockMaximized={sessionView.toggleDockMaximized}
		/>
	{/if}
</div>
