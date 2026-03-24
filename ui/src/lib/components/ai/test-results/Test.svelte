<script lang="ts">
	import { cn } from "$lib/utils";
	import { setTestContext, type TestStatus } from "./context";

	type Props = {
		name: string;
		status: TestStatus;
		duration?: number;
		class?: string;
		children?: () => any;
	};

	let {
		name,
		status,
		duration,
		class: className,
		children,
		...restProps
	}: Props = $props();

	const test = $state({
		name: "",
		status: "passed" as TestStatus,
		duration: undefined as number | undefined,
	});
	$effect(() => {
		test.name = name;
		test.status = status;
		test.duration = duration;
	});
	setTestContext(test);
</script>

<div
	class={cn("flex items-center gap-2 px-4 py-2 text-sm", className)}
	{...restProps}
>
	{@render children?.()}
</div>
