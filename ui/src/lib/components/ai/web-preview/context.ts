import { getContext, setContext } from "svelte";

export type WebPreviewViewport = "desktop" | "tablet" | "mobile";

const WEB_PREVIEW_CONTEXT_KEY = Symbol.for(
	"discobot-ui-ai-web-preview-context",
);

export type WebPreviewContextValue = {
	url: string;
	setUrl: (url: string) => void;
	consoleOpen: boolean;
	setConsoleOpen: (open: boolean) => void;
	viewport: WebPreviewViewport;
	setViewport: (viewport: WebPreviewViewport) => void;
};

export function setWebPreviewContext(
	value: WebPreviewContextValue,
): WebPreviewContextValue {
	return setContext(WEB_PREVIEW_CONTEXT_KEY, value);
}

export function useWebPreviewContext(): WebPreviewContextValue {
	const context = getContext<WebPreviewContextValue | undefined>(
		WEB_PREVIEW_CONTEXT_KEY,
	);
	if (!context) {
		throw new Error("WebPreview components must be used within WebPreview");
	}
	return context;
}
