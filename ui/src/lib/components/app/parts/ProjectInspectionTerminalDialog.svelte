<script lang="ts">
	import RotateCcwIcon from "@lucide/svelte/icons/rotate-ccw";
	import TerminalIcon from "@lucide/svelte/icons/terminal";
	import type {
		ILink,
		ILinkProvider,
		ITheme,
		Terminal as GhosttyTerminal,
	} from "ghostty-web";
	import { appendAuthToken, getWsBase } from "$lib/api-config";
	import * as Dialog from "$lib/components/ui/dialog";
	import { Button } from "$lib/components/ui/button";
	import { openUrl } from "$lib/shell";

	type ConnectionStatus = "disconnected" | "connecting" | "connected" | "error";

	type Props = {
		open: boolean;
		onOpenChange: (open: boolean) => void;
		projectId: string;
		title?: string;
		description?: string;
	};

	const MIN_TERMINAL_ROWS = 20;
	const MIN_TERMINAL_COLS = 80;
	const RESIZE_DEBOUNCE_MS = 150;

	let {
		open,
		onOpenChange,
		projectId,
		title = "Inspection shell",
		description = "Open a troubleshooting shell in the inspection container.",
	}: Props = $props();

	let terminalHost = $state<HTMLDivElement | null>(null);
	let connectionStatus = $state<ConnectionStatus>("disconnected");
	let terminalReady = $state(false);

	let terminal: GhosttyTerminal | null = null;
	let socket: WebSocket | null = null;
	let resizeTimeout: ReturnType<typeof setTimeout> | null = null;
	let dataSubscription: { dispose(): void } | null = null;
	let resizeSubscription: { dispose(): void } | null = null;
	let windowResizeHandler: (() => void) | null = null;
	let lastSize: { rows: number; cols: number } | null = null;

	const statusClass = $derived.by(() => {
		if (connectionStatus === "connected") return "bg-green-500";
		if (connectionStatus === "connecting") return "bg-yellow-500";
		if (connectionStatus === "error") return "bg-red-500";
		return "bg-muted-foreground/50";
	});

	const overlayMessage = $derived.by(() => {
		if (connectionStatus === "connecting") {
			return "Connecting…";
		}
		if (connectionStatus === "error") {
			return "Connection error — retry to reconnect to the inspection shell.";
		}
		if (connectionStatus === "disconnected") {
			return "Disconnected";
		}
		return "";
	});

	function readThemeValue(
		style: CSSStyleDeclaration,
		property: string,
		fallback: string,
	): string {
		const value = style.getPropertyValue(property).trim();
		return value.length > 0 ? value : fallback;
	}

	function getTerminalTheme(): ITheme {
		if (typeof window === "undefined") {
			return {
				background: "oklch(0.08 0 0)",
				foreground: "oklch(0.75 0.15 145)",
			};
		}

		const style = window.getComputedStyle(document.documentElement);
		const background = readThemeValue(
			style,
			"--terminal-bg",
			"oklch(0.08 0 0)",
		);
		const foreground = readThemeValue(
			style,
			"--terminal-fg",
			"oklch(0.75 0.15 145)",
		);
		const selectionBackground = readThemeValue(
			style,
			"--tree-selected",
			"oklch(0.65 0.15 250 / 0.2)",
		);

		return {
			background,
			foreground,
			cursor: foreground,
			cursorAccent: background,
			selectionBackground,
			selectionForeground: foreground,
			black: "#1e1e2e",
			red: "#f38ba8",
			green: "#a6e3a1",
			yellow: "#f9e2af",
			blue: "#89b4fa",
			magenta: "#cba6f7",
			cyan: "#94e2d5",
			white: "#cdd6f4",
			brightBlack: "#585b70",
			brightRed: "#f38ba8",
			brightGreen: "#a6e3a1",
			brightYellow: "#f9e2af",
			brightBlue: "#89b4fa",
			brightMagenta: "#cba6f7",
			brightCyan: "#94e2d5",
			brightWhite: "#a6adc8",
		};
	}

	function updateConnectionStatus(status: ConnectionStatus) {
		connectionStatus = status;
	}

	function clearResizeTimer() {
		if (resizeTimeout) {
			clearTimeout(resizeTimeout);
			resizeTimeout = null;
		}
	}

	function closeSocket() {
		if (socket) {
			socket.close();
			socket = null;
		}
	}

	function sendInput(data: string) {
		if (socket?.readyState !== WebSocket.OPEN) {
			return;
		}
		socket.send(JSON.stringify({ type: "input", data }));
	}

	function sendResize(rows: number, cols: number) {
		if (rows < MIN_TERMINAL_ROWS || cols < MIN_TERMINAL_COLS) {
			return;
		}
		if (lastSize?.rows === rows && lastSize?.cols === cols) {
			return;
		}

		clearResizeTimer();
		resizeTimeout = setTimeout(() => {
			if (lastSize?.rows === rows && lastSize?.cols === cols) {
				return;
			}

			lastSize = { rows, cols };
			if (socket?.readyState !== WebSocket.OPEN) {
				return;
			}

			socket.send(JSON.stringify({ type: "resize", data: { rows, cols } }));
		}, RESIZE_DEBOUNCE_MS);
	}

	function connect(nextProjectId: string) {
		if (!terminal) {
			return;
		}

		closeSocket();
		clearResizeTimer();
		updateConnectionStatus("connecting");
		const rows = terminal.rows;
		const cols = terminal.cols;
		const projectWsBase = getWsBase().replace(
			/\/projects\/[^/]+$/,
			`/projects/${encodeURIComponent(nextProjectId)}`,
		);
		const wsUrl = appendAuthToken(
			`${projectWsBase}/inspection/terminal/ws?rows=${rows}&cols=${cols}`,
		);
		const nextSocket = new WebSocket(wsUrl);
		socket = nextSocket;

		nextSocket.onopen = () => {
			if (socket !== nextSocket) {
				return;
			}
			updateConnectionStatus("connected");
		};

		nextSocket.onmessage = (event) => {
			if (socket !== nextSocket) {
				return;
			}

			try {
				const message = JSON.parse(event.data as string) as {
					type?: string;
					data?: string;
				};
				if (message.type === "output" && typeof message.data === "string") {
					terminal?.write(message.data);
					return;
				}
				if (message.type === "error" && typeof message.data === "string") {
					terminal?.writeln(`\x1b[31mError: ${message.data}\x1b[0m`);
				}
			} catch {
				if (typeof event.data === "string") {
					terminal?.write(event.data);
				}
			}
		};

		nextSocket.onerror = () => {
			if (socket !== nextSocket) {
				return;
			}
			updateConnectionStatus("error");
		};

		nextSocket.onclose = (event) => {
			if (socket !== nextSocket) {
				return;
			}
			socket = null;
			updateConnectionStatus(event.wasClean ? "disconnected" : "error");
		};
	}

	function reconnectTerminal() {
		if (!terminalReady || !terminal) {
			return;
		}
		terminal.clear();
		lastSize = null;
		connect(projectId);
	}

	$effect(() => {
		if (!open || !terminalHost) {
			return;
		}

		let cancelled = false;
		void (async () => {
			const { FitAddon, OSC8LinkProvider, Terminal, UrlRegexProvider, init } =
				await import("ghostty-web");
			await init();

			const createOpenUrlLinkProvider = (
				provider: ILinkProvider,
			): ILinkProvider => ({
				provideLinks: (y, callback) => {
					provider.provideLinks(y, (links) => {
						callback(
							links?.map(
								(link): ILink => ({
									...link,
									activate: (event: MouseEvent) => {
										event.preventDefault();
										void openUrl(link.text);
									},
								}),
							),
						);
					});
				},
				dispose: () => {
					provider.dispose?.();
				},
			});

			if (cancelled || !terminalHost || terminal) {
				return;
			}

			const nextTerminal = new Terminal({
				cursorBlink: true,
				cursorStyle: "block",
				fontFamily:
					'"JetBrains Mono", "Fira Code", "SF Mono", Monaco, "Cascadia Code", "Roboto Mono", Consolas, "Liberation Mono", Menlo, Courier, monospace',
				fontSize: 13,
				scrollback: 5000,
				theme: getTerminalTheme(),
			});
			const nextFitAddon = new FitAddon();

			nextTerminal.loadAddon(nextFitAddon);
			nextTerminal.open(terminalHost);
			nextTerminal.registerLinkProvider(
				createOpenUrlLinkProvider(new OSC8LinkProvider(nextTerminal)),
			);
			nextTerminal.registerLinkProvider(
				createOpenUrlLinkProvider(new UrlRegexProvider(nextTerminal)),
			);
			nextFitAddon.fit();
			nextFitAddon.observeResize();

			terminal = nextTerminal;
			dataSubscription = nextTerminal.onData((data) => {
				sendInput(data);
			});
			resizeSubscription = nextTerminal.onResize(({ cols, rows }) => {
				sendResize(rows, cols);
			});
			windowResizeHandler = () => {
				try {
					nextFitAddon.fit();
				} catch {
					// Ignore fit errors during rapid resizing.
				}
			};
			window.addEventListener("resize", windowResizeHandler);
			nextTerminal.focus();
			terminalReady = true;
		})();

		return () => {
			cancelled = true;
			terminalReady = false;
			lastSize = null;
			updateConnectionStatus("disconnected");
			clearResizeTimer();
			closeSocket();
			dataSubscription?.dispose();
			dataSubscription = null;
			resizeSubscription?.dispose();
			resizeSubscription = null;
			if (windowResizeHandler) {
				window.removeEventListener("resize", windowResizeHandler);
				windowResizeHandler = null;
			}
			terminal?.dispose();
			terminal = null;
		};
	});

	$effect(() => {
		const nextProjectId = projectId;
		const nextOpen = open;
		if (!nextOpen || !terminalReady || !terminal) {
			return;
		}

		terminal.clear();
		lastSize = null;
		connect(nextProjectId);

		return () => {
			clearResizeTimer();
			closeSocket();
		};
	});
</script>

<Dialog.Root {open} {onOpenChange}>
	<Dialog.Content
		class="fixed inset-x-0 bottom-0 top-10 z-50 flex h-auto w-screen max-w-none translate-x-0 translate-y-0 flex-col overflow-hidden rounded-none border-0 p-0 shadow-none sm:max-w-none"
	>
		<div
			class="flex items-center justify-between border-b border-border px-5 py-4"
		>
			<div class="min-w-0">
				<Dialog.Title class="flex items-center gap-2 text-sm">
					<TerminalIcon class="size-4" />
					<span class="truncate">{title}</span>
				</Dialog.Title>
				<Dialog.Description class="mt-1 text-xs">
					{description}
				</Dialog.Description>
			</div>
			<div class="flex items-center gap-2 pr-8 text-xs text-muted-foreground">
				<div class={`size-2 rounded-full ${statusClass}`}></div>
				<span class="capitalize">{connectionStatus}</span>
			</div>
		</div>

		<div class="relative min-h-0 flex-1 overflow-hidden p-4">
			{#if connectionStatus !== "connected"}
				<div
					class="absolute inset-4 z-10 flex items-center justify-center rounded-md bg-black/35"
				>
					<div class="flex flex-col items-center gap-3 text-center">
						<span class="text-xs text-white/70">{overlayMessage}</span>
						{#if connectionStatus !== "connecting"}
							<Button
								variant="outline"
								size="xs"
								onclick={reconnectTerminal}
								class="gap-2"
							>
								<RotateCcwIcon class="size-3.5" />
								Reconnect
							</Button>
						{/if}
					</div>
				</div>
			{/if}

			{#if open}
				<div
					bind:this={terminalHost}
					class="h-full w-full cursor-text overflow-hidden rounded-md border border-white/10 bg-terminal-bg p-3 outline-none [caret-color:transparent]"
				></div>
			{/if}
		</div>
	</Dialog.Content>
</Dialog.Root>
