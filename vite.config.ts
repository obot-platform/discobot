import { readFileSync } from "node:fs";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig, type Plugin } from "vite";
import tsconfigPaths from "vite-tsconfig-paths";
import "dotenv/config";

// Fix @novnc/novnc CJS build: its babel transpilation incorrectly emits
// top-level `await` in a non-async CJS context, which Rollup cannot parse.
// We intercept the file load and replace the broken await with a sync default.
function fixNoVncCjs(): Plugin {
	return {
		name: "fix-novnc-cjs",
		enforce: "pre",
		load(id) {
			if (id.includes("@novnc/novnc") && id.endsWith("browser.js")) {
				const code = readFileSync(id, "utf-8");
				return code.replace(
					/= await _checkWebCodecsH264DecodeSupport\(\)/g,
					"= false",
				);
			}
		},
	};
}

// Plugin to inject React DevTools script before React loads
function reactDevTools(): Plugin {
	const devToolsUrl = process.env.REACT_DEVTOOLS_URL;
	return {
		name: "react-devtools",
		transformIndexHtml(html) {
			if (!devToolsUrl) return html;

			// Inject script tag before the closing </head>
			return html.replace(
				"<!-- React DevTools - will be injected by Vite plugin -->",
				`<script src="${devToolsUrl}"></script>`,
			);
		},
	};
}

export default defineConfig({
	plugins: [
		fixNoVncCjs(),
		react({
			babel: {
				plugins: [["babel-plugin-react-compiler", {}]],
			},
		}),
		tailwindcss(),
		tsconfigPaths({
			// Only parse root tsconfig, ignore workspace directories
			projects: ["./tsconfig.json"],
		}),
		reactDevTools(),
	],
	define: {
		// Prevent process.env errors in browser
		"process.env.NODE_ENV": JSON.stringify(
			process.env.NODE_ENV || "development",
		),
	},
	server: {
		port: 3000,
		strictPort: true,
	},
	// Tauri expects a fixed port
	preview: {
		port: 3000,
		strictPort: true,
	},
	build: {
		outDir: "dist",
		emptyOutDir: true,
		// Optimize chunks
		rollupOptions: {
			output: {
				manualChunks: {
					monaco: ["@monaco-editor/react"],
					xterm: ["@xterm/xterm", "@xterm/addon-fit", "@xterm/addon-web-links"],
					// Combine ai-sdk and radix into one chunk to avoid circular dependency
					// (ai-sdk uses radix components internally)
					"ui-core": [
						"ai",
						"@ai-sdk/react",
						"@radix-ui/react-dialog",
						"@radix-ui/react-dropdown-menu",
						"@radix-ui/react-select",
						"@radix-ui/react-tabs",
						"@radix-ui/react-toast",
						"@radix-ui/react-tooltip",
						"@radix-ui/react-accordion",
					],
				},
			},
		},
		// Keep bundle-size warnings above the current known largest legacy React chunk.
		chunkSizeWarningLimit: 2200,
	},
	// Handle SSE streaming properly
	optimizeDeps: {
		exclude: ["@tauri-apps/api", "@tauri-apps/plugin-shell"],
		// Fix @novnc/novnc CJS build for esbuild pre-bundling (dev mode).
		// Same issue as the fixNoVncCjs Rollup plugin above, but for esbuild.
		esbuildOptions: {
			plugins: [
				{
					name: "fix-novnc-cjs",
					setup(build) {
						build.onLoad(
							{ filter: /browser\.js$/, namespace: "file" },
							(args) => {
								if (!args.path.includes("@novnc/novnc")) return;
								const code = readFileSync(args.path, "utf-8");
								return {
									contents: code.replace(
										/= await _checkWebCodecsH264DecodeSupport\(\)/g,
										"= false",
									),
									loader: "js",
								};
							},
						);
					},
				},
			],
		},
	},
	// Clear screen disabled for better logging during dev
	clearScreen: false,
});
