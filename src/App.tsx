import { type ReactNode, useEffect } from "react";
import { Route, Routes } from "react-router";
import { AppShell } from "@/components/app-shell";
import { MonacoEditorFix } from "@/components/monaco-editor-fix";
import { ResizeObserverFix } from "@/components/resize-observer-fix";
import { ThemeProvider } from "@/components/theme-provider";
import { Toaster } from "@/components/ui/sonner";
import { TooltipProvider } from "@/components/ui/tooltip";
import { isTauri } from "@/lib/api-config";
import { UpdateProvider } from "@/lib/contexts/update-context";
import { HomePage } from "./pages/HomePage";

function MaybeUpdateProvider({ children }: { children: ReactNode }) {
	if (!isTauri()) return <>{children}</>;
	return <UpdateProvider>{children}</UpdateProvider>;
}

export function App() {
	// Initialize theme attribute from localStorage on mount
	useEffect(() => {
		const saved = localStorage.getItem("theme.colorScheme") || "default";
		document.documentElement.setAttribute("data-theme", saved);
	}, []);

	return (
		<ThemeProvider
			attribute="class"
			defaultTheme="dark"
			enableSystem
			disableTransitionOnChange
			storageKey="theme"
		>
			<TooltipProvider delayDuration={700}>
				<ResizeObserverFix />
				<MonacoEditorFix />
				<Toaster />
				<MaybeUpdateProvider>
					<AppShell>
						<Routes>
							<Route path="/" element={<HomePage />} />
						</Routes>
					</AppShell>
				</MaybeUpdateProvider>
			</TooltipProvider>
		</ThemeProvider>
	);
}
