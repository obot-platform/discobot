import {
	CHAT_WIDTH_MODE_STORAGE_KEY,
	DEFAULT_MODEL_STORAGE_KEY,
	PREFERRED_IDE_STORAGE_KEY,
	readChatWidthMode,
	readDefaultModel,
	readPreferredIde,
	writeStorage,
} from "$lib/app/app-helpers";
import type { AppContextBootstrap, AppPreferences, ChatWidthMode } from "$lib/app/app-context.types";
import type { ThemeColorScheme } from "$lib/api-types";
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

export function createAppPreferencesDomain(args: CreateAppPreferencesDomainArgs): AppPreferences {
	let theme = $state<ThemeMode>("system");
	let resolvedTheme = $state<ResolvedTheme>("dark");
	let colorScheme = $state<ThemeColorScheme>("default");
	let preferredIde = $state<PreferredIde>(args.bootstrap.ideOptions[0]?.id ?? "cursor");
	let chatWidthMode = $state<ChatWidthMode>("constrained");
	let defaultModel = $state("");

	const availableThemes = $derived.by(() => getAvailableThemes(resolvedTheme));

	const ensureColorSchemeForMode = () => {
		if (!availableThemes.some((t) => t.id === colorScheme)) {
			colorScheme = "default";
		}
	};

	const applyThemeState = (mode: ThemeMode) => {
		theme = applyTheme(mode);
		resolvedTheme = resolveThemeMode(theme);
		ensureColorSchemeForMode();
		colorScheme = applyColorScheme(colorScheme);
	};

	// Initialize on construction
	applyThemeState(getThemeMode());
	colorScheme = getColorScheme();
	ensureColorSchemeForMode();
	colorScheme = applyColorScheme(colorScheme);
	preferredIde = readPreferredIde();
	chatWidthMode = readChatWidthMode();
	defaultModel = readDefaultModel();

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
		setTheme: (mode) => applyThemeState(mode),
		setColorScheme: (scheme) => {
			if (!availableThemes.some((t) => t.id === scheme)) return;
			colorScheme = applyColorScheme(scheme);
		},
		toggleTheme: () => applyThemeState(resolvedTheme === "dark" ? "light" : "dark"),
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
	};
}
