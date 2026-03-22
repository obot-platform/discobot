<script lang="ts">
	import CheckIcon from "@lucide/svelte/icons/check";
	import CopyIcon from "@lucide/svelte/icons/copy";
	import RotateCcwIcon from "@lucide/svelte/icons/rotate-ccw";
	import { appendAuthToken, getSSHPort, getWsBase } from "$lib/api-config";
	import { openUrl, writeClipboardText } from "$lib/tauri";
	import DockWindowChrome from "$lib/components/app/parts/DockWindowChrome.svelte";
	import { Button } from "$lib/components/ui/button";
	import { Switch } from "$lib/components/ui/switch";
	import type {
		ILink,
		ILinkProvider,
		ITheme,
		Terminal as GhosttyTerminal,
	} from "ghostty-web";

	type ConnectionStatus = "disconnected" | "connecting" | "connected" | "error";

	type Props = {
		dockMaximized: boolean;
		onClose: () => void;
		onRootEnabledChange: (value: boolean) => void;
		onToggleDockMaximized: () => void;
		rootEnabled: boolean;
		sessionId: string | null;
	};

	const MIN_TERMINAL_ROWS = 20;
	const MIN_TERMINAL_COLS = 80;
	const RESIZE_DEBOUNCE_MS = 150;
	const COPY_RESET_MS = 2000;

	let { dockMaximized, onClose, onRootEnabledChange, onToggleDockMaximized, rootEnabled, sessionId }: Props = $props();

	let terminalHost = $state<HTMLDivElement | null>(null);
	let connectionStatus = $state<ConnectionStatus>("disconnected");
	let copied = $state(false);
	let terminalReady = $state(false);

	let terminal: GhosttyTerminal | null = null;
	let socket: WebSocket | null = null;
	let resizeTimeout: ReturnType<typeof setTimeout> | null = null;
	let copyTimeout: ReturnType<typeof setTimeout> | null = null;
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

	const sessionLabel = $derived.by(() => {
		if (!sessionId) return "No session";
		return `Session: ${sessionId.slice(0, 8)}...`;
	});

	const sshCommand = $derived.by(() => {
		if (!sessionId) return null;
		return `ssh -p ${getSSHPort()} ${sessionId}@${getSSHHost()}`;
	});

	const overlayMessage = $derived.by(() => {
		if (!sessionId) {
			return "No session selected.";
		}
		if (connectionStatus === "connecting") {
			return "Connecting…";
		}
		if (connectionStatus === "error") {
			return "Connection error — reopen the terminal panel to retry.";
		}
		if (connectionStatus === "disconnected") {
			return "Disconnected";
		}
		return "";
	});

	const maximizeTitle = $derived.by(() => (dockMaximized ? "Restore split view" : "Maximize terminal panel"));

	function getSSHHost(): string {
		if (typeof window === "undefined") return "localhost";
		const { hostname } = window.location;
		if (hostname === "127.0.0.1" || hostname === "::1") {
			return "localhost";
		}
		return hostname;
	}

	function readThemeValue(style: CSSStyleDeclaration, property: string, fallback: string): string {
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
		const background = readThemeValue(style, "--terminal-bg", "oklch(0.08 0 0)");
		const foreground = readThemeValue(style, "--terminal-fg", "oklch(0.75 0.15 145)");
		const selectionBackground = readThemeValue(style, "--tree-selected", "oklch(0.65 0.15 250 / 0.2)");

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

	function clearCopyTimer() {
		if (copyTimeout) {
			clearTimeout(copyTimeout);
			copyTimeout = null;
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

			socket.send(
				JSON.stringify({
					type: "resize",
					data: { rows, cols },
				}),
			);
		}, RESIZE_DEBOUNCE_MS);
	}

	function connect(nextSessionId: string | null, nextRootEnabled: boolean) {
		if (!terminal) {
			return;
		}

		closeSocket();
		clearResizeTimer();

		if (!nextSessionId) {
			updateConnectionStatus("disconnected");
			terminal.writeln("\x1b[33mNo session selected. Select a session to connect to the terminal.\x1b[0m");
			return;
		}

		updateConnectionStatus("connecting");
		const rows = terminal.rows;
		const cols = terminal.cols;
		const rootParam = nextRootEnabled ? "&root=true" : "";
		const wsUrl = appendAuthToken(
			`${getWsBase()}/sessions/${nextSessionId}/terminal/ws?rows=${rows}&cols=${cols}${rootParam}`,
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

	function getTerminalCopyShortcut(event: KeyboardEvent): "ctrl-shift" | "meta" | null {
		const key = event.key.toLowerCase();
		if (key !== "c" || event.altKey) {
			return null;
		}

		if (event.ctrlKey && event.shiftKey && !event.metaKey) {
			return "ctrl-shift";
		}

		if (event.metaKey && !event.ctrlKey && !event.shiftKey) {
			return "meta";
		}

		return null;
	}

	function copyTerminalSelection() {
		const selection = terminal?.getSelection() ?? "";
		if (selection.length === 0) {
			return;
		}

		void writeClipboardText(selection);
	}

	async function copySshCommand() {
		if (!sshCommand) {
			return;
		}

		await writeClipboardText(sshCommand);
		copied = true;
		clearCopyTimer();
		copyTimeout = setTimeout(() => {
			copied = false;
		}, COPY_RESET_MS);
	}

	function reconnectTerminal() {
		if (!terminalReady || !terminal) {
			return;
		}

		terminal.clear();
		lastSize = null;
		connect(sessionId, rootEnabled);
	}

	$effect(() => {
		if (!terminalHost) {
			return;
		}

		let cancelled = false;
		void (async () => {
			const { FitAddon, OSC8LinkProvider, Terminal, UrlRegexProvider, init } = await import("ghostty-web");
			await init();

			const createOpenUrlLinkProvider = (provider: ILinkProvider): ILinkProvider => ({
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
			nextTerminal.attachCustomKeyEventHandler((event) => {
				const shortcut = getTerminalCopyShortcut(event);
				if (!shortcut) {
					return false;
				}

				if (shortcut === "ctrl-shift") {
					event.preventDefault();
					event.stopPropagation();
					if (nextTerminal.hasSelection()) {
						copyTerminalSelection();
					}
					return true;
				}

				if (!nextTerminal.hasSelection()) {
					return false;
				}

				event.preventDefault();
				event.stopPropagation();
				copyTerminalSelection();
				return true;
			});
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
			copied = false;
			lastSize = null;
			updateConnectionStatus("disconnected");
			clearResizeTimer();
			clearCopyTimer();
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
		const nextSessionId = sessionId;
		const nextRootEnabled = rootEnabled;
		if (!terminalReady || !terminal) {
			return;
		}

		terminal.clear();
		lastSize = null;
		connect(nextSessionId, nextRootEnabled);

		return () => {
			clearResizeTimer();
			closeSocket();
		};
	});
</script>

<DockWindowChrome
	dockMaximized={dockMaximized}
	onClose={onClose}
	onToggleDockMaximized={onToggleDockMaximized}
	closeLabel="Close terminal panel"
	minimizeLabel="Minimize terminal panel"
	maximizeTitle={maximizeTitle}
	shellClass="min-h-[28rem]"
>
	{#snippet title()}
		<span class="truncate text-xs text-sidebar-foreground/70">{sessionLabel}</span>
		<div class={`size-2 shrink-0 rounded-full ${statusClass}`} title={connectionStatus}></div>
	{/snippet}

	{#snippet actions()}
		<label class="flex items-center gap-2 text-xs text-sidebar-foreground/70">
			<span>root</span>
			<Switch checked={rootEnabled} disabled={!sessionId} onCheckedChange={(checked) => onRootEnabledChange(checked === true)} />
		</label>
		{#if sshCommand}
			<Button
				variant="ghost"
				size="xs"
				onclick={copySshCommand}
				class="gap-2 text-sidebar-foreground/70 hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"
				title={`Copy SSH command: ${sshCommand}`}
			>
				{#if copied}
					<CheckIcon class="size-3.5" />
				{:else}
					<CopyIcon class="size-3.5" />
				{/if}
				<span class="hidden sm:inline">{copied ? "Copied!" : "Copy SSH"}</span>
			</Button>
		{/if}
	{/snippet}

	<div class="relative h-full min-h-0 overflow-hidden p-3">
		{#if connectionStatus !== "connected"}
			<div class="absolute inset-3 z-10 flex items-center justify-center rounded-md bg-black/35">
				<div class="flex flex-col items-center gap-3 text-center">
					<span class="text-xs text-white/70">{overlayMessage}</span>
					{#if connectionStatus === "disconnected" && sessionId}
						<Button variant="outline" size="xs" onclick={reconnectTerminal} class="gap-2">
							<RotateCcwIcon class="size-3.5" />
							Reconnect
						</Button>
					{/if}
				</div>
			</div>
		{/if}

		<div
			bind:this={terminalHost}
			class="h-full w-full cursor-text overflow-hidden rounded-md border border-white/10 bg-terminal-bg p-3 outline-none [caret-color:transparent]"
		></div>
	</div>
</DockWindowChrome>
