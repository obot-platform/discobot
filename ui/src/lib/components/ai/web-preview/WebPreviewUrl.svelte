<script lang="ts">
	import { Input } from "$lib/components/ui/input";
	import { useWebPreviewContext } from "./context";

	type Props = {
		value?: string;
		class?: string;
		onchange?: (event: Event) => void;
		onkeydown?: (event: KeyboardEvent) => void;
	};

	let {
		value,
		class: className,
		onchange,
		onkeydown,
		...restProps
	}: Props = $props();

	const webPreview = useWebPreviewContext();
	let inputValue = $state(webPreview.url);

	$effect(() => {
		inputValue = webPreview.url;
	});

	function handleInput(event: Event) {
		const target = event.currentTarget as HTMLInputElement;
		inputValue = target.value;
		onchange?.(event);
	}

	function handleKeydown(event: KeyboardEvent) {
		if (event.key === "Enter") {
			webPreview.setUrl(inputValue);
		}
		onkeydown?.(event);
	}
</script>

<Input
	class={`h-8 flex-1 text-sm ${className ?? ""}`}
	placeholder="Enter URL..."
	value={value ?? inputValue}
	oninput={handleInput}
	onkeydown={handleKeydown}
	{...restProps}
/>
