<script lang="ts">
	import { useAppContext } from "$lib/context/app-context.svelte";
	import { useThreadContext } from "$lib/context/thread-context.svelte";
	import ConversationComposer from "$lib/components/ide/ConversationComposer.svelte";
	import { Message, MessageContent, MessageResponse } from "$lib/components/ai/message";
	import { Reasoning, ReasoningContent, ReasoningTrigger } from "$lib/components/ai/reasoning";
	import OptimizedToolRenderer from "$lib/components/ai/tool-renderers/OptimizedToolRenderer.svelte";
	import {
		getDynamicToolParts,
		getMessageText,
		getReasoningText,
	} from "$lib/session/domains/session-domain.helpers";

	const app = useAppContext();
	const preferences = app.preferences;
	const thread = useThreadContext();
	const conversationMessages = $derived.by(() => thread.messages);
	const hasMessages = $derived.by(() => conversationMessages.length > 0);

</script>

<div class="flex h-full min-h-0 flex-col overflow-hidden bg-background">
	<div
		class={`flex min-h-0 flex-1 flex-col transition-all duration-300 ease-out ${hasMessages ? "" : "justify-center"}`}
	>
		{#if hasMessages}
			<div class="min-h-0 flex-1 overflow-auto p-4">
				<div
					class={`w-full space-y-4 ${preferences.chatWidthMode === "constrained" ? "mx-auto max-w-3xl" : ""}`}
				>
					{#each conversationMessages as message (message.id)}
						<Message from={message.role === "assistant" ? "assistant" : "user"}>
							<MessageContent>
								{@const reasoning = getReasoningText(message)}
								{@const text = getMessageText(message)}
								{@const toolParts = getDynamicToolParts(message)}

								{#if reasoning.length > 0}
									<Reasoning
										defaultOpen={false}
										isStreaming={thread.status === "streaming"}
									>
										<ReasoningTrigger />
										<ReasoningContent text={reasoning} />
									</Reasoning>
								{/if}

								{#if text.length > 0}
									<MessageResponse text={text} />
								{/if}

								{#each toolParts as toolPart (toolPart.toolCallId)}
									<OptimizedToolRenderer {toolPart} />
								{/each}
							</MessageContent>
						</Message>
					{/each}
				</div>
			</div>
		{/if}

		<ConversationComposer />
	</div>
</div>
