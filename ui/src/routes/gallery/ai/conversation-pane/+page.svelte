<script lang="ts">
	import ArrowLeftIcon from "@lucide/svelte/icons/arrow-left";
	import type { ChatMessage } from "$lib/api-types";
	import type { DynamicToolPart, ToolState } from "$lib/components/ai/types";
	import ConversationPane from "$lib/components/app/ConversationPane.svelte";
	import { Badge } from "$lib/components/ui/badge";
	import { Button } from "$lib/components/ui/button";
	import {
		Card,
		CardContent,
		CardDescription,
		CardHeader,
		CardTitle,
	} from "$lib/components/ui/card";
	import { Switch } from "$lib/components/ui/switch";

	type ConversationStatus = "ready" | "loading" | "streaming";

	const demoImage =
		"iVBORw0KGgoAAAANSUhEUgAAAMAAAABgCAYAAABb7G8pAAAACXBIWXMAAAsSAAALEgHS3X78AAAAxUlEQVR4nO3RMQ0AAAgDINc/9K3hHFQgkYJumNnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dnZ2dl5AB8qA0R9q8fVAAAAAElFTkSuQmCC";
	const demoImageDataUrl = `data:image/png;base64,${demoImage}`;

	let chatWidthMode = $state<"full" | "constrained">("constrained");
	let conversationStatus = $state<ConversationStatus>("ready");
	let showErrors = $state(false);
	let showFullFixture = $state(true);
	let toolDefaultOpen = $state(false);
	let paneRenderVersion = $state(0);

	function createToolPart({
		toolCallId,
		toolName,
		state,
		input,
		output,
		errorText,
		approval,
		title,
	}: {
		toolCallId: string;
		toolName: string;
		state: ToolState;
		input: unknown;
		output?: unknown;
		errorText?: string;
		approval?: DynamicToolPart["approval"];
		title?: string;
	}): DynamicToolPart {
		return {
			type: "dynamic-tool",
			toolCallId,
			toolName,
			state,
			input,
			...(output !== undefined ? { output } : {}),
			...(errorText ? { errorText } : {}),
			...(approval ? { approval } : {}),
			...(title ? { title } : {}),
		};
	}

	function createMessage(
		id: string,
		role: ChatMessage["role"],
		parts: unknown[],
		extra?: Record<string, unknown>,
	): ChatMessage {
		return {
			id,
			role,
			parts,
			...(extra ?? {}),
		} as ChatMessage;
	}

	const mockConversation = [
		createMessage("user-1", "user", [
			{
				type: "text",
				text: "Show me every message part and tool renderer state we currently support so I can tune the interaction design.",
			},
		]),
		createMessage(
			"assistant-1",
			"assistant",
			[
				{
					type: "reasoning",
					text: "I can mock the full conversation surface. The current pane renders text parts, reasoning parts, and dynamic-tool parts; the heavier runtime coupling mostly lives in the composer rather than the message display.",
				},
				{
					type: "text",
					text: "This route uses the real `ConversationPane` with mocked `ChatMessage[]`, so spacing, markdown rendering, tool cards, and scroll behavior match the live shell while staying independent from a real session.",
				},
			],
			{ status: "streaming" },
		),
		createMessage("user-2", "user", [
			{
				type: "text",
				text: "Great. Include every tool renderer, multiple tool states, and at least one fallback case.",
			},
		]),
		createMessage("assistant-2", "assistant", [
			{
				type: "text",
				text: "Here’s the full fixture set. You can flip width, loading, streaming, and banner errors from the controls above.",
			},
		]),
		createMessage(
			"assistant-reasoning-streaming",
			"assistant",
			[
				{
					type: "reasoning",
					text: "Comparing open-by-default behavior against dense tool output and checking how partial reasoning should feel while the assistant is still thinking.",
				},
			],
			{ status: "streaming" },
		),
		createMessage("assistant-tool-bash", "assistant", [
			createToolPart({
				toolCallId: "tool-bash-ok",
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
					stdout:
						"svelte-check found 0 errors and 0 warnings\neslint found 0 problems",
				},
			}),
		]),
		createMessage("assistant-tool-read", "assistant", [
			createToolPart({
				toolCallId: "tool-read-streaming",
				toolName: "Read",
				state: "input-streaming",
				input: {
					file_path:
						"/home/discobot/workspace/ui/src/lib/components/app/ConversationPane.svelte",
					offset: 0,
					limit: 120,
				},
			}),
		]),
		createMessage("assistant-tool-write", "assistant", [
			createToolPart({
				toolCallId: "tool-write",
				toolName: "Write",
				state: "output-available",
				input: {
					file_path:
						"/home/discobot/workspace/ui/src/routes/gallery/ai/conversation-pane/+page.svelte",
					content:
						'<script lang="ts">\n\t// mocked route scaffold\n<\\/script>\n',
				},
				output: {
					success: true,
					bytes_written: 57,
				},
			}),
		]),
		createMessage("assistant-tool-edit", "assistant", [
			createToolPart({
				toolCallId: "tool-edit",
				toolName: "Edit",
				state: "output-available",
				input: {
					file_path:
						"/home/discobot/workspace/ui/src/lib/components/app/ConversationPane.svelte",
					old_string: "const preferences = app.preferences;",
					new_string:
						'const effectiveChatWidthMode = chatWidthMode ?? app?.preferences.chatWidthMode ?? "full";',
					replace_all: false,
				},
				output: {
					replacements: 1,
				},
			}),
		]),
		createMessage("assistant-tool-grep", "assistant", [
			createToolPart({
				toolCallId: "tool-grep",
				toolName: "Grep",
				state: "output-available",
				input: {
					pattern: "useThreadContext\\(",
					path: "/home/discobot/workspace/ui/src",
					glob: "**/*.{svelte,ts}",
					output_mode: "content",
				},
				output: {
					matches: [
						{
							file: "/home/discobot/workspace/ui/src/lib/components/app/ConversationPane.svelte",
							line: 16,
							content:
								'import { useThreadContext } from "$lib/context/thread-context.svelte";',
						},
					],
				},
			}),
		]),
		createMessage("assistant-tool-glob", "assistant", [
			createToolPart({
				toolCallId: "tool-glob",
				toolName: "Glob",
				state: "input-available",
				input: {
					path: "/home/discobot/workspace/ui/src/routes",
					pattern: "gallery/ai/**/*",
				},
				output: {
					files: [
						"/home/discobot/workspace/ui/src/routes/gallery/ai/+page.svelte",
						"/home/discobot/workspace/ui/src/routes/gallery/ai/conversation-pane/+page.svelte",
					],
				},
			}),
		]),
		createMessage("assistant-tool-websearch", "assistant", [
			createToolPart({
				toolCallId: "tool-websearch",
				toolName: "WebSearch",
				state: "output-available",
				input: {
					query: "Svelte 5 runes docs 2026",
					allowed_domains: ["svelte.dev"],
					blocked_domains: ["example.com"],
				},
				output: {
					results: [
						{
							title: "Svelte documentation",
							url: "https://svelte.dev/docs/svelte/overview",
							snippet: "Runes, components, and reactivity reference.",
						},
					],
				},
			}),
		]),
		createMessage("assistant-tool-webfetch", "assistant", [
			createToolPart({
				toolCallId: "tool-webfetch",
				toolName: "WebFetch",
				state: "output-available",
				input: {
					url: "https://example.com/ux-notes",
					prompt: "Summarize the messaging UX guidance.",
				},
				output: {
					content:
						"## Summary\n- Keep action rows lightweight\n- Preserve scroll position during streaming\n- Make error banners obvious but non-blocking",
				},
			}),
		]),
		createMessage("assistant-tool-todowrite", "assistant", [
			createToolPart({
				toolCallId: "tool-todowrite",
				toolName: "TodoWrite",
				state: "output-available",
				input: {
					todos: [
						{
							content: "Wire mock conversation route",
							activeForm: "Wiring mock conversation route",
							status: "completed",
						},
						{
							content: "Review tool card spacing",
							activeForm: "Reviewing tool card spacing",
							status: "in_progress",
						},
						{
							content: "Polish hover and focus states",
							activeForm: "Polishing hover and focus states",
							status: "pending",
						},
					],
				},
				output: {
					content:
						"Todo list updated.\n\nCurrent status is 1 completed, 1 in progress, and 1 pending.\n\n### Current tasks\n- [x] Wire mock conversation route\n- [ ] Review tool card spacing _(in progress: Reviewing tool card spacing)_\n- [ ] Polish hover and focus states",
				},
			}),
		]),
		createMessage("assistant-tool-task", "assistant", [
			createToolPart({
				toolCallId: "tool-task",
				toolName: "Task",
				state: "output-available",
				input: {
					subagent_type: "Explore",
					description: "Inspect conversation renderer usage",
					prompt:
						"Find everywhere the message pane depends on thread/session context.",
					max_turns: 5,
					model: "haiku",
					run_in_background: false,
				},
				output: {
					agentId: "agent-42",
					result:
						"Conversation display mainly depends on thread messages/status and chat width preference.",
				},
			}),
		]),
		createMessage("assistant-tool-skill", "assistant", [
			createToolPart({
				toolCallId: "tool-skill",
				toolName: "Skill",
				state: "output-available",
				input: {
					skill: "vercel-react-best-practices",
					args: "",
				},
				output: {
					result:
						"Loaded React and Next.js performance guidance from Vercel Engineering.",
				},
			}),
		]),
		createMessage("assistant-tool-ask-pending", "assistant", [
			createToolPart({
				toolCallId: "tool-ask-pending",
				toolName: "AskUserQuestion",
				state: "approval-requested",
				approval: { id: "approval-ask-pending" },
				input: {
					questions: [
						{
							header: "Message width",
							question: "Which interaction density should we optimize first?",
							multiSelect: false,
							options: [
								{
									label: "Dense",
									description: "Fit more tool output above the fold.",
								},
								{
									label: "Relaxed",
									description: "Give each message part more breathing room.",
								},
							],
						},
					],
				},
			}),
		]),
		createMessage("assistant-tool-ask-answered", "assistant", [
			createToolPart({
				toolCallId: "tool-ask-answered",
				toolName: "AskUserQuestion",
				state: "output-available",
				input: {
					questions: [
						{
							header: "Tool chrome",
							question: "Should tool cards stay expanded after completion?",
							multiSelect: false,
							options: [
								{
									label: "Expanded",
									description: "Keep results fully visible.",
								},
								{
									label: "Collapsed",
									description: "Compress completed work by default.",
								},
							],
						},
					],
				},
				output:
					'"Should tool cards stay expanded after completion?"="Expanded"',
			}),
		]),
		createMessage("assistant-tool-denied", "assistant", [
			createToolPart({
				toolCallId: "tool-denied",
				toolName: "Bash",
				state: "output-denied",
				input: {
					command: "git push origin main",
					description: "Push the branch",
					run_in_background: false,
				},
				errorText: "The user denied permission for this action.",
				approval: {
					id: "approval-push",
					approved: false,
					reason: "Keep this local for now",
				},
			}),
		]),
		createMessage("assistant-tool-error", "assistant", [
			createToolPart({
				toolCallId: "tool-error",
				toolName: "Write",
				state: "output-error",
				input: {
					file_path:
						"/home/discobot/workspace/ui/src/routes/gallery/ai/conversation-pane/+page.svelte",
					content: "<broken />",
				},
				errorText:
					"Write failed because the destination directory did not exist.",
			}),
		]),
		createMessage("assistant-tool-fallback", "assistant", [
			createToolPart({
				toolCallId: "tool-fallback",
				toolName: "DeployPreview",
				state: "approval-responded",
				title: "Custom tool fallback",
				input: {
					target: "preview",
					service: "ui-redesign",
					snapshot: demoImageDataUrl,
				},
				output: {
					url: "https://preview.example.com/discobot-ui",
					status: "ready",
				},
			}),
		]),
		createMessage("assistant-tool-then-text", "assistant", [
			createToolPart({
				toolCallId: "tool-collapse-demo",
				toolName: "Grep",
				state: "output-available",
				input: {
					pattern: "ConversationPane",
					path: "/home/discobot/workspace/ui/src/lib/components/app",
					glob: "**/*.svelte",
					output_mode: "files_with_matches",
				},
				output: {
					files: [
						"/home/discobot/workspace/ui/src/lib/components/app/ConversationPane.svelte",
					],
				},
			}),
			{
				type: "text",
				text: "This completed assistant turn should now collapse the earlier tool step into a single summary row while keeping this final reply visible.",
			},
		]),
		createMessage("assistant-3", "assistant", [
			{
				type: "text",
				text: "The current pane display itself is only lightly context-dependent: it needs message data, a status flag, optional error banners, and chat width. The composer is the part that depends on the wider app/session/thread runtime.",
			},
		]),
	] as ChatMessage[];

	const shortConversation = mockConversation.slice(0, 6);

	const activeMessages = $derived.by(() =>
		showFullFixture ? mockConversation : shortConversation,
	);
	const sessionError = $derived.by(() =>
		showErrors
			? "Mock session error: the sandbox reported a startup issue."
			: null,
	);
	const threadError = $derived.by(() =>
		showErrors
			? "Mock thread error: a streamed tool chunk failed validation."
			: null,
	);
</script>

<svelte:head>
	<title>Conversation Pane Gallery</title>
</svelte:head>

<div class="flex min-h-screen flex-col bg-background text-foreground">
	<div class="border-b bg-card/80 backdrop-blur">
		<div class="mx-auto flex max-w-7xl flex-col gap-4 px-6 py-6 lg:px-10">
			<div class="flex flex-wrap items-start justify-between gap-4">
				<div class="space-y-2">
					<div class="flex flex-wrap items-center gap-2">
						<Badge variant="secondary">Mocked route</Badge>
						<Badge variant="outline">Real ConversationPane</Badge>
					</div>
					<h1 class="text-3xl font-semibold tracking-tight">
						Conversation pane UX sandbox
					</h1>
					<p class="max-w-3xl text-sm text-muted-foreground">
						A dedicated preview route for tuning message spacing, tool chrome,
						reasoning, loading, and error states with mocked data only.
					</p>
				</div>

				<div class="flex flex-wrap items-center gap-2">
					<Button href="/gallery/ai" variant="outline">
						<ArrowLeftIcon class="size-4" />
						Back to AI gallery
					</Button>
				</div>
			</div>

			<div class="grid gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(22rem,0.8fr)]">
				<Card>
					<CardHeader class="pb-3">
						<CardTitle>Controls</CardTitle>
						<CardDescription
							>Flip the same layout states you’d want while iterating on message
							interaction.</CardDescription
						>
					</CardHeader>
					<CardContent class="space-y-4">
						<div class="space-y-2">
							<p class="font-medium text-sm">Chat width</p>
							<div class="flex flex-wrap gap-2">
								<Button
									variant={chatWidthMode === "constrained"
										? "default"
										: "outline"}
									size="sm"
									onclick={() => {
										chatWidthMode = "constrained";
									}}
								>
									Constrained
								</Button>
								<Button
									variant={chatWidthMode === "full" ? "default" : "outline"}
									size="sm"
									onclick={() => {
										chatWidthMode = "full";
									}}
								>
									Full width
								</Button>
							</div>
						</div>

						<div class="space-y-2">
							<p class="font-medium text-sm">Pane status</p>
							<div class="flex flex-wrap gap-2">
								<Button
									variant={conversationStatus === "ready"
										? "default"
										: "outline"}
									size="sm"
									onclick={() => {
										conversationStatus = "ready";
									}}
								>
									Ready
								</Button>
								<Button
									variant={conversationStatus === "loading"
										? "default"
										: "outline"}
									size="sm"
									onclick={() => {
										conversationStatus = "loading";
									}}
								>
									Loading
								</Button>
								<Button
									variant={conversationStatus === "streaming"
										? "default"
										: "outline"}
									size="sm"
									onclick={() => {
										conversationStatus = "streaming";
									}}
								>
									Streaming
								</Button>
							</div>
						</div>

						<div
							class="flex items-center justify-between rounded-xl border px-4 py-3"
						>
							<div>
								<p class="font-medium text-sm">Include banner errors</p>
								<p class="text-muted-foreground text-xs">
									Shows the same destructive banners the live pane renders.
								</p>
							</div>
							<Switch bind:checked={showErrors} />
						</div>

						<div
							class="flex items-center justify-between rounded-xl border px-4 py-3"
						>
							<div>
								<p class="font-medium text-sm">Show full fixture set</p>
								<p class="text-muted-foreground text-xs">
									Switch between a short transcript and the exhaustive
									message/tool matrix.
								</p>
							</div>
							<Switch bind:checked={showFullFixture} />
						</div>

						<div class="rounded-xl border px-4 py-3">
							<div class="flex items-start justify-between gap-4">
								<div>
									<p class="font-medium text-sm">Tool open state test</p>
									<p class="text-muted-foreground text-xs">
										Remount the mocked pane with all tool cards expanded or
										collapsed.
									</p>
								</div>
								<div class="flex gap-2">
									<Button
										size="sm"
										variant={toolDefaultOpen ? "default" : "outline"}
										onclick={() => {
											toolDefaultOpen = true;
											paneRenderVersion += 1;
										}}
									>
										Open all
									</Button>
									<Button
										size="sm"
										variant={!toolDefaultOpen ? "default" : "outline"}
										onclick={() => {
											toolDefaultOpen = false;
											paneRenderVersion += 1;
										}}
									>
										Close all
									</Button>
								</div>
							</div>
						</div>
					</CardContent>
				</Card>

				<Card>
					<CardHeader class="pb-3">
						<CardTitle>Dependency snapshot</CardTitle>
						<CardDescription
							>What the live display actually needs from context today.</CardDescription
						>
					</CardHeader>
					<CardContent class="space-y-3 text-sm text-muted-foreground">
						<p>
							The message display is lightly coupled: mocked messages, a status
							string, optional error banners, and chat width are enough to
							render the real pane.
						</p>
						<p>
							The composer is the heavy part — it pulls model selection,
							workspace setup, files, hooks, session credentials, submit/cancel
							behavior, and pending session state from app/session/thread
							context.
						</p>
						<p
							class="rounded-lg border bg-muted/30 p-3 font-mono text-xs text-foreground"
						>
							Current mock route: <code>/gallery/ai/conversation-pane</code>
						</p>
					</CardContent>
				</Card>
			</div>
		</div>
	</div>

	<div class="min-h-0 flex-1 overflow-hidden">
		<div class="mx-auto flex h-full max-w-7xl flex-col px-6 py-6 lg:px-10">
			<div
				class="min-h-0 flex-1 overflow-hidden rounded-2xl border bg-card shadow-sm"
			>
				{#key `${toolDefaultOpen}-${paneRenderVersion}`}
					<ConversationPane
						contentTopPadding={5}
						messages={activeMessages}
						status={conversationStatus}
						{sessionError}
						{threadError}
						{chatWidthMode}
						{toolDefaultOpen}
						showComposer={false}
					/>
				{/key}
			</div>
		</div>
	</div>
</div>
