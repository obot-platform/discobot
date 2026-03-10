<script lang="ts">
	import CopyIcon from "@lucide/svelte/icons/copy";
	import ExternalLinkIcon from "@lucide/svelte/icons/external-link";
	import XIcon from "@lucide/svelte/icons/x";
	import { Button } from "$lib/components/ui/button";
	import { openUrl } from "$lib/tauri";

	type Props = {
		url: string;
		isOpen: boolean;
		onClose: () => void;
		onConfirm?: () => void;
	};

	let { url, isOpen, onClose, onConfirm }: Props = $props();
	let copied = $state(false);

	async function handleCopyLink() {
		try {
			await navigator.clipboard.writeText(url);
			copied = true;
			setTimeout(() => {
				copied = false;
			}, 2000);
		} catch (error) {
			console.error("Failed to copy link:", error);
		}
	}

	async function handleOpenLink() {
		await openUrl(url);
		onConfirm?.();
		onClose();
	}
</script>

{#if isOpen}
	<div class="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
		<div class="relative mx-4 flex w-full max-w-md flex-col gap-4 rounded-xl border bg-background p-6 shadow-lg">
			<button
				class="absolute right-4 top-4 rounded-md p-1 text-muted-foreground transition-all hover:bg-muted hover:text-foreground"
				onclick={onClose}
				title="Close"
				type="button"
			>
				<XIcon class="h-4 w-4" />
			</button>

			<div class="flex flex-col gap-2">
				<div class="flex items-center gap-2 font-semibold text-lg">
					<ExternalLinkIcon class="h-5 w-5" />
					<span>Open external link?</span>
				</div>
				<p class="text-muted-foreground text-sm">
					You're about to visit an external website.
				</p>
			</div>

			<div class="break-all rounded-md bg-muted p-3 font-mono text-sm">{url}</div>

			<div class="flex gap-2">
				<Button class="flex-1" onclick={handleCopyLink} variant="outline">
					<CopyIcon class="mr-2 h-4 w-4" />
					<span>{copied ? "Copied!" : "Copy link"}</span>
				</Button>
				<Button class="flex-1" onclick={handleOpenLink}>
					<ExternalLinkIcon class="mr-2 h-4 w-4" />
					<span>Open link</span>
				</Button>
			</div>
		</div>
	</div>
{/if}
