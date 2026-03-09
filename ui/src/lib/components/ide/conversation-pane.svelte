<script lang="ts">
	import { Badge } from "$lib/components/ui/badge";
	import { Button } from "$lib/components/ui/button";
	import { Textarea } from "$lib/components/ui/textarea";
	import type { CenterPanel } from "$lib/shell-types";

	type Props = {
		centerPanel: CenterPanel;
		currentThread: string;
	};

	let { centerPanel, currentThread }: Props = $props();

	function activeToolLabel() {
		return centerPanel === "chat" ? "No tool selected" : centerPanel.replace("service:", "service ");
	}
</script>

<div class="grid gap-4 lg:grid-rows-[minmax(0,1fr)_9rem]">
	<div class="rounded-2xl border border-border bg-card p-4 shadow-[0_18px_40px_rgba(0,0,0,0.12)]">
		<div class="space-y-4">
			<div class="max-w-[78%] rounded-2xl border border-primary/30 bg-primary/18 px-4 py-3 text-sm leading-6 text-primary-foreground">
				Can we make <span class="font-medium">{currentThread}</span> feel more like an assistant-led IDE than a dashboard?
			</div>
			<div class="ms-auto max-w-[72%] rounded-2xl border border-border bg-background/70 px-4 py-3 text-sm leading-6 text-muted-foreground">
				Yes — keep the conversation timeline central, keep the active thread visible, and let tools dock beside the chat instead of replacing it.
			</div>
			<div class="max-w-[76%] rounded-2xl border border-primary/30 bg-primary/18 px-4 py-3 text-sm leading-6 text-primary-foreground">
				That way terminal, desktop preview, files, and services stay available without losing the conversational flow.
			</div>
			<div class="grid gap-3 md:grid-cols-3">
				<div class="rounded-xl border border-border bg-background/70 p-3 text-sm">
					<p class="font-medium">Thread context</p>
					<p class="mt-1 text-muted-foreground">Thread + session always visible</p>
				</div>
				<div class="rounded-xl border border-border bg-background/70 p-3 text-sm">
					<p class="font-medium">Primary canvas</p>
					<p class="mt-1 text-muted-foreground">Conversation + inline tool flow</p>
				</div>
				<div class="rounded-xl border border-border bg-background/70 p-3 text-sm">
					<p class="font-medium">Docked tools</p>
					<p class="mt-1 text-muted-foreground capitalize">{activeToolLabel()}</p>
				</div>
			</div>
		</div>
	</div>

	<div class="rounded-2xl border border-border bg-card px-4 py-3 shadow-[0_18px_40px_rgba(0,0,0,0.12)]">
		<div class="flex h-full items-center justify-between gap-3">
			<Textarea
				rows={2}
				value="Ask Discobot to refine the shell, inspect files, open a docked service, or compare the new Svelte UI with the current app..."
				class="min-h-0 flex-1 resize-none"
			/>
			<div class="flex flex-wrap gap-2">
				<Badge variant="outline">attachments</Badge>
				<Badge variant="outline">plans</Badge>
				<Button>Send</Button>
			</div>
		</div>
	</div>
</div>
