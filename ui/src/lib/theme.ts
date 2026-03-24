export type ResolvedTheme = "dark" | "light";
export type ThemeMode = ResolvedTheme | "system";

export type ThemeColorScheme =
	| "default"
	| "nord"
	| "tokyo-night"
	| "solarized"
	| "dracula"
	| "catppuccin-mocha"
	| "catppuccin-macchiato"
	| "catppuccin-frappe"
	| "alucard"
	| "catppuccin-latte";

export type ThemeMetadata = {
	id: ThemeColorScheme;
	name: string;
	mode: ResolvedTheme;
	preview: {
		background: string;
		primary: string;
		foreground: string;
	};
};

const THEME_KEY = "theme";
const COLOR_SCHEME_KEY = "theme.colorScheme";

export const THEME_METADATA: ThemeMetadata[] = [
	{
		id: "default",
		name: "Default",
		mode: "light",
		preview: {
			background: "#fafafa",
			primary: "#3b82f6",
			foreground: "#262626",
		},
	},
	{
		id: "solarized",
		name: "Solarized",
		mode: "light",
		preview: {
			background: "#fdf6e3",
			primary: "#268bd2",
			foreground: "#657b83",
		},
	},
	{
		id: "alucard",
		name: "Alucard",
		mode: "light",
		preview: {
			background: "#fffbeb",
			primary: "#644ac9",
			foreground: "#1f1f1f",
		},
	},
	{
		id: "catppuccin-latte",
		name: "Catppuccin Latte",
		mode: "light",
		preview: {
			background: "#eff1f5",
			primary: "#1e66f5",
			foreground: "#4c4f69",
		},
	},
	{
		id: "default",
		name: "Default",
		mode: "dark",
		preview: {
			background: "#1e1e1e",
			primary: "#3b82f6",
			foreground: "#ededed",
		},
	},
	{
		id: "nord",
		name: "Nord",
		mode: "dark",
		preview: {
			background: "#2e3440",
			primary: "#88c0d0",
			foreground: "#d8dee9",
		},
	},
	{
		id: "tokyo-night",
		name: "Tokyo Night",
		mode: "dark",
		preview: {
			background: "#1a1b26",
			primary: "#7aa2f7",
			foreground: "#a9b1d6",
		},
	},
	{
		id: "dracula",
		name: "Dracula",
		mode: "dark",
		preview: {
			background: "#282a36",
			primary: "#bd93f9",
			foreground: "#f8f8f2",
		},
	},
	{
		id: "catppuccin-mocha",
		name: "Catppuccin Mocha",
		mode: "dark",
		preview: {
			background: "#1e1e2e",
			primary: "#89b4fa",
			foreground: "#cdd6f4",
		},
	},
	{
		id: "catppuccin-macchiato",
		name: "Catppuccin Macchiato",
		mode: "dark",
		preview: {
			background: "#24273a",
			primary: "#8aadf4",
			foreground: "#cad3f5",
		},
	},
	{
		id: "catppuccin-frappe",
		name: "Catppuccin Frappé",
		mode: "dark",
		preview: {
			background: "#303446",
			primary: "#8caaee",
			foreground: "#c6d0f5",
		},
	},
];

function resolveStoredThemeMode(): ThemeMode | null {
	if (typeof window === "undefined") {
		return null;
	}

	const storedTheme = window.localStorage.getItem(THEME_KEY);
	return storedTheme === "light" ||
		storedTheme === "dark" ||
		storedTheme === "system"
		? storedTheme
		: null;
}

function resolveStoredColorScheme(): ThemeColorScheme | null {
	if (typeof window === "undefined") {
		return null;
	}

	const storedScheme = window.localStorage.getItem(COLOR_SCHEME_KEY);
	return storedScheme === "default" ||
		storedScheme === "nord" ||
		storedScheme === "tokyo-night" ||
		storedScheme === "solarized" ||
		storedScheme === "dracula" ||
		storedScheme === "catppuccin-mocha" ||
		storedScheme === "catppuccin-macchiato" ||
		storedScheme === "catppuccin-frappe" ||
		storedScheme === "alucard" ||
		storedScheme === "catppuccin-latte"
		? storedScheme
		: null;
}

function resolvePreferredTheme(): ResolvedTheme {
	if (typeof window === "undefined") {
		return "dark";
	}

	return window.matchMedia("(prefers-color-scheme: dark)").matches
		? "dark"
		: "light";
}

export function resolveThemeMode(theme: ThemeMode): ResolvedTheme {
	return theme === "system" ? resolvePreferredTheme() : theme;
}

export function getThemeMode(): ThemeMode {
	return resolveStoredThemeMode() ?? "system";
}

export function getTheme(): ResolvedTheme {
	return resolveThemeMode(getThemeMode());
}

export function getColorScheme(): ThemeColorScheme {
	return resolveStoredColorScheme() ?? "default";
}

export function applyTheme(theme: ThemeMode): ThemeMode {
	if (typeof document === "undefined") {
		return theme;
	}

	const resolved = resolveThemeMode(theme);
	document.documentElement.classList.toggle("dark", resolved === "dark");

	if (typeof window !== "undefined") {
		window.localStorage.setItem(THEME_KEY, theme);
	}

	return theme;
}

export function applyColorScheme(scheme: ThemeColorScheme): ThemeColorScheme {
	if (typeof document === "undefined") {
		return scheme;
	}

	document.documentElement.setAttribute("data-theme", scheme);

	if (typeof window !== "undefined") {
		window.localStorage.setItem(COLOR_SCHEME_KEY, scheme);
	}

	return scheme;
}

export function getAvailableThemes(mode: ResolvedTheme): ThemeMetadata[] {
	return THEME_METADATA.filter((theme) => theme.mode === mode);
}

export function toggleTheme(): ResolvedTheme {
	const nextTheme = getTheme() === "dark" ? "light" : "dark";
	applyTheme(nextTheme);
	return nextTheme;
}
