	<script lang="ts">
	import ArrowDownIcon from "@lucide/svelte/icons/arrow-down";
	import { tick } from "svelte";
	import type { ChatMessage } from "$lib/api-types";
	import type { ChatWidthMode } from "$lib/app/app-context.types";
	import { Loader } from "$lib/components/ai";
	import { Message, MessageContent, MessageResponse } from "$lib/components/ai/message";
	import { Reasoning, ReasoningContent, ReasoningTrigger } from "$lib/components/ai/reasoning";
	import OptimizedToolRenderer from "$lib/components/ai/tool-renderers/OptimizedToolRenderer.svelte";
	import type { DynamicToolPart } from "$lib/components/ai/types";
	import ConversationComposer from "$lib/components/app/ConversationComposer.svelte";
	import { getBottomSpacerHeight } from "$lib/components/app/conversation-pane-layout";
	import LazyMount from "$lib/components/app/parts/LazyMount.svelte";
	import { Alert, AlertDescription } from "$lib/components/ui/alert";
	import { Button } from "$lib/components/ui/button";
	import { getAppContextIfPresent } from "$lib/context/app-context.svelte";
	import { getSessionContextIfPresent } from "$lib/context/session-context.svelte";
	import { getThreadContextIfPresent } from "$lib/context/thread-context.svelte";
	import type { ThreadContextValue } from "$lib/session/session-context.types";

	type ConversationPaneStatus = ThreadContextValue["status"];
	type Props = {
		contentTopPadding?: number;
		messages?: ChatMessage[];
		status?: ConversationPaneStatus;
		threadError?: string | null;
		sessionError?: string | null;
		chatWidthMode?: ChatWidthMode;
		showComposer?: boolean;
		toolDefaultOpen?: boolean;
	};

	const SCROLL_TO_BOTTOM_BUFFER = 64;
	const USER_MESSAGE_PLACEHOLDER_HEIGHT = 96;
	const ASSISTANT_MESSAGE_PLACEHOLDER_HEIGHT = 320;

	let {
		contentTopPadding = 0,
		messages,
		status,
		threadError: threadErrorOverride = null,
		sessionError: sessionErrorOverride = null,
		chatWidthMode,
		showComposer = true,
		toolDefaultOpen = false,
	}: Props = $props();

	const app = getAppContextIfPresent();
	const session = getSessionContextIfPresent();
	const thread = getThreadContextIfPresent();
	const activeSessionId = $derived.by(
		() => session?.sessionId ?? app?.sessions.selectedId ?? null,
	);
	const activeThreadId = $derived.by(() => thread?.threadId ?? null);
	const conversationMessages = $derived.by(() => messages ?? thread?.messages ?? []);
	const conversationStatus = $derived.by(() => status ?? thread?.status ?? "ready");
	const effectiveChatWidthMode = $derived.by(
		() => chatWidthMode ?? app?.preferences.chatWidthMode ?? "full",
	);
	const turnStartMessageId = $derived.by(() => {
		for (let index = conversationMessages.length - 1; index >= 0; index -= 1) {
			const message = conversationMessages[index];
			if (message?.role === "user") {
				return message.id;
			}
		}
		return null;
	});
	const hasMessages = $derived.by(() => conversationMessages.length > 0);
	const isLoading = $derived.by(() => conversationStatus === "loading");
	const isStreaming = $derived.by(() => conversationStatus === "streaming");
	const sessionError = $derived.by(
		() => sessionErrorOverride ?? session?.current?.errorMessage ?? null,
	);
	const threadError = $derived.by(() => threadErrorOverride ?? thread?.error ?? null);
	const canShowComposer = $derived.by(
		() => showComposer && Boolean(app) && Boolean(session) && Boolean(thread),
	);

	type MessagePart = ChatMessage["parts"][number];

	let viewport = $state<HTMLDivElement | null>(null);
	let content = $state<HTMLDivElement | null>(null);
	let bottomSpacerHeight = $state(0);
	let lastAutoScrolledMessageId = $state<string | null>(null);
	let hasInitialBottomScroll = $state(false);
	let isNearBottom = $state(true);

	function isTextPart(part: MessagePart): part is Extract<MessagePart, { type: "text" }> {
		return part.type === "text";
	}

	function isReasoningPart(part: MessagePart): part is Extract<MessagePart, { type: "reasoning" }> {
		return part.type === "reasoning";
	}

	function isDynamicToolPart(
		part: MessagePart,
	): part is Extract<MessagePart, { type: "dynamic-tool" }> {
		return part.type === "dynamic-tool";
	}

	function isStreamingMessage(message: ChatMessage): boolean {
		return (
			(message as { status?: string } | undefined)?.status === "streaming"
		);
	}

	function isProvisionalUserMessage(
		message: ChatMessage | undefined,
	): message is ChatMessage & { role: "user"; provisional: true } {
		return message?.role === "user" && message.provisional === true;
	}

	function getMessagePlaceholderHeight(message: ChatMessage) {
		return message.role === "assistant"
			? ASSISTANT_MESSAGE_PLACEHOLDER_HEIGHT
			: USER_MESSAGE_PLACEHOLDER_HEIGHT;
	}

	function updateIsNearBottom() {
		const element = viewport;
		if (!element) {
			isNearBottom = true;
			return;
		}

		const distanceToBottom = element.scrollHeight - element.scrollTop - element.clientHeight;
		isNearBottom = distanceToBottom <= SCROLL_TO_BOTTOM_BUFFER;
	}

	function scrollToBottom(behavior: ScrollBehavior = "auto") {
		const element = viewport;
		if (!element) {
			return;
		}

		element.scrollTo({ top: element.scrollHeight, behavior });
		if (behavior === "auto") {
			requestAnimationFrame(() => {
				updateIsNearBottom();
			});
		}
	}

	function getAnchorMessageElement() {
		const anchorMessageId = turnStartMessageId;
		if (!anchorMessageId) {
			return null;
		}

		const messageElements = content?.querySelectorAll<HTMLElement>("[data-conversation-message-id]");
		return (
			Array.from(messageElements ?? []).find(
				(candidate) => candidate.dataset.conversationMessageId === anchorMessageId,
			) ?? null
		);
	}

	function updateBottomSpacer() {
		const element = viewport;
		const contentElement = content;
		const anchorElement = getAnchorMessageElement();
		if (!element || !contentElement || !anchorElement) {
			bottomSpacerHeight = 0;
			updateIsNearBottom();
			return;
		}

		const styles = window.getComputedStyle(element);
		const paddingTop = Number.parseFloat(styles.paddingTop) || 0;
		const paddingBottom = Number.parseFloat(styles.paddingBottom) || 0;
		const contentRect = contentElement.getBoundingClientRect();
		const anchorRect = anchorElement.getBoundingClientRect();

		bottomSpacerHeight = getBottomSpacerHeight({
			contentHeight: contentRect.height,
			existingSpacerHeight: bottomSpacerHeight,
			anchorOffsetTop: anchorRect.top - contentRect.top,
			contentTopPadding,
			viewportClientHeight: element.clientHeight,
			viewportPaddingBottom: paddingBottom,
			viewportPaddingTop: paddingTop,
		});
		updateIsNearBottom();
	}

	$effect(() => {
		const element = viewport;
		if (!element) {
			isNearBottom = true;
			return;
		}

		const handleScroll = () => {
			updateIsNearBottom();
		};

		updateIsNearBottom();
		element.addEventListener("scroll", handleScroll);

		return () => {
			element.removeEventListener("scroll", handleScroll);
		};
	});

	$effect(() => {
		const latestMessageId = conversationMessages.at(-1)?.id;
		const anchorMessageId = turnStartMessageId;
		const element = viewport;
		const contentElement = content;
		if (!latestMessageId || !anchorMessageId || !element || !contentElement) {
			bottomSpacerHeight = 0;
			updateIsNearBottom();
			return;
		}

		let frame = 0;
		let observer: ResizeObserver | null = null;
		const scheduleUpdate = () => {
			cancelAnimationFrame(frame);
			frame = requestAnimationFrame(() => {
				updateBottomSpacer();
			});
		};

		void tick().then(() => {
			scheduleUpdate();
			observer = new ResizeObserver(() => {
				scheduleUpdate();
			});
			observer.observe(element);
			observer.observe(contentElement);
		});

		return () => {
			cancelAnimationFrame(frame);
			observer?.disconnect();
		};
	});

	$effect(() => {
		if (hasInitialBottomScroll) {
			return;
		}
		if (!viewport || conversationMessages.length === 0) {
			return;
		}
		if (conversationStatus !== "ready" && conversationStatus !== "streaming") {
			return;
		}

		hasInitialBottomScroll = true;
		void tick()
			.then(() => {
				updateBottomSpacer();
				return tick();
			})
			.then(() => {
				if (conversationMessages.length > 0) {
					scrollToBottom("auto");
				}
			});
	});

	$effect(() => {
		const latestMessage = conversationMessages.at(-1);
		if (!isProvisionalUserMessage(latestMessage)) {
			return;
		}
		if (latestMessage.id === lastAutoScrolledMessageId) {
			return;
		}

		lastAutoScrolledMessageId = latestMessage.id;
		void tick()
			.then(() => {
				updateBottomSpacer();
				return tick();
			})
			.then(() => {
				scrollToBottom("auto");
			});
	});
</script>

<div class="flex h-full min-h-0 flex-col overflow-hidden bg-background">
	{#if sessionError || threadError}
		<div class="flex flex-col gap-2 p-3">
			{#if sessionError}
				<Alert variant="destructive">
					<AlertDescription>{sessionError}</AlertDescription>
				</Alert>
			{/if}
			{#if threadError}
				<Alert variant="destructive">
					<AlertDescription>{threadError}</AlertDescription>
				</Alert>
			{/if}
		</div>
	{/if}
	<div
		class={`flex min-h-0 flex-1 flex-col transition-all duration-300 ease-out ${hasMessages ? "" : "justify-center"}`}
	>
		{#if hasMessages}
			<div class="relative min-h-0 flex-1">
				<div bind:this={viewport} class="h-full overflow-auto p-4">
					<div
						bind:this={content}
						class={`w-full space-y-4 ${effectiveChatWidthMode === "constrained" ? "mx-auto max-w-3xl" : ""}`}
					>
						{#each conversationMessages as message (message.id)}
							<Message
								data-conversation-message-id={message.id}
								from={message.role === "assistant" ? "assistant" : "user"}
							>
								<LazyMount estimatedHeight={getMessagePlaceholderHeight(message)} root={viewport}>
									<MessageContent>
										{#each message.parts as part, index (`${message.id}-${index}`)}
											{#if isReasoningPart(part) && part.text.length > 0}
												<Reasoning defaultOpen={false} isStreaming={isStreamingMessage(message)}>
													<ReasoningTrigger />
													<ReasoningContent text={part.text} />
												</Reasoning>
											{:else if isTextPart(part) && part.text.length > 0}
												<MessageResponse text={part.text} />
											{:else if isDynamicToolPart(part)}
												<OptimizedToolRenderer
												toolPart={part as DynamicToolPart}
												sessionId={activeSessionId}
												threadId={activeThreadId}
												defaultOpen={toolDefaultOpen}
											/>
											{/if}
										{/each}
									</MessageContent>
								</LazyMount>
							</Message>
						{/each}
						{#if isStreaming}
							<Message from="assistant">
								<LazyMount estimatedHeight={ASSISTANT_MESSAGE_PLACEHOLDER_HEIGHT} root={viewport}>
									<MessageContent>
										<div class="text-muted-foreground">
											<Loader size={18} />
										</div>
									</MessageContent>
								</LazyMount>
							</Message>
						{/if}
						<div aria-hidden="true" style={`height: ${bottomSpacerHeight}px;`}></div>
					</div>
				</div>
				{#if !isNearBottom}
					<div class="pointer-events-none absolute inset-x-0 bottom-4 flex justify-center">
						<Button
							class="pointer-events-auto rounded-full shadow-sm"
							onclick={() => scrollToBottom("smooth")}
							size="icon"
							type="button"
							variant="outline"
						>
							<ArrowDownIcon class="size-4" />
						</Button>
					</div>
				{/if}
			</div>
		{:else if isLoading}
			<div class="flex min-h-0 flex-1 items-center justify-center p-4 text-muted-foreground text-sm">
				Loading conversation...
			</div>
		{/if}

		{#if canShowComposer}
			<ConversationComposer />
		{/if}
	</div>
</div>
