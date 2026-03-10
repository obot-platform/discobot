import { z } from "zod";
import { createValidator, type ToolSchema } from "./index";

/**
 * Task tool input schema (Zod)
 *
 * Launches a specialized agent for complex tasks
 */
export const TaskToolInputSchema = z.object({
	/** Type of specialized agent */
	subagent_type: z.string().optional(),
	/** The task for the agent to perform */
	prompt: z.string().optional(),
	/** Short description (3-5 words) */
	description: z.string().optional(),
	/** Optional model to use */
	model: z.enum(["sonnet", "opus", "haiku"]).optional(),
	/** Maximum number of turns */
	max_turns: z.number().optional(),
	/** Run agent in background */
	run_in_background: z.boolean().optional(),
	/** Agent ID to resume from */
	resume: z.string().optional(),
});

/**
 * Task tool input type (inferred from Zod schema)
 */
export type TaskToolInput = z.infer<typeof TaskToolInputSchema>;

/**
 * Task tool output schema (Zod)
 */
export const TaskToolOutputSchema = z.object({
	/** Agent ID for this task */
	agentId: z.string().optional(),
	/** Result from agent execution */
	result: z.string().optional(),
	/** Output file path (for background tasks) */
	output_file: z.string().optional(),
	/** Error message if task failed */
	error: z.string().optional(),
});

/**
 * Task tool output type (inferred from Zod schema)
 */
export type TaskToolOutput = z.infer<typeof TaskToolOutputSchema>;

/**
 * Validates Task tool input parameters using Zod
 */
export const validateTaskInput = createValidator(TaskToolInputSchema);

/**
 * Validates Task tool output using Zod
 */
export const validateTaskOutput = createValidator(
	z.union([TaskToolOutputSchema, z.object({}).transform(() => ({}))]),
);

/**
 * Task tool schema export
 */
export const TaskToolSchema: ToolSchema<TaskToolInput, TaskToolOutput> = {
	toolName: "Task",
	inputSchema: TaskToolInputSchema,
	outputSchema: TaskToolOutputSchema,
	validateInput: validateTaskInput,
	validateOutput: validateTaskOutput,
};
