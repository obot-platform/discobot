import { readFileSync } from "node:fs";
import devtoolsJson from "vite-plugin-devtools-json";
import tailwindcss from "@tailwindcss/vite";
import { sveltekit } from "@sveltejs/kit/vite";
import { defineConfig, type Plugin } from "vite";

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

export default defineConfig({
	plugins: [fixNoVncCjs(), sveltekit(), tailwindcss(), devtoolsJson()],
	server: { port: 3100, strictPort: true },
	preview: { port: 3100, strictPort: true },
	worker: {
		format: "es",
	},
	clearScreen: false,
	build: {
		// Increase chunk size warning limit for the Svelte UI bundle.
		chunkSizeWarningLimit: 4000,
		rollupOptions: {
			onwarn(warning, defaultHandler) {
				if (
					warning.code === "MODULE_LEVEL_DIRECTIVE" &&
					typeof warning.id === "string" &&
					warning.id.includes("/streamdown/")
				) {
					return;
				}
				defaultHandler(warning);
			},
			output: {
				manualChunks(id) {
					if (!id.includes("node_modules")) {
						return;
					}

					if (id.includes("/streamdown/") || id.includes("/@streamdown/")) {
						return "streamdown";
					}

					if (
						id.includes("/svelte/") ||
						id.includes("/@sveltejs/") ||
						id.includes("/bits-ui/") ||
						id.includes("/runed/") ||
						id.includes("/svelte-toolbelt/") ||
						id.includes("/paneforge/")
					) {
						return "svelte-core";
					}
				},
			},
		},
	},
	optimizeDeps: {
		include: ["@novnc/novnc/lib/rfb"],
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
});
