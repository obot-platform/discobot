import tsParser from "@typescript-eslint/parser";
import reactCompiler from "eslint-plugin-react-compiler";

export default [
	{
		// Ignore build outputs, dependencies, and Go code
		ignores: [
			"dist/**",
			"out/**",
			"node_modules/**",
			"ui/**",
			"server/**",
			"proxy/**",
			"agent/**",
			"src-tauri/**",
			".claude/**",
			".cursor/**",
			".opencode/**",
			".next/**",
			"scripts/**",
		],
	},
	{
		files: ["**/*.{js,jsx,ts,tsx}"],
		languageOptions: {
			parser: tsParser,
			parserOptions: {
				ecmaVersion: "latest",
				sourceType: "module",
				ecmaFeatures: {
					jsx: true,
				},
			},
		},
		plugins: {
			"react-compiler": reactCompiler,
		},
		rules: {
			"react-compiler/react-compiler": "error",
		},
	},
];
