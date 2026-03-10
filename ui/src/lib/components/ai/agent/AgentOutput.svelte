<script lang="ts">
	import type { HTMLAttributes } from "svelte/elements";
	import { CodeBlock } from "$lib/components/ai/code-block";
	import { cn } from "$lib/utils";

	type Props = HTMLAttributes<HTMLDivElement> & {
		schema: string;
		class?: string;
	};

	let { schema, class: className, ...restProps }: Props = $props();
</script>

<div class={cn("space-y-2", className)} {...restProps}>
	<span class="font-medium text-muted-foreground text-sm">Output Schema</span>
	<div class="rounded-md bg-muted/50">
		<CodeBlock code={schema} language="typescript" />
	</div>
</div>
