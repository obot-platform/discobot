<script lang="ts">
	import CheckIcon from "@lucide/svelte/icons/check";
	import Maximize2Icon from "@lucide/svelte/icons/maximize-2";
	import { MessageResponse } from "$lib/components/ai/message";
	import { Button } from "$lib/components/ui/button";
	import * as Dialog from "$lib/components/ui/dialog";
	import { cn } from "$lib/utils";
	import type { AskUserQuestionToolInput } from "$lib/components/ai/tool-schemas/askuserquestion-schema";

	const OTHER_LABEL = "__other__";
	const AUTO_ADVANCE_DELAY = 300;

	type QuestionItem = AskUserQuestionToolInput["questions"][number];

	type PendingQuestionLike = {
		toolUseID: string;
		questions: QuestionItem[];
	};

	type Props = {
		pendingQuestion: PendingQuestionLike;
		onSubmit: (
			toolUseID: string,
			answers: Record<string, string>,
		) => Promise<void>;
	};

	let { pendingQuestion, onSubmit }: Props = $props();

	let currentStep = $state(0);
	let answers = $state<Record<string, string>>({});
	let otherSelected = $state<Record<string, boolean>>({});
	let otherText = $state<Record<string, string>>({});
	let isSubmitting = $state(false);
	let contextExpanded = $state(false);
	let autoAdvanceTimeout = $state<ReturnType<typeof setTimeout> | null>(null);

	const notes = $derived.by(
		() => pendingQuestion.questions.find((question) => question.notes)?.notes,
	);
	const currentQuestion = $derived.by(
		() => pendingQuestion.questions[currentStep],
	);

	$effect(() => {
		const initialAnswers: Record<string, string> = {};
		for (const question of pendingQuestion.questions) {
			if (!question.multiSelect && question.options[0]?.label) {
				initialAnswers[question.question] = question.options[0].label;
			}
		}

		answers = initialAnswers;
		otherSelected = {};
		otherText = {};
		currentStep = 0;
		isSubmitting = false;
	});

	$effect(() => {
		return () => {
			if (autoAdvanceTimeout) {
				clearTimeout(autoAdvanceTimeout);
			}
		};
	});

	function selectedLabels(questionKey: string): string[] {
		return (answers[questionKey] ?? "").split(", ").filter(Boolean);
	}

	function isStepAnswered(stepIndex: number): boolean {
		const question = pendingQuestion.questions[stepIndex];
		if (!question) {
			return false;
		}

		if (otherSelected[question.question]) {
			return (otherText[question.question]?.trim().length ?? 0) > 0;
		}

		return (answers[question.question]?.trim().length ?? 0) > 0;
	}

	function allAnswered(): boolean {
		return pendingQuestion.questions.every((_, index) => isStepAnswered(index));
	}

	function isLastStep(): boolean {
		return currentStep >= pendingQuestion.questions.length - 1;
	}

	function currentIsAnswered(): boolean {
		return isStepAnswered(currentStep);
	}

	function findNextUnanswered(afterStep: number): number | null {
		for (
			let index = afterStep + 1;
			index < pendingQuestion.questions.length;
			index += 1
		) {
			if (!isStepAnswered(index)) {
				return index;
			}
		}
		return null;
	}

	function scheduleAutoAdvance() {
		if (autoAdvanceTimeout) {
			clearTimeout(autoAdvanceTimeout);
		}

		autoAdvanceTimeout = setTimeout(() => {
			const next = findNextUnanswered(currentStep);
			if (next !== null) {
				currentStep = next;
				return;
			}

			if (!isLastStep()) {
				currentStep += 1;
			}
		}, AUTO_ADVANCE_DELAY);
	}

	function handleOptionChange(
		question: QuestionItem,
		optionLabel: string,
		checked: boolean,
	) {
		if (optionLabel === OTHER_LABEL) {
			if (question.multiSelect) {
				otherSelected = {
					...otherSelected,
					[question.question]: checked,
				};
				if (!checked) {
					otherText = {
						...otherText,
						[question.question]: "",
					};
				}
			} else {
				otherSelected = {
					...otherSelected,
					[question.question]: true,
				};
				answers = {
					...answers,
					[question.question]: "",
				};
			}
			return;
		}

		if (question.multiSelect) {
			const current = selectedLabels(question.question);
			const next = checked
				? [...current, optionLabel]
				: current.filter((label) => label !== optionLabel);

			answers = {
				...answers,
				[question.question]: next.join(", "),
			};
			return;
		}

		otherSelected = {
			...otherSelected,
			[question.question]: false,
		};
		otherText = {
			...otherText,
			[question.question]: "",
		};
		answers = {
			...answers,
			[question.question]: optionLabel,
		};

		scheduleAutoAdvance();
	}

	function handleOtherTextChange(questionKey: string, text: string) {
		otherText = {
			...otherText,
			[questionKey]: text,
		};
	}

	function buildFinalAnswers(): Record<string, string> {
		const finalAnswers: Record<string, string> = {};

		for (const question of pendingQuestion.questions) {
			if (otherSelected[question.question]) {
				if (question.multiSelect) {
					const regular = answers[question.question] || "";
					const other = otherText[question.question]?.trim() || "";
					const parts = [regular, other].filter(Boolean);
					finalAnswers[question.question] = parts.join(", ");
				} else {
					finalAnswers[question.question] =
						otherText[question.question]?.trim() || "";
				}
			} else {
				finalAnswers[question.question] = answers[question.question] || "";
			}
		}

		return finalAnswers;
	}

	async function handleSubmit() {
		if (!allAnswered() || isSubmitting) {
			return;
		}

		isSubmitting = true;
		try {
			await onSubmit(pendingQuestion.toolUseID, buildFinalAnswers());
		} finally {
			isSubmitting = false;
		}
	}

	function handleContinue() {
		if (!currentIsAnswered()) {
			return;
		}

		const next = findNextUnanswered(currentStep);
		if (next !== null) {
			currentStep = next;
			return;
		}

		if (!isLastStep()) {
			currentStep += 1;
		}
	}
</script>

<div class="space-y-4">
	<div>
		<h3 class="font-semibold text-base">Agent needs input</h3>
		<p class="text-muted-foreground text-sm">
			Answer to help the agent continue with your task.
		</p>
	</div>

	{#if notes}
		<div
			class="relative max-h-64 overflow-y-auto rounded-md border bg-muted/30 p-3 text-sm"
		>
			<Button
				class="absolute right-1 top-1 h-6 w-6"
				onclick={() => {
					contextExpanded = true;
				}}
				size="icon"
				variant="ghost"
			>
				<Maximize2Icon class="h-3 w-3" />
			</Button>
			<MessageResponse text={notes} />
		</div>

		<Dialog.Root bind:open={contextExpanded}>
			<Dialog.Content
				class="sm:max-w-4xl max-h-[85vh] flex flex-col overflow-hidden"
			>
				<Dialog.Header>
					<Dialog.Title>Plan</Dialog.Title>
				</Dialog.Header>
				<div class="flex-1 overflow-y-auto text-sm">
					<MessageResponse text={notes} />
				</div>
			</Dialog.Content>
		</Dialog.Root>
	{/if}

	{#if pendingQuestion.questions.length > 1}
		<div class="flex gap-1">
			{#each pendingQuestion.questions as question, index (question.header ?? question.question)}
				{@const answered = isStepAnswered(index)}
				{@const active = index === currentStep}
				<button
					class={cn(
						"flex items-center gap-1.5 rounded-md border px-3 py-1.5 text-xs font-medium transition-colors",
						active
							? "border-primary/30 bg-primary/10 text-primary"
							: "border-transparent bg-muted/50 text-muted-foreground hover:bg-muted hover:text-foreground",
					)}
					onclick={() => {
						currentStep = index;
					}}
					type="button"
				>
					{#if answered}
						<CheckIcon class="size-3.5 shrink-0 text-primary" />
					{:else}
						<span
							class={cn(
								"flex size-3.5 shrink-0 items-center justify-center rounded-full border text-[10px] font-semibold",
								active
									? "border-primary text-primary"
									: "border-muted-foreground/40 text-muted-foreground/60",
							)}
						>
							{index + 1}
						</span>
					{/if}
					{question.header ?? `Question ${index + 1}`}
				</button>
			{/each}
		</div>
	{/if}

	{#if currentQuestion}
		<div class="flex flex-col gap-3">
			<p class="font-medium text-sm">{currentQuestion.question}</p>
			<div class="flex flex-col gap-1.5">
				{#each currentQuestion.options as option (option.label)}
					{@const isSelected =
						!otherSelected[currentQuestion.question] &&
						selectedLabels(currentQuestion.question).includes(option.label)}
					<label
						class={cn(
							"flex cursor-pointer items-start gap-3 rounded-md border px-3 py-2.5 transition-colors",
							isSelected
								? "border-primary bg-primary/5"
								: "border-border hover:bg-muted/50",
							isSubmitting && "cursor-not-allowed opacity-60",
						)}
					>
						<input
							checked={isSelected}
							disabled={isSubmitting}
							name={`question-${currentQuestion.question}`}
							onchange={(event) => {
								const checked = (event.currentTarget as HTMLInputElement)
									.checked;
								handleOptionChange(currentQuestion, option.label, checked);
							}}
							class="mt-0.5 shrink-0 accent-primary"
							type={currentQuestion.multiSelect ? "checkbox" : "radio"}
							value={option.label}
						/>
						<div class="flex flex-1 flex-col gap-0.5">
							<span class="text-sm font-medium leading-tight"
								>{option.label}</span
							>
							{#if option.description}
								<span class="text-muted-foreground text-xs"
									>{option.description}</span
								>
							{/if}
						</div>
					</label>
				{/each}

				<label
					class={cn(
						"flex cursor-pointer items-start gap-3 rounded-md border px-3 py-2.5 transition-colors",
						otherSelected[currentQuestion.question]
							? "border-primary bg-primary/5"
							: "border-border hover:bg-muted/50",
						isSubmitting && "cursor-not-allowed opacity-60",
					)}
				>
					<input
						checked={otherSelected[currentQuestion.question] ?? false}
						disabled={isSubmitting}
						name={`question-${currentQuestion.question}`}
						onchange={(event) => {
							const checked = (event.currentTarget as HTMLInputElement).checked;
							handleOptionChange(currentQuestion, OTHER_LABEL, checked);
						}}
						class="mt-0.5 shrink-0 accent-primary"
						type={currentQuestion.multiSelect ? "checkbox" : "radio"}
						value={OTHER_LABEL}
					/>
					<div class="flex flex-1 flex-col gap-1.5">
						<span class="text-sm font-medium leading-tight">Other</span>
						{#if otherSelected[currentQuestion.question]}
							<textarea
								class="w-full resize-none overflow-hidden rounded-md border border-input bg-background px-2.5 py-1.5 text-sm outline-none placeholder:text-muted-foreground focus:border-primary focus:ring-1 focus:ring-primary/30"
								disabled={isSubmitting}
								onclick={(event) => {
									event.stopPropagation();
								}}
								oninput={(event) => {
									const target = event.currentTarget as HTMLTextAreaElement;
									target.style.height = "auto";
									target.style.height = `${target.scrollHeight}px`;
									handleOtherTextChange(currentQuestion.question, target.value);
								}}
								onkeydown={(event) => {
									event.stopPropagation();
								}}
								placeholder="Type your answer..."
								rows={1}
								value={otherText[currentQuestion.question] ?? ""}
							></textarea>
						{/if}
					</div>
				</label>
			</div>
		</div>
	{/if}

	<div class="flex justify-between pt-2">
		<div>
			{#if currentStep > 0}
				<Button
					disabled={isSubmitting}
					onclick={() => {
						currentStep -= 1;
					}}
					variant="ghost"
				>
					Back
				</Button>
			{/if}
		</div>
		<div class="flex gap-2">
			{#if isLastStep() || allAnswered()}
				<Button
					disabled={!allAnswered() || isSubmitting}
					onclick={handleSubmit}
				>
					{isSubmitting ? "Submitting..." : "Submit"}
				</Button>
			{:else}
				<Button
					disabled={!currentIsAnswered() || isSubmitting}
					onclick={handleContinue}
				>
					Continue
				</Button>
			{/if}
		</div>
	</div>
</div>
