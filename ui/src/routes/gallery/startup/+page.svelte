<script lang="ts">
	import { onMount } from "svelte";
	import StartupScreen from "$lib/components/app/parts/StartupScreen.svelte";
	import { Badge } from "$lib/components/ui/badge";
	import { Button } from "$lib/components/ui/button";
	import {
		Card,
		CardContent,
		CardDescription,
		CardHeader,
		CardTitle,
	} from "$lib/components/ui/card";
	import { Switch } from "$lib/components/ui/switch";

	type StartupStepState = "pending" | "running" | "done" | "failed";

	type StartupFrame = {
		label: string;
		headline: string;
		detail: string;
		progress: number;
		statusLabel: string;
		statusVariant: "outline" | "secondary" | "destructive";
		apiState: "offline" | "retrying" | "online";
		retryCount?: number;
		errorMessage?: string;
		steps: Array<{
			label: string;
			detail: string;
			state: StartupStepState;
		}>;
		ready: boolean;
	};

	type StartupScenario = {
		id: string;
		title: string;
		description: string;
		frames: StartupFrame[];
	};

	const scenarios: StartupScenario[] = [
		{
			id: "healthy",
			title: "Healthy startup",
			description:
				"Tauri config initializes and the API is ready on the first check.",
			frames: [
				{
					label: "Phase 1",
					headline: "Booting the desktop shell",
					detail:
						"Initializing Tauri runtime settings and resolving the local server port.",
					progress: 18,
					statusLabel: "Initializing",
					statusVariant: "outline",
					apiState: "offline",
					steps: [
						{
							label: "Initialize Tauri config",
							detail: "Resolving port + auth token",
							state: "running",
						},
						{
							label: "Check backend API",
							detail: "Waiting to call /api/status",
							state: "pending",
						},
						{
							label: "Hydrate app data",
							detail: "Sessions, workspaces, startup tasks",
							state: "pending",
						},
					],
					ready: false,
				},
				{
					label: "Phase 2",
					headline: "Confirming backend availability",
					detail:
						"The app is polling /api/status before it renders the main shell.",
					progress: 46,
					statusLabel: "Waiting for API",
					statusVariant: "secondary",
					apiState: "retrying",
					steps: [
						{
							label: "Initialize Tauri config",
							detail: "Port resolved: localhost:3001",
							state: "done",
						},
						{
							label: "Check backend API",
							detail: "GET /api/status → 200 OK",
							state: "running",
						},
						{
							label: "Hydrate app data",
							detail: "Preparing initial fetches",
							state: "pending",
						},
					],
					ready: false,
				},
				{
					label: "Phase 3",
					headline: "Loading the workspace shell",
					detail:
						"The API is healthy, so the app is fetching sessions, workspaces, and startup tasks.",
					progress: 78,
					statusLabel: "Hydrating data",
					statusVariant: "secondary",
					apiState: "online",
					steps: [
						{
							label: "Initialize Tauri config",
							detail: "Resolved successfully",
							state: "done",
						},
						{
							label: "Check backend API",
							detail: "API ready and authenticated",
							state: "done",
						},
						{
							label: "Hydrate app data",
							detail: "Refreshing sessions and workspaces",
							state: "running",
						},
					],
					ready: false,
				},
				{
					label: "Phase 4",
					headline: "Discobot is ready",
					detail:
						"Startup checks passed and the main shell can render normally.",
					progress: 100,
					statusLabel: "Ready",
					statusVariant: "secondary",
					apiState: "online",
					steps: [
						{
							label: "Initialize Tauri config",
							detail: "Resolved successfully",
							state: "done",
						},
						{
							label: "Check backend API",
							detail: "API ready and authenticated",
							state: "done",
						},
						{
							label: "Hydrate app data",
							detail: "Initial app state loaded",
							state: "done",
						},
					],
					ready: true,
				},
			],
		},
		{
			id: "retrying",
			title: "Backend retry",
			description:
				"The shell comes up before the Go API is ready, then eventually succeeds after retries.",
			frames: [
				{
					label: "Attempt 1",
					headline: "Waiting for the backend to start",
					detail:
						"The window is open, but the API is not accepting requests yet.",
					progress: 28,
					statusLabel: "API unavailable",
					statusVariant: "destructive",
					apiState: "offline",
					errorMessage: "GET /api/status failed: connection refused",
					steps: [
						{
							label: "Initialize Tauri config",
							detail: "Resolved localhost:3001",
							state: "done",
						},
						{
							label: "Check backend API",
							detail: "First health probe failed",
							state: "failed",
						},
						{
							label: "Hydrate app data",
							detail: "Blocked until API is reachable",
							state: "pending",
						},
					],
					ready: false,
				},
				{
					label: "Attempt 2",
					headline: "Retrying backend checks",
					detail:
						"The shell is polling status again while the server finishes booting.",
					progress: 42,
					statusLabel: "Retrying",
					statusVariant: "outline",
					apiState: "retrying",
					retryCount: 2,
					errorMessage: "Still waiting for /api/status to return 200.",
					steps: [
						{
							label: "Initialize Tauri config",
							detail: "Ready",
							state: "done",
						},
						{
							label: "Check backend API",
							detail: "Retrying after backoff",
							state: "running",
						},
						{
							label: "Hydrate app data",
							detail: "Queued behind readiness gate",
							state: "pending",
						},
					],
					ready: false,
				},
				{
					label: "Recovered",
					headline: "Backend is finally responding",
					detail: "The next readiness probe succeeds, so startup can continue.",
					progress: 76,
					statusLabel: "API recovered",
					statusVariant: "secondary",
					apiState: "online",
					retryCount: 3,
					steps: [
						{
							label: "Initialize Tauri config",
							detail: "Ready",
							state: "done",
						},
						{
							label: "Check backend API",
							detail: "GET /api/status → 200 OK",
							state: "done",
						},
						{
							label: "Hydrate app data",
							detail: "Loading shell state",
							state: "running",
						},
					],
					ready: false,
				},
				{
					label: "Ready",
					headline: "Startup recovered successfully",
					detail:
						"A temporary outage delayed startup, but the app is now ready to use.",
					progress: 100,
					statusLabel: "Ready",
					statusVariant: "secondary",
					apiState: "online",
					retryCount: 3,
					steps: [
						{
							label: "Initialize Tauri config",
							detail: "Ready",
							state: "done",
						},
						{
							label: "Check backend API",
							detail: "Recovered after retries",
							state: "done",
						},
						{
							label: "Hydrate app data",
							detail: "Startup data loaded",
							state: "done",
						},
					],
					ready: true,
				},
			],
		},
		{
			id: "startup-tasks",
			title: "Startup tasks running",
			description:
				"The API is up, but the system still has visible initialization work in progress.",
			frames: [
				{
					label: "Tasks 1",
					headline: "API ready, environment still warming up",
					detail:
						"The shell can render, but background startup tasks are still progressing.",
					progress: 52,
					statusLabel: "Finishing setup",
					statusVariant: "outline",
					apiState: "online",
					steps: [
						{
							label: "Initialize Tauri config",
							detail: "Ready",
							state: "done",
						},
						{ label: "Check backend API", detail: "Healthy", state: "done" },
						{
							label: "Hydrate app data",
							detail: "Loading startup task statuses",
							state: "running",
						},
					],
					ready: false,
				},
				{
					label: "Tasks 2",
					headline: "Completing agent prerequisites",
					detail:
						"Mock startup tasks show how we might explain longer waits after the API is online.",
					progress: 86,
					statusLabel: "Almost ready",
					statusVariant: "secondary",
					apiState: "online",
					steps: [
						{
							label: "Initialize Tauri config",
							detail: "Ready",
							state: "done",
						},
						{ label: "Check backend API", detail: "Healthy", state: "done" },
						{
							label: "Hydrate app data",
							detail: "Finalizing startup task list",
							state: "running",
						},
					],
					ready: false,
				},
				{
					label: "Tasks 3",
					headline: "Shell ready with background work complete",
					detail:
						"The final startup tasks are complete and the full interface can take over.",
					progress: 100,
					statusLabel: "Ready",
					statusVariant: "secondary",
					apiState: "online",
					steps: [
						{
							label: "Initialize Tauri config",
							detail: "Ready",
							state: "done",
						},
						{ label: "Check backend API", detail: "Healthy", state: "done" },
						{
							label: "Hydrate app data",
							detail: "Startup tasks complete",
							state: "done",
						},
					],
					ready: true,
				},
			],
		},
		{
			id: "error",
			title: "Stuck failure",
			description:
				"A persistent backend problem that needs user action instead of silent retrying.",
			frames: [
				{
					label: "Failure",
					headline: "Discobot could not connect to the backend",
					detail:
						"This is the kind of terminal error state we could show when retries keep failing.",
					progress: 35,
					statusLabel: "Needs attention",
					statusVariant: "destructive",
					apiState: "offline",
					retryCount: 6,
					errorMessage:
						"Backend startup failed: timed out waiting for database migration lock.",
					steps: [
						{
							label: "Initialize Tauri config",
							detail: "Ready",
							state: "done",
						},
						{
							label: "Check backend API",
							detail: "Repeated probes failed",
							state: "failed",
						},
						{
							label: "Hydrate app data",
							detail: "Not started",
							state: "pending",
						},
					],
					ready: false,
				},
			],
		},
	];

	let selectedScenarioId = $state(scenarios[1]?.id ?? scenarios[0].id);
	let frameIndex = $state(0);
	let autoplay = $state(true);
	const scenario = $derived(getScenario());
	const frame = $derived(scenario.frames[frameIndex] ?? scenario.frames[0]);

	function getScenario() {
		return (
			scenarios.find((scenario) => scenario.id === selectedScenarioId) ??
			scenarios[0]
		);
	}

	function selectScenario(id: string) {
		selectedScenarioId = id;
		frameIndex = 0;
	}

	function nextFrame() {
		const scenario = getScenario();
		frameIndex = (frameIndex + 1) % scenario.frames.length;
	}

	function previousFrame() {
		const scenario = getScenario();
		frameIndex =
			(frameIndex - 1 + scenario.frames.length) % scenario.frames.length;
	}

	$effect(() => {
		if (!autoplay) {
			return;
		}

		const intervalId = window.setInterval(() => {
			nextFrame();
		}, 1800);

		return () => {
			window.clearInterval(intervalId);
		};
	});

	onMount(() => {
		frameIndex = 0;
	});
</script>

<svelte:head>
	<title>Startup simulation · Discobot UI</title>
</svelte:head>

<div class="min-h-screen bg-background text-foreground">
	<div
		class="mx-auto flex min-h-screen max-w-7xl flex-col gap-8 px-6 py-8 lg:px-10"
	>
		<header
			class="flex flex-col gap-6 rounded-3xl border border-border bg-card/80 p-6 shadow-sm backdrop-blur"
		>
			<div
				class="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between"
			>
				<div class="space-y-3">
					<div class="flex flex-wrap items-center gap-2">
						<Badge variant="secondary">Startup preview</Badge>
						<Badge variant="outline">Mock data only</Badge>
					</div>
					<div class="space-y-2">
						<p
							class="text-sm font-medium uppercase tracking-[0.24em] text-muted-foreground"
						>
							Startup simulation
						</p>
						<h1 class="text-3xl font-semibold tracking-tight sm:text-4xl">
							Preview the loading experience before wiring it into runtime
						</h1>
						<p
							class="max-w-3xl text-sm leading-6 text-muted-foreground sm:text-base"
						>
							Use this route to inspect how Discobot could behave while Tauri
							and the backend are still coming online.
						</p>
					</div>
				</div>

				<div class="flex flex-wrap items-center gap-3 self-start lg:self-auto">
					<Button variant="outline" href="/gallery">Back to gallery</Button>
					<Button variant="outline" href="/">Back to home</Button>
				</div>
			</div>

			<div class="grid gap-4 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-center">
				<div class="flex flex-wrap gap-2">
					{#each scenarios as item}
						<Button
							variant={item.id === selectedScenarioId ? "default" : "outline"}
							size="sm"
							onclick={() => selectScenario(item.id)}
						>
							{item.title}
						</Button>
					{/each}
				</div>

				<div
					class="flex items-center gap-3 rounded-2xl border border-border bg-background/70 px-4 py-3 text-sm"
				>
					<div class="space-y-0.5">
						<p class="font-medium">Autoplay sequence</p>
						<p class="text-xs text-muted-foreground">
							Cycle through mocked startup phases automatically
						</p>
					</div>
					<Switch bind:checked={autoplay} />
				</div>
			</div>
		</header>

		<main class="grid flex-1 gap-6 xl:grid-cols-[minmax(0,1.35fr)_22rem]">
			<section class="space-y-6">
				<StartupScreen
					modeLabel="Startup preview"
					metaLabel={frame.label}
					headline={frame.headline}
					detail={frame.detail}
					statusLabel={frame.statusLabel}
					statusVariant={frame.statusVariant}
					progress={frame.progress}
					apiState={frame.apiState}
					retryCount={frame.retryCount}
					errorMessage={frame.errorMessage}
					steps={frame.steps}
					ready={frame.ready}
				/>

				<Card>
					<CardHeader>
						<CardTitle>{scenario.title}</CardTitle>
						<CardDescription>{scenario.description}</CardDescription>
					</CardHeader>
					<CardContent class="flex flex-wrap items-center gap-3">
						<Button variant="outline" onclick={previousFrame}
							>Previous phase</Button
						>
						<Button variant="outline" onclick={nextFrame}>Next phase</Button>
						<Button
							variant={autoplay ? "secondary" : "outline"}
							onclick={() => (autoplay = !autoplay)}
						>
							{autoplay ? "Pause autoplay" : "Resume autoplay"}
						</Button>
					</CardContent>
				</Card>
			</section>

			<aside class="space-y-6">
				<Card>
					<CardHeader>
						<CardTitle>Mock API probes</CardTitle>
						<CardDescription>
							How the startup gate could narrate backend readiness.
						</CardDescription>
					</CardHeader>
					<CardContent class="space-y-3 text-sm">
						<div
							class="flex items-center justify-between rounded-xl border border-border px-3 py-2"
						>
							<span>GET /health</span>
							<span
								class={frame.apiState === "online"
									? "text-emerald-600 dark:text-emerald-400"
									: frame.apiState === "retrying"
										? "text-blue-600 dark:text-blue-400"
										: "text-destructive"}
							>
								{frame.apiState === "online"
									? "200 OK"
									: frame.apiState === "retrying"
										? "retrying"
										: "unreachable"}
							</span>
						</div>
						<div
							class="flex items-center justify-between rounded-xl border border-border px-3 py-2"
						>
							<span>GET /api/status</span>
							<span
								class={frame.apiState === "online"
									? "text-emerald-600 dark:text-emerald-400"
									: frame.apiState === "retrying"
										? "text-blue-600 dark:text-blue-400"
										: "text-destructive"}
							>
								{frame.apiState === "online"
									? "ready"
									: frame.apiState === "retrying"
										? "waiting"
										: "offline"}
							</span>
						</div>
						{#if frame.retryCount}
							<p class="text-muted-foreground">
								The current mock frame shows retry attempt {frame.retryCount}.
							</p>
						{/if}
					</CardContent>
				</Card>

				<Card>
					<CardHeader>
						<CardTitle>Why this route exists</CardTitle>
						<CardDescription>
							Preview the startup UX before wiring richer live states.
						</CardDescription>
					</CardHeader>
					<CardContent class="space-y-2 text-sm text-muted-foreground">
						<p>This route uses mocked data only.</p>
						<p>It is meant to answer questions like:</p>
						<ul class="list-disc space-y-1 pl-4">
							<li>How should repeated API failures look?</li>
							<li>What copy do we want while waiting?</li>
							<li>Should startup tasks be visible before the shell renders?</li>
						</ul>
					</CardContent>
				</Card>
			</aside>
		</main>
	</div>
</div>
