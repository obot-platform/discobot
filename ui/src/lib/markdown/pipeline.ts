import { harden } from "rehype-harden";
import rehypeRaw from "rehype-raw";
import rehypeSanitize, { defaultSchema } from "rehype-sanitize";
import remarkGfm from "remark-gfm";
import remarkParse from "remark-parse";
import remarkRehype from "remark-rehype";
import type { Root } from "hast";
import type { Pluggable } from "unified";
import { unified } from "unified";
import type { MarkdownPluginConfig } from "./types";
import { remarkCodeMeta } from "./remark-code-meta";

const sanitizeSchema = {
	...defaultSchema,
	protocols: {
		...defaultSchema.protocols,
		href: [...(defaultSchema.protocols?.href ?? []), "tel"],
	},
	attributes: {
		...defaultSchema.attributes,
		code: [...(defaultSchema.attributes?.code ?? []), "metastring"],
	},
};

function applyPluggable(processor: any, plugin: Pluggable) {
	if (Array.isArray(plugin)) {
		if (plugin.length === 0) {
			return;
		}
		const [pluginFunction, ...parameters] = plugin;
		processor.use(pluginFunction as any, ...(parameters as any[]));
		return;
	}

	processor.use(plugin as any);
}

export function parseMarkdownToHast(
	markdown: string,
	plugins?: MarkdownPluginConfig,
): Root {
	const processor = unified().use(remarkParse);

	if (plugins?.cjk) {
		for (const plugin of plugins.cjk.remarkPluginsBefore) {
			applyPluggable(processor, plugin);
		}
	}

	processor.use(remarkGfm).use(remarkCodeMeta);

	if (plugins?.cjk) {
		for (const plugin of plugins.cjk.remarkPluginsAfter) {
			applyPluggable(processor, plugin);
		}
	}

	if (plugins?.math) {
		applyPluggable(processor, plugins.math.remarkPlugin);
	}

	processor
		.use(remarkRehype, { allowDangerousHtml: true })
		.use(rehypeRaw)
		.use(rehypeSanitize, sanitizeSchema)
		.use(harden, {
			allowDataImages: true,
			allowedImagePrefixes: ["*"],
			allowedLinkPrefixes: ["*"],
			allowedProtocols: ["*"],
			defaultOrigin: undefined,
		});

	if (plugins?.math) {
		applyPluggable(processor, plugins.math.rehypePlugin);
	}

	return processor.runSync(processor.parse(markdown)) as Root;
}
