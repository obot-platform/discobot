<script lang="ts">
	import ChevronDownIcon from "@lucide/svelte/icons/chevron-down";
	import PanelLeftIcon from "@lucide/svelte/icons/panel-left";
	import { Button } from "$lib/components/ui/button";
	import { useAppContext } from "$lib/context/app-context.svelte";
	import { useSessionContext } from "$lib/context/session-context.svelte";
	import { useThreadContext } from "$lib/context/thread-context.svelte";

	type Props = {
		threadsOpen: boolean;
		onToggleThreads: () => void;
	};

	let { threadsOpen, onToggleThreads }: Props = $props();

	const app = useAppContext();
	const session = useSessionContext();
	const thread = useThreadContext();
	const threadUi = thread.ui;
	const sessionServices = $derived.by(() => session.services);

	function preferredIdeLabel() {
		return app.ideOptions.find((option) => option.id === app.preferredIde)?.label ?? "Cursor";
	}

	function isActiveService(serviceId: string) {
		return threadUi.centerPanel === `service:${serviceId}`;
	}

	const diffStats = $derived.by(() => {
		const changedFiles = (session.current?.files ?? []).filter((file) => file.type === "file");
		let additions = 0;
		let modifications = 0;
		let deletions = 0;

		for (const file of changedFiles) {
			if (file.status === "added") {
				additions += 1;
				continue;
			}

			if (file.status === "deleted") {
				deletions += 1;
				continue;
			}

			if (file.status === "modified" || file.status === "renamed" || file.changed) {
				modifications += 1;
			}
		}

		if (additions === 0 && modifications === 0 && deletions === 0) {
			return {
				additions: 12,
				modifications: 4,
				deletions: 3,
			};
		}

		return {
			additions,
			modifications,
			deletions,
		};
	});
</script>

<div class="grid h-10 grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] items-center gap-2 bg-background px-2">
	<div class="flex min-w-0 items-center gap-2">
		<Button
			variant={threadsOpen ? "secondary" : "ghost"}
			size="icon-xs"
			onclick={onToggleThreads}
			aria-label={threadsOpen ? "Collapse threads panel" : "Expand threads panel"}
			title={threadsOpen ? "Collapse threads panel" : "Expand threads panel"}
		>
			<PanelLeftIcon class="size-3.5" />
		</Button>
		<p class="truncate px-1 text-sm font-medium">
			{session.threads.selected?.name ?? "No thread selected"}
		</p>
	</div>

	<div class="justify-self-center inline-flex rounded-md border border-border bg-background p-0.5">
		<Button
			variant={threadUi.centerPanel === "terminal" ? "secondary" : "ghost"}
			size="xs"
			onclick={threadUi.openTerminal}
		>
			Terminal
		</Button>
		<Button
			variant={threadUi.centerPanel === "desktop" ? "secondary" : "ghost"}
			size="xs"
			onclick={threadUi.openDesktop}
		>
			Desktop
		</Button>
		<Button
			variant={threadUi.centerPanel === "files" ? "secondary" : "ghost"}
			size="xs"
			onclick={() => threadUi.openFiles()}
		>
			Files
		</Button>
		<Button
			variant={threadUi.centerPanel === "diff-review" ? "secondary" : "ghost"}
			size="xs"
			onclick={threadUi.openDiffReview}
			class="gap-1"
		>
			<span class="text-green-500">+{diffStats.additions}</span>
			<span class="text-muted-foreground">~{diffStats.modifications}</span>
			<span class="text-red-500">-{diffStats.deletions}</span>
		</Button>
		{#each sessionServices as service}
			<Button
				variant={isActiveService(service.id) ? "secondary" : "ghost"}
				size="xs"
				onclick={() => threadUi.openService(service.id)}
			>
				{service.label}
			</Button>
		{/each}
	</div>

	<div class="flex items-center justify-self-end gap-2">
		<div class="tauri-no-drag relative inline-flex items-center">
			<Button variant="outline" size="xs" class="rounded-r-none border-r-0">
				Open {preferredIdeLabel()}
			</Button>
			<Button
				variant="outline"
				size="xs"
				onclick={threadUi.toggleIdeMenu}
				class="rounded-l-none px-2"
				aria-label="Select preferred IDE"
			>
				<ChevronDownIcon class="size-3.5" />
			</Button>
			{#if threadUi.ideMenuOpen}
				<div class="absolute right-0 top-full z-50 mt-2 min-w-[11rem] rounded-md border border-border bg-popover p-1 shadow-md">
					<p class="px-3 py-2 text-xs font-medium uppercase tracking-[0.16em] text-muted-foreground">
						Preferred IDE
					</p>
					{#each app.ideOptions as option}
						<Button
							variant={app.preferredIde === option.id ? "secondary" : "ghost"}
							size="sm"
							onclick={() => app.setPreferredIde(option.id)}
							class="w-full justify-between"
						>
							<span>{option.label}</span>
							{#if app.preferredIde === option.id}
								<span class="text-xs font-medium">Default</span>
							{/if}
						</Button>
					{/each}
				</div>
			{/if}
		</div>
	</div>
</div>
