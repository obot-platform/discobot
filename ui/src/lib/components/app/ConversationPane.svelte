<script lang="ts">
	import ArrowDownIcon from "@lucide/svelte/icons/arrow-down";
	import { tick } from "svelte";
	import { api } from "$lib/api-client";
	import type { ChatMessage } from "$lib/api-types";
	import type { ChatWidthMode } from "$lib/app/app-context.types";
	import type {
		AssistantConversationPaneRenderablePart,
		HookFailureMessageMetadata,
		UserConversationPaneRenderablePart,
	} from "$lib/components/app/conversation-pane-message-parts";
	import {
		getAssistantMessagePartGroups,
		getHookFailureMessageMetadata,
		getUserMessageRenderableParts,
		isConversationPaneMessageStreaming,
	} from "$lib/components/app/conversation-pane-message-parts";
	import {
		Attachment,
		AttachmentInfo,
		AttachmentPreview,
		Attachments,
		Loader,
	} from "$lib/components/ai";
	import CodeBlock from "$lib/components/ai/code-block/CodeBlock.svelte";
	import {
		Message,
		MessageContent,
		MessageResponse,
	} from "$lib/components/ai/message";
	import {
		Reasoning,
		ReasoningContent,
		ReasoningTrigger,
	} from "$lib/components/ai/reasoning";
	import OptimizedToolRenderer from "$lib/components/ai/tool-renderers/OptimizedToolRenderer.svelte";
	import type { DynamicToolPart } from "$lib/components/ai/types";
	import ConversationComposer from "$lib/components/app/ConversationComposer.svelte";
	import MessageResponseWithCommand from "$lib/components/app/parts/MessageResponseWithCommand.svelte";
	import {
		getReservedTurnMinHeight,
		groupMessagesIntoTurns,
	} from "$lib/components/app/conversation-pane-layout";
	import { Alert, AlertDescription } from "$lib/components/ui/alert";
	import { Button } from "$lib/components/ui/button";
	import * as Dialog from "$lib/components/ui/dialog";
	import {
		Collapsible,
		CollapsibleContent,
		CollapsibleTrigger,
	} from "$lib/components/ui/collapsible";
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
	const conversationMessages = $derived.by(
		() => messages ?? thread?.messages ?? [],
	);
	const conversationHistoryReplayVersion = $derived.by(() =>
		messages ? 0 : (thread?.historyReplayVersion ?? 0),
	);
	const conversationStatus = $derived.by(
		() => status ?? thread?.status ?? "ready",
	);
	const conversationTurns = $derived.by(() =>
		groupMessagesIntoTurns(conversationMessages),
	);
	const activeTurnId = $derived.by(() => conversationTurns.at(-1)?.id ?? null);
	const effectiveChatWidthMode = $derived.by(
		() => chatWidthMode ?? app?.preferences.chatWidthMode ?? "full",
	);
	const hasMessages = $derived.by(() => conversationMessages.length > 0);
	const isLoading = $derived.by(() => conversationStatus === "loading");
	const isStreaming = $derived.by(() => conversationStatus === "streaming");
	const sessionError = $derived.by(
		() => sessionErrorOverride ?? session?.current?.errorMessage ?? null,
	);
	const threadError = $derived.by(
		() => threadErrorOverride ?? thread?.error ?? null,
	);
	const canShowComposer = $derived.by(
		() => showComposer && Boolean(app) && Boolean(session) && Boolean(thread),
	);
	const latestConversationMessageId = $derived.by(
		() => conversationMessages.at(-1)?.id ?? null,
	);

	let viewport = $state<HTMLDivElement | null>(null);
	let hasInitialBottomScroll = $state(false);
	let hasInitialHistoryReplayBottomScroll = $state(false);
	let isNearBottom = $state(true);
	let expandedAssistantStepMessages = $state<Record<string, boolean>>({});
	let expandedGeneratedUserMessages = $state<Record<string, boolean>>({});
	let lastReservedSubmitMessageId = $state<string | null>(null);
	let reservedTurnMinHeight = $state(0);
	let hookPreviewOpen = $state(false);
	let hookPreviewMetadata = $state<HookFailureMessageMetadata | null>(null);
	let hookPreviewContent = $state("");
	let hookPreviewLoading = $state(false);
	let hookPreviewError = $state<string | null>(null);

	function getHookFileLanguage(path: string | undefined): string {
		const extension = path?.split(".").at(-1)?.toLowerCase() ?? "";
		switch (extension) {
			case "sh":
			case "bash":
			case "zsh":
				return "shell";
			case "py":
				return "python";
			case "js":
				return "javascript";
			case "ts":
				return "typescript";
			case "rb":
				return "ruby";
			case "go":
				return "go";
			case "yaml":
			case "yml":
				return "yaml";
			default:
				return "plaintext";
		}
	}

	async function openHookPreview(metadata: HookFailureMessageMetadata) {
		hookPreviewMetadata = metadata;
		hookPreviewContent = "";
		hookPreviewError = null;
		hookPreviewOpen = true;

		if (!activeSessionId || !metadata.hookPath) {
			return;
		}

		hookPreviewLoading = true;
		try {
			const response = await api.readSessionFile(
				activeSessionId,
				metadata.hookPath,
			);
			hookPreviewContent = response.content;
		} catch (error) {
			hookPreviewError =
				error instanceof Error ? error.message : "Failed to load hook file.";
		} finally {
			hookPreviewLoading = false;
		}
	}

	async function editHookFile() {
		const hookPath = hookPreviewMetadata?.hookPath;
		if (!hookPath) {
			return;
		}
		await session?.files.open(hookPath);
		hookPreviewOpen = false;
	}

	function isProvisionalUserMessage(
		message: ChatMessage | undefined,
	): message is ChatMessage & { role: "user"; provisional: true } {
		return message?.role === "user" && message.provisional === true;
	}

	function isAssistantStepMessageExpanded(messageId: string): boolean {
		return expandedAssistantStepMessages[messageId] ?? false;
	}

	function isGeneratedUserMessageExpanded(messageId: string): boolean {
		return expandedGeneratedUserMessages[messageId] ?? false;
	}

	function setGeneratedUserMessageExpanded(messageId: string, open: boolean) {
		expandedGeneratedUserMessages = {
			...expandedGeneratedUserMessages,
			[messageId]: open,
		};
	}

	function setAssistantStepMessageExpanded(messageId: string, open: boolean) {
		expandedAssistantStepMessages = {
			...expandedAssistantStepMessages,
			[messageId]: open,
		};
	}

	function getCollapsedStepLabel(stepCount: number): string {
		return `${stepCount} ${stepCount === 1 ? "step" : "steps"}`;
	}

	function isActiveStreamingAssistantMessage(message: ChatMessage): boolean {
		return (
			isStreaming &&
			message.role === "assistant" &&
			message.id === latestConversationMessageId
		);
	}

	function updateIsNearBottom() {
		const element = viewport;
		if (!element) {
			isNearBottom = true;
			return;
		}

		const distanceToBottom =
			element.scrollHeight - element.scrollTop - element.clientHeight;
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

	function getTurnElement(turnId: string) {
		if (!viewport) {
			return null;
		}

		return viewport.querySelector<HTMLElement>(
			`[data-conversation-turn-id="${CSS.escape(turnId)}"]`,
		);
	}

	function captureReservedTurnHeight(turnId: string) {
		const element = viewport;
		const turnElement = getTurnElement(turnId);
		if (!element || !turnElement) {
			return 0;
		}

		const styles = window.getComputedStyle(element);
		const turnStyles = window.getComputedStyle(turnElement);
		const paddingTop = Number.parseFloat(styles.paddingTop) || 0;
		const paddingBottom = Number.parseFloat(styles.paddingBottom) || 0;
		const turnTopPadding = Number.parseFloat(turnStyles.paddingTop) || 0;

		return getReservedTurnMinHeight({
			currentTurnHeight: turnElement.getBoundingClientRect().height,
			contentTopPadding,
			turnTopPadding,
			viewportClientHeight: element.clientHeight,
			viewportPaddingBottom: paddingBottom,
			viewportPaddingTop: paddingTop,
		});
	}

	function getTurnStyle(isLastTurn: boolean) {
		if (!isLastTurn || reservedTurnMinHeight <= 0) {
			return undefined;
		}

		return `min-height: ${reservedTurnMinHeight}px;`;
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
		if (conversationMessages.length > 0) {
			return;
		}

		hasInitialBottomScroll = false;
		hasInitialHistoryReplayBottomScroll = false;
		expandedGeneratedUserMessages = {};
		lastReservedSubmitMessageId = null;
		reservedTurnMinHeight = 0;
		updateIsNearBottom();
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
		void tick().then(() => {
			if (conversationMessages.length > 0) {
				scrollToBottom("auto");
			}
		});
	});

	$effect(() => {
		if (!viewport || hasInitialHistoryReplayBottomScroll) {
			return;
		}
		if (conversationHistoryReplayVersion === 0) {
			return;
		}

		hasInitialHistoryReplayBottomScroll = true;
		void tick().then(() => {
			scrollToBottom("auto");
		});
	});

	$effect(() => {
		const latestMessage = conversationMessages.at(-1);
		const turnId = activeTurnId;
		if (!viewport || !turnId || !isProvisionalUserMessage(latestMessage)) {
			return;
		}
		if (latestMessage.id === lastReservedSubmitMessageId) {
			return;
		}

		lastReservedSubmitMessageId = latestMessage.id;
		void tick()
			.then(() => {
				reservedTurnMinHeight = captureReservedTurnHeight(turnId);
				return tick();
			})
			.then(() => {
				scrollToBottom("auto");
			});
	});
</script>

{#snippet renderUserMessageParts(
	message: ChatMessage,
	parts: UserConversationPaneRenderablePart[],
)}
	{@const fileParts = parts.filter((part) => part.type === "file")}
	{@const isGeneratedTextExpanded = isGeneratedUserMessageExpanded(message.id)}
	<MessageResponseWithCommand
		{message}
		{parts}
		{isGeneratedTextExpanded}
		onGeneratedTextExpandedChange={(open) =>
			setGeneratedUserMessageExpanded(message.id, open)}
	/>
	{#if fileParts.length > 0}
		<Attachments variant="inline" class="max-w-full">
			{#each fileParts as part, index (`${message.id}-file-${index}`)}
				<Attachment
					data={{
						id: `${message.id}-file-${index}`,
						type: "file",
						filename: part.filename,
						mediaType: part.mediaType,
						url: part.url,
					}}
				>
					<AttachmentPreview />
					<AttachmentInfo />
				</Attachment>
			{/each}
		</Attachments>
	{/if}
{/snippet}

{#snippet renderHookFailureMessage(metadata: HookFailureMessageMetadata)}
	<div
		class="w-full overflow-hidden rounded-xl border border-border bg-card shadow-sm"
	>
		<div
			class="flex items-center justify-between gap-3 border-border border-b bg-muted/20 px-4 py-3"
		>
			<div class="min-w-0 space-y-1">
				<div class="font-medium text-foreground text-sm">Hook failed</div>
				<div class="truncate text-muted-foreground text-sm">
					{metadata.hookName}
				</div>
			</div>
			<div
				class="rounded-md border border-border bg-background px-2 py-1 font-mono text-foreground text-xs"
			>
				exit {metadata.exitCode}
			</div>
		</div>

		<div class="space-y-4 p-4">
			<div class="grid gap-3 text-sm sm:grid-cols-2">
				{#if metadata.pattern}
					<div class="space-y-1">
						<div
							class="font-medium text-muted-foreground text-xs uppercase tracking-wide"
						>
							Pattern
						</div>
						<div class="font-mono text-foreground text-xs">
							{metadata.pattern}
						</div>
					</div>
				{/if}

				{#if metadata.hookPath}
					<div class="space-y-1 sm:col-span-2">
						<div
							class="font-medium text-muted-foreground text-xs uppercase tracking-wide"
						>
							Hook file
						</div>
						<Button
							class="h-auto justify-start px-0 font-mono text-xs"
							onclick={() => {
								void openHookPreview(metadata);
							}}
							size="sm"
							variant="link"
						>
							{metadata.hookPath}
						</Button>
					</div>
				{/if}

				{#if metadata.files && metadata.files.length > 0}
					<div class="space-y-1 sm:col-span-2">
						<div
							class="font-medium text-muted-foreground text-xs uppercase tracking-wide"
						>
							Files
						</div>
						<div class="space-y-1 font-mono text-foreground text-xs">
							{#each metadata.files as file}
								<div class="break-all">{file}</div>
							{/each}
							{#if metadata.extraFileCount}
								<div class="text-muted-foreground">
									and {metadata.extraFileCount} more
								</div>
							{/if}
						</div>
					</div>
				{/if}
			</div>

			{#if metadata.output}
				<div class="space-y-2">
					<div
						class="font-medium text-muted-foreground text-xs uppercase tracking-wide"
					>
						Output
					</div>
					<div
						class="overflow-x-auto rounded-md border border-border bg-background"
					>
						<pre
							class="min-w-max whitespace-pre p-3 font-mono text-foreground text-xs leading-5"><code
								>{metadata.output}</code
							></pre>
					</div>
				</div>
			{:else if metadata.outputPath}
				<div class="space-y-2">
					<div
						class="font-medium text-muted-foreground text-xs uppercase tracking-wide"
					>
						Output path
					</div>
					<div
						class="rounded-md border border-border bg-background px-3 py-2 font-mono text-foreground text-xs break-all"
					>
						{metadata.outputPath}
					</div>
				</div>
			{/if}
		</div>
	</div>
{/snippet}

{#snippet renderAssistantMessageParts(
	message: ChatMessage,
	parts: AssistantConversationPaneRenderablePart[],
)}
	{#each parts as part, index (`${message.id}-${part.type}-${index}`)}
		{#if part.type === "reasoning"}
			<Reasoning defaultOpen={false} isStreaming={part.state === "streaming"}>
				<ReasoningTrigger />
				<ReasoningContent text={part.text} />
			</Reasoning>
		{:else if part.type === "text"}
			<MessageResponse text={part.text} />
		{:else if part.type === "dynamic-tool"}
			<OptimizedToolRenderer
				toolPart={part as DynamicToolPart}
				sessionId={activeSessionId}
				threadId={activeThreadId}
				onToolApprovalResponse={thread?.addToolApprovalResponse}
				defaultOpen={toolDefaultOpen}
			/>
		{/if}
	{/each}
{/snippet}

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
				<div
					bind:this={viewport}
					class="scrollbar-gutter-stable h-full overflow-auto p-4"
				>
					<div
						class={`w-full space-y-4 ${effectiveChatWidthMode === "constrained" ? "mx-auto max-w-3xl" : ""}`}
					>
						{#each conversationTurns as turn, index (turn.id)}
							<div
								data-active-turn={turn.id === activeTurnId}
								data-conversation-turn-id={turn.id}
								class={`space-y-4 ${index > 0 && turn.userMessages.length > 0 ? "pt-20" : ""}`}
								style={getTurnStyle(turn.id === activeTurnId)}
							>
								{#each turn.userMessages as message (message.id)}
									{@const userParts = getUserMessageRenderableParts(message)}
									{@const hookFailure = getHookFailureMessageMetadata(message)}
									<Message
										data-conversation-message-id={message.id}
										from={hookFailure ? "assistant" : "user"}
									>
										<MessageContent>
											{#if hookFailure}
												{@render renderHookFailureMessage(hookFailure)}
											{:else}
												{@render renderUserMessageParts(message, userParts)}
											{/if}
										</MessageContent>
									</Message>
								{/each}
								{#if turn.assistantMessage}
									{@const assistantMessage = turn.assistantMessage}
									{@const partGroups = getAssistantMessagePartGroups(
										assistantMessage,
										{
											isMessageComplete:
												!isActiveStreamingAssistantMessage(assistantMessage),
										},
									)}
									<Message
										data-conversation-message-id={assistantMessage.id}
										from="assistant"
									>
										<MessageContent>
											{@const isCollapsedStepSectionExpanded =
												isAssistantStepMessageExpanded(assistantMessage.id)}
											{#if partGroups.hasCollapsedSteps}
												<Collapsible
													open={isCollapsedStepSectionExpanded}
													onOpenChange={(open) =>
														setAssistantStepMessageExpanded(
															assistantMessage.id,
															open,
														)}
												>
													<CollapsibleTrigger
														aria-label={`${isCollapsedStepSectionExpanded ? "Hide" : "Show"} ${getCollapsedStepLabel(partGroups.collapsedStepCount)}`}
														class="flex w-full items-center gap-3 py-1 text-left"
														type="button"
													>
														<span class="h-px flex-1 bg-border"></span>
														<span
															class="rounded-full border border-border/70 bg-background px-3 py-1 font-medium text-[11px] text-muted-foreground uppercase tracking-[0.14em] transition-colors hover:border-border hover:text-foreground"
														>
															{getCollapsedStepLabel(
																partGroups.collapsedStepCount,
															)}
														</span>
														<span class="h-px flex-1 bg-border"></span>
													</CollapsibleTrigger>
													<CollapsibleContent
														class="flex min-w-0 flex-col gap-2 overflow-hidden [&>[data-ai-stack]+[data-ai-stack]]:-mt-8"
													>
														{#if isCollapsedStepSectionExpanded}
															{@render renderAssistantMessageParts(
																assistantMessage,
																partGroups.collapsedParts,
															)}
														{/if}
													</CollapsibleContent>
												</Collapsible>
											{/if}
											{@render renderAssistantMessageParts(
												assistantMessage,
												partGroups.visibleParts,
											)}
										</MessageContent>
									</Message>
								{/if}
								{#if isStreaming && turn.id === activeTurnId}
									<Message from="assistant">
										<MessageContent>
											<div class="text-muted-foreground">
												<Loader size={18} />
											</div>
										</MessageContent>
									</Message>
								{/if}
							</div>
						{/each}
					</div>
				</div>
				{#if !isNearBottom}
					<div
						class="pointer-events-none absolute inset-x-0 bottom-4 flex justify-center"
					>
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
			<div
				class="flex min-h-0 flex-1 items-center justify-center p-4 text-muted-foreground text-sm"
			>
				Loading conversation...
			</div>
		{/if}

		<Dialog.Root bind:open={hookPreviewOpen}>
			<Dialog.Content class="sm:max-w-4xl">
				<Dialog.Header>
					<Dialog.Title
						>{hookPreviewMetadata?.hookName ?? "Hook file"}</Dialog.Title
					>
				</Dialog.Header>
				{#if hookPreviewMetadata?.hookPath}
					<div class="space-y-3">
						<div class="font-mono text-muted-foreground text-xs break-all">
							{hookPreviewMetadata.hookPath}
						</div>
						{#if hookPreviewLoading}
							<div
								class="rounded-md border border-border bg-background px-3 py-4 text-muted-foreground text-sm"
							>
								Loading hook file...
							</div>
						{:else if hookPreviewError}
							<div
								class="rounded-md border border-border bg-background px-3 py-4 text-destructive text-sm"
							>
								{hookPreviewError}
							</div>
						{:else if hookPreviewContent}
							<CodeBlock
								class="max-h-[60vh] overflow-auto"
								code={hookPreviewContent}
								language={getHookFileLanguage(hookPreviewMetadata.hookPath)}
								showLineNumbers={true}
							/>
						{:else}
							<div
								class="rounded-md border border-border bg-background px-3 py-4 text-muted-foreground text-sm"
							>
								Hook file is empty.
							</div>
						{/if}
					</div>
				{/if}
				<Dialog.Footer>
					<Button
						variant="ghost"
						size="sm"
						onclick={() => {
							hookPreviewOpen = false;
						}}
					>
						Close
					</Button>
					<Button
						disabled={!hookPreviewMetadata?.hookPath}
						size="sm"
						onclick={() => {
							void editHookFile();
						}}
					>
						Edit
					</Button>
				</Dialog.Footer>
			</Dialog.Content>
		</Dialog.Root>

		{#if canShowComposer}
			<ConversationComposer />
		{/if}
	</div>
</div>
