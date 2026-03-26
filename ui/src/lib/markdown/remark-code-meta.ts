// Adapted from Vercel Streamdown (packages/streamdown), Apache-2.0.
import type { Code, Root } from "mdast";
import type { Plugin } from "unified";
import { visit } from "unist-util-visit";

export const remarkCodeMeta: Plugin<[], Root> = () => (tree) => {
	visit(tree, "code", (node: Code) => {
		if (!node.meta) {
			return;
		}

		node.data = node.data ?? {};
		node.data.hProperties = {
			...((node.data.hProperties as Record<string, unknown>) ?? {}),
			metastring: node.meta,
		};
	});
};
