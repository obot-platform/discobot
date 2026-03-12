<script lang="ts">
	import ChevronDownIcon from "@lucide/svelte/icons/chevron-down";
	import PanelLeftIcon from "@lucide/svelte/icons/panel-left";
	import { Button } from "$lib/components/ui/button";
	import { useAppContext } from "$lib/context/app-context.svelte";
	import { useSessionContext } from "$lib/context/session-context.svelte";

	type Props = {
		threadsOpen: boolean;
		onToggleThreads: () => void;
	};

	let { threadsOpen, onToggleThreads }: Props = $props();

	const app = useAppContext();
	const preferences = app.preferences;
	const session = useSessionContext();
	const sessionView = session.ui;
	const sessionServices = $derived.by(() => session.services.list);

	function preferredIdeLabel() {
		return preferences.ideOptions.find((option) => option.id === preferences.preferredIde)?.label ?? "Cursor";
	}

	function isActiveService(serviceId: string) {
		return sessionView.activeServiceId === serviceId;
	}

	const diffStats = $derived.by(() => {
		const stats = session.files.diffStats;
		const files = session.files.diff;
		const additions = stats.additions;
		const deletions = stats.deletions;
		const modifications = files.filter((file) => file.status === "modified" || file.status === "renamed").length;
		return { additions, modifications, deletions };
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
			variant={sessionView.activeView.kind === "terminal" ? "secondary" : "ghost"}
			size="xs"
			onclick={sessionView.openTerminal}
		>
			Terminal
		</Button>
		<Button
			variant={sessionView.activeView.kind === "desktop" ? "secondary" : "ghost"}
			size="xs"
			onclick={sessionView.openDesktop}
		>
			Desktop
		</Button>
		<Button
			variant={sessionView.activeView.kind === "file" ? "secondary" : "ghost"}
			size="xs"
			onclick={() => void session.files.open()}
		>
			Files
		</Button>
		<Button
			variant={sessionView.activeView.kind === "diff-review" ? "secondary" : "ghost"}
			size="xs"
			onclick={sessionView.openDiffReview}
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
				onclick={() => session.services.open(service.id)}
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
				onclick={sessionView.toggleIdeMenu}
				class="rounded-l-none px-2"
				aria-label="Select preferred IDE"
			>
				<ChevronDownIcon class="size-3.5" />
			</Button>
			{#if sessionView.ideMenuOpen}
				<div class="absolute right-0 top-full z-50 mt-2 min-w-[11rem] rounded-md border border-border bg-popover p-1 shadow-md">
					<p class="px-3 py-2 text-xs font-medium uppercase tracking-[0.16em] text-muted-foreground">
						Preferred IDE
					</p>
					{#each preferences.ideOptions as option}
						<Button
							variant={preferences.preferredIde === option.id ? "secondary" : "ghost"}
							size="sm"
							onclick={() => preferences.setPreferredIde(option.id)}
							class="w-full justify-between"
						>
							<span>{option.label}</span>
							{#if preferences.preferredIde === option.id}
								<span class="text-xs font-medium">Default</span>
							{/if}
						</Button>
					{/each}
				</div>
			{/if}
		</div>
	</div>
</div>
