<script lang="ts">
	import MoonIcon from "@lucide/svelte/icons/moon";
	import SunIcon from "@lucide/svelte/icons/sun";
	import {
		Attachment,
		AttachmentEmpty,
		AttachmentHoverCard,
		AttachmentHoverCardContent,
		AttachmentHoverCardTrigger,
		AttachmentInfo,
		AttachmentPreview,
		AttachmentRemove,
		Attachments,
		Artifact,
		ArtifactAction,
		ArtifactActions,
		ArtifactClose,
		ArtifactContent,
		ArtifactDescription,
		ArtifactHeader,
		ArtifactTitle,
		Checkpoint,
		CheckpointIcon,
		CheckpointTrigger,
		Commit,
		CommitActions,
		CommitAuthor,
		CommitAuthorAvatar,
		CommitContent,
		CommitCopyButton,
		CommitFile,
		CommitFileAdditions,
		CommitFileChanges,
		CommitFileDeletions,
		CommitFileIcon,
		CommitFileInfo,
		CommitFilePath,
		CommitFiles,
		CommitFileStatus,
		CommitHash,
		CommitHeader,
		CommitInfo,
		CommitMessage,
		CommitMetadata,
		CommitSeparator,
		CommitTimestamp,
		Connection,
		Controls,
		Confirmation,
		ConfirmationAccepted,
		ConfirmationAction,
		ConfirmationActions,
		ConfirmationRejected,
		ConfirmationRequest,
		ConfirmationTitle,
		Conversation,
		ConversationContent,
		ConversationEmptyState,
		ConversationScrollButton,
		Context,
		ContextCacheUsage,
		ContextContent,
		ContextContentBody,
		ContextContentFooter,
		ContextContentHeader,
		ContextInputUsage,
		ContextOutputUsage,
		ContextReasoningUsage,
		ContextTrigger,
		EnvironmentVariable,
		EnvironmentVariableCopyButton,
		EnvironmentVariableGroup,
		EnvironmentVariableName,
		EnvironmentVariableRequired,
		EnvironmentVariableValue,
		EnvironmentVariables,
		EnvironmentVariablesContent,
		EnvironmentVariablesHeader,
		EnvironmentVariablesTitle,
		EnvironmentVariablesToggle,
		Image,
		Loader,
		Message,
		MessageAction,
		MessageActions,
		MessageBranch,
		MessageBranchContent,
		MessageBranchNext,
		MessageBranchPage,
		MessageBranchPrevious,
		MessageBranchSelector,
		MessageContent,
		MessageResponse,
		MessageToolbar,
		Node,
		NodeAction,
		NodeContent,
		NodeDescription,
		NodeFooter,
		NodeHeader,
		NodeTitle,
		OpenIn,
		OpenInChatGPT,
		OpenInClaude,
		OpenInContent,
		OpenInCursor,
		OpenInLabel,
		OpenInScira,
		OpenInSeparator,
		OpenInT3,
		OpenInTrigger,
		OpenInv0,
		PackageInfo,
		PackageInfoChangeType,
		PackageInfoDependencies,
		PackageInfoDependency,
		PackageInfoHeader,
		PackageInfoName,
		PackageInfoVersion,
		Panel,
		Persona,
		Plan,
		PlanAction,
		PlanContent,
		PlanDescription,
		PlanFooter,
		PlanHeader,
		PlanTitle,
		PlanTrigger,
 		OptimizedToolRenderer,
		Queue,
		QueueItem,
		QueueItemAction,
		QueueItemActions,
		QueueItemAttachment,
		QueueItemContent,
		QueueItemDescription,
		QueueItemFile,
		QueueItemImage,
		QueueItemIndicator,
		QueueList,
		QueueSection,
		QueueSectionContent,
		QueueSectionLabel,
		QueueSectionTrigger,
		Reasoning,
		ReasoningContent,
		ReasoningTrigger,
		SchemaDisplay,
		SchemaDisplayContent,
		SchemaDisplayDescription,
		SchemaDisplayHeader,
		SchemaDisplayMethod,
		SchemaDisplayParameter,
		SchemaDisplayParameters,
		SchemaDisplayPath,
		SchemaDisplayRequest,
		SchemaDisplayResponse,
		Shimmer,
		Snippet,
		SnippetAddon,
		SnippetCopyButton,
		SnippetInput,
		SnippetText,
		Test,
		TestDuration,
		TestError,
		TestErrorMessage,
		TestErrorStack,
		TestName,
		TestResults,
		TestResultsContent,
		TestResultsDuration,
		TestResultsHeader,
		TestResultsSummary,
		TestStatus,
		TestSuite,
		TestSuiteContent,
		TestSuiteName,
		TestSuiteStats,
		Source,
		Sources,
		SourcesContent,
		SourcesTrigger,
		Suggestion,
		Suggestions,
		ChainOfThought,
		ChainOfThoughtContent,
		ChainOfThoughtHeader,
		ChainOfThoughtImage,
		ChainOfThoughtSearchResult,
		ChainOfThoughtSearchResults,
		ChainOfThoughtStep,
		StackTrace,
		StackTraceActions,
		StackTraceContent,
		StackTraceCopyButton,
		StackTraceError,
		StackTraceErrorMessage,
		StackTraceErrorType,
		StackTraceExpandButton,
		StackTraceFrames,
		StackTraceHeader,
		Task,
		TaskContent,
		TaskItem,
		TaskItemFile,
		TaskTrigger,
		Tool,
		ToolContent,
		ToolHeader,
		ToolInput,
		ToolOutput,
		Toolbar,
	} from "$lib/components/ai";
	import {
		FileTree,
		FileTreeActions,
		FileTreeFile,
		FileTreeFolder,
		FileTreeName,
		MicSelector,
		MicSelectorContent,
		MicSelectorInput,
		MicSelectorList,
		MicSelectorTrigger,
		MicSelectorValue,
		ModelSelector,
		ModelSelectorContent,
		ModelSelectorEmpty,
		ModelSelectorGroup,
		ModelSelectorInput,
		ModelSelectorItem,
		ModelSelectorList,
		ModelSelectorLogo,
		ModelSelectorName,
		ModelSelectorTrigger,
		PromptInput,
		PromptInputActionAddAttachments,
		PromptInputActionMenu,
		PromptInputActionMenuContent,
		PromptInputActionMenuTrigger,
		PromptInputBody,
		PromptInputFiles,
		PromptInputFooter,
		PromptInputSubmit,
		PromptInputTextarea,
		PromptInputTools,
		SpeechInput,
		Terminal,
		TerminalActions,
		TerminalClearButton,
		TerminalContent,
		TerminalCopyButton,
		TerminalHeader,
		TerminalStatus,
		TerminalTitle,
		Transcription,
		TranscriptionSegment,
		VoiceSelector,
		VoiceSelectorAccent,
		VoiceSelectorAttributes,
		VoiceSelectorBullet,
		VoiceSelectorContent,
		VoiceSelectorDescription,
		VoiceSelectorGroup,
		VoiceSelectorInput,
		VoiceSelectorItem,
		VoiceSelectorList,
		VoiceSelectorName,
		VoiceSelectorPreview,
		VoiceSelectorTrigger,
		WebPreview,
		WebPreviewBody,
		WebPreviewConsole,
		WebPreviewNavigation,
		WebPreviewNavigationButton,
		WebPreviewUrl,
		WebPreviewViewportButtons,
	} from "$lib/components/ai";
	import type { AttachmentData, DynamicToolPart } from "$lib/components/ai";
	import { Badge } from "$lib/components/ui/badge";
	import { Button } from "$lib/components/ui/button";
	import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "$lib/components/ui/card";

	import { getTheme, toggleTheme } from "$lib/theme";

	const demoImage =
		"iVBORw0KGgoAAAANSUhEUgAAAMAAAABgCAYAAABb7G8pAAAACXBIWXMAAAsSAAALEgHS3X78AAAAxUlEQVR4nO3RMQ0AAAgDINc/9K3hHFQgkYJumNnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dl5AB8qA0R9q8fVAAAAAElFTkSuQmCC";

	const demoStackTrace = `TypeError: Cannot read properties of undefined (reading 'name')
    at getSessionName (ui/src/lib/context/session-context.svelte.ts:142:18)
    at renderConversationPane (ui/src/lib/components/app/ConversationPane.svelte:512:9)
    at processTicksAndRejections (node:internal/process/task_queues:95:5)`;

	const demoImageDataUrl = `data:image/png;base64,${demoImage}`;
	const gridAttachments: AttachmentData[] = [
		{ id: "g-1", type: "file", filename: "preview.png", mediaType: "image/png", url: demoImageDataUrl },
		{ id: "g-2", type: "file", filename: "clip.mp4", mediaType: "video/mp4", url: "https://example.com/video.mp4" },
		{ id: "g-3", type: "source-document", title: "Deployment notes", filename: "notes.md" },
	];

	let inlineAttachments = $state<AttachmentData[]>([
		{ id: "i-1", type: "file", filename: "session.log", mediaType: "text/plain" },
		{ id: "i-2", type: "source-document", title: "API reference", filename: "api.md" },
	]);

	let listAttachments = $state<AttachmentData[]>([
		{ id: "l-1", type: "file", filename: "architecture.pdf", mediaType: "application/pdf" },
		{ id: "l-2", type: "file", filename: "voice-note.mp3", mediaType: "audio/mpeg" },
	]);

	let selectedMessageBranch = $state(0);
	let showRawToolRenderer = $state(false);

	const demoToolPart: DynamicToolPart = {
		type: "dynamic-tool",
		toolCallId: "tool-123",
		toolName: "Bash",
		state: "output-available",
		input: {
			command: "pnpm --dir ui check",
			description: "Run UI checks",
			timeout: 600000,
			run_in_background: false,
		},
		output: {
			exitCode: 0,
			stdout: "svelte-check found 0 errors and 0 warnings",
		},
	};

	const demoReadToolPart: DynamicToolPart = {
		type: "dynamic-tool",
		toolCallId: "tool-124",
		toolName: "Read",
		state: "output-available",
		input: {
			file_path: "/home/discobot/workspace/ui/src/lib/components/ai/tool/Tool.svelte",
			limit: 40,
			offset: 0,
		},
		output: {
			content: "1\t<script lang=\"ts\">\n2\timport { Collapsible } from \"$lib/components/ui/collapsible\";",
		},
	};

	const demoTaskToolPart: DynamicToolPart = {
		type: "dynamic-tool",
		toolCallId: "tool-125",
		toolName: "Task",
		state: "output-available",
		input: {
			subagent_type: "Explore",
			description: "Search for message renderer files",
			prompt: "Find message rendering components in ui/src/lib/components/ai",
			max_turns: 4,
			run_in_background: false,
		},
		output: {
			agentId: "agent-789",
			result: "Found message components under ui/src/lib/components/ai/message",
		},
	};

	const demoWebSearchToolPart: DynamicToolPart = {
		type: "dynamic-tool",
		toolCallId: "tool-126",
		toolName: "WebSearch",
		state: "output-available",
		input: {
			query: "Svelte 5 rune docs",
			allowed_domains: ["svelte.dev"],
			blocked_domains: ["example.com"],
		},
		output: {
			results: [
				{
					title: "Svelte 5 documentation",
					url: "https://svelte.dev/docs/svelte/overview",
					snippet: "Learn runes, $state, $derived, and $effect.",
				},
			],
		},
	};

	const demoAskUserQuestionToolPart: DynamicToolPart = {
		type: "dynamic-tool",
		toolCallId: "tool-127",
		toolName: "AskUserQuestion",
		state: "output-available",
		input: {
			questions: [
				{
					header: "Runtime",
					question: "Which environment should we target?",
					multiSelect: false,
					options: [
						{ label: "Staging", description: "Validate first" },
						{ label: "Production", description: "Ship immediately" },
					],
				},
			],
		},
		output: '"Which environment should we target?"="Staging"',
	};

	const demoAskUserQuestionPendingToolPart: DynamicToolPart = {
		type: "dynamic-tool",
		toolCallId: "tool-128",
		toolName: "AskUserQuestion",
		state: "approval-requested",
		approval: { id: "approval-ask-user-question" },
		input: {
			questions: [
				{
					header: "Architecture",
					question: "How should we proceed with the remaining migration?",
					multiSelect: true,
					notes:
						"## Pending batch\n\n- Finish AskUserQuestion parity\n- Wire remaining runtime surfaces\n- Add focused coverage for new component ports",
					options: [
						{ label: "Prioritize runtime wiring", description: "Connect chat/session flows first" },
						{ label: "Prioritize gallery parity", description: "Complete all AI UI demos first" },
						{ label: "Prioritize tests", description: "Add tests around recent component ports" },
					],
				},
				{
					header: "Risk tolerance",
					question: "What level of change is acceptable in this pass?",
					multiSelect: false,
					options: [
						{ label: "Minimal", description: "Only finish direct parity gaps" },
						{ label: "Moderate", description: "Include runtime glue changes" },
						{ label: "Aggressive", description: "Large refactor toward full integration" },
					],
				},
			],
		},
	};

	let terminalOutput = $state(`$ pnpm --dir ui check\n> svelte-check found 0 errors and 0 warnings\n> eslint found 0 problems`);
	const demoConsoleLogs: {
		level: "log" | "warn" | "error";
		message: string;
		timestamp: Date;
	}[] = [
		{ level: "log", message: "Server started on :3001", timestamp: new Date() },
		{ level: "warn", message: "Source map missing for vendor chunk", timestamp: new Date() },
		{ level: "error", message: "Unhandled promise rejection in preview iframe", timestamp: new Date() },
	];
	const demoTranscriptionSegments = [
		{ startSecond: 0, endSecond: 1.8, text: "Port the non streamdown modules first." },
		{ startSecond: 1.8, endSecond: 4.5, text: "Then wire each one into the gallery." },
		{ startSecond: 4.5, endSecond: 6.2, text: "Run typecheck and lint before moving on." },
	];
	let demoTranscriptionTime = $state(2.1);
	let selectedModel = $state("claude-sonnet-4");
	let selectedVoice = $state("alloy");
	let selectedFileTreePath = $state("ui/src/lib/components/ai/message/MessageResponse.svelte");
	let promptSubmitStatus = $state<"ready" | "submitted" | "streaming" | "error">("ready");
	let promptSubmissionHistory = $state<string[]>([]);
	let speechTranscript = $state("");

	function clearTerminalOutput() {
		terminalOutput = "";
	}

	function appendTerminalOutput() {
		terminalOutput = `${terminalOutput}\n> watching for changes...`;
	}

	async function handlePromptSubmit(message: {
		text: string;
		files: { filename?: string }[];
	}) {
		promptSubmitStatus = "submitted";
		await new Promise((resolve) => {
			setTimeout(resolve, 250);
		});
		promptSubmitStatus = "streaming";
		await new Promise((resolve) => {
			setTimeout(resolve, 250);
		});
		promptSubmitStatus = "ready";
		const summary = `${message.text.trim() || "(empty message)"} · ${message.files.length} file${message.files.length === 1 ? "" : "s"}`;
		promptSubmissionHistory = [summary, ...promptSubmissionHistory].slice(0, 3);
	}

	async function handleAudioRecorded(_audioBlob: Blob): Promise<string> {
		return "Transcribed from recorded audio";
	}

	function removeInlineAttachment(id: string) {
		inlineAttachments = inlineAttachments.filter((item) => item.id !== id);
	}

	function removeListAttachment(id: string) {
		listAttachments = listAttachments.filter((item) => item.id !== id);
	}

	let theme = $state(getTheme());

	function handleThemeToggle() {
		theme = toggleTheme();
	}
</script>

<svelte:head>
	<title>AI Component Gallery</title>
</svelte:head>

<div class="min-h-screen bg-background text-foreground">
	<div class="mx-auto flex max-w-7xl flex-col gap-6 px-6 py-8 lg:px-10">
		<header class="flex items-center justify-between rounded-2xl border bg-card p-6">
			<div>
				<p class="text-xs uppercase tracking-[0.2em] text-muted-foreground">AI Elements</p>
				<h1 class="mt-2 text-3xl font-semibold tracking-tight">Svelte AI gallery</h1>
				<p class="mt-2 text-sm text-muted-foreground">Preview of ported AI components in <code>/ui/src/lib/components/ai</code>.</p>
			</div>
			<div class="flex gap-2">
				<Badge variant="secondary">Initial batch</Badge>
				<Button variant="outline" size="icon-sm" onclick={handleThemeToggle} title={theme === "dark" ? "Switch to light" : "Switch to dark"} aria-label={theme === "dark" ? "Switch to light" : "Switch to dark"}>
					{#if theme === "dark"}
						<SunIcon class="size-4" />
					{:else}
						<MoonIcon class="size-4" />
					{/if}
				</Button>
				<Button href="/">Back home</Button>
			</div>
		</header>

		<div class="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
			<Card>
				<CardHeader>
					<CardTitle>Loader</CardTitle>
					<CardDescription>Animated spinner icon.</CardDescription>
				</CardHeader>
				<CardContent><Loader size={18} /></CardContent>
			</Card>

			<Card>
				<CardHeader>
					<CardTitle>Shimmer</CardTitle>
					<CardDescription>Streaming-style text shimmer.</CardDescription>
				</CardHeader>
				<CardContent>
					<Shimmer text="Generating a production-ready plan..." class="font-medium" />
				</CardContent>
			</Card>

			<Card>
				<CardHeader>
					<CardTitle>Image</CardTitle>
					<CardDescription>Generated image renderer.</CardDescription>
				</CardHeader>
				<CardContent>
					<Image mediaType="image/png" base64={demoImage} class="max-h-24" alt="Generated preview" />
				</CardContent>
			</Card>

			<Card>
				<CardHeader>
					<CardTitle>Persona</CardTitle>
					<CardDescription>Svelte fallback for persona states.</CardDescription>
				</CardHeader>
				<CardContent class="flex items-center gap-3">
					<Persona state="idle" />
					<Persona state="listening" variant="mana" />
					<Persona state="thinking" variant="halo" />
					<Persona state="speaking" variant="glint" />
					<Persona state="asleep" variant="opal" />
				</CardContent>
			</Card>

			<Card>
				<CardHeader>
					<CardTitle>Suggestions</CardTitle>
					<CardDescription>Horizontal prompt chips.</CardDescription>
				</CardHeader>
				<CardContent>
					<Suggestions>
						<Suggestion suggestion="Summarize this diff" />
						<Suggestion suggestion="Run targeted tests" />
						<Suggestion suggestion="Draft release notes" />
					</Suggestions>
				</CardContent>
			</Card>

			<Card>
				<CardHeader>
					<CardTitle>Snippet</CardTitle>
					<CardDescription>Copyable code/value snippet input group.</CardDescription>
				</CardHeader>
				<CardContent>
					<Snippet code="pnpm --dir ./ui check">
						<SnippetAddon>
							<SnippetText>$</SnippetText>
						</SnippetAddon>
						<SnippetInput />
						<SnippetCopyButton />
					</Snippet>
				</CardContent>
			</Card>

			<Card>
				<CardHeader>
					<CardTitle>Attachments (grid)</CardTitle>
					<CardDescription>Media thumbnails with category fallbacks.</CardDescription>
				</CardHeader>
				<CardContent>
					<Attachments variant="grid">
						{#each gridAttachments as item (item.id)}
							<Attachment data={item}>
								<AttachmentPreview />
							</Attachment>
						{/each}
					</Attachments>
				</CardContent>
			</Card>

			<Card>
				<CardHeader>
					<CardTitle>Attachments (inline/list)</CardTitle>
					<CardDescription>Inline chips, list rows, remove actions, and hover card.</CardDescription>
				</CardHeader>
				<CardContent class="space-y-4">
					<Attachments variant="inline">
						{#if inlineAttachments.length === 0}
							<AttachmentEmpty />
						{:else}
							{#each inlineAttachments as item (item.id)}
								<Attachment data={item} onRemove={() => removeInlineAttachment(item.id)}>
									<AttachmentPreview />
									<AttachmentInfo />
									<AttachmentRemove />
								</Attachment>
							{/each}
						{/if}
					</Attachments>

					<Attachments variant="list">
						{#if listAttachments.length === 0}
							<AttachmentEmpty />
						{:else}
							{#each listAttachments as item (item.id)}
								<Attachment data={item} onRemove={() => removeListAttachment(item.id)}>
									<AttachmentHoverCard>
										<AttachmentHoverCardTrigger>
											<div class="flex flex-1 items-center gap-3">
												<AttachmentPreview />
												<AttachmentInfo showMediaType={true} />
											</div>
										</AttachmentHoverCardTrigger>
										<AttachmentHoverCardContent>
											<div class="text-xs text-muted-foreground">
												{item.filename ?? (item.type === "source-document" ? item.title : undefined) ?? "Attachment"}
											</div>
										</AttachmentHoverCardContent>
									</AttachmentHoverCard>
									<AttachmentRemove />
								</Attachment>
							{/each}
						{/if}
					</Attachments>
				</CardContent>
			</Card>

			<Card>
				<CardHeader>
					<CardTitle>Sources</CardTitle>
					<CardDescription>Collapsible source links.</CardDescription>
				</CardHeader>
				<CardContent>
					<Sources open={true}>
						<SourcesTrigger count={2} />
						<SourcesContent>
							<Source href="https://example.com/a" title="Architecture note" />
							<Source href="https://example.com/b" title="API reference" />
						</SourcesContent>
					</Sources>
				</CardContent>
			</Card>

			<Card>
				<CardHeader>
					<CardTitle>Conversation</CardTitle>
					<CardDescription>Scrollable conversation container with empty state.</CardDescription>
				</CardHeader>
				<CardContent>
					<Conversation class="h-48 rounded-lg border bg-muted/20">
						<ConversationContent>
							<ConversationEmptyState
								title="No conversation yet"
								description="Send a message to start the thread."
							/>
						</ConversationContent>
						<ConversationScrollButton />
					</Conversation>
				</CardContent>
			</Card>

			<Card>
				<CardHeader>
					<CardTitle>Message</CardTitle>
					<CardDescription>Assistant/user bubble styles with branch controls.</CardDescription>
				</CardHeader>
				<CardContent class="space-y-3">
					<Message from="assistant">
						<MessageContent>
							<MessageBranch bind:branch={selectedMessageBranch} totalBranches={2}>
								<MessageBranchContent branch={0}>
									<MessageResponse text="Here is a concise migration plan with safe, incremental changes." />
								</MessageBranchContent>
								<MessageBranchContent branch={1}>
									<MessageResponse text="Alternative plan: prioritize tool renderers first, then markdown parity." />
								</MessageBranchContent>

								<MessageToolbar>
									<MessageActions>
										<MessageAction tooltip="Copy message" label="Copy message">⧉</MessageAction>
									</MessageActions>
									<MessageBranchSelector from="assistant">
										<MessageBranchPrevious />
										<MessageBranchPage />
										<MessageBranchNext />
									</MessageBranchSelector>
								</MessageToolbar>
							</MessageBranch>
						</MessageContent>
					</Message>
					<Message from="user">
						<MessageContent>
							<MessageResponse text="Please prioritize the critical conversation/tool/message stack." />
						</MessageContent>
					</Message>
				</CardContent>
			</Card>

			<Card>
				<CardHeader>
					<CardTitle>Tool + renderer</CardTitle>
					<CardDescription>Core tool shell with generic optimized renderer fallback.</CardDescription>
				</CardHeader>
				<CardContent class="space-y-3">
					<Tool defaultOpen={true}>
						<ToolHeader
							type="dynamic-tool"
							toolName="Bash"
							state="output-available"
							title="RUN: pnpm --dir ui check"
						/>
						<ToolContent>
							<ToolInput input={demoToolPart.input} />
							<ToolOutput output={demoToolPart.output} errorText={demoToolPart.errorText} />
						</ToolContent>
					</Tool>

					<div class="space-y-2 rounded-lg border p-3">
						<div class="flex items-center justify-between">
							<p class="text-muted-foreground text-xs uppercase tracking-wide">OptimizedToolRenderer</p>
							<Button size="xs" variant="outline" onclick={() => (showRawToolRenderer = !showRawToolRenderer)}>
								{showRawToolRenderer ? "Show optimized" : "Show raw"}
							</Button>
						</div>
						<OptimizedToolRenderer toolPart={demoToolPart} forceRaw={showRawToolRenderer} />
					</div>

					<div class="space-y-2 rounded-lg border p-3">
						<p class="text-muted-foreground text-xs uppercase tracking-wide">Specialized: Read</p>
						<OptimizedToolRenderer toolPart={demoReadToolPart} />
					</div>

					<div class="space-y-2 rounded-lg border p-3">
						<p class="text-muted-foreground text-xs uppercase tracking-wide">Specialized: Task</p>
						<OptimizedToolRenderer toolPart={demoTaskToolPart} />
					</div>


					<div class="space-y-2 rounded-lg border p-3">
						<p class="text-muted-foreground text-xs uppercase tracking-wide">Specialized: AskUserQuestion</p>
						<OptimizedToolRenderer toolPart={demoAskUserQuestionToolPart} />
					</div>

					<div class="space-y-2 rounded-lg border p-3">
						<p class="text-muted-foreground text-xs uppercase tracking-wide">Specialized: AskUserQuestion (interactive)</p>
						<OptimizedToolRenderer toolPart={demoAskUserQuestionPendingToolPart} />
					</div>
				</CardContent>
			</Card>

			<Card>
				<CardHeader>
					<CardTitle>Reasoning</CardTitle>
					<CardDescription>Collapsible reasoning trace with streaming-friendly trigger.</CardDescription>
				</CardHeader>
				<CardContent>
					<Reasoning defaultOpen={true} isStreaming={false}>
						<ReasoningTrigger />
						<ReasoningContent text="I compared current Svelte ports with React components, identified missing modules, and prioritized the conversation/tool/message stack first." />
					</Reasoning>
				</CardContent>
			</Card>

			<Card>
				<CardHeader>
					<CardTitle>Chain of thought</CardTitle>
					<CardDescription>Structured thinking steps with search/result artifacts.</CardDescription>
				</CardHeader>
				<CardContent>
					<ChainOfThought defaultOpen={true}>
						<ChainOfThoughtHeader />
						<ChainOfThoughtContent>
							<ChainOfThoughtStep label="Mapped missing components" status="complete" description="Compared source and Svelte directories." />
							<ChainOfThoughtStep label="Prioritized critical stack" status="active" description="Conversation, message, tool, and renderer routing.">
								<ChainOfThoughtSearchResults>
									<ChainOfThoughtSearchResult>conversation.tsx</ChainOfThoughtSearchResult>
									<ChainOfThoughtSearchResult>message.tsx</ChainOfThoughtSearchResult>
									<ChainOfThoughtSearchResult>tool.tsx</ChainOfThoughtSearchResult>
								</ChainOfThoughtSearchResults>
							</ChainOfThoughtStep>
							<ChainOfThoughtImage caption="Reference artifact">
								<img src={demoImageDataUrl} alt="Reasoning artifact" class="max-h-40 rounded border" />
							</ChainOfThoughtImage>
						</ChainOfThoughtContent>
					</ChainOfThought>
				</CardContent>
			</Card>

			<Card>
				<CardHeader>
					<CardTitle>Terminal</CardTitle>
					<CardDescription>Streaming-friendly terminal output with copy and clear actions.</CardDescription>
				</CardHeader>
				<CardContent class="space-y-2">
					<Terminal output={terminalOutput} isStreaming={true} onClear={clearTerminalOutput} class="max-h-72">
						<TerminalHeader>
							<TerminalTitle />
							<TerminalActions>
								<TerminalStatus />
								<TerminalCopyButton />
								<TerminalClearButton />
							</TerminalActions>
						</TerminalHeader>
						<TerminalContent />
					</Terminal>
					<div class="flex gap-2">
						<Button size="xs" variant="outline" onclick={appendTerminalOutput}>Append output</Button>
						<Button size="xs" variant="outline" onclick={clearTerminalOutput}>Clear</Button>
					</div>
				</CardContent>
			</Card>

			<Card>
				<CardHeader>
					<CardTitle>Web Preview</CardTitle>
					<CardDescription>URL bar, viewport controls, iframe body, and collapsible console.</CardDescription>
				</CardHeader>
				<CardContent>
					<WebPreview defaultUrl="https://example.com" class="h-[28rem]">
						<WebPreviewNavigation>
							<WebPreviewNavigationButton tooltip="Open example.com" onclick={() => window.open("https://example.com", "_blank", "noreferrer")}>
								↗
							</WebPreviewNavigationButton>
							<WebPreviewViewportButtons />
							<WebPreviewUrl />
						</WebPreviewNavigation>
						<WebPreviewBody />
						<WebPreviewConsole logs={demoConsoleLogs} />
					</WebPreview>
				</CardContent>
			</Card>

			<Card>
				<CardHeader>
					<CardTitle>Transcription</CardTitle>
					<CardDescription>Segment highlighting with seek-aware transcript tokens.</CardDescription>
				</CardHeader>
				<CardContent class="space-y-3">
					<input type="range" min="0" max="6.2" step="0.1" bind:value={demoTranscriptionTime} class="w-full" />
					<Transcription segments={demoTranscriptionSegments as any} bind:currentTime={demoTranscriptionTime} onSeek={(time) => (demoTranscriptionTime = time)}>
						{#snippet children(segment, index)}
							<TranscriptionSegment {segment} {index} />
						{/snippet}
					</Transcription>
					<p class="text-muted-foreground text-xs">Current time: {demoTranscriptionTime.toFixed(1)}s</p>
				</CardContent>
			</Card>

			<Card>
				<CardHeader>
					<CardTitle>Model Selector</CardTitle>
					<CardDescription>Command-style model picker with provider logos.</CardDescription>
				</CardHeader>
				<CardContent>
					<ModelSelector>
						<ModelSelectorTrigger>
							<Button variant="outline" class="w-full justify-between">{selectedModel}</Button>
						</ModelSelectorTrigger>
						<ModelSelectorContent>
							<ModelSelectorInput placeholder="Search models..." />
							<ModelSelectorList>
								<ModelSelectorEmpty>No model found.</ModelSelectorEmpty>
								<ModelSelectorGroup heading="Anthropic">
									<ModelSelectorItem value="claude-sonnet-4" onSelect={() => (selectedModel = "claude-sonnet-4")}>
										<ModelSelectorLogo provider="anthropic" />
										<ModelSelectorName>Claude Sonnet 4</ModelSelectorName>
									</ModelSelectorItem>
								</ModelSelectorGroup>
								<ModelSelectorGroup heading="OpenAI">
									<ModelSelectorItem value="gpt-5" onSelect={() => (selectedModel = "gpt-5")}>
										<ModelSelectorLogo provider="openai" />
										<ModelSelectorName>GPT-5</ModelSelectorName>
									</ModelSelectorItem>
								</ModelSelectorGroup>
							</ModelSelectorList>
						</ModelSelectorContent>
					</ModelSelector>
				</CardContent>
			</Card>

			<Card>
				<CardHeader>
					<CardTitle>Mic Selector</CardTitle>
					<CardDescription>Device picker backed by MediaDevices enumeration.</CardDescription>
				</CardHeader>
				<CardContent>
					<MicSelector>
						<MicSelectorTrigger class="w-full justify-between">
							<MicSelectorValue />
						</MicSelectorTrigger>
						<MicSelectorContent>
							<MicSelectorInput />
							<MicSelectorList />
						</MicSelectorContent>
					</MicSelector>
				</CardContent>
			</Card>

			<Card>
				<CardHeader>
					<CardTitle>Voice Selector</CardTitle>
					<CardDescription>Selectable voice rows with attributes and preview control.</CardDescription>
				</CardHeader>
				<CardContent>
					<VoiceSelector bind:value={selectedVoice}>
						<VoiceSelectorTrigger>
							<Button variant="outline" class="w-full justify-between">{selectedVoice}</Button>
						</VoiceSelectorTrigger>
						<VoiceSelectorContent>
							<VoiceSelectorInput placeholder="Search voices..." />
							<VoiceSelectorList>
								<VoiceSelectorGroup heading="Default">
									<VoiceSelectorItem value="alloy">
										<VoiceSelectorName>Alloy</VoiceSelectorName>
										<VoiceSelectorAttributes>
											<VoiceSelectorAccent value="american" />
											<VoiceSelectorBullet />
											<VoiceSelectorDescription>Neutral and clear</VoiceSelectorDescription>
										</VoiceSelectorAttributes>
										<VoiceSelectorPreview />
									</VoiceSelectorItem>
									<VoiceSelectorItem value="verse">
										<VoiceSelectorName>Verse</VoiceSelectorName>
										<VoiceSelectorAttributes>
											<VoiceSelectorAccent value="british" />
											<VoiceSelectorBullet />
											<VoiceSelectorDescription>Warm narration</VoiceSelectorDescription>
										</VoiceSelectorAttributes>
										<VoiceSelectorPreview />
									</VoiceSelectorItem>
								</VoiceSelectorGroup>
							</VoiceSelectorList>
						</VoiceSelectorContent>
					</VoiceSelector>
				</CardContent>
			</Card>

			<Card>
				<CardHeader>
					<CardTitle>File Tree</CardTitle>
					<CardDescription>Nested folder/file tree with selection and collapsible state.</CardDescription>
				</CardHeader>
				<CardContent class="space-y-2">
					<FileTree
						bind:selectedPath={selectedFileTreePath}
						defaultExpandedPaths={[
							"ui/src/lib/components/ai",
							"ui/src/lib/components/ai/message",
						]}
						class="max-h-72 overflow-auto"
					>
						<FileTreeFolder path="ui/src/lib/components/ai" name="ai">
							<FileTreeFolder path="ui/src/lib/components/ai/message" name="message">
								<FileTreeFile path="ui/src/lib/components/ai/message/Message.svelte" name="Message.svelte" />
								<FileTreeFile path="ui/src/lib/components/ai/message/MessageResponse.svelte" name="MessageResponse.svelte">
									<span class="size-4"></span>
									<FileTreeName>MessageResponse.svelte</FileTreeName>
									<FileTreeActions>
										<Button size="icon-xs" variant="ghost">⋯</Button>
									</FileTreeActions>
								</FileTreeFile>
							</FileTreeFolder>
							<FileTreeFolder path="ui/src/lib/components/ai/reasoning" name="reasoning">
								<FileTreeFile path="ui/src/lib/components/ai/reasoning/Reasoning.svelte" name="Reasoning.svelte" />
							</FileTreeFolder>
						</FileTreeFolder>
					</FileTree>
					<p class="text-muted-foreground text-xs">Selected: {selectedFileTreePath}</p>
				</CardContent>
			</Card>

			<Card>
				<CardHeader>
					<CardTitle>Speech Input</CardTitle>
					<CardDescription>Speech recognition with MediaRecorder fallback.</CardDescription>
				</CardHeader>
				<CardContent class="space-y-3">
					<SpeechInput
						onTranscriptionChange={(text) => {
							speechTranscript = speechTranscript ? `${speechTranscript} ${text}` : text;
						}}
						onAudioRecorded={handleAudioRecorded}
					/>
					<p class="min-h-10 rounded border bg-muted/30 p-2 text-sm">
						{speechTranscript || "Use the mic button to append transcript text."}
					</p>
				</CardContent>
			</Card>

			<Card>
				<CardHeader>
					<CardTitle>Prompt Input</CardTitle>
					<CardDescription>Composer shell with attachments menu and submit states.</CardDescription>
				</CardHeader>
				<CardContent class="space-y-3">
					<PromptInput onSubmit={(message) => handlePromptSubmit(message)}>
						<PromptInputFiles />
						<PromptInputBody>
							<PromptInputTextarea placeholder="Describe the change you want..." rows={2} />
						</PromptInputBody>
						<PromptInputFooter>
							<PromptInputTools>
								<PromptInputActionMenu>
									<PromptInputActionMenuTrigger />
									<PromptInputActionMenuContent>
										<PromptInputActionAddAttachments />
									</PromptInputActionMenuContent>
								</PromptInputActionMenu>
							</PromptInputTools>
							<PromptInputSubmit status={promptSubmitStatus} />
						</PromptInputFooter>
					</PromptInput>

					{#if promptSubmissionHistory.length > 0}
						<ul class="space-y-1 text-muted-foreground text-xs">
							{#each promptSubmissionHistory as entry, index (`${entry}-${index}`)}
								<li class="rounded border bg-muted/20 px-2 py-1">{entry}</li>
							{/each}
						</ul>
					{/if}
				</CardContent>
			</Card>

			<Card>
				<CardHeader>
					<CardTitle>Task</CardTitle>
					<CardDescription>Task trace with file chips.</CardDescription>
				</CardHeader>
				<CardContent>
					<Task>
						<TaskTrigger title="Searching project for context providers" />
						<TaskContent>
							<TaskItem>
								Opened <TaskItemFile>ui/src/routes/+page.svelte</TaskItemFile>
							</TaskItem>
							<TaskItem>
								Found references in <TaskItemFile>context/session-context.svelte.ts</TaskItemFile>
							</TaskItem>
						</TaskContent>
					</Task>
				</CardContent>
			</Card>

			<Card>
				<CardHeader>
					<CardTitle>Plan</CardTitle>
					<CardDescription>Collapsible plan card with streaming shimmer.</CardDescription>
				</CardHeader>
				<CardContent>
					<Plan open={true} isStreaming={true}>
						<PlanHeader>
							<div>
								<PlanTitle text="Port AI components to Svelte" />
								<PlanDescription text="Migrating structure, state, and renderer behavior." />
							</div>
							<PlanAction>
								<PlanTrigger />
							</PlanAction>
						</PlanHeader>
						<PlanContent>
							<ul class="list-disc space-y-1 pl-5 text-sm text-muted-foreground">
								<li>Port foundational components first.</li>
								<li>Add each to gallery on completion.</li>
								<li>Run typecheck after each batch.</li>
							</ul>
						</PlanContent>
						<PlanFooter class="text-xs text-muted-foreground">Streaming plan preview</PlanFooter>
					</Plan>
				</CardContent>
			</Card>

			<Card>
				<CardHeader>
					<CardTitle>Queue</CardTitle>
					<CardDescription>Collapsible queue sections with task rows.</CardDescription>
				</CardHeader>
				<CardContent>
					<Queue>
						<QueueSection defaultOpen={true}>
							<QueueSectionTrigger>
								<QueueSectionLabel count={2} label="queued tasks" />
							</QueueSectionTrigger>
							<QueueSectionContent>
								<QueueList>
									<QueueItem>
										<div class="flex items-start gap-2">
											<QueueItemIndicator completed={false} />
											<QueueItemContent>
												Port the remaining AI tool renderers
											</QueueItemContent>
											<QueueItemActions>
												<QueueItemAction aria-label="Mark complete">✓</QueueItemAction>
											</QueueItemActions>
										</div>
										<QueueItemDescription completed={false}>
											Focus on non-streamdown modules first.
										</QueueItemDescription>
										<QueueItemAttachment>
											<QueueItemImage src={demoImageDataUrl} alt="queue preview" />
											<QueueItemFile>migration-plan.md</QueueItemFile>
										</QueueItemAttachment>
									</QueueItem>

									<QueueItem>
										<div class="flex items-start gap-2">
											<QueueItemIndicator completed={true} />
											<QueueItemContent completed={true}>
												Add theme switcher in gallery
											</QueueItemContent>
										</div>
										<QueueItemDescription completed={true}>
											Completed in previous batch.
										</QueueItemDescription>
									</QueueItem>
								</QueueList>
							</QueueSectionContent>
						</QueueSection>
					</Queue>
				</CardContent>
			</Card>

			<Card>
				<CardHeader>
					<CardTitle>Commit</CardTitle>
					<CardDescription>Commit summary with expandable file changes.</CardDescription>
				</CardHeader>
				<CardContent>
					<Commit defaultOpen={true}>
						<CommitHeader>
							<CommitAuthor>
								<CommitAuthorAvatar initials="DB" />
							</CommitAuthor>
							<CommitInfo>
								<CommitHash>9a8b7c1</CommitHash>
								<CommitMessage>Port attachments, queue, and stack-trace components</CommitMessage>
								<CommitMetadata>
									<span>Discobot</span>
									<CommitSeparator />
									<CommitTimestamp date={new Date(Date.now() - 1000 * 60 * 60 * 24)} />
								</CommitMetadata>
							</CommitInfo>
							<CommitActions>
								<CommitCopyButton hash="9a8b7c1" />
							</CommitActions>
						</CommitHeader>
						<CommitContent>
							<CommitFiles>
								<CommitFile>
									<CommitFileInfo>
										<CommitFileStatus status="added" />
										<CommitFileIcon />
										<CommitFilePath>ui/src/lib/components/ai/attachments/Attachment.svelte</CommitFilePath>
									</CommitFileInfo>
									<CommitFileChanges>
										<CommitFileAdditions count={132} />
									</CommitFileChanges>
								</CommitFile>
								<CommitFile>
									<CommitFileInfo>
										<CommitFileStatus status="modified" />
										<CommitFileIcon />
										<CommitFilePath>ui/src/routes/ai-gallery/+page.svelte</CommitFilePath>
									</CommitFileInfo>
									<CommitFileChanges>
										<CommitFileAdditions count={98} />
										<CommitFileDeletions count={6} />
									</CommitFileChanges>
								</CommitFile>
							</CommitFiles>
						</CommitContent>
					</Commit>
				</CardContent>
			</Card>

			<Card>
				<CardHeader>
					<CardTitle>Connection + Toolbar</CardTitle>
					<CardDescription>Flow fallback primitives for links and node actions.</CardDescription>
				</CardHeader>
				<CardContent class="space-y-3">
					<svg viewBox="0 0 220 70" class="h-16 w-full rounded border bg-muted/20 p-2">
						<Connection fromX={10} fromY={20} toX={200} toY={50} />
					</svg>
					<Toolbar>
						<Button size="xs" variant="outline">Edit</Button>
						<Button size="xs" variant="outline">Run</Button>
						<Button size="xs" variant="outline">Delete</Button>
					</Toolbar>
				</CardContent>
			</Card>

			<Card>
				<CardHeader>
					<CardTitle>Node + Controls</CardTitle>
					<CardDescription>Flow node shell with fallback handles and controls.</CardDescription>
				</CardHeader>
				<CardContent class="space-y-3">
					<Node handles={{ target: true, source: true }} class="max-w-sm">
						<NodeHeader>
							<div>
								<NodeTitle>Agent node</NodeTitle>
								<NodeDescription>Queued for execution</NodeDescription>
							</div>
							<NodeAction>
								<Button size="xs" variant="ghost">•••</Button>
							</NodeAction>
						</NodeHeader>
						<NodeContent>
							<p class="text-muted-foreground text-sm">Porting React AI primitives to Svelte modules.</p>
						</NodeContent>
						<NodeFooter>
							<Controls>
								<Button size="icon-xs" variant="outline">+</Button>
								<Button size="icon-xs" variant="outline">−</Button>
							</Controls>
						</NodeFooter>
					</Node>
				</CardContent>
			</Card>

			<Card>
				<CardHeader>
					<CardTitle>Panel</CardTitle>
					<CardDescription>General framed panel surface.</CardDescription>
				</CardHeader>
				<CardContent>
					<Panel class="m-0">
						<div class="rounded bg-muted/40 p-3 text-sm text-muted-foreground">
							Panel content area
						</div>
					</Panel>
				</CardContent>
			</Card>

			<Card>
				<CardHeader>
					<CardTitle>Artifact</CardTitle>
					<CardDescription>Framed artifact container with actions.</CardDescription>
				</CardHeader>
				<CardContent>
					<Artifact>
						<ArtifactHeader>
							<div>
								<ArtifactTitle>README diff</ArtifactTitle>
								<ArtifactDescription>Preview generated patch</ArtifactDescription>
							</div>
							<ArtifactActions>
								<ArtifactAction tooltip="Open" aria-label="Open artifact">↗</ArtifactAction>
								<ArtifactClose />
							</ArtifactActions>
						</ArtifactHeader>
						<ArtifactContent>
							<pre class="rounded border bg-muted/40 p-2 text-xs">+ Added AI gallery route</pre>
						</ArtifactContent>
					</Artifact>
				</CardContent>
			</Card>

			<Card>
				<CardHeader>
					<CardTitle>Checkpoint</CardTitle>
					<CardDescription>Inline checkpoint action strip.</CardDescription>
				</CardHeader>
				<CardContent>
					<Checkpoint>
						<CheckpointIcon />
						<CheckpointTrigger tooltip="Restore this checkpoint">Restore checkpoint</CheckpointTrigger>
					</Checkpoint>
				</CardContent>
			</Card>

			<Card>
				<CardHeader>
					<CardTitle>Confirmation</CardTitle>
					<CardDescription>Approval flow primitives.</CardDescription>
				</CardHeader>
				<CardContent class="space-y-3">
					<Confirmation approval={{ id: "a1" }} state="approval-requested">
						<ConfirmationTitle>Allow running this shell command?</ConfirmationTitle>
						<ConfirmationRequest>
							<ConfirmationActions>
								<ConfirmationAction variant="outline">Deny</ConfirmationAction>
								<ConfirmationAction>Approve</ConfirmationAction>
							</ConfirmationActions>
						</ConfirmationRequest>
					</Confirmation>
					<Confirmation approval={{ id: "a2", approved: true }} state="output-available">
						<ConfirmationAccepted>
							<ConfirmationTitle>Approval granted.</ConfirmationTitle>
						</ConfirmationAccepted>
					</Confirmation>
					<Confirmation approval={{ id: "a3", approved: false }} state="output-denied">
						<ConfirmationRejected>
							<ConfirmationTitle>Approval denied.</ConfirmationTitle>
						</ConfirmationRejected>
					</Confirmation>
				</CardContent>
			</Card>

			<Card>
				<CardHeader>
					<CardTitle>Environment variables</CardTitle>
					<CardDescription>Secret visibility + copy actions.</CardDescription>
				</CardHeader>
				<CardContent>
					<EnvironmentVariables>
						<EnvironmentVariablesHeader>
							<EnvironmentVariablesTitle />
							<EnvironmentVariablesToggle />
						</EnvironmentVariablesHeader>
						<EnvironmentVariablesContent>
							<EnvironmentVariable name="OPENAI_API_KEY" value="sk-live-secret-value">
								<EnvironmentVariableGroup>
									<EnvironmentVariableName />
									<EnvironmentVariableRequired />
								</EnvironmentVariableGroup>
								<div class="flex items-center gap-2">
									<EnvironmentVariableValue />
									<EnvironmentVariableCopyButton />
								</div>
							</EnvironmentVariable>
							<EnvironmentVariable name="NODE_ENV" value="development">
								<div class="flex items-center gap-2">
									<EnvironmentVariableName />
								</div>
								<div class="flex items-center gap-2">
									<EnvironmentVariableValue />
									<EnvironmentVariableCopyButton copyFormat="export" />
								</div>
							</EnvironmentVariable>
						</EnvironmentVariablesContent>
					</EnvironmentVariables>
				</CardContent>
			</Card>

			<Card>
				<CardHeader>
					<CardTitle>Package info</CardTitle>
					<CardDescription>Dependency change summary card.</CardDescription>
				</CardHeader>
				<CardContent>
					<PackageInfo
						name="@discobot/ui-redesign"
						currentVersion="0.1.0"
						newVersion="0.2.0"
						changeType="minor"
					>
						<PackageInfoHeader>
							<PackageInfoName />
							<PackageInfoChangeType />
						</PackageInfoHeader>
						<PackageInfoVersion />
						<PackageInfoDependencies class="mt-3">
							<PackageInfoDependency name="svelte" version="^5.0.0" />
							<PackageInfoDependency name="bits-ui" version="^2.14.4" />
						</PackageInfoDependencies>
					</PackageInfo>
				</CardContent>
			</Card>

			<Card>
				<CardHeader>
					<CardTitle>Schema display</CardTitle>
					<CardDescription>API endpoint schema with collapsible sections.</CardDescription>
				</CardHeader>
				<CardContent>
					<SchemaDisplay
						method="POST"
						path={"/api/projects/{projectId}/sessions"}
						description="Create a new session in a project"
						parameters={[{ name: "projectId", type: "string", required: true, location: "path" }]}
						requestBody={[
							{
								name: "name",
								type: "string",
								required: true,
								description: "Session display name",
							},
							{
								name: "workspaceId",
								type: "string",
								required: false,
								description: "Optional workspace identifier",
							},
							{
								name: "mode",
								type: "string",
								required: false,
							},
						]}
						responseBody={[
							{ name: "id", type: "string", required: true },
							{ name: "status", type: "string", required: true },
						]}
					>
						<SchemaDisplayHeader>
							<div class="flex items-center gap-3">
								<SchemaDisplayMethod />
								<SchemaDisplayPath />
							</div>
						</SchemaDisplayHeader>
						<SchemaDisplayDescription />
						<SchemaDisplayContent>
							<SchemaDisplayParameters>
								<SchemaDisplayParameter
									name="projectId"
									type="string"
									required={true}
									location="path"
									description="Target project identifier"
								/>
							</SchemaDisplayParameters>
							<SchemaDisplayRequest />
							<SchemaDisplayResponse />
						</SchemaDisplayContent>
					</SchemaDisplay>
				</CardContent>
			</Card>

			<Card>
				<CardHeader>
					<CardTitle>Test results</CardTitle>
					<CardDescription>Suite and test status breakdown.</CardDescription>
				</CardHeader>
				<CardContent>
					<TestResults summary={{ passed: 8, failed: 1, skipped: 2, total: 11, duration: 1820 }}>
						<TestResultsHeader>
							<TestResultsSummary />
							<TestResultsDuration />
						</TestResultsHeader>
						<TestResultsContent>
							<TestSuite name="session context" status="failed" open={true}>
								<TestSuiteName>
									session-context.svelte.ts
									<TestSuiteStats passed={3} failed={1} skipped={0} />
								</TestSuiteName>
								<TestSuiteContent>
									<Test name="loads selected session" status="passed" duration={42}>
										<TestStatus />
										<TestName />
										<TestDuration />
									</Test>
									<Test name="maintains active file" status="failed" duration={61}>
										<TestStatus />
										<TestName />
										<TestDuration />
										<TestError>
											<TestErrorMessage>Expected selected file to persist across session switch</TestErrorMessage>
											<TestErrorStack>AssertionError: selectedFile mismatch\n at context/session-context.test.ts:88:12</TestErrorStack>
										</TestError>
									</Test>
								</TestSuiteContent>
							</TestSuite>
						</TestResultsContent>
					</TestResults>
				</CardContent>
			</Card>

			<Card>
				<CardHeader>
					<CardTitle>Open in chat</CardTitle>
					<CardDescription>Provider shortcuts in a dropdown menu.</CardDescription>
				</CardHeader>
				<CardContent>
					<OpenIn query="How can I optimize this Svelte component?">
						<OpenInTrigger />
						<OpenInContent>
							<OpenInLabel>Choose provider</OpenInLabel>
							<OpenInSeparator />
							<OpenInChatGPT />
							<OpenInClaude />
							<OpenInT3 />
							<OpenInScira />
							<OpenInv0 />
							<OpenInCursor />
						</OpenInContent>
					</OpenIn>
				</CardContent>
			</Card>

			<Card>
				<CardHeader>
					<CardTitle>Context usage</CardTitle>
					<CardDescription>Token and cost breakdown hover card.</CardDescription>
				</CardHeader>
				<CardContent>
					<Context
						usedTokens={48200}
						maxTokens={128000}
						usage={{
							inputTokens: 32000,
							outputTokens: 15400,
							reasoningTokens: 720,
							cachedInputTokens: 6200,
						}}
						modelId="gpt-5.3"
					>
						<ContextTrigger />
						<ContextContent>
							<ContextContentHeader />
							<ContextContentBody class="space-y-2">
								<ContextInputUsage />
								<ContextOutputUsage />
								<ContextReasoningUsage />
								<ContextCacheUsage />
							</ContextContentBody>
							<ContextContentFooter />
						</ContextContent>
					</Context>
				</CardContent>
			</Card>

			<Card>
				<CardHeader>
					<CardTitle>Stack trace</CardTitle>
					<CardDescription>Collapsible parsed stack frame renderer.</CardDescription>
				</CardHeader>
				<CardContent>
					<StackTrace trace={demoStackTrace} defaultOpen={true}>
						<StackTraceHeader>
							<StackTraceError>
								<StackTraceErrorType />
								<StackTraceErrorMessage />
							</StackTraceError>
							<StackTraceActions>
								<StackTraceCopyButton />
								<StackTraceExpandButton />
							</StackTraceActions>
						</StackTraceHeader>
						<StackTraceContent maxHeight={180}>
							<StackTraceFrames showInternalFrames={true} />
						</StackTraceContent>
					</StackTrace>
				</CardContent>
			</Card>
		</div>
	</div>
</div>
