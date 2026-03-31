import type {
	Element,
	Node as HastNode,
	Properties,
	Root,
	RootContent,
	Text,
} from "hast";
import { downloadFile } from "$lib/tauri";
import { cn } from "$lib/utils";
import type { HighlightResult, RenderMarkdownOptions } from "./types";

const languagePattern = /language-([^\s]+)/;
const startLinePattern = /startLine=(\d+)/;
const noLineNumbersPattern = /\bnoLineNumbers\b/;
const fileExtensionPattern = /\.[^/.]+$/;

function isElement(node: RootContent | HastNode): node is Element {
	return node.type === "element";
}

function toClassName(value: unknown): string | undefined {
	if (Array.isArray(value)) {
		return value.filter(Boolean).join(" ");
	}

	if (typeof value === "string") {
		return value;
	}

	return undefined;
}

function toAttributeValue(value: unknown): string | undefined {
	if (value === null || value === undefined) {
		return undefined;
	}

	if (Array.isArray(value)) {
		return value.join(" ");
	}

	if (typeof value === "boolean") {
		return value ? "" : undefined;
	}

	return String(value);
}

function setElementProperties(
	element: HTMLElement,
	properties: Properties = {},
) {
	for (const [key, value] of Object.entries(properties)) {
		if (key === "className") {
			const className = toClassName(value);
			if (className) {
				element.className = className;
			}
			continue;
		}

		if (key === "style") {
			if (typeof value === "string") {
				element.setAttribute("style", value);
			} else if (value && typeof value === "object") {
				for (const [styleName, styleValue] of Object.entries(value)) {
					if (styleValue !== undefined && styleValue !== null) {
						element.style.setProperty(styleName, String(styleValue));
					}
				}
			}
			continue;
		}

		if (key === "checked" && value === true) {
			element.setAttribute(key, "");
			continue;
		}

		const attributeValue = toAttributeValue(value);
		if (attributeValue !== undefined) {
			element.setAttribute(key, attributeValue);
		}
	}
}

function getTextContent(node: HastNode): string {
	if (node.type === "text") {
		return (node as Text).value;
	}

	if (!("children" in node) || !Array.isArray(node.children)) {
		return "";
	}

	return node.children.map((child: HastNode) => getTextContent(child)).join("");
}

function appendChildren(
	parent: HTMLElement | DocumentFragment,
	children: RootContent[],
	options: RenderMarkdownOptions,
	parentTag?: string,
) {
	for (const child of children) {
		const rendered = renderNode(child, options, parentTag);
		if (rendered) {
			parent.append(rendered);
		}
	}
}

function renderLink(
	node: Element,
	options: RenderMarkdownOptions,
): HTMLElement {
	const href = toAttributeValue(node.properties?.href);
	const isIncomplete = href === "streamdown:incomplete-link";

	if (href && options.onLinkClick) {
		const button = document.createElement("button");
		button.type = "button";
		button.className = cn(
			"wrap-anywhere appearance-none text-left font-medium text-primary underline",
			toClassName(node.properties?.className),
		);
		if (isIncomplete) {
			button.dataset.incomplete = "true";
			button.disabled = true;
		} else {
			button.addEventListener("click", (event) => {
				event.preventDefault();
				options.onLinkClick?.(href);
			});
		}
		appendChildren(button, node.children as RootContent[], options, "a");
		return button;
	}

	const anchor = document.createElement("a");
	anchor.className = cn(
		"wrap-anywhere font-medium text-primary underline",
		toClassName(node.properties?.className),
	);
	setElementProperties(anchor, node.properties);
	if (href) {
		anchor.href = href;
		anchor.rel = "noreferrer";
		anchor.target = "_blank";
	}
	appendChildren(anchor, node.children as RootContent[], options, "a");
	return anchor;
}

function createCopyButton(code: string): HTMLButtonElement {
	const button = document.createElement("button");
	button.type = "button";
	button.className = cn(
		"inline-flex items-center justify-center rounded-md border border-sidebar bg-sidebar/80 px-2 py-1 text-xs shadow-sm supports-[backdrop-filter]:bg-sidebar/70 supports-[backdrop-filter]:backdrop-blur hover:bg-muted",
	);
	button.textContent = "Copy";
	button.addEventListener("click", async () => {
		try {
			await navigator.clipboard.writeText(code);
			button.textContent = "Copied";
			window.setTimeout(() => {
				button.textContent = "Copy";
			}, 2000);
		} catch {
			button.textContent = "Error";
			window.setTimeout(() => {
				button.textContent = "Copy";
			}, 2000);
		}
	});
	return button;
}

function parseRootStyle(rootStyle: string): Record<string, string> {
	const style: Record<string, string> = {};
	for (const declaration of rootStyle.split(";")) {
		const separatorIndex = declaration.indexOf(":");
		if (separatorIndex <= 0) {
			continue;
		}

		const property = declaration.slice(0, separatorIndex).trim();
		const value = declaration.slice(separatorIndex + 1).trim();
		if (property && value) {
			style[property] = value;
		}
	}
	return style;
}

function createRawHighlightResult(code: string): HighlightResult {
	return {
		bg: "transparent",
		fg: "inherit",
		tokens: code.split("\n").map((line) => [
			{
				bgColor: "transparent",
				color: "inherit",
				content: line,
				htmlStyle: {},
				offset: 0,
			},
		]),
	};
}

function shouldHighlightCode(
	language: string,
	options: RenderMarkdownOptions,
): boolean {
	const codePlugin = options.plugins?.code;
	if (!codePlugin) {
		return false;
	}

	if (!language) {
		return true;
	}

	if (codePlugin.supportsLanguage) {
		return codePlugin.supportsLanguage(language);
	}

	const supportedLanguages = codePlugin.getSupportedLanguages?.();
	if (supportedLanguages) {
		return supportedLanguages.includes(language);
	}

	return true;
}

function renderHighlightedCodeBody(
	container: HTMLElement,
	result: HighlightResult,
	language: string,
	startLine: number,
	showLineNumbers: boolean,
	className?: string,
) {
	container.replaceChildren();

	const body = document.createElement("div");
	body.className = cn(
		"overflow-x-auto rounded-md border border-border bg-background p-4 text-sm",
		className,
	);
	body.dataset.language = language;
	body.dataset.streamdown = "code-block-body";

	const pre = document.createElement("pre");
	pre.className = cn(className);
	if (result.bg) {
		pre.style.setProperty("--sdm-bg", result.bg);
	}
	if (result.fg) {
		pre.style.setProperty("--sdm-fg", result.fg);
	}
	if (result.rootStyle) {
		for (const [property, value] of Object.entries(
			parseRootStyle(result.rootStyle),
		)) {
			pre.style.setProperty(property, value);
		}
	}
	pre.style.backgroundColor =
		"var(--shiki-dark-bg, var(--sdm-bg, transparent))";

	const codeElement = document.createElement("code");
	if (showLineNumbers) {
		codeElement.style.counterIncrement = "line 0";
		codeElement.style.counterReset = `line ${Math.max(0, startLine - 1)}`;
	}

	for (const row of result.tokens) {
		const lineElement = document.createElement("span");
		lineElement.className = showLineNumbers ? "block" : "block";
		if (showLineNumbers) {
			lineElement.style.counterIncrement = "line";

			const lineNumber = document.createElement("span");
			lineNumber.className =
				"mr-4 inline-block w-6 select-none text-right font-mono text-[13px] text-muted-foreground/50";
			lineNumber.textContent = String(startLine++);
			lineElement.append(lineNumber);
		}

		if (row.length === 0 || (row.length === 1 && row[0].content === "")) {
			lineElement.append(document.createTextNode("\n"));
			codeElement.append(lineElement);
			continue;
		}

		for (const token of row) {
			const tokenElement = document.createElement("span");
			if (token.color) {
				tokenElement.style.setProperty("--sdm-c", token.color);
			}
			if (token.bgColor) {
				tokenElement.style.setProperty("--sdm-tbg", token.bgColor);
				tokenElement.style.backgroundColor =
					"var(--shiki-dark-bg, var(--sdm-tbg))";
			}
			if (token.htmlStyle) {
				for (const [key, value] of Object.entries(token.htmlStyle)) {
					if (key === "color") {
						tokenElement.style.setProperty("--sdm-c", value);
					} else if (key === "background-color") {
						tokenElement.style.setProperty("--sdm-tbg", value);
						tokenElement.style.backgroundColor =
							"var(--shiki-dark-bg, var(--sdm-tbg))";
					} else {
						tokenElement.style.setProperty(key, value);
					}
				}
			}
			if (token.htmlAttrs) {
				for (const [key, value] of Object.entries(token.htmlAttrs)) {
					tokenElement.setAttribute(key, value);
				}
			}
			tokenElement.style.color = "var(--shiki-dark, var(--sdm-c, inherit))";
			tokenElement.textContent = token.content;
			lineElement.append(tokenElement);
		}

		codeElement.append(lineElement);
	}

	pre.append(codeElement);
	body.append(pre);
	container.append(body);
}

function createImageFilename(src: string, alt: string): string {
	const urlPath = new URL(src, window.location.origin).pathname;
	const originalFilename = urlPath.split("/").pop() || "";
	const extension = originalFilename.split(".").pop();
	const hasExtension =
		originalFilename.includes(".") &&
		extension !== undefined &&
		extension.length <= 4;

	if (hasExtension) {
		return originalFilename;
	}

	const baseName = (alt || originalFilename || "image").replace(
		fileExtensionPattern,
		"",
	);
	return `${baseName}.png`;
}

function renderImage(node: Element): HTMLElement | HTMLImageElement {
	const src = toAttributeValue(node.properties?.src);
	const alt = toAttributeValue(node.properties?.alt) ?? "";
	const className = toClassName(node.properties?.className);

	if (!src) {
		const fallback = document.createElement("span");
		fallback.className = "text-xs italic text-muted-foreground";
		fallback.textContent = "Image not available";
		return fallback;
	}

	const wrapper = document.createElement("div");
	wrapper.className = cn("group relative my-4 inline-block", className);
	wrapper.dataset.streamdown = "image-wrapper";

	const image = document.createElement("img");
	image.alt = alt;
	image.className = cn("max-w-full rounded-lg", className);
	image.dataset.streamdown = "image";
	setElementProperties(image, node.properties);

	const fallback = document.createElement("span");
	fallback.className = "hidden text-xs italic text-muted-foreground";
	fallback.dataset.streamdown = "image-fallback";
	fallback.textContent = "Image not available";

	const overlay = document.createElement("div");
	overlay.className = cn(
		"pointer-events-none absolute inset-0 hidden rounded-lg bg-black/10 group-hover:block",
	);

	const downloadButton = document.createElement("button");
	downloadButton.type = "button";
	downloadButton.className = cn(
		"absolute bottom-2 right-2 flex h-8 w-8 items-center justify-center rounded-md border border-border bg-background/90 text-xs shadow-sm opacity-0 backdrop-blur-sm transition-all duration-200 group-hover:opacity-100 hover:bg-background",
	);
	downloadButton.textContent = "↓";
	downloadButton.title = "Download image";
	downloadButton.hidden = true;
	downloadButton.addEventListener("click", async () => {
		try {
			const response = await fetch(src);
			const blob = await response.blob();
			await downloadFile({
				filename: createImageFilename(src, alt),
				content: await blob.arrayBuffer(),
				mimeType: blob.type,
			});
		} catch {
			window.open(src, "_blank", "noopener,noreferrer");
		}
	});

	const setLoaded = () => {
		fallback.classList.add("hidden");
		image.classList.remove("hidden");
		downloadButton.hidden = false;
	};

	const setError = () => {
		image.classList.add("hidden");
		fallback.classList.remove("hidden");
		downloadButton.hidden = true;
	};

	image.addEventListener("load", setLoaded);
	image.addEventListener("error", setError);

	if (image.complete) {
		if (image.naturalWidth > 0) {
			setLoaded();
		} else {
			setError();
		}
	}

	wrapper.append(image, fallback, overlay, downloadButton);
	return wrapper;
}

function renderCodeBlock(
	node: Element,
	options: RenderMarkdownOptions,
): HTMLElement {
	const codeNode = node.children.find(
		(child) => isElement(child) && child.tagName === "code",
	);
	if (!codeNode || !isElement(codeNode)) {
		const pre = document.createElement("pre");
		appendChildren(pre, node.children as RootContent[], options, "pre");
		return pre;
	}

	const className = toClassName(codeNode.properties?.className);
	const language = className?.match(languagePattern)?.[1] ?? "";
	const metastring = toAttributeValue(codeNode.properties?.metastring);
	const startLine = Number.parseInt(
		metastring?.match(startLinePattern)?.[1] ?? "1",
		10,
	);
	const showLineNumbers = !(
		metastring && noLineNumbersPattern.test(metastring)
	);
	const code = getTextContent(codeNode);

	const container = document.createElement("div");
	container.className = cn(
		"my-4 flex w-full flex-col gap-2 rounded-xl border border-border bg-sidebar p-2",
	);
	container.dataset.language = language;
	container.dataset.streamdown = "code-block";
	if (options.isIncompleteCodeFence) {
		container.dataset.incomplete = "true";
	}
	container.style.contentVisibility = "auto";
	container.style.containIntrinsicSize = "auto 200px";

	const header = document.createElement("div");
	header.className = cn("flex h-8 items-center text-muted-foreground text-xs");
	header.dataset.streamdown = "code-block-header";
	if (language) {
		const title = document.createElement("span");
		title.className = "ml-1 font-mono lowercase";
		title.textContent = language;
		header.append(title);
	}
	container.append(header);

	const actionsRow = document.createElement("div");
	actionsRow.className = cn(
		"pointer-events-none sticky top-2 z-10 -mt-10 flex h-8 items-center justify-end",
	);
	const actions = document.createElement("div");
	actions.className = cn(
		"pointer-events-auto flex shrink-0 items-center gap-2 rounded-md border border-sidebar bg-sidebar/80 px-1.5 py-1 supports-[backdrop-filter]:bg-sidebar/70 supports-[backdrop-filter]:backdrop-blur",
	);
	actions.dataset.streamdown = "code-block-actions";
	actions.append(createCopyButton(code));
	actionsRow.append(actions);
	container.append(actionsRow);

	const bodyContainer = document.createElement("div");
	const rawResult = createRawHighlightResult(code);
	renderHighlightedCodeBody(
		bodyContainer,
		rawResult,
		language,
		startLine,
		showLineNumbers,
		className,
	);
	container.append(bodyContainer);

	if (!shouldHighlightCode(language, options)) {
		return container;
	}

	const themes = options.plugins?.code?.getThemes?.() ?? [
		"github-light",
		"github-dark",
	];
	const highlightedResult = options.plugins?.code?.highlight(
		{
			code,
			language,
			themes,
		},
		(result) => {
			renderHighlightedCodeBody(
				bodyContainer,
				result,
				language,
				startLine,
				showLineNumbers,
				className,
			);
		},
	);

	if (highlightedResult) {
		renderHighlightedCodeBody(
			bodyContainer,
			highlightedResult,
			language,
			startLine,
			showLineNumbers,
			className,
		);
	}

	return container;
}

function renderTable(
	node: Element,
	options: RenderMarkdownOptions,
): HTMLElement {
	const wrapper = document.createElement("div");
	wrapper.className = cn("my-4 rounded-lg border border-border bg-background");

	const scrollContainer = document.createElement("div");
	scrollContainer.className = "relative w-full overflow-x-auto";

	const table = document.createElement("table");
	table.className = cn(
		"w-full caption-bottom text-sm",
		toClassName(node.properties?.className),
	);
	setElementProperties(table, node.properties);
	appendChildren(table, node.children as RootContent[], options, "table");

	scrollContainer.append(table);
	wrapper.append(scrollContainer);
	return wrapper;
}

function renderElement(
	node: Element,
	options: RenderMarkdownOptions,
	parentTag?: string,
): HTMLElement | DocumentFragment | null {
	if (node.tagName === "pre") {
		return renderCodeBlock(node, options);
	}

	if (node.tagName === "table") {
		return renderTable(node, options);
	}

	if (node.tagName === "a") {
		return renderLink(node, options);
	}

	if (node.tagName === "img") {
		return renderImage(node);
	}

	if (
		node.tagName === "p" &&
		node.children.length === 1 &&
		isElement(node.children[0]) &&
		node.children[0].tagName === "img"
	) {
		return renderImage(node.children[0]);
	}

	const element = document.createElement(node.tagName);
	setElementProperties(element, node.properties);

	switch (node.tagName) {
		case "blockquote":
			element.className = cn(
				"my-4 border-l-4 border-muted-foreground/30 pl-4 italic text-muted-foreground",
				element.className,
			);
			break;
		case "code":
			if (parentTag !== "pre") {
				element.className = cn(
					"rounded bg-muted px-1.5 py-0.5 font-mono text-sm",
					element.className,
				);
			}
			break;
		case "h1":
			element.className = cn(
				"mt-6 mb-2 text-3xl font-semibold",
				element.className,
			);
			break;
		case "h2":
			element.className = cn(
				"mt-6 mb-2 text-2xl font-semibold",
				element.className,
			);
			break;
		case "h3":
			element.className = cn(
				"mt-6 mb-2 text-xl font-semibold",
				element.className,
			);
			break;
		case "h4":
			element.className = cn(
				"mt-6 mb-2 text-lg font-semibold",
				element.className,
			);
			break;
		case "h5":
			element.className = cn(
				"mt-6 mb-2 text-base font-semibold",
				element.className,
			);
			break;
		case "h6":
			element.className = cn(
				"mt-6 mb-2 text-sm font-semibold",
				element.className,
			);
			break;
		case "hr":
			element.className = cn("my-6 border-border", element.className);
			break;
		case "li":
			element.className = cn("py-1", element.className);
			break;
		case "ol":
			element.className = cn(
				"list-outside list-decimal whitespace-normal pl-6 [li_&]:pl-6",
				element.className,
			);
			break;
		case "strong":
			element.className = cn("font-semibold", element.className);
			break;
		case "tbody":
			element.className = cn("[&_tr:last-child]:border-0", element.className);
			break;
		case "td":
			element.className = cn(
				"bg-clip-padding p-2 align-middle whitespace-nowrap",
				element.className,
			);
			break;
		case "th":
			element.className = cn(
				"h-10 bg-clip-padding px-2 text-start align-middle font-medium text-foreground whitespace-nowrap",
				element.className,
			);
			break;
		case "thead":
			element.className = cn("[&_tr]:border-b", element.className);
			break;
		case "tr":
			element.className = cn("border-b transition-colors", element.className);
			break;
		case "ul":
			element.className = cn(
				"list-outside list-disc whitespace-normal pl-6 [li_&]:pl-6",
				element.className,
			);
			break;
		default:
			break;
	}

	appendChildren(
		element,
		node.children as RootContent[],
		options,
		node.tagName,
	);
	return element;
}

function renderNode(
	node: RootContent,
	options: RenderMarkdownOptions,
	parentTag?: string,
): globalThis.Node | null {
	if (node.type === "text") {
		return document.createTextNode((node as Text).value);
	}

	if (node.type === "element") {
		return renderElement(node as Element, options, parentTag);
	}

	return null;
}

export function renderMarkdownTree(
	tree: Root,
	options: RenderMarkdownOptions = {},
): DocumentFragment {
	const fragment = document.createDocumentFragment();
	appendChildren(fragment, tree.children, options);
	return fragment;
}
