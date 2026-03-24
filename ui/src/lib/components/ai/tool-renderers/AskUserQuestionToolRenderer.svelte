<script lang="ts">
	import MessageSquareQuoteIcon from "@lucide/svelte/icons/message-square-quote";
	import {
		ToolContent,
		ToolHeaderControls,
		ToolHeaderStatus,
	} from "$lib/components/ai/tool";
	import {
		type AskUserQuestionToolInput,
		validateAskUserQuestionInput,
		validateAskUserQuestionOutput,
	} from "$lib/components/ai/tool-schemas/askuserquestion-schema";
	import { api } from "$lib/api-client";
	import AskUserQuestionWizard from "./AskUserQuestionWizard.svelte";
	import type { ToolRendererComponentProps } from "./types";

	let {
		toolPart,
		sessionId = null,
		threadId = null,
		onToolApprovalResponse,
		isRaw,
		onToggleRaw,
	}: ToolRendererComponentProps = $props();

	type PendingQuestionLike = {
		toolUseID: string;
		questions: AskUserQuestionToolInput["questions"];
	};

	type PendingQuestionResponse = {
		status: "pending" | "answered" | "expired";
		question: PendingQuestionLike | null;
	};

	let pendingQuestion = $state<PendingQuestionLike | null>(null);
	let approvalStatus = $state<
		"idle" | "loading" | "pending" | "answered" | "error"
	>("idle");
	let approvalError = $state<string | null>(null);
	let localAnswers = $state<Record<string, string> | null>(null);

	function parseAnswers(value: unknown): Record<string, string> | null {
		const parseCandidate = (
			candidate: unknown,
		): Record<string, string> | null => {
			if (Array.isArray(candidate)) {
				const pairs: Record<string, string> = {};
				for (const item of candidate) {
					if (!item || typeof item !== "object") {
						continue;
					}
					const question = (item as { question?: unknown }).question;
					const answer = (item as { answer?: unknown }).answer;
					if (typeof question === "string" && typeof answer === "string") {
						pairs[question] = answer;
					}
				}
				return Object.keys(pairs).length > 0 ? pairs : null;
			}

			if (candidate && typeof candidate === "object") {
				const pairs: Record<string, string> = {};
				for (const [question, answer] of Object.entries(candidate)) {
					if (typeof answer === "string") {
						pairs[question] = answer;
					}
				}
				return Object.keys(pairs).length > 0 ? pairs : null;
			}

			return null;
		};

		if (typeof value === "string") {
			const trimmed = value.trim();
			if (!trimmed) {
				return null;
			}
			try {
				const parsed = JSON.parse(trimmed);
				return parseCandidate(parsed);
			} catch {
				const pairRegex =
					/"([^"\\]*(?:\\.[^"\\]*)*)"="([^"\\]*(?:\\.[^"\\]*)*)"/g;
				const pairs: Record<string, string> = {};
				for (const match of trimmed.matchAll(pairRegex)) {
					pairs[match[1]] = match[2];
				}
				return Object.keys(pairs).length > 0 ? pairs : null;
			}
		}

		return parseCandidate(value);
	}

	function getApprovalId(): string | null {
		const approval = toolPart.approval;
		if (approval && typeof approval === "object" && "id" in approval) {
			return typeof approval.id === "string" ? approval.id : null;
		}
		return toolPart.toolCallId || null;
	}

	const isStreaming = $derived.by(
		() =>
			toolPart.state === "input-streaming" ||
			toolPart.state === "input-available" ||
			toolPart.state === "approval-requested",
	);
	const approvalId = $derived.by(() => getApprovalId());
	const inputValidation = $derived.by(() =>
		validateAskUserQuestionInput(toolPart.input),
	);
	const validInput = $derived.by(() =>
		inputValidation.success
			? (inputValidation.data as AskUserQuestionToolInput)
			: undefined,
	);
	const outputValidation = $derived.by(() =>
		toolPart.output !== undefined
			? validateAskUserQuestionOutput(toolPart.output)
			: null,
	);
	const outputText = $derived.by(() => {
		if (toolPart.output === undefined || toolPart.output === null) {
			return null;
		}
		if (typeof toolPart.output === "string") {
			return toolPart.output;
		}
		try {
			return JSON.stringify(toolPart.output, null, 2);
		} catch {
			return String(toolPart.output);
		}
	});
	const parsedAnswers = $derived.by(() => parseAnswers(toolPart.output));
	const resolvedAnswers = $derived.by(() => parsedAnswers ?? localAnswers);
	const questions = $derived.by(() => validInput?.questions ?? []);
	const summaryQuestions = $derived.by(
		() => pendingQuestion?.questions ?? questions,
	);

	async function fetchPendingQuestion(
		sessionId: string,
		threadId: string,
		questionId: string,
	): Promise<PendingQuestionResponse> {
		return (await api.getThreadChatQuestion(
			sessionId,
			threadId,
			questionId,
		)) as PendingQuestionResponse;
	}

	$effect(() => {
		if (toolPart.state !== "approval-requested") {
			approvalStatus = "idle";
			approvalError = null;
			pendingQuestion = null;
			return;
		}

		approvalError = null;
		localAnswers = null;

		if (questions.length > 0) {
			pendingQuestion = {
				toolUseID: approvalId ?? toolPart.toolCallId,
				questions,
			};
			approvalStatus = "pending";
			return;
		}

		if (!sessionId || !threadId || !approvalId) {
			approvalStatus = "loading";
			pendingQuestion = null;
			return;
		}

		approvalStatus = "loading";
		pendingQuestion = null;

		let cancelled = false;
		void fetchPendingQuestion(sessionId, threadId, approvalId)
			.then((result) => {
				if (cancelled) {
					return;
				}

				if (result.status === "pending" && result.question) {
					pendingQuestion = result.question;
					approvalStatus = "pending";
					return;
				}

				pendingQuestion = null;
				approvalStatus = "answered";
			})
			.catch((error) => {
				if (cancelled) {
					return;
				}
				approvalStatus = "error";
				approvalError =
					error instanceof Error ? error.message : "Failed to load question";
			});

		return () => {
			cancelled = true;
		};
	});

	async function submitAnswers(
		toolUseID: string,
		answers: Record<string, string>,
	) {
		localAnswers = answers;
		approvalError = null;

		if (!sessionId) {
			approvalStatus = "answered";
			pendingQuestion = null;
			return;
		}

		try {
			if (!threadId) {
				approvalStatus = "pending";
				approvalError = "Missing thread context";
				return;
			}

			await api.submitThreadChatAnswer(sessionId, threadId, {
				toolUseID,
				answers,
			});

			onToolApprovalResponse?.({ id: toolUseID, approved: true });
			approvalStatus = "answered";
			pendingQuestion = null;
		} catch (error) {
			approvalStatus = "pending";
			approvalError =
				error instanceof Error
					? error.message
					: "Failed to submit question answer";
		}
	}
</script>

<div class="flex items-center justify-between gap-4 px-4 pt-4">
	<div class="flex min-w-0 flex-1 items-center gap-2 text-left">
		<MessageSquareQuoteIcon class="size-4 shrink-0 text-muted-foreground" />
		<span class="truncate font-medium text-sm">Agent question</span>
		<ToolHeaderStatus state={toolPart.state} />
	</div>
	<ToolHeaderControls {isRaw} {onToggleRaw} canCollapse={false} />
</div>

<ToolContent>
	{#if toolPart.state === "approval-requested"}
		<div class="space-y-4 p-4 pt-3">
			{#if approvalStatus === "loading"}
				<p class="text-muted-foreground text-sm">Loading question...</p>
			{:else if approvalStatus === "error"}
				<div class="space-y-1.5">
					<h4
						class="font-medium text-destructive text-xs uppercase tracking-wide"
					>
						Error
					</h4>
					<p class="text-destructive text-sm">
						{approvalError ?? "Failed to load question"}
					</p>
				</div>
			{:else if approvalStatus === "answered"}
				{#if summaryQuestions.length > 0 && resolvedAnswers}
					<div class="space-y-2">
						{#each summaryQuestions as question}
							<div class="space-y-1">
								<p class="font-medium text-sm">{question.question}</p>
								<p class="text-muted-foreground text-sm">
									{resolvedAnswers[question.question] ?? "No answer"}
								</p>
							</div>
						{/each}
					</div>
				{:else}
					<p class="text-muted-foreground text-sm">Question answered.</p>
				{/if}
			{:else if pendingQuestion}
				<div class="rounded-lg border bg-card p-4">
					<AskUserQuestionWizard {pendingQuestion} onSubmit={submitAnswers} />
				</div>
			{:else}
				<p class="text-muted-foreground text-sm">
					Waiting for question details...
				</p>
			{/if}

			{#if approvalError && approvalStatus !== "error"}
				<p class="text-destructive text-sm">{approvalError}</p>
			{/if}

			{#if toolPart.errorText}
				<div class="space-y-1.5">
					<h4
						class="font-medium text-destructive text-xs uppercase tracking-wide"
					>
						Error
					</h4>
					<p class="text-destructive text-sm">{toolPart.errorText}</p>
				</div>
			{/if}
		</div>
	{:else if !toolPart.input || typeof toolPart.input !== "object"}
		<div class="p-4 pt-3 text-muted-foreground text-sm">
			{isStreaming ? "Loading question..." : "No input data"}
		</div>
	{:else if !inputValidation.success}
		<div class="space-y-3 p-4 pt-3">
			<p class="text-muted-foreground text-sm">
				{isStreaming
					? "Loading question..."
					: "Could not parse question details."}
			</p>
			{#if outputText}
				<div class="rounded-md border border-dashed bg-muted/20 p-3">
					<pre
						class="overflow-x-auto whitespace-pre-wrap break-words font-mono text-xs">{outputText}</pre>
				</div>
			{/if}
			{#if toolPart.errorText}
				<p class="text-destructive text-sm">{toolPart.errorText}</p>
			{/if}
		</div>
	{:else}
		<div class="space-y-4 p-4 pt-3">
			{#if questions.length > 0}
				<div class="space-y-2">
					{#if resolvedAnswers}
						{#each questions as question}
							<div class="space-y-1">
								<p class="font-medium text-sm">{question.question}</p>
								<p class="text-muted-foreground text-sm">
									{resolvedAnswers[question.question] ?? "No answer"}
								</p>
							</div>
						{/each}
					{:else}
						<ul class="list-disc space-y-1 pl-5 text-sm">
							{#each questions as question}
								<li>{question.question}</li>
							{/each}
						</ul>
					{/if}
				</div>
			{/if}

			{#if outputText && !resolvedAnswers}
				<div class="space-y-1.5">
					<h4
						class="font-medium text-muted-foreground text-xs uppercase tracking-wide"
					>
						Response
					</h4>
					<pre
						class="overflow-x-auto whitespace-pre-wrap break-words rounded-md border bg-muted/30 p-3 text-sm">{outputText}</pre>
				</div>
			{/if}

			{#if outputValidation && !outputValidation.success}
				<div
					class="rounded-md border border-dashed px-3 py-2 text-muted-foreground text-xs"
				>
					Could not parse tool output.
				</div>
			{/if}

			{#if toolPart.errorText}
				<div class="space-y-1.5">
					<h4
						class="font-medium text-destructive text-xs uppercase tracking-wide"
					>
						Error
					</h4>
					<p class="text-destructive text-sm">{toolPart.errorText}</p>
				</div>
			{/if}
		</div>
	{/if}
</ToolContent>
