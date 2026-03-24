<script lang="ts">
	import ChevronDownIcon from "@lucide/svelte/icons/chevron-down";
	import { Button } from "$lib/components/ui/button";
	import {
		Collapsible,
		CollapsibleContent,
		CollapsibleTrigger,
	} from "$lib/components/ui/collapsible";
	import { cn } from "$lib/utils";
	import { useWebPreviewContext } from "./context";

	type WebPreviewLog = {
		level: "log" | "warn" | "error";
		message: string;
		timestamp: Date | string;
	};

	type Props = {
		logs?: WebPreviewLog[];
		class?: string;
		children?: () => any;
	};

	let { logs = [], class: className, children, ...restProps }: Props = $props();
	const webPreview = useWebPreviewContext();

	function formatTimestamp(value: Date | string): string {
		const parsed = typeof value === "string" ? new Date(value) : value;
		return Number.isNaN(parsed.getTime())
			? "--:--:--"
			: parsed.toLocaleTimeString();
	}
</script>

<Collapsible
	open={webPreview.consoleOpen}
	onOpenChange={(nextOpen) => webPreview.setConsoleOpen(nextOpen)}
	class={cn("border-t bg-muted/50 font-mono text-sm", className)}
	{...restProps}
>
	<CollapsibleTrigger class="w-full">
		<Button
			class="flex w-full items-center justify-between rounded-none p-4 text-left font-medium hover:bg-muted/50"
			variant="ghost"
		>
			Console
			<ChevronDownIcon
				class={cn(
					"h-4 w-4 transition-transform duration-200",
					webPreview.consoleOpen && "rotate-180",
				)}
			/>
		</Button>
	</CollapsibleTrigger>
	<CollapsibleContent
		class="data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95 px-4 pb-4 outline-none data-[state=closed]:animate-out data-[state=open]:animate-in"
	>
		<div class="max-h-48 space-y-1 overflow-y-auto">
			{#if logs.length === 0}
				<p class="text-muted-foreground">No console output</p>
			{:else}
				{#each logs as log, index (`${String(log.timestamp)}-${index}`)}
					<div
						class={cn(
							"text-xs",
							log.level === "error" && "text-destructive",
							log.level === "warn" && "text-yellow-600",
							log.level === "log" && "text-foreground",
						)}
					>
						<span class="text-muted-foreground"
							>{formatTimestamp(log.timestamp)}</span
						>
						{log.message}
					</div>
				{/each}
			{/if}
			{@render children?.()}
		</div>
	</CollapsibleContent>
</Collapsible>
