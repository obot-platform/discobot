<script lang="ts">
	import { Badge } from "$lib/components/ui/badge";
	import { Button } from "$lib/components/ui/button";
	import type { WindowControlsSide } from "$lib/shell-types";

	type Props = {
		currentThread: string;
		onThemeToggle: () => void;
		sessionStatus: string;
		theme: string;
		windowControls: string[];
		windowControlsSide: WindowControlsSide;
		workflowActions: string[];
		workspaceTarget: string;
	};

	let {
		currentThread,
		onThemeToggle,
		sessionStatus,
		theme,
		windowControls,
		windowControlsSide,
		workflowActions,
		workspaceTarget,
	}: Props = $props();
</script>

<header class="bg-card/95 text-card-foreground backdrop-blur" data-tauri-drag-region>
	<div class="px-4 py-3">
		<div class="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
			<div class="flex min-w-0 items-center gap-3">
				<div class="flex min-w-[5.5rem] items-center gap-1.5">
					{#if windowControlsSide === "left"}
						{#each windowControls as control, index}
							<button
								type="button"
								class={`tauri-no-drag flex size-7 items-center justify-center rounded-md border text-xs transition ${control === "×" ? "border-destructive/30 text-destructive hover:bg-destructive/10" : "border-border text-muted-foreground hover:bg-accent"}`}
								aria-label={index === 0 ? "Minimize" : index === 1 ? "Maximize" : "Close"}
							>
								{control}
							</button>
						{/each}
					{/if}
				</div>

				<div class="flex min-w-0 flex-1 flex-wrap items-center gap-3">
				<div class="tauri-no-drag flex size-9 items-center justify-center rounded-xl bg-primary/12 font-semibold text-primary">
					DB
				</div>
					<div class="min-w-0">
						<p class="truncate text-sm font-semibold">Discobot</p>
						<p class="truncate text-xs text-muted-foreground">AI coding shell</p>
					</div>
					<Button variant="outline" size="sm" class="tauri-no-drag">
						Session: {currentThread} ▾
					</Button>
					<Badge variant="secondary">{sessionStatus}</Badge>
					<Badge variant="outline">{workspaceTarget}</Badge>
				</div>
			</div>

			<div class="flex items-center justify-between gap-3 lg:min-w-[16rem] lg:justify-end">
				<div class="tauri-no-drag flex flex-wrap items-center gap-2">
					<div class="inline-flex rounded-xl border border-border bg-background p-1 shadow-sm">
						{#each workflowActions as action}
							<Button variant="ghost" size="sm">{action}</Button>
						{/each}
					</div>
					<Button variant="outline" size="sm" onclick={onThemeToggle}>Theme: {theme}</Button>
					<Button variant="outline" size="sm">Settings</Button>
				</div>

				<div class="flex min-w-[5.5rem] items-center justify-end gap-1.5">
					{#if windowControlsSide === "right"}
						{#each windowControls as control, index}
							<button
								type="button"
								class={`tauri-no-drag flex size-7 items-center justify-center rounded-md border text-xs transition ${control === "×" ? "border-destructive/30 text-destructive hover:bg-destructive/10" : "border-border text-muted-foreground hover:bg-accent"}`}
								aria-label={index === 0 ? "Minimize" : index === 1 ? "Maximize" : "Close"}
							>
								{control}
							</button>
						{/each}
					{/if}
				</div>
			</div>
		</div>
	</div>
</header>
