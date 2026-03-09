<script lang="ts">
	import { Button } from "$lib/components/ui/button";

	type Props = {
		onClose: () => void;
	};

	let { onClose }: Props = $props();
</script>

<div class="flex h-full min-h-[28rem] flex-col overflow-hidden rounded-2xl border border-border bg-terminal-bg text-terminal-fg">
	<div class="flex items-center justify-between border-b border-white/10 px-4 py-2 text-xs uppercase tracking-[0.2em] text-white/60">
		<span>Terminal</span>
		<Button variant="outline" size="sm" onclick={onClose}>Close panel</Button>
	</div>
	<div class="space-y-2 px-4 py-4 font-mono text-sm leading-6 text-terminal-fg/90">
		<p>$ pnpm ui:dev:backend</p>
		<p>&gt; ui dev server running on http://localhost:3100</p>
		<p>&gt; go backend ready on http://localhost:3001</p>
		<p>&gt; agent watcher connected</p>
		<p class="text-white/50">Ready for the next prompt...</p>
	</div>
</div>
