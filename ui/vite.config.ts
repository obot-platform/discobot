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

function trackSSRBuild(): Plugin {
	let isSSRBuild = false;

	return {
		name: "track-ssr-build",
		configResolved(config) {
			isSSRBuild = Boolean(config.build.ssr);
		},
		config() {
			return {
				build: {
					rollupOptions: {
						output: {
							manualChunks(id: string) {
								if (isSSRBuild || !id.includes("node_modules")) {
									return;
								}

								// Isolate Monaco into its own chunk so Rollup can GC the rest
								// of the module graph before processing it. Monaco's source is
								// ~75 MB and includes 5 large worker bundles (including the full
								// TypeScript compiler), making it the primary driver of OOM
								// failures during CI builds.
								if (id.includes("/monaco-editor/")) {
									return "monaco-editor";
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
			};
		},
	};
}

export default defineConfig(() => ({
	plugins: [
		trackSSRBuild(),
		fixNoVncCjs(),
		sveltekit(),
		tailwindcss(),
		devtoolsJson(),
	],
	server: { port: 3100, strictPort: true },
	preview: { port: 3100, strictPort: true },
	worker: {
		format: "es",
	},
	clearScreen: false,
	build: {
		sourcemap: true,
		// Increase chunk size warning limit for the Svelte UI bundle.
		chunkSizeWarningLimit: 4000,
		rollupOptions: {
			// Limit parallel file reads to reduce peak memory during the build.
			maxParallelFileOps: 20,
			onwarn(warning, defaultHandler) {
				defaultHandler(warning);
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
}));
