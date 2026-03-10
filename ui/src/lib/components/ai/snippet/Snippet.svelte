<script lang="ts">
	import { InputGroup } from "$lib/components/ui/input-group";
	import { cn } from "$lib/utils";
	import { setSnippetContext } from "./context";

	type Props = {
		code: string;
		class?: string;
		children?: () => any;
	};

	let { code, class: className, children, ...restProps }: Props = $props();
	const snippet = $state({ code: "" });
	$effect(() => {
		snippet.code = code;
	});
	setSnippetContext(snippet);
</script>

<InputGroup class={cn("font-mono", className)} {...restProps}>
	{@render children?.()}
</InputGroup>
