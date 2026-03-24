<script lang="ts">
	import FileTextIcon from "@lucide/svelte/icons/file-text";
	import Loader2Icon from "@lucide/svelte/icons/loader-2";
	import { api } from "$lib/api-client";
	import { MessageResponse } from "$lib/components/ai/message";
	import { Button } from "$lib/components/ui/button";
	import * as Dialog from "$lib/components/ui/dialog";
	import type { LatestPlanState } from "$lib/session/domains/session-domain.helpers";

	type PendingQuestionResponse = {
		status: "pending" | "answered" | "expired";
		question: {
			toolUseID: string;
			questions: Array<{ notes?: string }>;
		} | null;
	};

	type Props = {
		latestPlan: LatestPlanState | null;
		sessionId?: string | null;
		threadId?: string | null;
	};

	let { latestPlan, sessionId = null, threadId = null }: Props = $props();

	let open = $state(false);
	let pendingNotes = $state<string | null>(null);
	let notesStatus = $state<"idle" | "loading" | "ready" | "error">("idle");
	let notesError = $state<string | null>(null);

	const planMarkdown = $derived.by(
		() => latestPlan?.planMarkdown ?? pendingNotes ?? null,
	);
	const buttonLabel = $derived.by(() => {
		if (!latestPlan) {
			return "Plan";
		}
		if (latestPlan.phase === "awaiting_approval") {
			return "Review";
		}
		if (latestPlan.phase === "approved" || latestPlan.phase === "auto_exited") {
			return "Plan";
		}
		if (latestPlan.phase === "feedback") {
			return "Revise";
		}
		if (latestPlan.phase === "error") {
			return "Blocked";
		}
		return "Plan";
	});

	function shortenPlanPath(path: string): string {
		return path.replace(/^\/home\/discobot/, "~");
	}

	async function fetchPendingNotes(
		sessionId: string,
		threadId: string,
		questionId: string,
	): Promise<string | null> {
		const result = (await api.getThreadChatQuestion(
			sessionId,
			threadId,
			questionId,
		)) as PendingQuestionResponse;
		if (result.status !== "pending" || !result.question) {
			return null;
		}
		return (
			result.question.questions.find((question) => question.notes)?.notes ??
			null
		);
	}

	$effect(() => {
		if (!open) {
			return;
		}

		if (
			latestPlan?.phase !== "awaiting_approval" ||
			latestPlan.planMarkdown ||
			!sessionId ||
			!threadId ||
			!latestPlan.approvalId
		) {
			notesStatus = planMarkdown ? "ready" : "idle";
			notesError = null;
			return;
		}

		notesStatus = "loading";
		notesError = null;

		let cancelled = false;
		void fetchPendingNotes(sessionId, threadId, latestPlan.approvalId)
			.then((notes) => {
				if (cancelled) {
					return;
				}
				pendingNotes = notes;
				notesStatus = "ready";
			})
			.catch((error) => {
				if (cancelled) {
					return;
				}
				notesStatus = "error";
				notesError =
					error instanceof Error ? error.message : "Failed to load plan";
			});

		return () => {
			cancelled = true;
		};
	});
</script>

{#if latestPlan}
	<Button
		variant="ghost"
		size="xs"
		class="h-8 gap-1.5 px-2"
		onclick={() => {
			open = true;
		}}
	>
		{#if latestPlan.phase === "awaiting_approval"}
			<Loader2Icon class="size-3.5 animate-spin text-blue-500" />
		{:else}
			<FileTextIcon class="size-3.5" />
		{/if}
		<span class="text-xs font-medium">{buttonLabel}</span>
	</Button>

	<Dialog.Root bind:open>
		<Dialog.Content
			class="sm:max-w-4xl max-h-[85vh] flex flex-col overflow-hidden"
		>
			<Dialog.Header>
				<Dialog.Title>Latest plan</Dialog.Title>
				<Dialog.Description>
					{#if latestPlan.phase === "awaiting_approval"}
						Review the current plan before answering the approval request.
					{:else if latestPlan.phase === "feedback"}
						The latest plan needs revision.
					{:else if latestPlan.phase === "error"}
						Plan mode is blocked until the issue is resolved.
					{:else}
						Latest plan shared by the agent.
					{/if}
				</Dialog.Description>
			</Dialog.Header>

			<div class="flex-1 space-y-4 overflow-y-auto text-sm">
				{#if planMarkdown}
					<MessageResponse text={planMarkdown} />
				{:else if notesStatus === "loading"}
					<div class="flex items-center gap-2 text-muted-foreground text-sm">
						<Loader2Icon class="size-4 animate-spin" />
						<span>Loading plan…</span>
					</div>
				{:else}
					<div class="rounded-md border bg-muted/20 p-3 text-sm">
						{#if latestPlan.phase === "entered"}
							Plan mode is active, but no rendered plan is available yet.
						{:else if latestPlan.phase === "awaiting_approval"}
							The plan approval request is waiting for its latest markdown
							content.
						{:else if latestPlan.phase === "feedback" && latestPlan.feedback}
							{latestPlan.feedback}
						{:else if latestPlan.phase === "error" && latestPlan.errorText}
							{latestPlan.errorText}
						{:else}
							No plan markdown is available for this state.
						{/if}
					</div>
				{/if}

				{#if latestPlan.planFilePath}
					<div class="space-y-1.5">
						<h4
							class="font-medium text-muted-foreground text-xs uppercase tracking-wide"
						>
							Plan file
						</h4>
						<code
							class="block overflow-x-auto rounded-md border bg-muted/20 px-3 py-2 font-mono text-xs text-foreground"
						>
							{shortenPlanPath(latestPlan.planFilePath)}
						</code>
					</div>
				{/if}

				{#if notesStatus === "error" && notesError}
					<div
						class="rounded-md border border-destructive/20 bg-destructive/10 p-3 text-destructive text-sm"
					>
						{notesError}
					</div>
				{/if}
			</div>
		</Dialog.Content>
	</Dialog.Root>
{/if}
