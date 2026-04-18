<script lang="ts">
	import { onMount } from "svelte";
	import { fade } from "svelte/transition";
	import { api } from "$lib/api-client";
	import { initServerConfig } from "$lib/api-config";
	import { initDesktopConfig } from "$lib/shell";
	import type { AuthUser, StartupTask } from "$lib/api-types";
	import DiscobotBrand from "$lib/components/app/parts/DiscobotBrand.svelte";
	import Button from "$lib/components/ui/button/button.svelte";
	import StartupScreen, {
		type StartupApiState,
		type StartupScreenStep,
		type StartupStatusVariant,
	} from "$lib/components/app/parts/StartupScreen.svelte";
	import type { AppContext } from "$lib/context/app-context.svelte";

	type Props = {
		app: AppContext;
		children?: () => any;
	};

	type StartupPhase =
		| "initializing"
		| "waiting"
		| "auth"
		| "loading"
		| "ready"
		| "error";

	type StartupScreenData = {
		headline: string;
		detail: string;
		statusLabel: string;
		statusVariant: StartupStatusVariant;
		progress: number;
		apiState: StartupApiState;
		retryCount: number;
		steps: StartupScreenStep[];
	};

	const STARTUP_SCREEN_MIN_VISIBLE_MS = 350;
	const STARTUP_SCREEN_FADE_MS = 180;
	const STARTUP_STATUS_POLL_MS = 1000;

	let { app, children }: Props = $props();

	let ready = $state(false);
	let errorMessage = $state<string | null>(null);
	let startupPhase = $state<StartupPhase>("initializing");
	let authenticatedUser = $state<AuthUser | null>(null);
	const loginHref =
		typeof window !== "undefined"
			? api.getLoginUrl(window.location.href)
			: api.getLoginUrl();
	let retryCount = $state(0);
	let startupTasks = $state<StartupTask[]>([]);
	let startupOverlayDismissed = $state(false);

	function sleep(ms: number) {
		return new Promise((resolve) => {
			window.setTimeout(resolve, ms);
		});
	}

	function formatBytes(value: number) {
		if (value < 1024) {
			return `${value} B`;
		}

		const units = ["KB", "MB", "GB", "TB"];
		let size = value;
		let unitIndex = -1;
		while (size >= 1024 && unitIndex < units.length - 1) {
			size /= 1024;
			unitIndex += 1;
		}

		return `${size.toFixed(size >= 10 ? 0 : 1)} ${units[unitIndex]}`;
	}

	function getTaskDetail(task: StartupTask) {
		if (task.error) {
			return task.error;
		}

		if (task.currentOperation) {
			return task.currentOperation;
		}

		if (
			typeof task.bytesDownloaded === "number" &&
			typeof task.totalBytes === "number" &&
			task.totalBytes > 0
		) {
			return `${formatBytes(task.bytesDownloaded)} of ${formatBytes(task.totalBytes)}`;
		}

		return "Waiting for the server to report more detail.";
	}

	function getTaskProgress(task: StartupTask) {
		if (typeof task.progress === "number") {
			return task.progress;
		}

		if (
			typeof task.bytesDownloaded === "number" &&
			typeof task.totalBytes === "number" &&
			task.totalBytes > 0
		) {
			return Math.round((task.bytesDownloaded / task.totalBytes) * 100);
		}

		switch (task.state) {
			case "completed":
				return 100;
			case "in_progress":
				return 50;
			default:
				return 0;
		}
	}

	function mapTaskState(task: StartupTask): StartupScreenStep["state"] {
		switch (task.state) {
			case "in_progress":
				return "running";
			case "completed":
				return "done";
			case "failed":
				return "failed";
			default:
				return "pending";
		}
	}

	function hasActiveStartupTasks(tasks: StartupTask[]) {
		return tasks.some(
			(task) => task.state === "pending" || task.state === "in_progress",
		);
	}

	function dismissStartupOverlay() {
		startupOverlayDismissed = true;
		ready = true;
	}

	async function refreshStartupStatus() {
		const status = await api.getSystemStatus();
		startupTasks = status.startupTasks ?? [];
		return status;
	}

	const startupScreen = $derived.by((): StartupScreenData => {
		const visibleTasks = startupTasks.filter(
			(task) => task.state !== "completed",
		);
		const hasFailedTask = visibleTasks.some((task) => task.state === "failed");
		const hasRunningTask = visibleTasks.some(
			(task) => task.state === "in_progress",
		);
		const hasPendingTask = visibleTasks.some(
			(task) => task.state === "pending",
		);
		const progress =
			visibleTasks.length > 0
				? Math.round(
						visibleTasks.reduce((sum, task) => sum + getTaskProgress(task), 0) /
							visibleTasks.length,
					)
				: startupPhase === "ready"
					? 100
					: startupPhase === "loading"
						? 80
						: startupPhase === "waiting"
							? 30
							: 15;
		const steps: StartupScreenStep[] =
			visibleTasks.length > 0
				? visibleTasks.map((task) => ({
						label: task.name,
						detail: getTaskDetail(task),
						state: mapTaskState(task),
					}))
				: [];

		if (startupPhase === "error") {
			return {
				headline: "Discobot could not connect to the backend",
				detail:
					"The shell is still waiting for the server to become available before it can render.",
				statusLabel: "Needs attention",
				statusVariant: "destructive",
				progress,
				apiState: "offline",
				retryCount,
				steps,
			};
		}

		if (hasFailedTask) {
			return {
				headline: "A startup task needs attention",
				detail:
					"Discobot connected to the backend, but one of the startup tasks reported an error.",
				statusLabel: "Task failed",
				statusVariant: "destructive",
				progress,
				apiState: "online",
				retryCount,
				steps,
			};
		}

		if (hasRunningTask || hasPendingTask) {
			return {
				headline: "Completing startup tasks",
				detail:
					visibleTasks.length === 1
						? "Discobot is waiting for one backend startup task to finish."
						: `Discobot is waiting for ${visibleTasks.length} backend startup tasks to finish.`,
				statusLabel: hasRunningTask ? "Running tasks" : "Queued tasks",
				statusVariant: hasRunningTask ? "secondary" : "outline",
				progress,
				apiState: "online",
				retryCount,
				steps,
			};
		}

		if (startupPhase === "auth") {
			return {
				headline: "Sign in to Discobot",
				detail:
					"The backend is ready, but you need to authenticate before the workspace shell can load.",
				statusLabel: "Authentication required",
				statusVariant: "outline",
				progress: 100,
				apiState: "online",
				retryCount,
				steps,
			};
		}

		if (startupPhase === "loading") {
			return {
				headline: "Loading the workspace shell",
				detail:
					"The backend is ready, and Discobot is fetching the first set of app data.",
				statusLabel: "Hydrating data",
				statusVariant: "secondary",
				progress,
				apiState: "online",
				retryCount,
				steps,
			};
		}

		if (startupPhase === "waiting") {
			return {
				headline:
					retryCount > 0
						? "Retrying backend startup checks"
						: "Waiting for the backend API",
				detail:
					retryCount > 0
						? "Discobot is polling the live status endpoint until the backend is reachable."
						: "Discobot is waiting for the backend status endpoint before it reveals the shell.",
				statusLabel: retryCount > 0 ? "Retrying API" : "Waiting for API",
				statusVariant: retryCount > 0 ? "outline" : "secondary",
				progress,
				apiState: retryCount > 0 ? "retrying" : "offline",
				retryCount,
				steps,
			};
		}

		if (startupPhase === "ready") {
			return {
				headline: "Discobot is ready",
				detail:
					"Startup checks passed and the workspace shell is ready to render.",
				statusLabel: "Ready",
				statusVariant: "secondary",
				progress: 100,
				apiState: "online",
				retryCount,
				steps,
			};
		}

		return {
			headline: "Booting the desktop shell",
			detail:
				"Discobot is initializing the desktop runtime and preparing to contact the backend.",
			statusLabel: "Initializing",
			statusVariant: "outline",
			progress,
			apiState: "offline",
			retryCount,
			steps,
		};
	});

	const canDismissStartupOverlay = $derived.by(
		() =>
			!startupOverlayDismissed &&
			startupPhase !== "initializing" &&
			startupPhase !== "waiting" &&
			hasActiveStartupTasks(startupTasks),
	);

	function getStartupErrorMessage(error: unknown) {
		return error instanceof Error
			? error.message
			: "Discobot could not connect to the backend.";
	}

	function isAbortError(error: unknown, signal: AbortSignal) {
		return signal.aborted && error === signal.reason;
	}

	onMount(() => {
		const abortController = new AbortController();
		const startupScreenShownAt = Date.now();
		let stopProjectEvents = () => {};

		async function waitForSuccessfulStartupRequest<T>(
			request: () => Promise<T>,
			phase: StartupPhase,
		): Promise<T> {
			for (;;) {
				if (abortController.signal.aborted) {
					throw abortController.signal.reason ?? new Error("aborted");
				}

				try {
					return await request();
				} catch (error) {
					if (
						abortController.signal.aborted ||
						isAbortError(error, abortController.signal)
					) {
						throw error;
					}
					startupPhase = phase;
					retryCount += 1;
					errorMessage = getStartupErrorMessage(error);
					await sleep(STARTUP_STATUS_POLL_MS);
				}
			}
		}

		void (async () => {
			try {
				startupPhase = "initializing";
				await initDesktopConfig();
				startupPhase = "waiting";

				await waitForSuccessfulStartupRequest(
					() => refreshStartupStatus(),
					"waiting",
				);

				errorMessage = null;
				authenticatedUser = await waitForSuccessfulStartupRequest(
					() => api.getCurrentUser(),
					"waiting",
				);
				if (!authenticatedUser) {
					startupPhase = "auth";
					return;
				}
				startupPhase = "loading";
				await initServerConfig();
				await app.refresh();
				startupTasks = app.startup.tasks;
				stopProjectEvents = app.connectProjectEvents();

				while (!abortController.signal.aborted) {
					try {
						await refreshStartupStatus();
						if (
							startupOverlayDismissed ||
							!hasActiveStartupTasks(startupTasks)
						) {
							break;
						}
					} catch (error) {
						if (
							abortController.signal.aborted ||
							isAbortError(error, abortController.signal)
						) {
							throw error;
						}
						retryCount += 1;
						errorMessage = getStartupErrorMessage(error);
					}
					await sleep(STARTUP_STATUS_POLL_MS);
				}

				errorMessage = null;
				startupPhase = "ready";

				const elapsed = Date.now() - startupScreenShownAt;
				if (elapsed < STARTUP_SCREEN_MIN_VISIBLE_MS) {
					await sleep(STARTUP_SCREEN_MIN_VISIBLE_MS - elapsed);
				}

				if (!abortController.signal.aborted && !ready) {
					ready = true;
				}
			} catch (error) {
				if (!abortController.signal.aborted) {
					startupPhase = "error";
					errorMessage = getStartupErrorMessage(error);
				}
			}
		})();

		return () => {
			abortController.abort();
			stopProjectEvents();
		};
	});
</script>

<div
	class={ready && startupPhase !== "auth"
		? "transition-opacity duration-200 opacity-100"
		: "pointer-events-none select-none transition-opacity duration-200 opacity-0"}
>
	{@render children?.()}
</div>

{#if startupPhase === "auth"}
	<div
		class="fixed inset-0 z-50 flex items-center justify-center bg-background/95 px-6 py-10 text-foreground backdrop-blur-sm"
	>
		<div
			class="w-full max-w-md rounded-2xl border border-border bg-card p-8 text-center shadow-sm"
		>
			<div class="mb-5 flex justify-center">
				<DiscobotBrand heightClass="h-8" />
			</div>
			<h1 class="mb-2 font-semibold text-2xl text-foreground">
				Sign in required
			</h1>
			<p class="mb-6 text-muted-foreground text-sm leading-6">
				Sign in to continue to your Discobot workspace.
			</p>
			<div class="flex justify-center">
				<Button href={loginHref} size="lg">Sign in</Button>
			</div>
		</div>
	</div>
{:else if !ready}
	<div
		in:fade={{ duration: STARTUP_SCREEN_FADE_MS }}
		out:fade={{ duration: STARTUP_SCREEN_FADE_MS }}
		class="fixed inset-0 z-50 flex items-center justify-center bg-background/95 px-6 py-10 text-foreground backdrop-blur-sm"
	>
		<StartupScreen
			class="max-w-lg"
			modeLabel="Startup gate"
			metaLabel="Live app"
			headline={startupScreen.headline}
			detail={startupScreen.detail}
			statusLabel={startupScreen.statusLabel}
			statusVariant={startupScreen.statusVariant}
			progress={startupScreen.progress}
			apiState={startupScreen.apiState}
			retryCount={startupScreen.retryCount}
			{errorMessage}
			steps={startupScreen.steps}
			ready={startupPhase === "ready"}
			dismissLabel="Continue while tasks finish"
			onDismiss={canDismissStartupOverlay ? dismissStartupOverlay : null}
			showShellPreview={false}
		/>
	</div>
{/if}
