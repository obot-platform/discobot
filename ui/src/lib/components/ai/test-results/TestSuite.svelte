<script lang="ts">
	import { Collapsible } from "$lib/components/ui/collapsible";
	import { cn } from "$lib/utils";
	import { setTestSuiteContext, type TestStatus } from "./context";

	type Props = {
		name: string;
		status: TestStatus;
		open?: boolean;
		class?: string;
		children?: () => any;
	};

	let {
		name,
		status,
		open = $bindable(false),
		class: className,
		children,
		...restProps
	}: Props = $props();

	const testSuite = $state({ name: "", status: "passed" as TestStatus });
	$effect(() => {
		testSuite.name = name;
		testSuite.status = status;
	});
	setTestSuiteContext(testSuite);
</script>

<Collapsible
	bind:open
	class={cn("rounded-lg border", className)}
	{...restProps}
>
	{@render children?.()}
</Collapsible>
