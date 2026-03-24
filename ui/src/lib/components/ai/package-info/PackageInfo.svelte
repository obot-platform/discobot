<script lang="ts">
	import { cn } from "$lib/utils";
	import { setPackageInfoContext, type ChangeType } from "./context";

	type Props = {
		name: string;
		currentVersion?: string;
		newVersion?: string;
		changeType?: ChangeType;
		class?: string;
		children?: () => any;
	};

	let {
		name,
		currentVersion,
		newVersion,
		changeType,
		class: className,
		children,
		...restProps
	}: Props = $props();

	const packageInfo = $state({
		name: "",
		currentVersion: undefined as string | undefined,
		newVersion: undefined as string | undefined,
		changeType: undefined as ChangeType | undefined,
	});
	$effect(() => {
		packageInfo.name = name;
		packageInfo.currentVersion = currentVersion;
		packageInfo.newVersion = newVersion;
		packageInfo.changeType = changeType;
	});
	setPackageInfoContext(packageInfo);
</script>

<div
	class={cn("rounded-lg border bg-background p-4", className)}
	{...restProps}
>
	{@render children?.()}
</div>
