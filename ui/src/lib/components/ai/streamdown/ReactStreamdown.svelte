<script lang="ts">
	import { onMount } from "svelte";
	import { createElement, useState } from "react";
	import { createRoot, type Root } from "react-dom/client";
	import type { LinkSafetyModalProps, PluginConfig } from "streamdown";
	import { openUrl } from "$lib/tauri";

	type Props = {
		text: string;
		class?: string;
		plugins?: PluginConfig;
		mode?: "static" | "streaming";
		isAnimating?: boolean;
		animated?: boolean;
	};

	type StreamdownComponent = typeof import("streamdown").Streamdown;

	let {
		text,
		class: className,
		plugins,
		mode = "streaming",
		isAnimating = false,
		animated = false,
	}: Props = $props();

	let host = $state<HTMLDivElement | null>(null);
	let root = $state<Root | null>(null);
	let Streamdown = $state<StreamdownComponent | null>(null);

	function StreamdownLinkSafetyModal({
		url,
		isOpen,
		onClose,
	}: LinkSafetyModalProps) {
		const [copied, setCopied] = useState(false);

		if (!isOpen) {
			return null;
		}

		const handleCopy = async () => {
			try {
				await navigator.clipboard.writeText(url);
				setCopied(true);
				setTimeout(() => setCopied(false), 1500);
			} catch {
				// noop
			}
		};

		const handleOpen = () => {
			void openUrl(url);
			onClose();
		};

		return createElement(
			"div",
			{
				className: "fixed inset-0 z-50 flex items-center justify-center bg-black/50",
			},
			createElement(
				"div",
				{
					className:
						"mx-4 w-full max-w-md rounded-xl border bg-background p-6 shadow-lg",
				},
				createElement("h3", { className: "font-semibold text-base" }, "Open external link?"),
				createElement(
					"p",
					{ className: "mt-1 text-muted-foreground text-sm" },
					"You are about to open an external URL.",
				),
				createElement(
					"div",
					{
						className:
							"mt-4 break-all rounded-md border bg-muted/40 px-3 py-2 font-mono text-xs",
					},
					url,
				),
				createElement(
					"div",
					{ className: "mt-4 flex justify-end gap-2" },
					createElement(
						"button",
						{
							type: "button",
							onClick: onClose,
							className:
								"inline-flex items-center justify-center rounded-md border px-3 py-1.5 text-sm",
						},
						"Cancel",
					),
					createElement(
						"button",
						{
							type: "button",
							onClick: handleCopy,
							className:
								"inline-flex items-center justify-center rounded-md border px-3 py-1.5 text-sm",
						},
						copied ? "Copied" : "Copy",
					),
					createElement(
						"button",
						{
							type: "button",
							onClick: handleOpen,
							className:
								"inline-flex items-center justify-center rounded-md bg-primary px-3 py-1.5 text-primary-foreground text-sm",
						},
						"Open link",
					),
				),
			),
		);
	}

	const streamdownProps = $derived.by(() => ({
		className,
		plugins,
		mode,
		isAnimating,
		animated,
		linkSafety: {
			enabled: true,
			renderModal: (modalProps: LinkSafetyModalProps) =>
				createElement(StreamdownLinkSafetyModal, modalProps),
		},
	}));

	onMount(() => {
		if (!host) {
			return;
		}

		let cancelled = false;
		root = createRoot(host);
		void import("streamdown").then((module) => {
			if (cancelled) {
				return;
			}
			Streamdown = module.Streamdown;
		});

		return () => {
			cancelled = true;
			root?.unmount();
			root = null;
			Streamdown = null;
		};
	});

	$effect(() => {
		if (!root || !Streamdown) {
			return;
		}
		root.render(createElement(Streamdown, streamdownProps, text));
	});
</script>

<div bind:this={host}></div>
