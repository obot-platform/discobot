# Attribution

Parts of this markdown renderer are adapted from [Vercel Streamdown](https://github.com/vercel/streamdown/tree/main/packages/streamdown), licensed under Apache-2.0.

## Files with direct or close adaptations

- `parse-blocks.ts`
  - Adapted from Streamdown's block-splitting logic for streaming markdown.
- `remark-code-meta.ts`
  - Adapted from Streamdown's remark plugin that forwards code fence metadata.
- `incomplete-code-utils.ts`
  - Adapted from Streamdown's incomplete code fence detection logic.

## Files primarily inspired by Streamdown's architecture

- `pipeline.ts`
  - Reimplements a similar unified/remark/rehype pipeline in a Svelte-native renderer.
- `render-dom.ts`
  - Reimplements Streamdown-like rendering behavior for code blocks, links, tables, and images using DOM APIs instead of React.
- `../components/ai/streamdown/SvelteStreamdown.svelte`
  - Reimplements Streamdown's streaming wrapper approach for incremental block rendering in Svelte.

This folder does not contain a verbatim copy of the Streamdown package, but it does include partial ports and structural adaptations derived from that project.
