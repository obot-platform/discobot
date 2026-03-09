import tailwindcss from "@tailwindcss/vite";
import { sveltekit } from "@sveltejs/kit/vite";
import { defineConfig } from "vite";

export default defineConfig({
	plugins: [sveltekit(), tailwindcss()],
	server: {
		port: 3100,
		strictPort: true,
	},
	preview: {
		port: 3100,
		strictPort: true,
	},
	clearScreen: false,
});
