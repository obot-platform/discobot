<script lang="ts">
	import { cn } from "$lib/utils";
	import {
		type WebPreviewViewport,
		setWebPreviewContext,
	} from "./context";

	type Props = {
		defaultUrl?: string;
		onUrlChange?: (url: string) => void;
		class?: string;
		children?: () => any;
	};

	let {
		defaultUrl = "",
		onUrlChange,
		class: className,
		children,
		...restProps
	}: Props = $props();

	let url = $state("");
	let consoleOpen = $state(false);
	let viewport = $state<WebPreviewViewport>("desktop");
	let initialized = $state(false);

	const webPreview = $state({
		url: "",
		setUrl: (nextUrl: string) => {
			url = nextUrl;
			onUrlChange?.(nextUrl);
		},
		consoleOpen: false,
		setConsoleOpen: (nextOpen: boolean) => {
			consoleOpen = nextOpen;
		},
		viewport: "desktop" as WebPreviewViewport,
		setViewport: (nextViewport: WebPreviewViewport) => {
			viewport = nextViewport;
		},
	});

	$effect(() => {
		if (!initialized) {
			url = defaultUrl;
			initialized = true;
		}
	});

	$effect(() => {
		webPreview.url = url;
		webPreview.consoleOpen = consoleOpen;
		webPreview.viewport = viewport;
	});

	setWebPreviewContext(webPreview);
</script>

<div
	class={cn("flex size-full flex-col rounded-lg border bg-card", className)}
	{...restProps}
>
	{@render children?.()}
</div>
