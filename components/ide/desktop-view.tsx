import RFB from "@novnc/novnc/lib/rfb";
import { Loader2, Monitor } from "lucide-react";
import * as React from "react";
import { getApiRootBase, isTauri } from "@/lib/api-config";
import { readClipboardText } from "@/lib/tauri";
import { cn } from "@/lib/utils";

type ConnectionStatus = "connecting" | "connected" | "disconnected";

interface DesktopViewProps {
	sessionId: string;
	className?: string;
}

/**
 * Build the WebSocket URL for the desktop VNC service.
 * Uses the same subdomain proxy pattern as other services:
 * ws://{sessionId}-svc-desktop.{host}/
 */
function getDesktopWsUrl(sessionId: string): string {
	const apiRoot = getApiRootBase();
	const parsed = new URL(apiRoot);
	const subdomain = `${sessionId}-svc-discobot-desktop`;
	const protocol = parsed.protocol === "https:" ? "wss:" : "ws:";
	return `${protocol}//${subdomain}.${parsed.host}`;
}

/**
 * DesktopView renders a VNC viewer using noVNC that connects to the
 * container's virtual X11 display via websockify.
 */
export function DesktopView({ sessionId, className }: DesktopViewProps) {
	const containerRef = React.useRef<HTMLDivElement>(null);
	const rfbRef = React.useRef<RFB | null>(null);
	const [status, setStatus] = React.useState<ConnectionStatus>("connecting");

	React.useEffect(() => {
		const container = containerRef.current;
		if (!container) return;

		const wsUrl = getDesktopWsUrl(sessionId);

		const rfb = new RFB(container, wsUrl);
		rfb.scaleViewport = true;
		rfb.resizeSession = true;
		rfb.background = "rgb(24, 24, 27)";
		rfbRef.current = rfb;

		const onConnect = () => setStatus("connected");
		const onDisconnect = () => setStatus("disconnected");

		// Remote → local clipboard: write remote clipboard to browser clipboard
		const onClipboard = (e: CustomEvent<{ text: string }>) => {
			navigator.clipboard.writeText(e.detail.text).catch(() => {});
		};

		// Local → remote clipboard: intercept Ctrl/Cmd+V in capture phase so we
		// can read the host clipboard before the event reaches the noVNC canvas.
		// Canvas elements never fire paste events, so a paste listener doesn't work.
		// Capture phase (third arg = true) lets us stopPropagation before noVNC sees
		// the keystroke, then we sync the clipboard and re-send Ctrl+V manually.
		const onKeyDown = (e: KeyboardEvent) => {
			if ((e.ctrlKey || e.metaKey) && e.key === "v") {
				e.stopPropagation();
				(isTauri() ? readClipboardText() : navigator.clipboard.readText())
					.then((text) => {
						rfb.clipboardPasteFrom(text);
						// Send Ctrl+V to the Linux remote to trigger the paste now
						// that the remote clipboard is up to date.
						rfb.sendKey(0xffe3, "ControlLeft", true);
						rfb.sendKey(0x76, "KeyV", true);
						rfb.sendKey(0x76, "KeyV", false);
						rfb.sendKey(0xffe3, "ControlLeft", false);
					})
					.catch(() => {});
			}
		};

		rfb.addEventListener("connect", onConnect);
		rfb.addEventListener("disconnect", onDisconnect);
		rfb.addEventListener("clipboard", onClipboard);
		container.addEventListener("keydown", onKeyDown, true);

		return () => {
			rfb.removeEventListener("connect", onConnect);
			rfb.removeEventListener("disconnect", onDisconnect);
			rfb.removeEventListener("clipboard", onClipboard);
			container.removeEventListener("keydown", onKeyDown, true);
			rfb.disconnect();
			rfbRef.current = null;
		};
	}, [sessionId]);

	const handleReconnect = React.useCallback(() => {
		const container = containerRef.current;
		if (!container) return;

		// Disconnect existing connection
		if (rfbRef.current) {
			rfbRef.current.disconnect();
			rfbRef.current = null;
		}

		// Clear the container (RFB adds a canvas)
		while (container.firstChild) {
			container.removeChild(container.firstChild);
		}

		setStatus("connecting");

		const wsUrl = getDesktopWsUrl(sessionId);
		const rfb = new RFB(container, wsUrl);
		rfb.scaleViewport = true;
		rfb.resizeSession = true;
		rfb.background = "rgb(24, 24, 27)";
		rfbRef.current = rfb;

		const onConnect = () => setStatus("connected");
		const onDisconnect = () => setStatus("disconnected");

		const onClipboard = (e: CustomEvent<{ text: string }>) => {
			navigator.clipboard.writeText(e.detail.text).catch(() => {});
		};

		const onKeyDown = (e: KeyboardEvent) => {
			if ((e.ctrlKey || e.metaKey) && e.key === "v") {
				e.stopPropagation();
				(isTauri() ? readClipboardText() : navigator.clipboard.readText())
					.then((text) => {
						rfb.clipboardPasteFrom(text);
						rfb.sendKey(0xffe3, "ControlLeft", true);
						rfb.sendKey(0x76, "KeyV", true);
						rfb.sendKey(0x76, "KeyV", false);
						rfb.sendKey(0xffe3, "ControlLeft", false);
					})
					.catch(() => {});
			}
		};

		rfb.addEventListener("connect", onConnect);
		rfb.addEventListener("disconnect", onDisconnect);
		rfb.addEventListener("clipboard", onClipboard);
		container.addEventListener("keydown", onKeyDown, true);
	}, [sessionId]);

	return (
		<div className={cn("flex flex-col h-full bg-zinc-900", className)}>
			{/* VNC canvas container */}
			<div ref={containerRef} className="flex-1 relative min-h-0" />

			{/* Overlay for non-connected states */}
			{status !== "connected" && (
				<div className="absolute inset-0 flex items-center justify-center bg-zinc-900/90">
					<div className="flex flex-col items-center gap-3 text-muted-foreground">
						{status === "connecting" ? (
							<>
								<Loader2 className="h-8 w-8 animate-spin" />
								<span className="text-sm">Connecting to desktop...</span>
							</>
						) : (
							<>
								<Monitor className="h-8 w-8" />
								<span className="text-sm">Desktop disconnected</span>
								<button
									type="button"
									onClick={handleReconnect}
									className="text-xs text-primary hover:underline"
								>
									Reconnect
								</button>
							</>
						)}
					</div>
				</div>
			)}
		</div>
	);
}
