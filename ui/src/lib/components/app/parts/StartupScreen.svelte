<script lang="ts">
	import DiscobotBrand from "$lib/components/app/parts/DiscobotBrand.svelte";
	import { Badge } from "$lib/components/ui/badge";
	import {
		Card,
		CardContent,
		CardDescription,
		CardHeader,
		CardTitle,
	} from "$lib/components/ui/card";
	import Progress from "$lib/components/ui/progress/progress.svelte";
	import Skeleton from "$lib/components/ui/skeleton/skeleton.svelte";
	import Spinner from "$lib/components/ui/spinner/spinner.svelte";
	import { cn } from "$lib/utils";

	export type StartupStatusVariant =
		| "default"
		| "secondary"
		| "outline"
		| "destructive";
	export type StartupApiState = "offline" | "retrying" | "online";
	export type StartupStepState = "pending" | "running" | "done" | "failed";

	export type StartupScreenStep = {
		label: string;
		detail: string;
		state: StartupStepState;
	};

	type Props = {
		class?: string;
		modeLabel?: string;
		metaLabel?: string | null;
		headline: string;
		detail: string;
		statusLabel?: string;
		statusVariant?: StartupStatusVariant;
		progress?: number;
		apiState?: StartupApiState;
		retryCount?: number;
		errorMessage?: string | null;
		steps?: ReadonlyArray<StartupScreenStep>;
		detailsOpen?: boolean;
		dismissLabel?: string | null;
		onDismiss?: (() => void) | null;
		ready?: boolean;
		showShellPreview?: boolean;
	};

	let {
		class: className,
		modeLabel = "Starting up",
		metaLabel = null,
		headline,
		detail,
		statusLabel = "Starting",
		statusVariant = "outline",
		progress = 0,
		apiState = "offline",
		retryCount,
		errorMessage = null,
		steps = [],
		detailsOpen = $bindable(false),
		dismissLabel = null,
		onDismiss = null,
		ready = false,
		showShellPreview = true,
	}: Props = $props();

	function getStepTone(state: StartupStepState) {
		if (state === "done") return "text-emerald-600 dark:text-emerald-400";
		if (state === "running") return "text-blue-600 dark:text-blue-400";
		if (state === "failed") return "text-destructive";
		return "text-muted-foreground";
	}

	function getStepDotClass(state: StartupStepState) {
		if (state === "done") return "bg-emerald-500";
		if (state === "running") return "bg-blue-500 animate-pulse";
		if (state === "failed") return "bg-destructive";
		return "bg-muted-foreground/30";
	}

	function getApiTone(nextApiState: StartupApiState) {
		if (nextApiState === "online")
			return "text-emerald-600 dark:text-emerald-400";
		if (nextApiState === "retrying") return "text-blue-600 dark:text-blue-400";
		return "text-destructive";
	}
</script>

<div class={cn("w-full space-y-4", className)}>
	<div
		class={cn(
			"mx-auto flex w-full flex-col items-center rounded-2xl border border-border bg-background text-center shadow-sm",
			showShellPreview
				? "max-w-md gap-4 px-8 py-8"
				: "max-w-sm gap-3 px-6 py-6",
		)}
	>
		<DiscobotBrand heightClass={showShellPreview ? "h-[2.5rem]" : "h-5"} />

		{#if detailsOpen}
			<div class="flex flex-wrap items-center justify-center gap-2">
				<Badge variant="secondary">{modeLabel}</Badge>
				{#if metaLabel}
					<Badge variant="outline">{metaLabel}</Badge>
				{/if}
			</div>
		{/if}

		{#if ready}
			<div
				class="flex size-9 items-center justify-center rounded-full bg-emerald-500/10 text-emerald-600 dark:text-emerald-400"
			>
				<div class="size-3.5 rounded-full bg-current"></div>
			</div>
		{:else}
			<Spinner class="size-7" />
		{/if}

		{#if detailsOpen}
			<div class="space-y-1.5">
				<h2 class="font-semibold text-lg sm:text-xl">{headline}</h2>
				<p class="text-muted-foreground text-sm leading-5">{detail}</p>
			</div>
		{/if}

		<div class="w-full space-y-2.5 text-left">
			{#if detailsOpen}
				<div class="flex items-center justify-between gap-3 text-xs sm:text-sm">
					<span class={getApiTone(apiState)}>
						API: {apiState === "online"
							? "reachable"
							: apiState === "retrying"
								? "retrying"
								: "offline"}
					</span>
					<div class="flex items-center gap-2">
						<span class="text-muted-foreground">{statusLabel}</span>
						{#if retryCount}
							<span class="text-muted-foreground">retry {retryCount}</span>
						{/if}
					</div>
				</div>
			{/if}
			<Progress value={progress} />
			{#if errorMessage || steps.length > 0 || showShellPreview || onDismiss}
				<div class="flex items-center justify-between gap-3">
					{#if errorMessage || steps.length > 0 || showShellPreview}
						<button
							type="button"
							class="text-muted-foreground text-xs transition-colors hover:text-foreground"
							onclick={() => (detailsOpen = !detailsOpen)}
						>
							{detailsOpen ? "Hide details" : "Show details"}
						</button>
					{:else}
						<span></span>
					{/if}
					{#if onDismiss}
						<button
							type="button"
							class="text-muted-foreground text-xs transition-colors hover:text-foreground"
							onclick={onDismiss}
						>
							{dismissLabel ?? "Continue in background"}
						</button>
					{/if}
				</div>
			{/if}
		</div>

		{#if detailsOpen && errorMessage}
			<div
				class="w-full rounded-xl border border-destructive/30 bg-destructive/5 px-3 py-2.5 text-left text-sm text-destructive"
			>
				{errorMessage}
			</div>
		{/if}

		{#if detailsOpen && steps.length > 0}
			<div class="w-full space-y-2 text-left">
				{#each steps as step}
					<div class="rounded-xl border border-border px-3 py-2.5">
						<div class="flex items-start gap-2.5">
							<div
								class={cn(
									"mt-1 size-2.5 shrink-0 rounded-full",
									getStepDotClass(step.state),
								)}
							></div>
							<div class="min-w-0 space-y-1">
								<div class="flex items-center justify-between gap-3">
									<p class="font-medium text-sm">{step.label}</p>
									<span
										class={cn("text-xs capitalize", getStepTone(step.state))}
										>{step.state}</span
									>
								</div>
								<p class="text-sm text-muted-foreground">{step.detail}</p>
							</div>
						</div>
					</div>
				{/each}
			</div>
		{/if}
	</div>

	{#if detailsOpen && showShellPreview}
		{#if ready}
			<div class="grid gap-3 md:grid-cols-2">
				<Card class="bg-background/70">
					<CardHeader class="pb-3">
						<CardTitle class="text-base">Main shell</CardTitle>
						<CardDescription
							>What the interface might reveal right after startup completes.</CardDescription
						>
					</CardHeader>
					<CardContent class="space-y-3">
						<div
							class="flex items-center justify-between rounded-xl border border-border px-3 py-2 text-sm"
						>
							<span>Workspaces</span>
							<Badge variant="secondary">12 loaded</Badge>
						</div>
						<div
							class="flex items-center justify-between rounded-xl border border-border px-3 py-2 text-sm"
						>
							<span>Sessions</span>
							<Badge variant="outline">3 active</Badge>
						</div>
					</CardContent>
				</Card>

				<Card class="bg-background/70">
					<CardHeader class="pb-3">
						<CardTitle class="text-base">Recent activity</CardTitle>
						<CardDescription
							>The startup screen hands off to real shell content here.</CardDescription
						>
					</CardHeader>
					<CardContent class="space-y-3">
						<Skeleton class="h-10 w-full" />
						<Skeleton class="h-10 w-full" />
						<Skeleton class="h-10 w-4/5" />
					</CardContent>
				</Card>
			</div>
		{:else}
			<div class="grid gap-3 md:grid-cols-2">
				<Skeleton class="h-24 w-full rounded-2xl" />
				<Skeleton class="h-24 w-full rounded-2xl" />
				<Skeleton class="h-16 w-full rounded-2xl md:col-span-2" />
			</div>
		{/if}
	{/if}
</div>
