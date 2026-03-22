<script lang="ts">
	import Loader2Icon from "@lucide/svelte/icons/loader-2";
	import MonitorIcon from "@lucide/svelte/icons/monitor";
	import RotateCcwIcon from "@lucide/svelte/icons/rotate-ccw";
	import { appendAuthToken, getApiRootBase } from "$lib/api-config";
	import DockWindowChrome from "$lib/components/app/parts/DockWindowChrome.svelte";
	import { Button } from "$lib/components/ui/button";
	import { readClipboardText, writeClipboardText } from "$lib/tauri";
	import { cn } from "$lib/utils";

	type Props = {
		dockMaximized: boolean;
		sessionId: string;
		desktopAvailable: boolean;
		onClose: () => void;
		onToggleDockMaximized: () => void;
	};

	type ConnectionStatus = "connecting" | "connected" | "disconnected" | "unavailable";

	type RFBEventMap = {
		connect: CustomEvent;
		disconnect: CustomEvent<{ clean: boolean }>;
		clipboard: CustomEvent<{ text: string }>;
		desktopname: CustomEvent<{ name: string }>;
	};

	type RFBInstance = {
		scaleViewport: boolean;
		resizeSession: boolean;
		background: string;
		disconnect: () => void;
		clipboardPasteFrom: (text: string) => void;
		sendKey: (keysym: number, code: string | null, down?: boolean) => void;
		addEventListener: <K extends keyof RFBEventMap>(
			type: K,
			listener: (event: RFBEventMap[K]) => void,
		) => void;
		removeEventListener: <K extends keyof RFBEventMap>(
			type: K,
			listener: (event: RFBEventMap[K]) => void,
		) => void;
	};

	type RFBConstructor = new (target: HTMLElement, urlOrChannel: string | WebSocket) => RFBInstance;

	let { sessionId, desktopAvailable, dockMaximized, onClose, onToggleDockMaximized }: Props = $props();

	let desktopHost = $state<HTMLDivElement | null>(null);
	let connectionStatus = $state<ConnectionStatus>("connecting");
	let desktopName = $state("");
	let reconnectVersion = $state(0);

	const maximizeTitle = $derived.by(() =>
		dockMaximized ? "Restore split view" : "Maximize desktop panel",
	);
	const statusLabel = $derived.by(() => {
		switch (connectionStatus) {
			case "connected":
				return "Connected";
			case "disconnected":
				return "Disconnected";
			case "unavailable":
				return "Unavailable";
			default:
				return "Connecting";
		}
	});
	const statusDotClass = $derived.by(() =>
		cn(
			"size-2 shrink-0 rounded-full",
			connectionStatus === "connected" && "bg-green-500",
			connectionStatus === "connecting" && "bg-yellow-500",
			connectionStatus === "disconnected" && "bg-red-500",
			connectionStatus === "unavailable" && "bg-sidebar-foreground/30",
		),
	);
	const overlayMessage = $derived.by(() => {
		if (!desktopAvailable) {
			return "Desktop is not available for this session.";
		}

		switch (connectionStatus) {
			case "connected":
				return "";
			case "disconnected":
				return "Desktop disconnected";
			default:
				return "Connecting to desktop...";
		}
	});
	const canReconnect = $derived.by(() => desktopAvailable && connectionStatus === "disconnected");
	const titleLabel = $derived.by(() => desktopName.trim() || statusLabel);

	function getDesktopWsUrl(sessionId: string) {
		const apiRoot = getApiRootBase();
		const parsed = new URL(apiRoot);
		const subdomain = `${sessionId}-svc-discobot-desktop`;
		const protocol = parsed.protocol === "https:" ? "wss:" : "ws:";
		return appendAuthToken(`${protocol}//${subdomain}.${parsed.host}`);
	}

	function clearDesktopHost(host: HTMLDivElement) {
		while (host.firstChild) {
			host.removeChild(host.firstChild);
		}
	}

	function reconnectDesktop() {
		reconnectVersion += 1;
	}

	$effect(() => {
		const host = desktopHost;
		const currentSessionId = sessionId;
		const available = desktopAvailable;
		reconnectVersion;

		if (!host) {
			return;
		}

		clearDesktopHost(host);
		desktopName = "";

		if (!available) {
			connectionStatus = "unavailable";
			return;
		}

		connectionStatus = "connecting";

		let disposed = false;
		let rfb: RFBInstance | null = null;

		const onConnect = () => {
			if (disposed) {
				return;
			}
			connectionStatus = "connected";
		};

		const onDisconnect = () => {
			if (disposed) {
				return;
			}
			connectionStatus = "disconnected";
		};

		const onClipboard = (event: CustomEvent<{ text: string }>) => {
			void writeClipboardText(event.detail.text).catch(() => {});
		};

		const onDesktopName = (event: CustomEvent<{ name: string }>) => {
			if (disposed) {
				return;
			}
			desktopName = event.detail.name;
		};

		const onKeyDown = (event: KeyboardEvent) => {
			if (!rfb) {
				return;
			}
			if ((event.ctrlKey || event.metaKey) && event.key === "v") {
				event.stopPropagation();
				void readClipboardText()
					.then((text) => {
						if (disposed || !rfb) {
							return;
						}
						rfb.clipboardPasteFrom(text);
						rfb.sendKey(0xffe3, "ControlLeft", true);
						rfb.sendKey(0x76, "KeyV", true);
						rfb.sendKey(0x76, "KeyV", false);
						rfb.sendKey(0xffe3, "ControlLeft", false);
					})
					.catch(() => {});
			}
		};

		void import("@novnc/novnc/lib/rfb")
			.then((module) => {
				if (disposed) {
					return;
				}

				const RFB = module.default as unknown as RFBConstructor;
				rfb = new RFB(host, getDesktopWsUrl(currentSessionId));
				rfb.scaleViewport = true;
				rfb.resizeSession = true;
				rfb.background = "rgb(24, 24, 27)";
				rfb.addEventListener("connect", onConnect);
				rfb.addEventListener("disconnect", onDisconnect);
				rfb.addEventListener("clipboard", onClipboard);
				rfb.addEventListener("desktopname", onDesktopName);
				host.addEventListener("keydown", onKeyDown, true);
			})
			.catch((error) => {
				if (disposed) {
					return;
				}
				console.error("Failed to initialize desktop viewer", error);
				connectionStatus = "disconnected";
			});

		return () => {
			disposed = true;
			host.removeEventListener("keydown", onKeyDown, true);
			if (rfb) {
				rfb.removeEventListener("connect", onConnect);
				rfb.removeEventListener("disconnect", onDisconnect);
				rfb.removeEventListener("clipboard", onClipboard);
				rfb.removeEventListener("desktopname", onDesktopName);
				rfb.disconnect();
				rfb = null;
			}
			clearDesktopHost(host);
		};
	});
</script>

<DockWindowChrome
	dockMaximized={dockMaximized}
	onClose={onClose}
	onToggleDockMaximized={onToggleDockMaximized}
	closeLabel="Close desktop panel"
	minimizeLabel="Minimize desktop panel"
	maximizeTitle={maximizeTitle}
	shellClass="min-h-[28rem]"
>
	{#snippet title()}
		<div class="flex min-w-0 items-center gap-2 text-xs">
			<p class="truncate text-sm font-medium">Desktop</p>
			<span class="truncate text-sidebar-foreground/70">{titleLabel}</span>
			<div class={statusDotClass} title={statusLabel}></div>
		</div>
	{/snippet}

	<div class="relative h-full min-h-0 overflow-hidden p-3">
		{#if connectionStatus !== "connected"}
			<div class="absolute inset-3 z-10 flex items-center justify-center rounded-md bg-black/35">
				<div class="flex max-w-xs flex-col items-center gap-3 px-4 text-center">
					{#if connectionStatus === "connecting"}
						<Loader2Icon class="size-8 animate-spin text-white/70" />
					{:else}
						<MonitorIcon class="size-8 text-white/70" />
					{/if}
					<span class="text-xs text-white/70">{overlayMessage}</span>
					{#if canReconnect}
						<Button variant="outline" size="xs" onclick={reconnectDesktop} class="gap-2">
							<RotateCcwIcon class="size-3.5" />
							Reconnect
						</Button>
					{/if}
				</div>
			</div>
		{/if}

		<div
			bind:this={desktopHost}
			class="h-full w-full overflow-hidden rounded-md border border-white/10 bg-zinc-900"
		></div>
	</div>
</DockWindowChrome>
