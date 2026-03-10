<script lang="ts">
	import { cn } from "$lib/utils";
	import { setEnvironmentVariablesContext } from "./context";

	type Props = {
		showValues?: boolean;
		defaultShowValues?: boolean;
		onShowValuesChange?: (show: boolean) => void;
		class?: string;
		children?: () => any;
	};

	let {
		showValues: controlledShowValues,
		defaultShowValues = false,
		onShowValuesChange,
		class: className,
		children,
		...restProps
	}: Props = $props();

	let internalShowValues = $state(false);
	$effect(() => {
		if (!isControlled) {
			internalShowValues = defaultShowValues;
		}
	});

	const isControlled = $derived(controlledShowValues !== undefined);
	const showValues = $derived(isControlled ? !!controlledShowValues : internalShowValues);

	const contextValue = $state({
		showValues: false,
		setShowValues: (next: boolean) => {
			internalShowValues = next;
			onShowValuesChange?.(next);
		},
	});

	$effect(() => {
		contextValue.showValues = showValues;
	});

	setEnvironmentVariablesContext(contextValue);
</script>

<div class={cn("rounded-lg border bg-background", className)} {...restProps}>
	{@render children?.()}
</div>
