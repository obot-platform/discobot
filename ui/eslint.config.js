import js from "@eslint/js";
import svelte from "eslint-plugin-svelte";
import globals from "globals";
import tseslint from "typescript-eslint";
import svelteConfig from "./svelte.config.js";

export default tseslint.config(
	{
		ignores: [".svelte-kit/**", "build/**", "dist/**", "coverage/**"],
	},
	js.configs.recommended,
	...tseslint.configs.recommended,
	...svelte.configs.recommended,
	{
		languageOptions: {
			globals: {
				...globals.browser,
				...globals.node,
			},
		},
	},
	{
		files: ["**/*.svelte", "**/*.svelte.js", "**/*.svelte.ts"],
		languageOptions: {
			parserOptions: {
				extraFileExtensions: [".svelte"],
				parser: tseslint.parser,
				svelteConfig,
			},
		},
	},
	{
		rules: {
			"@typescript-eslint/no-explicit-any": "off",
			"@typescript-eslint/no-unused-vars": "off",
			"no-undef": "off",
			"no-useless-assignment": "off",
			"svelte/no-immutable-reactive-statements": "off",
			"svelte/no-navigation-without-resolve": "off",
			"svelte/prefer-writable-derived": "off",
			"svelte/require-each-key": "off",
		},
	},
);
