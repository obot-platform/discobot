<script lang="ts">
	import { cn } from "$lib/utils";

	type Props = {
		code: string;
		showLineNumbers?: boolean;
		class?: string;
	};

	let { code, showLineNumbers = false, class: className }: Props = $props();

	const lineNumberClasses = cn(
		"block",
		"before:content-[counter(line)]",
		"before:inline-block",
		"before:[counter-increment:line]",
		"before:w-8",
		"before:mr-4",
		"before:text-right",
		"before:text-muted-foreground/50",
		"before:font-mono",
		"before:select-none",
	);

	const lines = $derived.by(() => code.split("\n"));
</script>

<div class="relative overflow-auto">
	<pre class={cn("m-0 p-4 text-sm", className)}>
		<code
			class={cn(
				"font-mono text-sm",
				showLineNumbers && "[counter-increment:line_0] [counter-reset:line]",
			)}>
			{#each lines as line}
				<span class={showLineNumbers ? lineNumberClasses : "block"}
					>{line || "\n"}</span
				>
			{/each}
		</code>
	</pre>
</div>
