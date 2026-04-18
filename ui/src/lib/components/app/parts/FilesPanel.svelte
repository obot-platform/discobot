<script lang="ts">
	import CheckIcon from "@lucide/svelte/icons/check";
	import ChevronDownIcon from "@lucide/svelte/icons/chevron-down";
	import ChevronRightIcon from "@lucide/svelte/icons/chevron-right";
	import Columns2Icon from "@lucide/svelte/icons/columns-2";
	import DownloadIcon from "@lucide/svelte/icons/download";
	import EyeIcon from "@lucide/svelte/icons/eye";
	import FileCodeIcon from "@lucide/svelte/icons/file-code";
	import FileImageIcon from "@lucide/svelte/icons/file-image";
	import FileMinusIcon from "@lucide/svelte/icons/file-minus";
	import FilePlusIcon from "@lucide/svelte/icons/file-plus";
	import FolderIcon from "@lucide/svelte/icons/folder";
	import FolderOpenIcon from "@lucide/svelte/icons/folder-open";
	import RefreshCwIcon from "@lucide/svelte/icons/refresh-cw";
	import SaveIcon from "@lucide/svelte/icons/save";
	import XIcon from "@lucide/svelte/icons/x";
	import { onMount, tick } from "svelte";
	import editorWorker from "monaco-editor/esm/vs/editor/editor.worker?worker";
	import cssWorker from "monaco-editor/esm/vs/language/css/css.worker?worker";
	import htmlWorker from "monaco-editor/esm/vs/language/html/html.worker?worker";
	import jsonWorker from "monaco-editor/esm/vs/language/json/json.worker?worker";
	import tsWorker from "monaco-editor/esm/vs/language/typescript/ts.worker?worker";

	import DockWindowChrome from "$lib/components/app/parts/DockWindowChrome.svelte";
	import ImageAttachment from "$lib/components/ai/image-attachment/ImageAttachment.svelte";
	import { MessageResponse } from "$lib/components/ai/message";
	import {
		AlertDialog,
		AlertDialogAction,
		AlertDialogCancel,
		AlertDialogContent,
		AlertDialogDescription,
		AlertDialogFooter,
		AlertDialogHeader,
		AlertDialogTitle,
	} from "$lib/components/ui/alert-dialog";
	import { Badge } from "$lib/components/ui/badge";
	import { Button } from "$lib/components/ui/button";
	import {
		ContextMenu,
		ContextMenuContent,
		ContextMenuItem,
		ContextMenuTrigger,
	} from "$lib/components/ui/context-menu";
	import {
		Dialog,
		DialogContent,
		DialogDescription,
		DialogFooter,
		DialogHeader,
		DialogTitle,
	} from "$lib/components/ui/dialog";
	import { Input } from "$lib/components/ui/input";
	import * as Resizable from "$lib/components/ui/resizable";
	import { Switch } from "$lib/components/ui/switch";
	import {
		ToggleGroup,
		ToggleGroupItem,
	} from "$lib/components/ui/toggle-group";
	import type { FileStatus } from "$lib/api-types";
	import type {
		SessionFileTreeNode,
		SessionFilesDomain,
	} from "$lib/session/session-context.types";
	import type { ResolvedTheme, ThemeColorScheme } from "$lib/theme";
	import { downloadFile } from "$lib/shell";
	import { cn } from "$lib/utils";
	import type * as Monaco from "monaco-editor";

	type MarkdownViewMode = "preview" | "split" | "editor";

	type Props = {
		colorScheme: ThemeColorScheme;
		dockMaximized: boolean;
		files: SessionFilesDomain;
		onClose: () => void;
		onToggleDockMaximized: () => void;
		resolvedTheme: ResolvedTheme;
	};

	let {
		colorScheme,
		dockMaximized,
		files,
		onClose,
		onToggleDockMaximized,
		resolvedTheme,
	}: Props = $props();

	const MONACO_THEME_PREFIX = "discobot";
	let monacoLoader: Promise<typeof import("monaco-editor")> | null = null;
	let monacoConfigured = false;

	function fileLabel(path: string) {
		return path.split("/").at(-1) ?? path;
	}

	function statusLetter(status?: FileStatus) {
		switch (status) {
			case "added":
				return "A";
			case "modified":
				return "M";
			case "deleted":
				return "D";
			case "renamed":
				return "R";
			default:
				return "";
		}
	}

	const maximizeTitle = $derived.by(() =>
		dockMaximized ? "Restore split view" : "Maximize files panel",
	);

	function statusBadgeClass(status?: FileStatus) {
		switch (status) {
			case "added":
				return "border-green-500/40 text-green-500";
			case "modified":
				return "border-yellow-500/40 text-yellow-500";
			case "deleted":
				return "border-red-500/40 text-red-500";
			case "renamed":
				return "border-purple-500/40 text-purple-500";
			default:
				return "border-border text-muted-foreground";
		}
	}

	function nodeIcon(node: SessionFileTreeNode, expanded = false) {
		if (node.type === "directory") {
			return expanded ? FolderOpenIcon : FolderIcon;
		}
		switch (node.status) {
			case "added":
				return FilePlusIcon;
			case "deleted":
				return FileMinusIcon;
			default:
				return isImageFile(node.path) ? FileImageIcon : FileCodeIcon;
		}
	}

	function getLanguage(path: string): string {
		const extension = path.split(".").at(-1)?.toLowerCase() ?? "";
		switch (extension) {
			case "ts":
			case "tsx":
				return "typescript";
			case "js":
			case "jsx":
			case "mjs":
			case "cjs":
				return "javascript";
			case "json":
				return "json";
			case "go":
				return "go";
			case "md":
			case "mdx":
				return "markdown";
			case "css":
				return "css";
			case "scss":
				return "scss";
			case "html":
				return "html";
			case "svelte":
				return "html";
			case "yml":
			case "yaml":
				return "yaml";
			case "sh":
				return "shell";
			case "py":
				return "python";
			case "rs":
				return "rust";
			default:
				return "plaintext";
		}
	}

	function isMarkdownFile(path: string): boolean {
		const extension = path.split(".").at(-1)?.toLowerCase() ?? "";
		return ["md", "mdx"].includes(extension);
	}

	function isImageFile(path: string): boolean {
		const extension = path.split(".").at(-1)?.toLowerCase() ?? "";
		return [
			"png",
			"jpg",
			"jpeg",
			"gif",
			"webp",
			"svg",
			"ico",
			"bmp",
			"avif",
			"tif",
			"tiff",
		].includes(extension);
	}

	function isPdfFile(path: string): boolean {
		const extension = path.split(".").at(-1)?.toLowerCase() ?? "";
		return extension === "pdf";
	}

	function getDataUrl(mimeType: string, content: string): string {
		return `data:${mimeType};base64,${content}`;
	}

	function getImageMimeType(path: string): string {
		const extension = path.split(".").at(-1)?.toLowerCase() ?? "";
		switch (extension) {
			case "jpg":
			case "jpeg":
				return "image/jpeg";
			case "gif":
				return "image/gif";
			case "webp":
				return "image/webp";
			case "svg":
				return "image/svg+xml";
			case "ico":
				return "image/x-icon";
			case "bmp":
				return "image/bmp";
			case "avif":
				return "image/avif";
			case "tif":
			case "tiff":
				return "image/tiff";
			default:
				return "image/png";
		}
	}

	function getDownloadMimeType(
		path: string,
		encoding: "utf8" | "base64",
	): string {
		if (encoding === "utf8") {
			return "text/plain;charset=utf-8";
		}
		if (isImageFile(path)) {
			return getImageMimeType(path);
		}
		if (isPdfFile(path)) {
			return "application/pdf";
		}
		return "application/octet-stream";
	}

	function decodeBase64(content: string): Uint8Array {
		const decoded = globalThis.atob(content);
		const bytes = new Uint8Array(decoded.length);
		for (let index = 0; index < decoded.length; index += 1) {
			bytes[index] = decoded.charCodeAt(index);
		}
		return bytes;
	}

	async function downloadActiveFile() {
		if (!activePath || !activeBuffer) {
			return;
		}

		const mimeType = getDownloadMimeType(activePath, activeBuffer.encoding);
		await downloadFile({
			filename: fileLabel(activePath),
			content:
				activeBuffer.encoding === "utf8"
					? activeBuffer.content
					: decodeBase64(activeBuffer.content),
			mimeType,
		});
	}

	function componentToHex(value: number): string {
		return Math.round(value).toString(16).padStart(2, "0").toUpperCase();
	}

	function rgbStringToHex(value: string): string | null {
		const normalized = value.trim();
		if (normalized.startsWith("#")) {
			return normalized.toUpperCase();
		}

		const match = normalized.match(
			/^rgba?\(\s*(\d+(?:\.\d+)?)\s*,\s*(\d+(?:\.\d+)?)\s*,\s*(\d+(?:\.\d+)?)(?:\s*,\s*(\d*(?:\.\d+)?))?\s*\)$/,
		);
		if (!match) {
			return null;
		}

		const [, red, green, blue, alpha] = match;
		const alphaHex =
			typeof alpha === "string" && alpha.length > 0 && Number(alpha) < 1
				? componentToHex(Number(alpha) * 255)
				: "";

		return `#${componentToHex(Number(red))}${componentToHex(Number(green))}${componentToHex(Number(blue))}${alphaHex}`;
	}

	function readThemeValue(
		style: CSSStyleDeclaration,
		property: string,
		fallback: string,
	): string {
		const value = style.getPropertyValue(property).trim();
		return value.length > 0 ? value : fallback;
	}

	function resolveColorToHex(value: string, fallback: string): string {
		if (typeof document === "undefined" || typeof window === "undefined") {
			return fallback;
		}

		const probe = document.createElement("span");
		probe.style.position = "fixed";
		probe.style.pointerEvents = "none";
		probe.style.opacity = "0";
		probe.style.color = fallback;
		if (CSS.supports("color", value)) {
			probe.style.color = value;
		}
		document.body.appendChild(probe);
		const resolved = window.getComputedStyle(probe).color;
		probe.remove();
		return rgbStringToHex(resolved) ?? fallback;
	}

	function readThemeColor(
		style: CSSStyleDeclaration,
		property: string,
		fallback: string,
	): string {
		return resolveColorToHex(
			readThemeValue(style, property, fallback),
			fallback,
		);
	}

	function tokenColor(value: string): string {
		return value.replace(/^#/, "");
	}

	function defineMonacoTheme(
		monaco: typeof import("monaco-editor"),
		mode: ResolvedTheme,
		scheme: ThemeColorScheme,
	): string {
		if (typeof window === "undefined" || typeof document === "undefined") {
			return mode === "dark" ? "vs-dark" : "vs";
		}

		const style = window.getComputedStyle(document.documentElement);
		const background = readThemeColor(
			style,
			"--background",
			mode === "dark" ? "#1E1E1E" : "#FFFFFF",
		);
		const foreground = readThemeColor(
			style,
			"--foreground",
			mode === "dark" ? "#D4D4D4" : "#24292E",
		);
		const border = readThemeColor(
			style,
			"--border",
			mode === "dark" ? "#3F3F46" : "#D0D7DE",
		);
		const primary = readThemeColor(
			style,
			"--primary",
			mode === "dark" ? "#88C0D0" : "#0969DA",
		);
		const mutedForeground = readThemeColor(
			style,
			"--muted-foreground",
			mode === "dark" ? "#8B949E" : "#57606A",
		);
		const accent = readThemeColor(
			style,
			"--accent",
			mode === "dark" ? "#2A2D2E" : "#F6F8FA",
		);
		const popover = readThemeColor(
			style,
			"--popover",
			mode === "dark" ? "#1F2328" : "#FFFFFF",
		);
		const popoverForeground = readThemeColor(
			style,
			"--popover-foreground",
			mode === "dark" ? "#D4D4D4" : "#24292E",
		);
		const selection = readThemeColor(
			style,
			"--tree-selected",
			mode === "dark" ? "#264F78" : "#ADD6FF",
		);
		const chart1 = readThemeColor(style, "--chart-1", primary);
		const chart2 = readThemeColor(
			style,
			"--chart-2",
			mode === "dark" ? "#A6E3A1" : "#0A7A45",
		);
		const chart3 = readThemeColor(
			style,
			"--chart-3",
			mode === "dark" ? "#EBCB8B" : "#B35900",
		);
		const chart4 = readThemeColor(
			style,
			"--chart-4",
			mode === "dark" ? "#B48EAD" : "#7C3AED",
		);
		const chart5 = readThemeColor(
			style,
			"--chart-5",
			mode === "dark" ? "#81A1C1" : "#0550AE",
		);
		const destructive = readThemeColor(
			style,
			"--destructive",
			mode === "dark" ? "#BF616A" : "#CF222E",
		);
		const themeName = `${MONACO_THEME_PREFIX}-${mode}-${scheme}`;

		monaco.editor.defineTheme(themeName, {
			base: mode === "dark" ? "vs-dark" : "vs",
			inherit: true,
			rules: [
				{ token: "", foreground: tokenColor(foreground) },
				{
					token: "comment",
					foreground: tokenColor(mutedForeground),
					fontStyle: "italic",
				},
				{ token: "keyword", foreground: tokenColor(primary) },
				{ token: "operator", foreground: tokenColor(foreground) },
				{ token: "string", foreground: tokenColor(chart2) },
				{ token: "number", foreground: tokenColor(chart3) },
				{ token: "regexp", foreground: tokenColor(chart4) },
				{ token: "constant", foreground: tokenColor(chart4) },
				{ token: "type", foreground: tokenColor(chart5) },
				{ token: "type.identifier", foreground: tokenColor(chart5) },
				{ token: "class", foreground: tokenColor(chart5) },
				{ token: "function", foreground: tokenColor(chart1) },
				{ token: "function.call", foreground: tokenColor(chart1) },
				{ token: "tag", foreground: tokenColor(chart4) },
				{ token: "attribute.name", foreground: tokenColor(chart5) },
				{ token: "attribute.value", foreground: tokenColor(chart2) },
				{ token: "invalid", foreground: tokenColor(destructive) },
			],
			colors: {
				"editor.background": background,
				"editor.foreground": foreground,
				"editorCursor.foreground": primary,
				"editor.lineHighlightBackground": accent,
				"editor.selectionBackground": selection,
				"editor.inactiveSelectionBackground": selection,
				"editorLineNumber.foreground": mutedForeground,
				"editorLineNumber.activeForeground": foreground,
				"editorGutter.background": background,
				"editorIndentGuide.background1": border,
				"editorIndentGuide.activeBackground1": primary,
				"editorWhitespace.foreground": border,
				"editorWidget.background": popover,
				"editorWidget.foreground": popoverForeground,
				"editorWidget.border": border,
				"editorHoverWidget.background": popover,
				"editorHoverWidget.foreground": popoverForeground,
				"editorHoverWidget.border": border,
			},
		});

		return themeName;
	}

	function applyMonacoTheme(monaco: typeof import("monaco-editor")) {
		monaco.editor.setTheme(
			defineMonacoTheme(monaco, resolvedTheme, colorScheme),
		);
	}

	async function loadMonaco() {
		if (!monacoLoader) {
			monacoLoader = import("monaco-editor").then((loadedMonaco) => {
				if (!monacoConfigured) {
					(
						self as typeof globalThis & {
							MonacoEnvironment?: {
								getWorker: (_moduleId: string, label: string) => Worker;
							};
						}
					).MonacoEnvironment = {
						getWorker(_moduleId: string, label: string) {
							if (label === "json") return new jsonWorker();
							if (label === "css" || label === "scss" || label === "less") {
								return new cssWorker();
							}
							if (
								label === "html" ||
								label === "handlebars" ||
								label === "razor"
							) {
								return new htmlWorker();
							}
							if (label === "typescript" || label === "javascript") {
								return new tsWorker();
							}
							return new editorWorker();
						},
					};
					const typescriptLanguage = loadedMonaco.languages
						.typescript as unknown as {
						typescriptDefaults: {
							setDiagnosticsOptions: (options: {
								noSemanticValidation: boolean;
								noSyntaxValidation: boolean;
							}) => void;
						};
						javascriptDefaults: {
							setDiagnosticsOptions: (options: {
								noSemanticValidation: boolean;
								noSyntaxValidation: boolean;
							}) => void;
						};
					};
					typescriptLanguage.typescriptDefaults.setDiagnosticsOptions({
						noSemanticValidation: true,
						noSyntaxValidation: false,
					});
					typescriptLanguage.javascriptDefaults.setDiagnosticsOptions({
						noSemanticValidation: true,
						noSyntaxValidation: false,
					});
					monacoConfigured = true;
				}
				return loadedMonaco;
			});
		}
		return monacoLoader;
	}

	let editorContainer = $state<HTMLDivElement | null>(null);
	let monacoInstance = $state<typeof import("monaco-editor") | null>(null);
	let editorInstance = $state<Monaco.editor.IStandaloneCodeEditor | null>(null);
	let activeEditorPath = $state<string | null>(null);
	let editorDisposed = false;
	let modelChangeDisposable: Monaco.IDisposable | null = null;
	let syncingModelValue = false;
	let markdownViewMode = $state<MarkdownViewMode>("preview");
	let markdownModePath = $state<string | null>(null);

	const openPaths = $derived.by(() => files.openPaths);
	const activePath = $derived.by(() => files.activePath);
	const activeBuffer = $derived.by(() =>
		activePath ? files.getBuffer(activePath) : null,
	);
	const activeDiff = $derived.by(
		() => files.diff.find((entry) => entry.path === activePath) ?? null,
	);
	const activeStatus = $derived.by(() => activeDiff?.status);
	const canRenderTextEditor = $derived.by(() =>
		Boolean(activePath && activeBuffer && activeBuffer.encoding === "utf8"),
	);
	const canEditActiveFile = $derived.by(() =>
		Boolean(
			activePath &&
			activeBuffer &&
			activeBuffer.encoding === "utf8" &&
			!activeBuffer.fromBase,
		),
	);
	const isMarkdownPreview = $derived.by(() =>
		Boolean(
			activePath &&
			activeBuffer &&
			activeBuffer.encoding === "utf8" &&
			isMarkdownFile(activePath),
		),
	);
	const showMarkdownPreview = $derived.by(() =>
		Boolean(isMarkdownPreview && markdownViewMode !== "editor"),
	);
	const showMarkdownSplitView = $derived.by(() =>
		Boolean(isMarkdownPreview && markdownViewMode === "split"),
	);
	const showTextEditor = $derived.by(() =>
		Boolean(
			canRenderTextEditor &&
			(!isMarkdownPreview || markdownViewMode !== "preview"),
		),
	);
	const isImagePreview = $derived.by(() =>
		Boolean(
			activePath &&
			activeBuffer &&
			activeBuffer.encoding === "base64" &&
			isImageFile(activePath),
		),
	);
	const isPdfPreview = $derived.by(() =>
		Boolean(
			activePath &&
			activeBuffer &&
			activeBuffer.encoding === "base64" &&
			isPdfFile(activePath),
		),
	);
	const isBinaryPreview = $derived.by(() =>
		Boolean(
			activePath &&
			activeBuffer &&
			activeBuffer.encoding === "base64" &&
			!isImageFile(activePath) &&
			!isPdfFile(activePath),
		),
	);
	const imageSource = $derived.by(() => {
		if (!activePath || !activeBuffer || !isImagePreview) {
			return "";
		}
		return getDataUrl(getImageMimeType(activePath), activeBuffer.content);
	});
	const pdfSource = $derived.by(() => {
		if (!activeBuffer || !isPdfPreview) {
			return "";
		}
		return getDataUrl("application/pdf", activeBuffer.content);
	});
	let renameDialogOpen = $state(false);
	let renamePath = $state<string | null>(null);
	let renameDraft = $state("");
	let renaming = $state(false);
	let closeTabDialogOpen = $state(false);
	let closeTabPath = $state<string | null>(null);
	let deleteDialogOpen = $state(false);
	let deletePath = $state<string | null>(null);
	let deleting = $state(false);

	function canManageNode(node: SessionFileTreeNode): boolean {
		return (
			node.type === "file" &&
			node.status !== "deleted" &&
			!files.hasDirtyChanges(node.path)
		);
	}

	function openRenameDialog(node: SessionFileTreeNode) {
		if (!canManageNode(node)) {
			return;
		}
		renamePath = node.path;
		renameDraft = fileLabel(node.path);
		renameDialogOpen = true;
	}

	function closeRenameDialog() {
		renameDialogOpen = false;
		renamePath = null;
		renameDraft = "";
		renaming = false;
	}

	async function handleRename() {
		if (!renamePath || renaming) {
			return;
		}
		renaming = true;
		const renamed = await files.rename(renamePath, renameDraft);
		renaming = false;
		if (renamed) {
			closeRenameDialog();
		}
	}

	function handleRenameInputKeydown(event: KeyboardEvent) {
		if (event.key === "Enter") {
			event.preventDefault();
			void handleRename();
		}
	}

	function openDeleteDialog(node: SessionFileTreeNode) {
		if (!canManageNode(node)) {
			return;
		}
		deletePath = node.path;
		deleteDialogOpen = true;
	}

	function closeDeleteDialog() {
		deleteDialogOpen = false;
		deletePath = null;
		deleting = false;
	}

	function requestCloseTab(path: string) {
		if (isDirty(path)) {
			closeTabPath = path;
			closeTabDialogOpen = true;
			return;
		}
		files.close(path);
	}

	function closeCloseTabDialog() {
		closeTabDialogOpen = false;
		closeTabPath = null;
	}

	function confirmCloseTab() {
		if (!closeTabPath) {
			return;
		}
		files.close(closeTabPath);
		closeCloseTabDialog();
	}

	async function handleDelete() {
		if (!deletePath || deleting) {
			return;
		}
		deleting = true;
		const removed = await files.remove(deletePath);
		deleting = false;
		if (removed) {
			closeDeleteDialog();
		}
	}

	function saveCurrentViewState() {
		if (!editorInstance || !activeEditorPath) {
			return;
		}
		files.setEditorViewState(activeEditorPath, editorInstance.saveViewState());
	}

	function syncActiveModelContent(path: string, content: string) {
		const model = files.getEditorModel(path) as Monaco.editor.ITextModel | null;
		if (!model || model.getValue() === content) {
			return;
		}
		syncingModelValue = true;
		model.setValue(content);
		syncingModelValue = false;
	}

	async function ensureEditorForActiveFile() {
		if (!editorInstance || !monacoInstance) {
			return;
		}

		applyMonacoTheme(monacoInstance);

		if (!activePath || !activeBuffer || !canRenderTextEditor) {
			saveCurrentViewState();
			activeEditorPath = null;
			editorInstance.setModel(null);
			return;
		}

		let model = files.getEditorModel(
			activePath,
		) as Monaco.editor.ITextModel | null;
		if (!model) {
			model = monacoInstance.editor.createModel(
				activeBuffer.content,
				getLanguage(activePath),
				monacoInstance.Uri.from({ scheme: "file", path: `/${activePath}` }),
			);
			files.setEditorModel(activePath, model);
		} else {
			monacoInstance.editor.setModelLanguage(model, getLanguage(activePath));
			syncActiveModelContent(activePath, activeBuffer.content);
		}

		if (activeEditorPath !== activePath) {
			saveCurrentViewState();
			activeEditorPath = activePath;
			editorInstance.setModel(model);
			const savedViewState = files.getEditorViewState(
				activePath,
			) as Monaco.editor.ICodeEditorViewState | null;
			if (savedViewState) {
				editorInstance.restoreViewState(savedViewState);
			}
		}

		editorInstance.updateOptions({
			readOnly: !canEditActiveFile,
			domReadOnly: !canEditActiveFile,
		});
		syncActiveModelContent(activePath, activeBuffer.content);
	}

	async function setupEditor() {
		if (editorInstance || !editorContainer || editorDisposed) {
			return;
		}

		const monaco = await loadMonaco();
		if (editorDisposed || !editorContainer) {
			return;
		}

		monacoInstance = monaco;
		applyMonacoTheme(monaco);
		editorInstance = monaco.editor.create(editorContainer, {
			automaticLayout: true,
			fontSize: 13,
			minimap: { enabled: false },
			renderLineHighlight: "line",
			scrollBeyondLastLine: false,
			wordWrap: "off",
			padding: { top: 8 },
			scrollbar: {
				verticalScrollbarSize: 10,
				horizontalScrollbarSize: 10,
			},
		});

		modelChangeDisposable = editorInstance.onDidChangeModelContent(() => {
			if (syncingModelValue || !activeEditorPath) {
				return;
			}
			const model = editorInstance?.getModel();
			if (!model) {
				return;
			}
			files.updateBuffer(activeEditorPath, model.getValue());
		});

		await ensureEditorForActiveFile();
	}

	onMount(() => {
		editorDisposed = false;

		const handleKeyDown = async (event: KeyboardEvent) => {
			if (
				!(event.metaKey || event.ctrlKey) ||
				event.key.toLowerCase() !== "s" ||
				!activePath
			) {
				return;
			}
			event.preventDefault();
			await files.save(activePath);
		};

		window.addEventListener("keydown", handleKeyDown);

		return () => {
			editorDisposed = true;
			window.removeEventListener("keydown", handleKeyDown);
			saveCurrentViewState();
			modelChangeDisposable?.dispose();
			modelChangeDisposable = null;
			editorInstance?.dispose();
			editorInstance = null;
			monacoInstance = null;
			activeEditorPath = null;
		};
	});

	$effect(() => {
		void editorContainer;
		if (editorContainer && !editorInstance) {
			void setupEditor();
		}
	});

	$effect(() => {
		if (activePath === markdownModePath) {
			return;
		}
		markdownModePath = activePath;
		markdownViewMode =
			activePath && isMarkdownFile(activePath) ? "preview" : "editor";
	});

	$effect(() => {
		void activePath;
		void activeBuffer?.content;
		void canEditActiveFile;
		void canRenderTextEditor;
		void colorScheme;
		void resolvedTheme;
		void ensureEditorForActiveFile();
	});

	$effect(() => {
		void canRenderTextEditor;
		void showTextEditor;
		void showMarkdownSplitView;
		if (editorInstance && canRenderTextEditor && showTextEditor) {
			void tick().then(() => {
				editorInstance?.layout();
			});
		}
	});

	function isExpanded(path: string) {
		return files.expandedPaths.includes(path);
	}

	function isSelected(path: string) {
		return activePath === path;
	}

	function isDirty(path: string) {
		return files.getBuffer(path)?.isDirty ?? false;
	}

	function treeButtonClass(node: SessionFileTreeNode, selected: boolean) {
		return cn(
			"flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-sm text-sidebar-foreground/80 transition hover:bg-sidebar-accent hover:text-sidebar-accent-foreground",
			selected &&
				"bg-sidebar-accent text-sidebar-accent-foreground shadow-inner",
			node.status === "deleted" && "text-sidebar-foreground/40 line-through",
		);
	}
</script>

<DockWindowChrome
	{dockMaximized}
	{onClose}
	{onToggleDockMaximized}
	closeLabel="Close files panel"
	minimizeLabel="Minimize files panel"
	{maximizeTitle}
	contentClass="min-h-0 flex-1 overflow-hidden p-3"
>
	{#snippet title()}
		<span class="truncate text-xs text-sidebar-foreground/70">
			{activePath ? activePath : "Files panel"}
		</span>
		<div
			class={cn(
				"size-2 shrink-0 rounded-full",
				activeBuffer?.isDirty
					? "bg-sidebar-primary"
					: "bg-sidebar-foreground/30",
			)}
			title={activeBuffer?.isDirty ? "Unsaved changes" : "No unsaved changes"}
		></div>
	{/snippet}

	{#snippet actions()}
		<label class="flex items-center gap-2 text-xs text-sidebar-foreground/70">
			<span>changed only</span>
			<Switch
				checked={files.showChangedOnly}
				onCheckedChange={() => files.toggleChangedOnly()}
			/>
		</label>
		<Button
			variant="ghost"
			size="xs"
			onclick={() => files.refresh()}
			class="text-sidebar-foreground/70 hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"
		>
			<RefreshCwIcon class="size-3.5" />
			Refresh
		</Button>
	{/snippet}

	<Resizable.PaneGroup
		direction="horizontal"
		autoSaveId="discobot-ui-files-panel-layout-right"
		class="min-h-0 h-full flex-1"
	>
		<Resizable.Pane minSize={35} class="min-h-0">
			<div
				class="flex h-full min-h-0 flex-col overflow-hidden rounded-md border border-sidebar-border bg-sidebar shadow-sm"
			>
				<div
					class="flex min-h-10 items-end gap-1 overflow-x-auto border-b border-sidebar-border bg-sidebar px-2 py-2"
				>
					{#if openPaths.length === 0}
						<p class="px-2 text-sm text-sidebar-foreground/50">
							Open a file from the explorer.
						</p>
					{:else}
						{#each openPaths as path (path)}
							{@const status = files.diff.find(
								(entry) => entry.path === path,
							)?.status}
							<div
								role="button"
								tabindex={0}
								onclick={() => files.open(path)}
								onkeydown={(event) => {
									if (event.key === "Enter" || event.key === " ") {
										event.preventDefault();
										void files.open(path);
									}
								}}
								class={cn(
									"flex shrink-0 items-center gap-2 rounded-md border px-3 py-1.5 text-sm transition",
									activePath === path
										? "border-sidebar-border bg-background text-foreground shadow-sm"
										: "border-transparent bg-sidebar-accent/60 text-sidebar-foreground/75 hover:bg-sidebar-accent hover:text-sidebar-accent-foreground",
								)}
							>
								<span class="truncate max-w-36">{fileLabel(path)}</span>
								{#if isDirty(path)}
									<span class="size-2 rounded-full bg-sidebar-primary"></span>
								{/if}
								{#if status}
									<Badge
										variant="outline"
										class={cn(
											"px-1 py-0 text-[10px]",
											statusBadgeClass(status),
										)}
									>
										{statusLetter(status)}
									</Badge>
								{/if}
								<button
									type="button"
									onclick={(event) => {
										event.stopPropagation();
										requestCloseTab(path);
									}}
									class="rounded p-0.5 text-sidebar-foreground/45 transition hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"
								>
									<XIcon class="size-3.5" />
								</button>
							</div>
						{/each}
					{/if}
				</div>

				{#if activePath && activeBuffer}
					<div
						class="flex flex-wrap items-center justify-between gap-3 border-b border-sidebar-border bg-sidebar px-3 py-2 text-sm"
					>
						<div class="min-w-0">
							<p class="truncate font-mono text-sidebar-foreground">
								{activePath}
							</p>
							<div
								class="mt-1 flex items-center gap-2 text-xs text-sidebar-foreground/70"
							>
								{#if activeStatus}
									<span>{statusLetter(activeStatus)} · {activeStatus}</span>
								{/if}
								{#if activeBuffer.fromBase}
									<span>Deleted file · read only</span>
								{/if}
								{#if activeBuffer.isDirty}
									<span>Unsaved changes</span>
								{/if}
							</div>
						</div>
						<div class="flex flex-wrap items-center justify-end gap-2">
							{#if activeBuffer.saveError && !activeBuffer.hasConflict}
								<span class="text-xs text-destructive"
									>{activeBuffer.saveError}</span
								>
							{/if}
							<Button
								variant="ghost"
								size="icon-xs"
								onclick={downloadActiveFile}
								class="text-sidebar-foreground/55 hover:text-sidebar-accent-foreground"
								aria-label="Download file"
								title="Download file"
							>
								<DownloadIcon class="size-3.5" />
							</Button>
							{#if isMarkdownPreview}
								<ToggleGroup
									type="single"
									value={markdownViewMode}
									onValueChange={(value) => {
										if (
											value === "preview" ||
											value === "split" ||
											value === "editor"
										) {
											markdownViewMode = value;
										}
									}}
									variant="default"
									size="sm"
									spacing={1}
									class="rounded-md bg-transparent"
								>
									<ToggleGroupItem
										value="preview"
										class="w-7 px-0 text-sidebar-foreground/55 hover:text-sidebar-accent-foreground data-[state=on]:bg-background data-[state=on]:text-foreground"
										aria-label="Preview markdown"
										title="Preview"
									>
										<EyeIcon class="size-3.5" />
									</ToggleGroupItem>
									<ToggleGroupItem
										value="split"
										class="w-7 px-0 text-sidebar-foreground/55 hover:text-sidebar-accent-foreground data-[state=on]:bg-background data-[state=on]:text-foreground"
										aria-label="Split markdown view"
										title="Split"
									>
										<Columns2Icon class="size-3.5" />
									</ToggleGroupItem>
									<ToggleGroupItem
										value="editor"
										class="w-7 px-0 text-sidebar-foreground/55 hover:text-sidebar-accent-foreground data-[state=on]:bg-background data-[state=on]:text-foreground"
										aria-label="Edit markdown"
										title="Editor"
									>
										<FileCodeIcon class="size-3.5" />
									</ToggleGroupItem>
								</ToggleGroup>
							{/if}
							{#if activeBuffer.isDirty}
								<Button
									variant="outline"
									size="sm"
									onclick={() => files.discard(activePath)}
								>
									<XIcon class="size-4" />
									Discard
								</Button>
								<Button
									size="sm"
									disabled={!canEditActiveFile || activeBuffer.isSaving}
									onclick={() => files.save(activePath)}
								>
									{#if activeBuffer.isSaving}
										<RefreshCwIcon class="size-4 animate-spin" />
									{:else}
										<SaveIcon class="size-4" />
									{/if}
									Save
								</Button>
							{/if}
						</div>
					</div>
				{/if}

				{#if activeBuffer?.hasConflict && activePath}
					<Dialog open={true}>
						<DialogContent class="max-w-xl">
							<DialogHeader>
								<DialogTitle>File modified externally</DialogTitle>
								<DialogDescription>
									{activePath} changed on disk while you were editing it.
								</DialogDescription>
							</DialogHeader>
							<div
								class="rounded-md border border-yellow-500/30 bg-yellow-500/10 p-3 text-sm text-yellow-900 dark:text-yellow-100"
							>
								Choose whether to keep the disk version or overwrite it with
								your local changes.
							</div>
							<DialogFooter>
								<Button
									variant="outline"
									onclick={() => files.acceptConflict(activePath)}
								>
									<CheckIcon class="size-4" />
									Use disk version
								</Button>
								<Button onclick={() => files.forceSave(activePath)}>
									<SaveIcon class="size-4" />
									Save my changes
								</Button>
							</DialogFooter>
						</DialogContent>
					</Dialog>
				{/if}

				<Dialog bind:open={renameDialogOpen}>
					<DialogContent class="sm:max-w-md">
						<DialogHeader>
							<DialogTitle>Rename file</DialogTitle>
							<DialogDescription>
								Choose a new name for {renamePath
									? `"${renamePath}"`
									: "this file"}.
							</DialogDescription>
						</DialogHeader>
						<Input
							value={renameDraft}
							oninput={(event) => {
								renameDraft = (event.currentTarget as HTMLInputElement).value;
							}}
							onkeydown={handleRenameInputKeydown}
							maxlength={255}
							placeholder="File name"
						/>
						<DialogFooter>
							<Button
								variant="ghost"
								size="sm"
								onclick={closeRenameDialog}
								disabled={renaming}
							>
								Cancel
							</Button>
							<Button
								size="sm"
								onclick={() => void handleRename()}
								disabled={renaming || renameDraft.trim().length === 0}
							>
								Save
							</Button>
						</DialogFooter>
					</DialogContent>
				</Dialog>

				<AlertDialog bind:open={deleteDialogOpen}>
					<AlertDialogContent>
						<AlertDialogHeader>
							<AlertDialogTitle>Delete file?</AlertDialogTitle>
							<AlertDialogDescription>
								Delete {deletePath ? `"${deletePath}"` : "this file"}? This
								action cannot be undone.
							</AlertDialogDescription>
						</AlertDialogHeader>
						<AlertDialogFooter>
							<AlertDialogCancel
								onclick={closeDeleteDialog}
								disabled={deleting}
							>
								Cancel
							</AlertDialogCancel>
							<AlertDialogAction
								onclick={() => void handleDelete()}
								disabled={deleting}
							>
								Delete
							</AlertDialogAction>
						</AlertDialogFooter>
					</AlertDialogContent>
				</AlertDialog>

				<AlertDialog bind:open={closeTabDialogOpen}>
					<AlertDialogContent>
						<AlertDialogHeader>
							<AlertDialogTitle
								>Close tab with unsaved changes?</AlertDialogTitle
							>
							<AlertDialogDescription>
								{#if closeTabPath}
									Close "{fileLabel(closeTabPath)}"? Your unsaved draft will be
									kept in memory and restored if you reopen the file.
								{:else}
									Close this tab? Your unsaved draft will be kept in memory and
									restored if you reopen the file.
								{/if}
							</AlertDialogDescription>
						</AlertDialogHeader>
						<AlertDialogFooter>
							<AlertDialogCancel onclick={closeCloseTabDialog}>
								Keep open
							</AlertDialogCancel>
							<AlertDialogAction onclick={confirmCloseTab}>
								Close tab
							</AlertDialogAction>
						</AlertDialogFooter>
					</AlertDialogContent>
				</AlertDialog>

				<div class="relative min-h-0 flex-1 overflow-hidden bg-background">
					<div
						class={cn(
							"absolute inset-0 z-0 grid min-h-0",
							showMarkdownSplitView
								? "grid-cols-[minmax(0,1fr)_minmax(0,1fr)]"
								: "grid-cols-1",
						)}
					>
						<div class={cn("relative min-w-0", !showTextEditor && "hidden")}>
							<div bind:this={editorContainer} class="absolute inset-0"></div>
						</div>
						{#if showMarkdownPreview && activeBuffer}
							<div
								class={cn(
									"min-w-0 overflow-auto bg-card/10",
									showMarkdownSplitView && "border-l border-border",
								)}
							>
								<div class="mx-auto flex min-h-full w-full max-w-4xl p-6">
									<MessageResponse
										text={activeBuffer.content}
										mode="static"
										class="w-full text-sm [&>*:first-child]:mt-0 [&>*:last-child]:mb-0"
									/>
								</div>
							</div>
						{/if}
					</div>
					{#if !activePath}
						<div
							class="relative z-10 flex h-full items-center justify-center p-6 text-sm text-muted-foreground"
						>
							Select a file to start editing.
						</div>
					{:else if isImagePreview}
						<div
							class="relative z-10 flex h-full items-center justify-center overflow-auto bg-card/20 p-4"
						>
							<ImageAttachment
								src={imageSource}
								filename={fileLabel(activePath)}
								class="max-w-full rounded-md border border-border bg-background shadow-sm [&_img]:max-h-[calc(100vh-12rem)] [&_img]:object-contain"
							/>
						</div>
					{:else if isPdfPreview}
						<div class="relative z-10 h-full bg-card/10 p-4">
							<iframe
								src={pdfSource}
								title={`PDF preview: ${activePath}`}
								class="size-full rounded-md border border-border bg-background shadow-sm"
							></iframe>
						</div>
					{:else if isBinaryPreview}
						<div
							class="relative z-10 flex h-full items-center justify-center p-6 text-center text-sm text-muted-foreground"
						>
							Binary file preview is not supported for {activePath}.
						</div>
					{:else if !canRenderTextEditor}
						<div
							class="relative z-10 flex h-full items-center justify-center p-6 text-sm text-muted-foreground"
						>
							Loading file contents…
						</div>
					{/if}
				</div>
			</div>
		</Resizable.Pane>
		<Resizable.Handle class="w-3 bg-transparent after:w-3" />
		<Resizable.Pane defaultSize={24} minSize={16} maxSize={40} class="min-h-0">
			<div
				class="h-full min-h-0 overflow-auto rounded-md border border-sidebar-border bg-sidebar shadow-sm"
			>
				<div
					class="flex items-center justify-between gap-2 border-b border-sidebar-border px-3 py-2 text-xs text-sidebar-foreground/70"
				>
					<span>Explorer</span>
					<div class="flex items-center gap-2">
						<button
							type="button"
							class="hover:text-sidebar-accent-foreground"
							onclick={() => files.expandAll()}
						>
							Expand all
						</button>
						<button
							type="button"
							class="hover:text-sidebar-accent-foreground"
							onclick={files.collapseAll}
						>
							Collapse
						</button>
					</div>
				</div>
				<div class="space-y-0.5 p-2 font-mono text-xs">
					{#snippet treeRow(
						node: SessionFileTreeNode,
						depth: number,
						selected: boolean,
						expanded: boolean,
						Icon: typeof FolderIcon,
					)}
						<button
							type="button"
							onclick={() =>
								node.type === "directory"
									? files.toggleDirectory(node.path)
									: files.open(node.path)}
							class={treeButtonClass(node, selected)}
							style={`padding-left: ${8 + depth * 14}px`}
						>
							{#if node.type === "directory"}
								{#if files.isPathLoading(node.path)}
									<RefreshCwIcon
										class="size-3.5 animate-spin text-sidebar-foreground/40"
									/>
								{:else if expanded}
									<ChevronDownIcon
										class="size-3.5 text-sidebar-foreground/40"
									/>
								{:else}
									<ChevronRightIcon
										class="size-3.5 text-sidebar-foreground/40"
									/>
								{/if}
							{:else}
								<span class="size-3.5 shrink-0"></span>
							{/if}
							<Icon
								class={cn(
									"size-4 shrink-0",
									node.type === "directory" &&
										node.changed &&
										"text-yellow-500",
									node.type === "directory" &&
										!node.changed &&
										"text-sidebar-foreground/55",
									node.status === "added" && "text-green-500",
									node.status === "modified" && "text-yellow-500",
									node.status === "deleted" && "text-red-500",
									node.status === "renamed" && "text-purple-500",
									node.type === "file" &&
										!node.status &&
										"text-sidebar-foreground/55",
								)}
							/>
							<span class="min-w-0 flex-1 truncate">{node.name}</span>
							{#if node.type === "file" && node.status}
								<span
									class={cn(
										"text-[10px] font-semibold",
										statusBadgeClass(node.status),
									)}
								>
									{statusLetter(node.status)}
								</span>
							{/if}
						</button>
					{/snippet}
					{#snippet renderTree(nodes: SessionFileTreeNode[], depth: number)}
						{#each nodes as node (node.path)}
							{@const selected = isSelected(node.path)}
							{@const expanded =
								node.type === "directory" && isExpanded(node.path)}
							{@const Icon = nodeIcon(node, expanded)}
							{#if node.type === "file"}
								<ContextMenu>
									<ContextMenuTrigger>
										{@render treeRow(node, depth, selected, expanded, Icon)}
									</ContextMenuTrigger>
									<ContextMenuContent class="w-36">
										<ContextMenuItem
											onclick={() => openRenameDialog(node)}
											disabled={!canManageNode(node)}
										>
											Rename
										</ContextMenuItem>
										<ContextMenuItem
											variant="destructive"
											onclick={() => openDeleteDialog(node)}
											disabled={!canManageNode(node)}
										>
											Delete
										</ContextMenuItem>
									</ContextMenuContent>
								</ContextMenu>
							{:else}
								{@render treeRow(node, depth, selected, expanded, Icon)}
							{/if}
							{#if node.type === "directory" && expanded && node.children?.length}
								{@render renderTree(node.children, depth + 1)}
							{/if}
						{/each}
					{/snippet}
					{#if files.tree.length === 0}
						<p class="px-2 py-6 text-center text-sm text-sidebar-foreground/50">
							{files.showChangedOnly ? "No changed files" : "No files"}
						</p>
					{:else}
						{@render renderTree(files.tree, 0)}
					{/if}
				</div>
			</div>
		</Resizable.Pane>
	</Resizable.PaneGroup>
</DockWindowChrome>
