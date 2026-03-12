import {
	CHAT_WIDTH_MODE_STORAGE_KEY,
	DEFAULT_MODEL_STORAGE_KEY,
	IGNORED_UPDATE_VERSION_STORAGE_KEY,
	PREFERRED_IDE_STORAGE_KEY,
	readChatWidthMode,
	readDefaultModel,
	readIgnoredUpdateVersion,
	readPreferredIde,
	writeStorage,
} from "$lib/app/app-helpers";
import type { AppStore } from "$lib/app/store/app-store.svelte";
import { applyColorScheme, applyTheme, resolveThemeMode, getColorScheme, getThemeMode } from "$lib/theme";
import type { ThemeColorScheme } from "$lib/api-types";
import type { ThemeMode } from "$lib/theme";

export type AppPreferencesService = {
	initialize: () => void;
	setTheme: (theme: ThemeMode) => void;
	setColorScheme: (scheme: ThemeColorScheme) => void;
	toggleTheme: () => void;
	setPreferredIde: (ide: AppStore["preferredIde"]) => void;
	setChatWidthMode: (mode: AppStore["chatWidthMode"]) => void;
	setDefaultModel: (modelId: string) => void;
};

type CreateAppPreferencesServiceArgs = {
	store: AppStore;
};

export function createAppPreferencesService(
	args: CreateAppPreferencesServiceArgs,
): AppPreferencesService {
	const ensureColorSchemeForMode = () => {
		if (!args.store.availableThemes.some((theme) => theme.id === args.store.colorScheme)) {
			args.store.colorScheme = "default";
		}
	};

	const applyThemeState = (theme: ThemeMode) => {
		args.store.theme = applyTheme(theme);
		args.store.resolvedTheme = resolveThemeMode(args.store.theme);
		ensureColorSchemeForMode();
		args.store.colorScheme = applyColorScheme(args.store.colorScheme);
	};

	return {
		initialize: () => {
			applyThemeState(getThemeMode());
			args.store.colorScheme = getColorScheme();
			ensureColorSchemeForMode();
			args.store.colorScheme = applyColorScheme(args.store.colorScheme);
			args.store.preferredIde = readPreferredIde();
			args.store.chatWidthMode = readChatWidthMode();
			args.store.defaultModel = readDefaultModel();
			args.store.ignoredUpdateVersion = readIgnoredUpdateVersion();

			if (typeof window !== "undefined") {
				const mediaQuery = window.matchMedia("(prefers-color-scheme: dark)");
				mediaQuery.addEventListener("change", () => {
					if (args.store.theme !== "system") {
						return;
					}
					applyThemeState("system");
				});
			}
		},
		setTheme: (theme) => {
			applyThemeState(theme);
		},
		setColorScheme: (scheme) => {
			if (!args.store.availableThemes.some((theme) => theme.id === scheme)) {
				return;
			}
			args.store.colorScheme = applyColorScheme(scheme);
		},
		toggleTheme: () => {
			applyThemeState(args.store.resolvedTheme === "dark" ? "light" : "dark");
		},
		setPreferredIde: (ide) => {
			args.store.preferredIde = ide;
			writeStorage(PREFERRED_IDE_STORAGE_KEY, ide);
		},
		setChatWidthMode: (mode) => {
			args.store.chatWidthMode = mode;
			writeStorage(CHAT_WIDTH_MODE_STORAGE_KEY, mode);
		},
		setDefaultModel: (modelId) => {
			args.store.defaultModel = modelId;
			writeStorage(DEFAULT_MODEL_STORAGE_KEY, modelId || null);
		},
	};
}

export function ignoreVersionPreference(version: string | null) {
	writeStorage(IGNORED_UPDATE_VERSION_STORAGE_KEY, version);
}
