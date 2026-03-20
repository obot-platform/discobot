<script lang="ts">
	import MessageSquareQuoteIcon from "@lucide/svelte/icons/message-square-quote";
	import { ToolInput, ToolOutput } from "$lib/components/ai/tool";
	import {
		type AskUserQuestionToolInput,
		validateAskUserQuestionInput,
		validateAskUserQuestionOutput,
	} from "$lib/components/ai/tool-schemas/askuserquestion-schema";
	import { api } from "$lib/api-client";
	import AskUserQuestionWizard from "./AskUserQuestionWizard.svelte";
	import type { ToolRendererComponentProps } from "./types";

	let { toolPart, sessionId = null, threadId = null }: ToolRendererComponentProps = $props();

	type PendingQuestionLike = {
		toolUseID: string;
		questions: AskUserQuestionToolInput["questions"];
	};

	type PendingQuestionResponse = {
		status: "pending" | "answered" | "expired";
		question: PendingQuestionLike | null;
	};

	let pendingQuestion = $state<PendingQuestionLike | null>(null);
	let approvalStatus = $state<"idle" | "loading" | "pending" | "answered" | "error">(
		"idle",
	);
	let approvalError = $state<string | null>(null);
	let localAnswers = $state<Record<string, string> | null>(null);

	function parseAnswersFromText(text: string): Record<string, string> | null {
		const pairRegex = /"([^"\\]*(?:\\.[^"\\]*)*)"="([^"\\]*(?:\\.[^"\\]*)*)"/g;
		const pairs: Record<string, string> = {};
		for (const match of text.matchAll(pairRegex)) {
			pairs[match[1]] = match[2];
		}
		return Object.keys(pairs).length > 0 ? pairs : null;
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
	const parsedAnswers = $derived.by(() =>
		outputText ? parseAnswersFromText(outputText) : null,
	);
	const resolvedAnswers = $derived.by(() => parsedAnswers ?? localAnswers);
	const questions = $derived.by(() => validInput?.questions ?? []);
	const summaryQuestions = $derived.by(() => pendingQuestion?.questions ?? questions);

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

	async function submitAnswers(toolUseID: string, answers: Record<string, string>) {
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

			approvalStatus = "answered";
			pendingQuestion = null;
		} catch (error) {
			approvalStatus = "pending";
			approvalError =
				error instanceof Error ? error.message : "Failed to submit question answer";
		}
	}
</script>

{#if toolPart.state === "approval-requested"}
	<div class="space-y-4 p-4">
		<div class="flex items-center gap-2">
			<MessageSquareQuoteIcon class="size-4 text-muted-foreground" />
			<h4 class="font-medium text-muted-foreground text-xs uppercase tracking-wide">Agent question</h4>
		</div>

		{#if approvalStatus === "loading"}
			<p class="text-muted-foreground text-sm">Loading question...</p>
		{:else if approvalStatus === "error"}
			<div class="space-y-1.5">
				<h4 class="font-medium text-destructive text-xs uppercase tracking-wide">Error</h4>
				<p class="text-destructive text-sm">{approvalError ?? "Failed to load question"}</p>
			</div>
		{:else if approvalStatus === "answered"}
			{#if summaryQuestions.length > 0 && resolvedAnswers}
				<div class="space-y-2">
					{#each summaryQuestions as question}
						<div class="space-y-1">
							<p class="font-medium text-sm">{question.question}</p>
							<p class="text-muted-foreground text-sm">{resolvedAnswers[question.question] ?? "No answer"}</p>
						</div>
					{/each}
				</div>
			{:else}
				<p class="text-muted-foreground text-sm">Question answered.</p>
			{/if}
		{:else if pendingQuestion}
			<div class="rounded-lg border bg-card p-4">
				<AskUserQuestionWizard pendingQuestion={pendingQuestion} onSubmit={submitAnswers} />
			</div>
		{:else}
			<p class="text-muted-foreground text-sm">Waiting for question details...</p>
		{/if}

		{#if approvalError && approvalStatus !== "error"}
			<p class="text-destructive text-sm">{approvalError}</p>
		{/if}

		{#if toolPart.errorText}
			<div class="space-y-1.5">
				<h4 class="font-medium text-destructive text-xs uppercase tracking-wide">Error</h4>
				<p class="text-destructive text-sm">{toolPart.errorText}</p>
			</div>
		{/if}
	</div>
{:else if !toolPart.input || typeof toolPart.input !== "object"}
	<div class="p-4 text-muted-foreground text-sm">{isStreaming ? "Loading question..." : "No input data"}</div>
{:else if !inputValidation.success}
	{#if isStreaming}
		<div class="p-4 text-muted-foreground text-sm">Loading question...</div>
	{:else}
		<ToolInput input={toolPart.input} />
		<ToolOutput output={toolPart.output} errorText={toolPart.errorText} />
	{/if}
{:else}
	<div class="space-y-4 p-4">
		<div class="flex items-center gap-2">
			<MessageSquareQuoteIcon class="size-4 text-muted-foreground" />
			<h4 class="font-medium text-muted-foreground text-xs uppercase tracking-wide">Agent question</h4>
		</div>

		{#if questions.length > 0}
			<div class="space-y-2">
				{#if resolvedAnswers}
					{#each questions as question}
						<div class="space-y-1">
							<p class="font-medium text-sm">{question.question}</p>
							<p class="text-muted-foreground text-sm">{resolvedAnswers[question.question] ?? "No answer"}</p>
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
				<h4 class="font-medium text-muted-foreground text-xs uppercase tracking-wide">Response</h4>
				<pre class="overflow-x-auto whitespace-pre-wrap break-words rounded-md border bg-muted/30 p-3 text-sm">{outputText}</pre>
			</div>
		{/if}

		{#if outputValidation && !outputValidation.success}
			<div class="rounded-md border border-dashed px-3 py-2 text-muted-foreground text-xs">
				Could not parse tool output.
			</div>
		{/if}

		{#if toolPart.errorText}
			<div class="space-y-1.5">
				<h4 class="font-medium text-destructive text-xs uppercase tracking-wide">Error</h4>
				<p class="text-destructive text-sm">{toolPart.errorText}</p>
			</div>
		{/if}

		<ToolOutput output={toolPart.output} errorText={toolPart.errorText} />
	</div>
{/if}
