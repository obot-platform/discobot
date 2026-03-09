<script lang="ts">
	import { Badge } from "$lib/components/ui/badge";
	import { Button } from "$lib/components/ui/button";
	import type { CenterPanel, IdeOption, PreferredIde, ServiceItem } from "$lib/shell-types";

	type Props = {
		baseBranch: string;
		baseCommit: string;
		centerPanel: CenterPanel;
		currentThread: string;
		ideMenuOpen: boolean;
		ideOptions: IdeOption[];
		issueReference: string;
		onChooseIde: (ide: PreferredIde) => void;
		onOpenDesktop: () => void;
		onOpenFiles: () => void;
		onOpenService: (serviceId: string) => void;
		onOpenTerminal: () => void;
		onToggleIdeMenu: () => void;
		preferredIde: PreferredIde;
		preferredIdeLabel: string;
		pullRequestReference: string;
		services: ServiceItem[];
	};

	let {
		baseBranch,
		baseCommit,
		centerPanel,
		currentThread,
		ideMenuOpen,
		ideOptions,
		issueReference,
		onChooseIde,
		onOpenDesktop,
		onOpenFiles,
		onOpenService,
		onOpenTerminal,
		onToggleIdeMenu,
		preferredIde,
		preferredIdeLabel,
		pullRequestReference,
		services,
	}: Props = $props();

	function isActiveService(serviceId: string) {
		return centerPanel === `service:${serviceId}`;
	}
</script>

<div class="border-b border-border bg-card/80 px-4 py-3 shadow-[inset_0_-1px_0_rgba(255,255,255,0.02)] backdrop-blur">
	<div class="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
		<div class="flex flex-wrap items-center gap-2">
			<p class="text-sm font-semibold">{currentThread}</p>
			<Badge variant="outline">{baseBranch} @ {baseCommit}</Badge>
			<Badge variant="outline">{issueReference} / {pullRequestReference}</Badge>

			<div class="inline-flex rounded-xl border border-border bg-background p-1 shadow-sm">
				<button
					type="button"
					onclick={onOpenTerminal}
					class={`rounded-lg px-3 py-1.5 text-sm font-medium transition ${centerPanel === "terminal" ? "bg-primary text-primary-foreground shadow-sm" : "text-muted-foreground hover:bg-accent"}`}
				>
					Terminal
				</button>
				<button
					type="button"
					onclick={onOpenDesktop}
					class={`rounded-lg px-3 py-1.5 text-sm font-medium transition ${centerPanel === "desktop" ? "bg-primary text-primary-foreground shadow-sm" : "text-muted-foreground hover:bg-accent"}`}
				>
					Desktop
				</button>
				<button
					type="button"
					onclick={onOpenFiles}
					class={`rounded-lg px-3 py-1.5 text-sm font-medium transition ${centerPanel === "files" ? "bg-primary text-primary-foreground shadow-sm" : "text-muted-foreground hover:bg-accent"}`}
				>
					Files
				</button>
				{#each services as service}
					<button
						type="button"
						onclick={() => onOpenService(service.id)}
						class={`rounded-lg px-3 py-1.5 text-sm font-medium transition ${isActiveService(service.id) ? "bg-primary text-primary-foreground shadow-sm" : "text-muted-foreground hover:bg-accent"}`}
					>
						{service.label}
					</button>
				{/each}
			</div>

			<div class="rounded-full border border-border bg-background px-3 py-1 text-xs font-medium text-muted-foreground">
				+12 ~4 -3
			</div>

			<div class="tauri-no-drag relative inline-flex items-center">
				<Button variant="outline" size="sm">Open {preferredIdeLabel}</Button>
				<button
					type="button"
					onclick={onToggleIdeMenu}
					class="border-input bg-background hover:bg-accent text-muted-foreground relative -ms-px flex h-9 items-center rounded-r-md border px-2 text-xs transition"
					aria-label="Select preferred IDE"
				>
					▾
				</button>
				{#if ideMenuOpen}
					<div class="absolute right-0 top-full z-50 mt-2 min-w-[11rem] rounded-xl border border-border bg-popover p-1 shadow-md">
						<p class="px-3 py-2 text-xs font-medium uppercase tracking-[0.18em] text-muted-foreground">
							Preferred IDE
						</p>
						{#each ideOptions as option}
							<button
								type="button"
								onclick={() => onChooseIde(option.id)}
								class={`flex w-full items-center justify-between rounded-lg px-3 py-2 text-sm transition ${preferredIde === option.id ? "bg-accent text-foreground" : "text-muted-foreground hover:bg-accent hover:text-foreground"}`}
							>
								<span>{option.label}</span>
								{#if preferredIde === option.id}
									<span class="text-xs font-medium">Default</span>
								{/if}
							</button>
						{/each}
					</div>
				{/if}
			</div>
		</div>
	</div>
</div>
