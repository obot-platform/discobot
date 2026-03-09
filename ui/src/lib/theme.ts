export type ThemeMode = "dark" | "light";

const THEME_KEY = "theme";

function resolveStoredTheme(): ThemeMode | null {
	if (typeof window === "undefined") {
		return null;
	}

	const storedTheme = window.localStorage.getItem(THEME_KEY);
	return storedTheme === "light" || storedTheme === "dark"
		? storedTheme
		: null;
}

function resolvePreferredTheme(): ThemeMode {
	if (typeof window === "undefined") {
		return "dark";
	}

	return window.matchMedia("(prefers-color-scheme: dark)").matches
		? "dark"
		: "light";
}

export function getTheme(): ThemeMode {
	return resolveStoredTheme() ?? resolvePreferredTheme();
}

export function applyTheme(theme: ThemeMode): ThemeMode {
	if (typeof document === "undefined") {
		return theme;
	}

	document.documentElement.classList.toggle("dark", theme === "dark");

	if (typeof window !== "undefined") {
		window.localStorage.setItem(THEME_KEY, theme);
	}

	return theme;
}

export function toggleTheme(): ThemeMode {
	return applyTheme(getTheme() === "dark" ? "light" : "dark");
}
