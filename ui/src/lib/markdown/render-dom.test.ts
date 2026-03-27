import "../../../../test/setup.js";
import assert from "node:assert/strict";
import test from "node:test";

import { parseMarkdownToHast } from "./pipeline";
import { renderMarkdownTree } from "./render-dom";

function renderMarkdown(markdown: string): HTMLDivElement {
	const container = document.createElement("div");
	container.append(renderMarkdownTree(parseMarkdownToHast(markdown)));
	return container;
}

test("renderMarkdownTree keeps loose ordered and unordered list markers outside the content block", () => {
	const container =
		renderMarkdown(`1. **Restart during question phase with no cached snapshot at all**

   - This should now be fine because only persisted history exists.
`);

	const orderedList = container.querySelector("ol");
	assert.ok(orderedList);
	assert.match(orderedList.className, /list-outside/);
	assert.match(orderedList.className, /list-decimal/);
	assert.match(orderedList.className, /pl-6/);
	assert.doesNotMatch(orderedList.className, /list-inside/);

	const unorderedList = container.querySelector("ul");
	assert.ok(unorderedList);
	assert.match(unorderedList.className, /list-outside/);
	assert.match(unorderedList.className, /list-disc/);
	assert.match(unorderedList.className, /pl-6/);
	assert.doesNotMatch(unorderedList.className, /list-inside/);

	const orderedListItem = orderedList.querySelector("li");
	assert.ok(orderedListItem?.querySelector("p"));
});

test("renderMarkdownTree omits the default text label for unlabeled code fences", () => {
	const container = renderMarkdown("```\nplain text\n```");
	const header = container.querySelector(
		'[data-streamdown="code-block-header"]',
	);

	assert.ok(header);
	assert.equal(header?.textContent?.trim(), "");
});

test("renderMarkdownTree shows explicit code fence languages", () => {
	const container = renderMarkdown("```yaml\nkey: value\n```");
	const header = container.querySelector(
		'[data-streamdown="code-block-header"]',
	);

	assert.ok(header);
	assert.equal(header?.textContent?.trim(), "yaml");
});
