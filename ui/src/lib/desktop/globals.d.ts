import type { DesktopRendererBridge } from "$lib/desktop/types";

declare global {
	interface Window {
		__DISCOBOT_DESKTOP__?: DesktopRendererBridge;
	}
}

export {};
