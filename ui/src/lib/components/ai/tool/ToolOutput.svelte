<script lang="ts">
	import { cn } from "$lib/utils";

	type Props = {
		output?: unknown;
		errorText?: string;
		class?: string;
	};

	let { output, errorText, class: className, ...restProps }: Props = $props();

	const renderedOutput = $derived.by(() => {
		if (typeof output === "string") {
			return output;
		}
		if (output && typeof output === "object") {
			return JSON.stringify(output, null, 2);
		}
		if (output !== undefined) {
			return String(output);
		}
		return "";
	});
</script>

{#if output || errorText}
	<div class={cn("space-y-2 p-4", className)} {...restProps}>
		<h4
			class="font-medium text-muted-foreground text-xs uppercase tracking-wide"
		>
			{errorText ? "Error" : "Result"}
		</h4>
		<div
			class={cn(
				"overflow-x-auto rounded-md text-xs [&_table]:w-full",
				errorText
					? "bg-destructive/10 text-destructive"
					: "bg-muted/50 text-foreground",
			)}
		>
			{#if errorText}
				<div class="p-3">{errorText}</div>
			{/if}
			{#if renderedOutput}
				<pre class="overflow-x-auto p-3 font-mono text-xs"><code
						>{renderedOutput}</code
					></pre>
			{/if}
		</div>
	</div>
{/if}
