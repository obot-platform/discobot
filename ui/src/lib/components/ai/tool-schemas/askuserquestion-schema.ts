import { z } from "zod";
import { createValidator, type ToolSchema } from "./index";

export const AskUserQuestionOptionSchema = z.object({
	label: z.string(),
	description: z.string().optional(),
	markdown: z.string().optional(),
});

export const AskUserQuestionItemSchema = z.object({
	header: z.string().optional(),
	question: z.string(),
	multiSelect: z.boolean().optional(),
	options: z.array(AskUserQuestionOptionSchema).default([]),
	notes: z.string().optional(),
});

export const AskUserQuestionToolInputSchema = z.object({
	questions: z.array(AskUserQuestionItemSchema).default([]),
	metadata: z.record(z.string(), z.unknown()).optional(),
});

export type AskUserQuestionToolInput = z.infer<typeof AskUserQuestionToolInputSchema>;

export const AskUserQuestionToolOutputSchema = z.union([
	z.string(),
	z.record(z.string(), z.unknown()),
	z.array(z.unknown()),
]);

export type AskUserQuestionToolOutput = z.infer<typeof AskUserQuestionToolOutputSchema>;

export const validateAskUserQuestionInput = createValidator(
	AskUserQuestionToolInputSchema,
);

export const validateAskUserQuestionOutput = createValidator(
	AskUserQuestionToolOutputSchema,
);

export const AskUserQuestionToolSchema: ToolSchema<
	AskUserQuestionToolInput,
	AskUserQuestionToolOutput
> = {
	toolName: "AskUserQuestion",
	inputSchema: AskUserQuestionToolInputSchema,
	outputSchema: AskUserQuestionToolOutputSchema,
	validateInput: validateAskUserQuestionInput,
	validateOutput: validateAskUserQuestionOutput,
};
