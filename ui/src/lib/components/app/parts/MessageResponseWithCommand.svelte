<script lang="ts">
	import ChevronDownIcon from "@lucide/svelte/icons/chevron-down";
	import ChevronRightIcon from "@lucide/svelte/icons/chevron-right";
	import type { ChatMessage } from "$lib/api-types";
	import {
		getUserMessageOriginalCommandDisplay,
		getUserMessageOriginalText,
		type UserConversationPaneRenderablePart,
	} from "$lib/components/app/conversation-pane-message-parts";
	import { MessageResponse } from "$lib/components/ai/message";
	import { Button } from "$lib/components/ui/button";
	import {
		Collapsible,
		CollapsibleContent,
		CollapsibleTrigger,
	} from "$lib/components/ui/collapsible";

	type Props = {
		message: ChatMessage;
		parts: UserConversationPaneRenderablePart[];
		isGeneratedTextExpanded: boolean;
		onGeneratedTextExpandedChange: (open: boolean) => void;
	};

	let {
		message,
		parts,
		isGeneratedTextExpanded,
		onGeneratedTextExpandedChange,
	}: Props = $props();

	const textParts = $derived(parts.filter((part) => part.type === "text"));
	const originalText = $derived(getUserMessageOriginalText(message));
	const originalCommand = $derived(
		getUserMessageOriginalCommandDisplay(message),
	);
</script>

{#if originalCommand}
	<Collapsible
		open={isGeneratedTextExpanded}
		onOpenChange={onGeneratedTextExpandedChange}
	>
		<div class="group space-y-2">
			<CollapsibleTrigger
				aria-label={`${isGeneratedTextExpanded ? "Hide" : "Show"} generated text`}
				class="flex w-full items-center gap-2 rounded-sm text-left text-muted-foreground transition hover:text-foreground"
				type="button"
			>
				<ChevronRightIcon class="size-3.5 shrink-0" />
				<p class="text-[11px] uppercase tracking-[0.14em]">
					Command: {originalCommand.command}
				</p>
				<ChevronDownIcon
					class={`size-3 transition-all group-hover:opacity-100 ${isGeneratedTextExpanded ? "rotate-180 opacity-100" : "opacity-0"}`}
				/>
			</CollapsibleTrigger>
			{#if originalCommand.args}
				<MessageResponse text={originalCommand.args} />
			{/if}
			<CollapsibleContent>
				<div
					class="w-full space-y-2 rounded-md border border-border/60 bg-muted/30 p-3"
				>
					<p class="text-muted-foreground text-xs uppercase tracking-[0.14em]">
						Generated text
					</p>
					{#each textParts as part, index (`${message.id}-${part.type}-${index}`)}
						<MessageResponse text={part.text} />
					{/each}
				</div>
			</CollapsibleContent>
		</div>
	</Collapsible>
{:else if originalText}
	<MessageResponse text={originalText} />
{:else}
	{#each textParts as part, index (`${message.id}-${part.type}-${index}`)}
		<MessageResponse text={part.text} />
	{/each}
{/if}

{#if originalText && !originalCommand && textParts.length > 0}
	<div class="mt-2 flex flex-col items-start gap-1">
		<Button
			class="h-auto px-0 font-normal text-[11px] text-muted-foreground hover:text-foreground"
			onclick={() => onGeneratedTextExpandedChange(!isGeneratedTextExpanded)}
			size="sm"
			type="button"
			variant="ghost"
		>
			{isGeneratedTextExpanded ? "Hide generated text" : "Show generated text"}
		</Button>
		{#if isGeneratedTextExpanded}
			<div
				class="w-full space-y-2 rounded-md border border-border/60 bg-muted/30 p-3"
			>
				<p class="text-muted-foreground text-xs uppercase tracking-[0.14em]">
					Generated text
				</p>
				{#each textParts as part, index (`${message.id}-${part.type}-${index}`)}
					<MessageResponse text={part.text} />
				{/each}
			</div>
		{/if}
	</div>
{/if}
