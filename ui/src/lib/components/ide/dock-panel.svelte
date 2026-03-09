<script lang="ts">
	import type { CenterPanel, ServiceItem } from "$lib/shell-types";
	import DesktopPanel from "$lib/components/ide/desktop-panel.svelte";
	import FilesPanel from "$lib/components/ide/files-panel.svelte";
	import ServicePanel from "$lib/components/ide/service-panel.svelte";
	import TerminalPanel from "$lib/components/ide/terminal-panel.svelte";

	type Props = {
		centerPanel: CenterPanel;
		fileContents: Record<string, string>;
		files: string[];
		onClose: () => void;
		onSelectFile: (file: string) => void;
		selectedFile: string;
		services: ServiceItem[];
		suggestedPrompts: string[];
	};

	let {
		centerPanel,
		fileContents,
		files,
		onClose,
		onSelectFile,
		selectedFile,
		services,
		suggestedPrompts,
	}: Props = $props();

	const service = $derived(
		services.find((item) => centerPanel === `service:${item.id}`) ?? null,
	);
</script>

<div class="rounded-2xl border border-border bg-card p-4 shadow-[0_20px_60px_rgba(0,0,0,0.18)]">
	{#if centerPanel === "terminal"}
		<TerminalPanel {onClose} />
	{:else if centerPanel === "desktop"}
		<DesktopPanel {onClose} {suggestedPrompts} />
	{:else if centerPanel === "files"}
		<FilesPanel {fileContents} {files} {onClose} {onSelectFile} {selectedFile} />
	{:else if service}
		<ServicePanel {service} {onClose} />
	{/if}
</div>
