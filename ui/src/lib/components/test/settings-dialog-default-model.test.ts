import { readFileSync } from "node:fs";
import test from "node:test";
import assert from "node:assert/strict";

const source = readFileSync(
	new URL("../app/SettingsDialog.svelte", import.meta.url),
	"utf8",
);

test("settings dialog groups default models by provider with optgroups", () => {
	assert.match(
		source,
		/const modelProviderEntries = \$derived\.by\(\(\) => \{/,
	);
	assert.match(source, /<optgroup label=\{provider\}>/);
	assert.match(
		source,
		/\{#each modelProviderEntries as \[provider, providerModels\] \(provider\)\}/,
	);
});

test("settings dialog preserves the selected default model when dedupe would hide it", () => {
	assert.match(source, /const selectedDefaultModel = \$derived\.by\(\(\) =>/);
	assert.match(
		source,
		/models\.list\.find\(\(model\) => model\.id === preferences\.defaultModel\)/,
	);
	assert.match(
		source,
		/selectedDefaultModel &&[\s\S]*!dedupedModels\.some\(\(model\) => model\.id === selectedDefaultModel\.id\)/,
	);
	assert.match(
		source,
		/grouped\[provider\] = \[selectedDefaultModel, \.\.\.grouped\[provider\]\];/,
	);
});

test("settings dialog only shows updates when the runtime supports them", () => {
	assert.match(
		source,
		/const showUpdateTab = \$derived\(environment\.supportsAppUpdates\);/,
	);
	assert.doesNotMatch(source, /environment\.runtime === "tauri"/);
});
