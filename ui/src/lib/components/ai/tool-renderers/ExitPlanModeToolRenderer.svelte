<script lang="ts">
	import ClipboardCheckIcon from "@lucide/svelte/icons/clipboard-check";
	import Loader2Icon from "@lucide/svelte/icons/loader-2";
	import Maximize2Icon from "@lucide/svelte/icons/maximize-2";
	import { api } from "$lib/api-client";
	import { MessageResponse } from "$lib/components/ai/message";
	import {
		ToolContent,
		ToolHeaderControls,
		ToolHeaderStatus,
	} from "$lib/components/ai/tool";
	import { Button } from "$lib/components/ui/button";
	import { CollapsibleTrigger } from "$lib/components/ui/collapsible";
	import * as Dialog from "$lib/components/ui/dialog";
	import { getPlanToolState } from "$lib/session/domains/session-domain.helpers";
	import type { AskUserQuestionToolInput } from "$lib/components/ai/tool-schemas/askuserquestion-schema";
	import AskUserQuestionWizard from "./AskUserQuestionWizard.svelte";
	import type { ToolRendererComponentProps } from "./types";
	import { shortenPath } from "./utils";

	type PendingQuestionLike = {
		toolUseID: string;
		questions: AskUserQuestionToolInput["questions"];
	};

	type PendingQuestionResponse = {
		status: "pending" | "answered" | "expired";
		question: PendingQuestionLike | null;
	};

	let {
		toolPart,
		sessionId = null,
		threadId = null,
		onToolApprovalResponse,
		isRaw,
		onToggleRaw,
	}: ToolRendererComponentProps = $props();

	let pendingQuestion = $state<PendingQuestionLike | null>(null);
	let approvalStatus = $state<
		"idle" | "loading" | "pending" | "answered" | "error"
	>("idle");
	let approvalError = $state<string | null>(null);
	let localAnswers = $state<Record<string, string> | null>(null);
	let planExpanded = $state(false);

	const planState = $derived.by(() => getPlanToolState(toolPart));
	const approvalId = $derived.by(() => planState?.approvalId ?? null);
	const planMarkdown = $derived.by(
		() =>
			planState?.planMarkdown ??
			pendingQuestion?.questions.find((question) => question.notes)?.notes ??
			null,
	);

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

	const resolvedAnswers = $derived.by(
		() => parseAnswers(toolPart.output) ?? localAnswers,
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
			localAnswers = null;
			return;
		}

		if (!sessionId || !threadId || !approvalId) {
			approvalStatus = "loading";
			approvalError = null;
			pendingQuestion = null;
			return;
		}

		approvalStatus = "loading";
		approvalError = null;
		pendingQuestion = null;
		localAnswers = null;

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
					error instanceof Error
						? error.message
						: "Failed to load plan approval";
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

		if (!sessionId || !threadId) {
			approvalStatus = "answered";
			pendingQuestion = null;
			return;
		}

		try {
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
					: "Failed to submit plan approval";
		}
	}

	const answerSummary = $derived.by(
		() =>
			pendingQuestion?.questions
				.map((question) => ({
					question: question.question,
					answer: resolvedAnswers?.[question.question] ?? null,
				}))
				.filter((item) => item.answer) ?? [],
	);
</script>

<div class="flex items-center justify-between gap-4 px-4 pt-4">
	<CollapsibleTrigger
		class="flex min-w-0 flex-1 items-center gap-2 text-left text-muted-foreground"
		disabled={toolPart.state === "approval-requested"}
	>
		<ClipboardCheckIcon class="size-4 shrink-0 text-muted-foreground" />
		<span class="truncate font-medium text-sm">Exit Plan Mode</span>
		<ToolHeaderStatus state={toolPart.state} />
	</CollapsibleTrigger>
	<ToolHeaderControls
		{isRaw}
		{onToggleRaw}
		canCollapse={toolPart.state !== "approval-requested"}
	/>
</div>

<ToolContent>
	<div class="space-y-4 p-4 pt-3">
		{#if planState?.phase === "awaiting_approval"}
			{#if approvalStatus === "loading"}
				<div class="flex items-center gap-2 text-muted-foreground text-sm">
					<Loader2Icon class="size-4 animate-spin" />
					<span>Loading plan approval…</span>
				</div>
			{:else if approvalStatus === "error"}
				<div
					class="rounded-md border border-destructive/20 bg-destructive/10 p-3 text-destructive text-sm"
				>
					{approvalError ?? "Failed to load plan approval."}
				</div>
			{:else if approvalStatus === "answered"}
				<div class="rounded-md border bg-muted/20 p-3 text-sm">
					Plan approval submitted. Waiting for the agent to continue.
				</div>
				{#if answerSummary.length > 0}
					<div class="space-y-2">
						{#each answerSummary as item}
							<div class="space-y-1">
								<p class="font-medium text-sm">{item.question}</p>
								<p class="text-muted-foreground text-sm">{item.answer}</p>
							</div>
						{/each}
					</div>
				{/if}
			{:else if pendingQuestion}
				<div class="rounded-lg border bg-card p-4">
					<AskUserQuestionWizard {pendingQuestion} onSubmit={submitAnswers} />
				</div>
			{:else}
				<p class="text-muted-foreground text-sm">
					Waiting for plan approval details…
				</p>
			{/if}
		{:else if planMarkdown}
			<div
				class="relative max-h-72 overflow-y-auto rounded-md border bg-muted/20 p-3 text-sm"
			>
				<Button
					class="absolute right-1 top-1 h-6 w-6"
					size="icon"
					variant="ghost"
					onclick={() => {
						planExpanded = true;
					}}
				>
					<Maximize2Icon class="size-3" />
				</Button>
				<MessageResponse text={planMarkdown} />
			</div>
		{:else if planState?.phase === "feedback"}
			<div class="rounded-md border bg-muted/20 p-3 text-sm">
				<p class="font-medium text-sm">Plan revision requested.</p>
				{#if planState.feedback}
					<p class="mt-1 text-muted-foreground text-sm">{planState.feedback}</p>
				{/if}
			</div>
		{:else if planState?.phase === "rejected"}
			<div class="rounded-md border bg-muted/20 p-3 text-sm">
				Plan rejected. Revise the plan and try again.
			</div>
		{:else if planState?.phase === "auto_exited"}
			<div class="rounded-md border bg-muted/20 p-3 text-sm">
				Plan mode exited. The agent can continue with implementation.
			</div>
		{:else if planState?.phase === "error"}
			<div
				class="rounded-md border border-destructive/20 bg-destructive/10 p-3 text-destructive text-sm"
			>
				{planState.errorText ?? "Plan mode could not exit yet."}
			</div>
		{:else}
			<p class="text-muted-foreground text-sm">Plan status is unavailable.</p>
		{/if}

		{#if planState?.planFilePath}
			<div class="space-y-1.5">
				<h4
					class="font-medium text-muted-foreground text-xs uppercase tracking-wide"
				>
					Plan file
				</h4>
				<code
					class="block overflow-x-auto rounded-md border bg-muted/20 px-3 py-2 font-mono text-xs text-foreground"
				>
					{shortenPath(planState.planFilePath)}
				</code>
			</div>
		{/if}

		{#if approvalError && approvalStatus !== "error"}
			<div
				class="rounded-md border border-destructive/20 bg-destructive/10 p-3 text-destructive text-sm"
			>
				{approvalError}
			</div>
		{/if}
	</div>
</ToolContent>

<Dialog.Root bind:open={planExpanded}>
	<Dialog.Content
		class="sm:max-w-4xl max-h-[85vh] flex flex-col overflow-hidden"
	>
		<Dialog.Header>
			<Dialog.Title>Plan</Dialog.Title>
		</Dialog.Header>
		<div class="flex-1 overflow-y-auto text-sm">
			{#if planMarkdown}
				<MessageResponse text={planMarkdown} />
			{:else}
				<p class="text-muted-foreground text-sm">
					Plan markdown is unavailable.
				</p>
			{/if}
		</div>
	</Dialog.Content>
</Dialog.Root>
