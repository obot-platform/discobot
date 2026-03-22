declare module "@novnc/novnc/lib/rfb" {
	interface RFBOptions {
		shared?: boolean;
		credentials?: { password?: string; username?: string; target?: string };
		wsProtocols?: string[];
	}

	interface RFBEventMap {
		connect: CustomEvent;
		disconnect: CustomEvent<{ clean: boolean }>;
		credentialsrequired: CustomEvent<{ types: string[] }>;
		clipboard: CustomEvent<{ text: string }>;
		bell: CustomEvent;
		desktopname: CustomEvent<{ name: string }>;
		capabilities: CustomEvent<{ capabilities: Record<string, boolean> }>;
	}

	class RFB {
		constructor(
			target: HTMLElement,
			urlOrChannel: string | WebSocket,
			options?: RFBOptions,
		);

		scaleViewport: boolean;
		resizeSession: boolean;
		clipViewport: boolean;
		showDotCursor: boolean;
		background: string;
		qualityLevel: number;
		compressionLevel: number;
		viewOnly: boolean;
		focusOnClick: boolean;

		readonly capabilities: Record<string, boolean>;

		disconnect(): void;
		sendCredentials(credentials: {
			password?: string;
			username?: string;
			target?: string;
		}): void;
		sendKey(keysym: number, code: string | null, down?: boolean): void;
		sendCtrlAltDel(): void;
		focus(): void;
		blur(): void;
		machineShutdown(): void;
		machineReboot(): void;
		machineReset(): void;
		clipboardPasteFrom(text: string): void;

		addEventListener<K extends keyof RFBEventMap>(
			type: K,
			listener: (ev: RFBEventMap[K]) => void,
		): void;
		removeEventListener<K extends keyof RFBEventMap>(
			type: K,
			listener: (ev: RFBEventMap[K]) => void,
		): void;
	}

	export default RFB;
}
