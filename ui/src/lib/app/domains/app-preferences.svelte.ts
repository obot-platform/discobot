import {
	CHAT_WIDTH_MODE_STORAGE_KEY,
	DEFAULT_MODEL_STORAGE_KEY,
	DEFAULT_PREFERRED_IDE,
	DEFAULT_RECENT_THREADS_VISIBLE_LIMIT,
	PINNED_PROMPTS_STORAGE_KEY,
	PROMPT_HISTORY_STORAGE_KEY,
	PREFERRED_IDE_STORAGE_KEY,
	readPinnedPrompts,
	readPromptHistory,
	readRecentThreadsVisibleLimit,
	SIDEBAR_ALL_OPEN_STORAGE_KEY,
	SIDEBAR_ALL_GROUPED_STORAGE_KEY,
	SIDEBAR_RECENT_OPEN_STORAGE_KEY,
	RECENT_THREADS_VISIBLE_LIMIT_STORAGE_KEY,
	readChatWidthMode,
	readDefaultModel,
	readPreferredIde,
	readSidebarAllOpen,
	readSidebarAllGrouped,
	readSidebarRecentOpen,
	writeStorage,
} from "$lib/app/app-helpers";
import type {
	AppContextBootstrap,
	AppPreferences,
	ChatWidthMode,
} from "$lib/app/app-context.types";
import type { ThemeColorScheme } from "$lib/api-types";
import {
	appendPromptHistoryEntry,
	appendPinnedPrompt,
	removePinnedPrompt,
	removePromptHistoryEntry,
} from "$lib/prompt-history-storage";
import type { PreferredIde } from "$lib/shell-types";
import {
	applyColorScheme,
	applyTheme,
	getAvailableThemes,
	getColorScheme,
	getThemeMode,
	resolveThemeMode,
	type ResolvedTheme,
	type ThemeMode,
} from "$lib/theme";

type CreateAppPreferencesDomainArgs = {
	bootstrap: AppContextBootstrap;
};

export function createAppPreferencesDomain(
	args: CreateAppPreferencesDomainArgs,
): AppPreferences {
	let theme = $state<ThemeMode>("system");
	let resolvedTheme = $state<ResolvedTheme>("dark");
	let colorScheme = $state<ThemeColorScheme>("default");
	let preferredIde = $state<PreferredIde>(DEFAULT_PREFERRED_IDE);
	let chatWidthMode = $state<ChatWidthMode>("constrained");
	let defaultModel = $state("");
	let recentThreadsVisibleLimit = $state(DEFAULT_RECENT_THREADS_VISIBLE_LIMIT);
	let sidebarRecentOpen = $state(true);
	let sidebarAllOpen = $state(true);
	let sidebarAllGroupedByWorkspace = $state(true);
	let promptHistory = $state<string[]>([]);
	let pinnedPrompts = $state<string[]>([]);

	const availableThemes = $derived.by(() => getAvailableThemes(resolvedTheme));

	const ensureColorSchemeForMode = () => {
		if (!availableThemes.some((t) => t.id === colorScheme)) {
			colorScheme = "default";
		}
	};

	const syncAppliedColorScheme = () => {
		colorScheme = applyColorScheme(colorScheme);
	};

	const applyThemeState = (mode: ThemeMode) => {
		theme = applyTheme(mode);
		resolvedTheme = resolveThemeMode(theme);
		ensureColorSchemeForMode();
		syncAppliedColorScheme();
	};

	const initializePreferences = () => {
		const savedColorScheme = getColorScheme();
		applyThemeState(getThemeMode());
		colorScheme = savedColorScheme;
		ensureColorSchemeForMode();
		syncAppliedColorScheme();
		preferredIde = readPreferredIde();
		chatWidthMode = readChatWidthMode();
		defaultModel = readDefaultModel();
		recentThreadsVisibleLimit = readRecentThreadsVisibleLimit();
		sidebarRecentOpen = readSidebarRecentOpen();
		sidebarAllOpen = readSidebarAllOpen();
		sidebarAllGroupedByWorkspace = readSidebarAllGrouped();
		promptHistory = readPromptHistory();
		pinnedPrompts = readPinnedPrompts();
	};

	// Initialize on construction
	initializePreferences();

	if (typeof window !== "undefined") {
		const mediaQuery = window.matchMedia("(prefers-color-scheme: dark)");
		mediaQuery.addEventListener("change", () => {
			if (theme !== "system") return;
			applyThemeState("system");
		});
	}

	return {
		get theme() {
			return theme;
		},
		get resolvedTheme() {
			return resolvedTheme;
		},
		get colorScheme() {
			return colorScheme;
		},
		get availableThemes() {
			return availableThemes;
		},
		get promptHistory() {
			return promptHistory;
		},
		get pinnedPrompts() {
			return pinnedPrompts;
		},
		get preferredIde() {
			return preferredIde;
		},
		ideOptions: args.bootstrap.ideOptions,
		get chatWidthMode() {
			return chatWidthMode;
		},
		get defaultModel() {
			return defaultModel;
		},
		get recentThreadsVisibleLimit() {
			return recentThreadsVisibleLimit;
		},
		get sidebarRecentOpen() {
			return sidebarRecentOpen;
		},
		get sidebarAllOpen() {
			return sidebarAllOpen;
		},
		get sidebarAllGroupedByWorkspace() {
			return sidebarAllGroupedByWorkspace;
		},
		setTheme: (mode) => applyThemeState(mode),
		setColorScheme: (scheme) => {
			if (!availableThemes.some((t) => t.id === scheme)) return;
			colorScheme = applyColorScheme(scheme);
		},
		toggleTheme: () =>
			applyThemeState(resolvedTheme === "dark" ? "light" : "dark"),
		addPromptToHistory: (prompt) => {
			promptHistory = appendPromptHistoryEntry(promptHistory, prompt);
			writeStorage(PROMPT_HISTORY_STORAGE_KEY, JSON.stringify(promptHistory));
		},
		removePromptFromHistory: (prompt) => {
			promptHistory = removePromptHistoryEntry(promptHistory, prompt);
			writeStorage(PROMPT_HISTORY_STORAGE_KEY, JSON.stringify(promptHistory));
		},
		pinPrompt: (prompt) => {
			pinnedPrompts = appendPinnedPrompt(pinnedPrompts, prompt);
			writeStorage(PINNED_PROMPTS_STORAGE_KEY, JSON.stringify(pinnedPrompts));
		},
		unpinPrompt: (prompt) => {
			pinnedPrompts = removePinnedPrompt(pinnedPrompts, prompt);
			writeStorage(PINNED_PROMPTS_STORAGE_KEY, JSON.stringify(pinnedPrompts));
		},
		isPromptPinned: (prompt) => pinnedPrompts.includes(prompt),
		setPreferredIde: (ide) => {
			preferredIde = ide;
			writeStorage(PREFERRED_IDE_STORAGE_KEY, ide);
		},
		setChatWidthMode: (mode) => {
			chatWidthMode = mode;
			writeStorage(CHAT_WIDTH_MODE_STORAGE_KEY, mode);
		},
		setDefaultModel: (modelId) => {
			defaultModel = modelId;
			writeStorage(DEFAULT_MODEL_STORAGE_KEY, modelId || null);
		},
		setRecentThreadsVisibleLimit: (value) => {
			recentThreadsVisibleLimit = value;
			writeStorage(RECENT_THREADS_VISIBLE_LIMIT_STORAGE_KEY, String(value));
		},
		setSidebarRecentOpen: (value) => {
			sidebarRecentOpen = value;
			writeStorage(SIDEBAR_RECENT_OPEN_STORAGE_KEY, String(value));
		},
		setSidebarAllOpen: (value) => {
			sidebarAllOpen = value;
			writeStorage(SIDEBAR_ALL_OPEN_STORAGE_KEY, String(value));
		},
		setSidebarAllGroupedByWorkspace: (value) => {
			sidebarAllGroupedByWorkspace = value;
			writeStorage(SIDEBAR_ALL_GROUPED_STORAGE_KEY, String(value));
		},
	};
}
