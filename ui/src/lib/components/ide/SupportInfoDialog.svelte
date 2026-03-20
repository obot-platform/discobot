<script lang="ts">
	import CopyIcon from "@lucide/svelte/icons/copy";
	import DownloadIcon from "@lucide/svelte/icons/download";
	import InfoIcon from "@lucide/svelte/icons/info";
	import Loader2Icon from "@lucide/svelte/icons/loader-2";
	import { onDestroy, onMount } from "svelte";
	import { Button } from "$lib/components/ui/button";
	import * as Dialog from "$lib/components/ui/dialog";
	import { useAppContext } from "$lib/context/app-context.svelte";

	const app = useAppContext();
	const supportInfo = app.supportInfo;
	const ui = app.ui;
	let copied = $state(false);
	let copiedTimeout = $state<ReturnType<typeof setTimeout> | null>(null);

	const supportJson = $derived.by(() =>
		supportInfo.data ? JSON.stringify(supportInfo.data, null, 2) : "",
	);

	function clearCopiedTimeout() {
		if (!copiedTimeout) {
			return;
		}
		clearTimeout(copiedTimeout);
		copiedTimeout = null;
	}

	async function copySupportInfo() {
		if (!supportJson || typeof navigator === "undefined" || !navigator.clipboard?.writeText) {
			return;
		}
		await navigator.clipboard.writeText(supportJson);
		copied = true;
		clearCopiedTimeout();
		copiedTimeout = setTimeout(() => {
			copied = false;
			copiedTimeout = null;
		}, 1800);
	}

	function downloadSupportInfo() {
		if (!supportJson || typeof window === "undefined") {
			return;
		}

		const filename = `discobot-support-info-${new Date().toISOString().split("T")[0]}.json`;
		const blob = new Blob([supportJson], { type: "application/json" });
		const url = URL.createObjectURL(blob);
		const link = document.createElement("a");
		link.href = url;
		link.download = filename;
		document.body.appendChild(link);
		link.click();
		document.body.removeChild(link);
		URL.revokeObjectURL(url);
	}

	function resetCopiedState() {
		copied = false;
		clearCopiedTimeout();
	}

	function handleOpenChange(open: boolean) {
		if (open) {
			return;
		}
		ui.closeSupportInfo();
	}

	onMount(() => {
		resetCopiedState();
		void supportInfo.fetch();
	});

	onDestroy(() => {
		clearCopiedTimeout();
	});
</script>

<Dialog.Root open={true} onOpenChange={handleOpenChange}>
	<Dialog.Content class="sm:max-w-3xl max-h-[88vh] flex flex-col overflow-hidden">
		<Dialog.Header>
			<Dialog.Title class="flex items-center gap-2">
				<InfoIcon class="size-4" />
				Support Information
			</Dialog.Title>
			<Dialog.Description>
				Diagnostic data snapshot for support and debugging.
			</Dialog.Description>
		</Dialog.Header>

		<div class="mt-1 min-h-0 flex-1 overflow-auto rounded-md border border-border bg-muted/30 p-3">
			{#if supportInfo.status === "loading"}
				<div class="flex min-h-40 items-center justify-center gap-2 text-sm text-muted-foreground">
					<Loader2Icon class="size-4 animate-spin" />
					Loading support information...
				</div>
			{:else if supportInfo.status === "error"}
				<div class="rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive">
					{supportInfo.error ?? "Failed to load support information."}
				</div>
			{:else if supportJson}
				<pre class="overflow-x-auto rounded-md border border-border bg-background p-3 font-mono text-xs leading-5"><code>{supportJson}</code></pre>
			{:else}
				<div class="flex min-h-40 items-center justify-center text-sm text-muted-foreground">
					No support information available.
				</div>
			{/if}
		</div>

		<Dialog.Footer class="mt-3">
			<Button variant="outline" size="sm" onclick={copySupportInfo} disabled={!supportJson}>
				<CopyIcon class="size-3.5" />
				{copied ? "Copied" : "Copy JSON"}
			</Button>
			<Button variant="outline" size="sm" onclick={downloadSupportInfo} disabled={!supportJson}>
				<DownloadIcon class="size-3.5" />
				Download JSON
			</Button>
			<Button variant="default" size="sm" onclick={() => handleOpenChange(false)}>
				Close
			</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
