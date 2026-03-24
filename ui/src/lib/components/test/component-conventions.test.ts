/**
 * Enforces component folder conventions for the Svelte UI.
 *
 * Rules:
 *  - ui/**       Pure primitives. No global context imports ever.
 *  - ai/**       Self-contained compound components. No global context imports ever.
 *  - app/parts/  Pure sub-components of app/ context consumers. No global context imports.
 *
 * "Global context" means the three app-wide contexts:
 *   $lib/context/app-context.svelte
 *   $lib/context/session-context.svelte
 *   $lib/context/thread-context.svelte
 *
 * app/ root components (excluding parts/) are intentionally context consumers and are not checked.
 */

import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { glob } from "node:fs/promises";
import path from "node:path";
import test from "node:test";

const COMPONENTS_ROOT = path.resolve(import.meta.dirname, "..");

// Matches any import from the three global context modules
const GLOBAL_CONTEXT_IMPORT =
	/from\s+["'][$]lib\/context\/(app|session|thread)-context(?:\.svelte)?["']/;

async function findSvelteFiles(dir: string): Promise<string[]> {
	const files: string[] = [];
	for await (const file of glob("**/*.svelte", { cwd: dir })) {
		files.push(path.join(dir, file));
	}
	return files.sort();
}

function importsGlobalContext(filePath: string): boolean {
	const content = readFileSync(filePath, "utf-8");
	return GLOBAL_CONTEXT_IMPORT.test(content);
}

function relPath(filePath: string): string {
	return path.relative(COMPONENTS_ROOT, filePath);
}

async function assertNoGlobalContext(
	dir: string,
	label: string,
): Promise<void> {
	const files = await findSvelteFiles(dir);
	assert.ok(files.length > 0, `expected to find .svelte files in ${label}`);

	const violations = files.filter(importsGlobalContext).map(relPath);

	assert.deepEqual(
		violations,
		[],
		`${label} components must not import global context (app/session/thread).\nViolations:\n  ${violations.join("\n  ")}`,
	);
}

test("ui/ components are pure — no global context imports", async () => {
	await assertNoGlobalContext(path.join(COMPONENTS_ROOT, "ui"), "ui/");
});

test("ai/ components are self-contained — no global context imports", async () => {
	await assertNoGlobalContext(path.join(COMPONENTS_ROOT, "ai"), "ai/");
});

test("app/parts/ components are pure — no global context imports", async () => {
	await assertNoGlobalContext(
		path.join(COMPONENTS_ROOT, "app/parts"),
		"app/parts/",
	);
});
