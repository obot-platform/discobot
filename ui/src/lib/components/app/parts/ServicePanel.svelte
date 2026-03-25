<script lang="ts">
	import AlertCircleIcon from "@lucide/svelte/icons/alert-circle";
	import ArrowDownIcon from "@lucide/svelte/icons/arrow-down";
	import CircleIcon from "@lucide/svelte/icons/circle";
	import ExternalLinkIcon from "@lucide/svelte/icons/external-link";
	import GlobeIcon from "@lucide/svelte/icons/globe";
	import Loader2Icon from "@lucide/svelte/icons/loader-2";
	import MonitorIcon from "@lucide/svelte/icons/monitor";
	import PlayIcon from "@lucide/svelte/icons/play";
	import RefreshCwIcon from "@lucide/svelte/icons/refresh-cw";
	import SmartphoneIcon from "@lucide/svelte/icons/smartphone";
	import SquareIcon from "@lucide/svelte/icons/square";
	import TabletIcon from "@lucide/svelte/icons/tablet";
	import TerminalIcon from "@lucide/svelte/icons/terminal";
	import XCircleIcon from "@lucide/svelte/icons/x-circle";
	import { tick, untrack } from "svelte";
	import { api } from "$lib/api-client";
	import { getApiRootBase } from "$lib/api-config";
	import type { ServiceOutputEvent } from "$lib/api-types";
	import DockWindowChrome from "$lib/components/app/parts/DockWindowChrome.svelte";
	import { Button } from "$lib/components/ui/button";
	import { Input } from "$lib/components/ui/input";
	import type { ServiceItem } from "$lib/shell-types";
	import { openUrl } from "$lib/tauri";
	import { cn } from "$lib/utils";

	type Props = {
		activeServiceId: string | null;
		dockMaximized: boolean;
		onClose: () => void;
		onSelectService: (serviceId: string) => void;
		onStart: (serviceId: string) => Promise<void>;
		onStop: (serviceId: string) => Promise<void>;
		onToggleDockMaximized: () => void;
		services: ServiceItem[];
		sessionId: string;
	};

	type ViewMode = "preview" | "logs";
	type Viewport = "desktop" | "tablet" | "mobile";

	const VIEWPORT_WIDTHS: Record<Viewport, string | null> = {
		desktop: null,
		tablet: "768px",
		mobile: "390px",
	};
	const LOG_SCROLL_THRESHOLD_PX = 16;

	const VIEWPORT_OPTIONS = [
		{ value: "mobile" as const, Icon: SmartphoneIcon, label: "Mobile (390px)" },
		{ value: "tablet" as const, Icon: TabletIcon, label: "Tablet (768px)" },
		{ value: "desktop" as const, Icon: MonitorIcon, label: "Desktop" },
	];

	let {
		activeServiceId,
		dockMaximized,
		onClose,
		onSelectService,
		onStart,
		onStop,
		onToggleDockMaximized,
		services,
		sessionId,
	}: Props = $props();

	let isLoading = $state(true);
	let error = $state<string | null>(null);
	let internalRefreshKey = $state(0);
	let currentPath = $state("/");
	let inputPath = $state("/");
	let viewport = $state<Viewport>("desktop");
	let viewMode = $state<ViewMode>("logs");
	let logEvents = $state<ServiceOutputEvent[]>([]);
	let logsConnected = $state(false);
	let logsContainer = $state<HTMLDivElement | null>(null);
	let isLogsNearBottom = $state(true);
	let hasUnreadLogs = $state(false);
	let isMutatingService = $state(false);
	let previousStatus = $state<ServiceItem["status"]>("stopped");

	const service = $derived.by(
		() =>
			services.find((item) => item.id === activeServiceId) ??
			services[0] ??
			null,
	);
	const hasHttp = $derived.by(() =>
		service ? hasHttpService(service) : false,
	);
	const passive = $derived.by(() => service?.passive === true);
	const showViewTabs = $derived.by(() => hasHttp && !passive);
	const shouldShowIframe = $derived.by(
		() => !!service && (passive || service.status === "running"),
	);
	const baseUrl = $derived.by(() =>
		service ? buildServiceBaseUrl(sessionId, service) : "",
	);
	const serviceUrl = $derived.by(() => {
		if (!baseUrl) {
			return "";
		}
		return `${baseUrl}${normalizePath(currentPath)}`;
	});
	const constrainedWidth = $derived.by(() => VIEWPORT_WIDTHS[viewport]);
	const iframeKey = $derived.by(
		() => `${service?.id ?? "service"}-${internalRefreshKey}-${currentPath}`,
	);
	const isRunnable = $derived.by(() => !!service && !passive);
	const canStart = $derived.by(
		() => !!service && isRunnable && service.status === "stopped",
	);
	const canStop = $derived.by(
		() => !!service && isRunnable && service.status === "running",
	);
	const actionLabel = $derived.by(() => {
		if (!service) {
			return "Start";
		}
		if (service.status === "starting") {
			return "Starting";
		}
		if (service.status === "stopping") {
			return "Stopping";
		}
		if (canStop) {
			return "Stop";
		}
		return "Start";
	});
	const statusLabel = $derived.by(() => {
		if (!service) {
			return "No services";
		}
		if (passive) {
			return "External service";
		}
		if (service.status === "starting") {
			return "Starting";
		}
		if (service.status === "stopping") {
			return "Stopping";
		}
		if (service.status === "running") {
			return "Running";
		}
		if (service.exitCode !== undefined && service.exitCode !== 0) {
			return `Stopped (exit ${service.exitCode})`;
		}
		return "Stopped";
	});
	const maximizeTitle = $derived.by(() =>
		dockMaximized ? "Restore split view" : "Maximize service panel",
	);

	function hasHttpService(value: ServiceItem): boolean {
		return typeof value.http === "number" || typeof value.https === "number";
	}

	function normalizePath(path?: string): string {
		const trimmed = path?.trim() ?? "";
		if (!trimmed) {
			return "/";
		}
		return trimmed.startsWith("/") ? trimmed : `/${trimmed}`;
	}

	function buildServiceBaseUrl(
		nextSessionId: string,
		nextService: ServiceItem,
	): string {
		if (!hasHttpService(nextService) || typeof window === "undefined") {
			return "";
		}

		const apiRoot = getApiRootBase();
		const parsed = new URL(apiRoot);
		const subdomain = `${nextSessionId}-svc-${nextService.id}`;
		const protocol =
			typeof nextService.https === "number" ? "https:" : parsed.protocol;
		return `${protocol}//${subdomain}.${parsed.host}`;
	}

	function refreshPreview() {
		isLoading = true;
		error = null;
		internalRefreshKey += 1;
	}

	function submitPath() {
		const nextPath = normalizePath(inputPath);
		currentPath = nextPath;
		inputPath = nextPath;
		refreshPreview();
	}

	function handlePathKeydown(event: KeyboardEvent) {
		if (event.key !== "Enter") {
			return;
		}
		event.preventDefault();
		submitPath();
	}

	function handleIframeLoad() {
		isLoading = false;
		error = null;
	}

	function handleIframeError() {
		isLoading = false;
		error = "Failed to load service";
	}

	function openExternal() {
		if (!serviceUrl || !service) {
			return;
		}
		void openUrl(serviceUrl);
	}

	function statusClass(): string {
		if (!service) {
			return "text-muted-foreground";
		}
		if (passive || service.status === "running") {
			return "text-green-500";
		}
		if (service.status === "starting" || service.status === "stopping") {
			return "text-yellow-500";
		}
		if (service.exitCode !== undefined && service.exitCode !== 0) {
			return "text-red-500";
		}
		return "text-muted-foreground";
	}

	function getStatusIcon() {
		if (!service) {
			return CircleIcon;
		}
		if (service.status === "starting" || service.status === "stopping") {
			return Loader2Icon;
		}
		if (
			service.exitCode !== undefined &&
			service.exitCode !== 0 &&
			service.status === "stopped"
		) {
			return XCircleIcon;
		}
		return CircleIcon;
	}

	function isNearBottom(element: HTMLDivElement): boolean {
		return (
			element.scrollHeight - element.clientHeight - element.scrollTop <=
			LOG_SCROLL_THRESHOLD_PX
		);
	}

	function scrollLogsToBottom() {
		const container = logsContainer;
		if (!container) {
			return;
		}
		container.scrollTo({ top: container.scrollHeight, behavior: "smooth" });
		hasUnreadLogs = false;
	}

	async function handleServiceAction() {
		if (isMutatingService || !isRunnable || !service) {
			return;
		}
		isMutatingService = true;
		try {
			if (canStop) {
				await onStop(service.id);
				return;
			}
			if (canStart) {
				await onStart(service.id);
			}
		} finally {
			isMutatingService = false;
		}
	}

	$effect(() => {
		const currentService = service;
		void currentService?.id;
		void currentService?.urlPath;
		void currentService?.http;
		void currentService?.https;
		currentPath = normalizePath(currentService?.urlPath);
		inputPath = normalizePath(currentService?.urlPath);
		isLoading = true;
		error = null;
		internalRefreshKey = 0;
		logEvents = [];
		logsConnected = false;
		isLogsNearBottom = true;
		hasUnreadLogs = false;
		isMutatingService = false;
		viewMode =
			currentService && hasHttpService(currentService) ? "preview" : "logs";
		previousStatus = untrack(() => currentService?.status ?? "stopped");
	});

	$effect(() => {
		const nextStatus = service?.status ?? "stopped";
		if (!service || !hasHttp || typeof window === "undefined") {
			previousStatus = nextStatus;
			return;
		}
		if (previousStatus !== "running" && nextStatus === "running") {
			const timer = window.setTimeout(() => {
				refreshPreview();
			}, 500);
			previousStatus = nextStatus;
			return () => {
				window.clearTimeout(timer);
			};
		}
		previousStatus = nextStatus;
	});

	$effect(() => {
		const container = logsContainer;
		if (!container) {
			isLogsNearBottom = true;
			hasUnreadLogs = false;
			return;
		}

		const updateIsNearBottom = () => {
			isLogsNearBottom = isNearBottom(container);
			if (isLogsNearBottom) {
				hasUnreadLogs = false;
			}
		};

		updateIsNearBottom();
		container.addEventListener("scroll", updateIsNearBottom);

		return () => {
			container.removeEventListener("scroll", updateIsNearBottom);
		};
	});

	$effect(() => {
		const container = logsContainer;
		void logEvents.length;
		if (!container) {
			return;
		}
		if (!isLogsNearBottom) {
			return;
		}
		hasUnreadLogs = false;
		void tick().then(() => {
			container.scrollTo(0, container.scrollHeight);
		});
	});

	$effect(() => {
		if (passive || typeof window === "undefined" || !service) {
			logEvents = [];
			logsConnected = false;
			return;
		}

		void service.status;
		logEvents = [];
		logsConnected = false;
		const source = new EventSource(
			api.getServiceOutputUrl(sessionId, service.id),
		);

		source.onopen = () => {
			logsConnected = true;
		};

		source.onerror = () => {
			logsConnected = false;
		};

		source.onmessage = (event) => {
			if (event.data === "[DONE]") {
				source.close();
				logsConnected = false;
				return;
			}

			try {
				const parsed = JSON.parse(event.data) as ServiceOutputEvent;
				if (!isLogsNearBottom) {
					hasUnreadLogs = true;
				}
				logEvents = [...logEvents, parsed];
			} catch (nextError) {
				console.error("Failed to parse service output event:", nextError);
			}
		};

		return () => {
			source.close();
			logsConnected = false;
		};
	});
</script>

<DockWindowChrome
	{dockMaximized}
	{onClose}
	{onToggleDockMaximized}
	closeLabel="Close service panel"
	minimizeLabel="Minimize service panel"
	{maximizeTitle}
	contentClass="flex min-h-0 flex-1 flex-col overflow-hidden"
>
	{#snippet title()}
		<div class="flex min-w-0 items-center gap-2 text-xs">
			<p class="truncate text-sm font-medium">Services</p>
			{#if service}
				<span class="truncate text-sidebar-foreground/70">{service.label}</span>
			{/if}
		</div>
	{/snippet}

	{#snippet actions()}
		{@const CurrentStatusIcon = getStatusIcon()}
		{#if service && showViewTabs}
			<div class="flex items-center gap-1">
				<Button
					variant={viewMode === "preview" ? "secondary" : "ghost"}
					size="xs"
					class="gap-1"
					onclick={() => {
						viewMode = "preview";
					}}
				>
					<GlobeIcon class="size-3" />
					Preview
				</Button>
				<Button
					variant={viewMode === "logs" ? "secondary" : "ghost"}
					size="xs"
					class="gap-1"
					onclick={() => {
						viewMode = "logs";
					}}
				>
					<TerminalIcon class="size-3" />
					Logs
				</Button>
			</div>
		{/if}
		<div class="flex items-center gap-2">
			<div class={cn("flex items-center gap-1 text-xs", statusClass())}>
				<CurrentStatusIcon
					class={cn(
						"size-3.5",
						(service.status === "starting" || service.status === "stopping") &&
							"animate-spin",
					)}
				/>
				<span>{statusLabel}</span>
			</div>
		</div>
	{/snippet}

	{#if services.length > 0}
		<div
			class="flex min-h-10 items-end gap-1 overflow-x-auto border-b border-sidebar-border bg-sidebar px-2 py-2"
		>
			{#each services as item (item.id)}
				<div
					role="button"
					tabindex={0}
					onclick={() => onSelectService(item.id)}
					onkeydown={(event) => {
						if (event.key === "Enter" || event.key === " ") {
							event.preventDefault();
							onSelectService(item.id);
						}
					}}
					class={cn(
						"flex shrink-0 items-center gap-2 rounded-md border px-3 py-1.5 text-sm transition",
						activeServiceId === item.id
							? "border-sidebar-border bg-background text-foreground shadow-sm"
							: "border-transparent bg-sidebar-accent/60 text-sidebar-foreground/75 hover:bg-sidebar-accent hover:text-sidebar-accent-foreground",
					)}
				>
					<div class="flex min-w-0 items-center gap-2">
						<span class="max-w-40 truncate">{item.label}</span>
						<div
							class={cn(
								"size-2 shrink-0 rounded-full",
								item.status === "running" && "bg-green-500",
								item.status === "starting" && "bg-yellow-500",
								item.status === "stopping" && "bg-yellow-500",
								item.status === "stopped" &&
									(item.exitCode !== undefined && item.exitCode !== 0
										? "bg-red-500"
										: "bg-sidebar-foreground/30"),
							)}
						></div>
					</div>
					{#if activeServiceId === item.id && service?.id === item.id && isRunnable}
						<Button
							variant="ghost"
							size="icon-xs"
							class="size-5 rounded-sm p-0 text-foreground/60 hover:text-foreground"
							title={`${actionLabel} ${item.label}`}
							aria-label={`${actionLabel} ${item.label}`}
							disabled={isMutatingService ||
								service.status === "starting" ||
								service.status === "stopping"}
							onclick={(event) => {
								event.stopPropagation();
								void handleServiceAction();
							}}
							onkeydown={(event) => {
								event.stopPropagation();
							}}
						>
							{#if service.status === "starting" || service.status === "stopping" || isMutatingService}
								<Loader2Icon class="size-3 animate-spin" />
							{:else if canStop}
								<SquareIcon class="size-3 fill-current" />
							{:else}
								<PlayIcon class="size-3 fill-current" />
							{/if}
						</Button>
					{/if}
				</div>
			{/each}
		</div>
	{:else}
		<div
			class="flex min-h-10 items-center border-b border-sidebar-border bg-sidebar px-3 py-2 text-sm text-sidebar-foreground/60"
		>
			No services available.
		</div>
	{/if}

	{#if service && hasHttp && (passive || viewMode === "preview")}
		<div class="flex items-center gap-1 border-b bg-muted/50 px-2 py-1.5">
			<span
				class="max-w-[18rem] shrink-0 truncate font-mono text-xs text-muted-foreground"
			>
				{baseUrl}
			</span>
			<Input
				class="h-7 min-w-[4rem] flex-1 font-mono text-xs"
				placeholder="/"
				value={inputPath}
				oninput={(event) => {
					const target = event.currentTarget as HTMLInputElement;
					inputPath = target.value;
				}}
				onkeydown={handlePathKeydown}
			/>
			{#each VIEWPORT_OPTIONS as option}
				<Button
					variant={viewport === option.value ? "secondary" : "ghost"}
					size="icon-xs"
					aria-label={option.label}
					title={option.label}
					onclick={() => {
						viewport = option.value;
					}}
				>
					<option.Icon class="size-3.5" />
				</Button>
			{/each}
			<Button
				variant="ghost"
				size="icon-xs"
				title="Refresh"
				aria-label="Refresh preview"
				onclick={refreshPreview}
			>
				<RefreshCwIcon
					class={cn(
						"size-3.5",
						shouldShowIframe && isLoading && "animate-spin",
					)}
				/>
			</Button>
			<Button
				variant="ghost"
				size="icon-xs"
				title="Open in browser"
				aria-label="Open preview externally"
				onclick={openExternal}
			>
				<ExternalLinkIcon class="size-3.5" />
			</Button>
		</div>
	{/if}

	<div class="relative min-h-0 flex-1">
		{#if !service}
			<div
				class="flex h-full items-center justify-center text-sm text-muted-foreground"
			>
				Select a service to view its preview or logs.
			</div>
		{:else if hasHttp && (passive || viewMode === "preview")}
			<div
				class={cn(
					"relative flex h-full min-h-0 flex-1",
					constrainedWidth ? "overflow-auto justify-center bg-muted/30" : "",
				)}
			>
				{#if !passive && service.status !== "running"}
					<div
						class="absolute inset-0 z-10 flex items-center justify-center bg-background"
					>
						<div
							class="flex flex-col items-center gap-3 text-center text-muted-foreground"
						>
							{#if service.status === "starting" || service.status === "stopping"}
								<Loader2Icon class="size-6 animate-spin" />
								<span class="text-sm">
									{service.status === "starting"
										? "Service is starting..."
										: "Service is stopping..."}
								</span>
							{:else}
								<PlayIcon class="size-8 text-foreground" />
								<div class="space-y-1">
									<p class="text-sm font-medium text-foreground">
										Service is not running
									</p>
									<p class="text-xs text-muted-foreground">
										Start it to load the preview.
									</p>
								</div>
								<Button
									size="sm"
									class="gap-2"
									disabled={isMutatingService || !canStart}
									onclick={handleServiceAction}
								>
									<PlayIcon class="size-4 fill-current" />
									Start service
								</Button>
							{/if}
						</div>
					</div>
				{/if}

				{#if shouldShowIframe && error}
					<div
						class="absolute inset-0 z-10 flex items-center justify-center bg-background"
					>
						<div class="flex flex-col items-center gap-2 text-destructive">
							<AlertCircleIcon class="size-6" />
							<span class="text-sm">{error}</span>
							<Button variant="outline" size="sm" onclick={refreshPreview}>
								Retry
							</Button>
						</div>
					</div>
				{/if}

				{#if shouldShowIframe && serviceUrl}
					<div
						class="h-full shrink-0"
						style={constrainedWidth
							? `width: ${constrainedWidth};`
							: "width: 100%;"}
					>
						{#key iframeKey}
							<iframe
								src={serviceUrl}
								class="size-full border-0"
								onload={handleIframeLoad}
								onerror={handleIframeError}
								title={`Service: ${service.id}`}
								sandbox="allow-scripts allow-forms allow-same-origin allow-popups allow-popups-to-escape-sandbox"
							></iframe>
						{/key}
					</div>
				{/if}
			</div>
		{:else}
			<div class="relative h-full">
				<div
					bind:this={logsContainer}
					class="h-full overflow-auto bg-background p-3 font-mono text-sm text-foreground"
				>
					{#if logEvents.length === 0 && !logsConnected}
						<div class="italic text-muted-foreground">No output yet</div>
					{/if}
					{#each logEvents as event, index (`${event.timestamp}-${index}`)}
						<div
							class={cn(
								"break-all whitespace-pre-wrap",
								event.type === "stderr" && "text-red-400",
								event.type === "exit" && "mt-2 font-semibold text-yellow-400",
								event.type === "error" && "font-semibold text-red-500",
							)}
						>
							{event.data ??
								(event.type === "exit"
									? `Process exited with code ${event.exitCode ?? "unknown"}`
									: (event.error ?? ""))}
						</div>
					{/each}
				</div>
				{#if hasUnreadLogs && !isLogsNearBottom}
					<Button
						class="absolute bottom-4 left-[50%] translate-x-[-50%] rounded-full dark:bg-background dark:hover:bg-muted"
						onclick={scrollLogsToBottom}
						size="icon"
						type="button"
						variant="outline"
					>
						<ArrowDownIcon class="size-4" />
					</Button>
				{/if}
			</div>
		{/if}
	</div>
</DockWindowChrome>
