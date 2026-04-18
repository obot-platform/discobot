export type DesktopRuntimeKind = "browser" | "tauri";

export type DesktopServerConfig = {
	port: number;
	secret: string;
};

export type DownloadFileOptions = {
	filename: string;
	content: string | Uint8Array | ArrayBuffer;
	mimeType?: string;
};

export type DesktopWindow = {
	minimize: () => Promise<void>;
	maximize: () => Promise<void>;
	unmaximize: () => Promise<void>;
	isMaximized: () => Promise<boolean>;
	close: () => Promise<void>;
	isFullscreen: () => Promise<boolean>;
	onResized: (listener: () => void) => Promise<() => void>;
};

export type DesktopWindowCallback<T> = (
	window: DesktopWindow,
) => T | Promise<T>;

export type DesktopDownloadEvent =
	| {
			event: "Started";
			data: {
				contentLength?: number;
			};
	  }
	| {
			event: "Progress";
			data: {
				chunkLength: number;
			};
	  }
	| {
			event: "Finished";
	  };

export type DesktopUpdateMetadata = {
	rid: number;
	currentVersion: string;
	version: string;
	date?: string;
	body?: string;
	rawJson: Record<string, unknown>;
};
