import { z } from "zod";
import { createValidator, type ToolSchema } from "./index";

/**
 * Skill tool input schema (Zod)
 *
 * Executes a skill within the conversation
 */
export const SkillToolInputSchema = z.object({
	/** The skill name to execute */
	skill: z.string().optional(),
	/** Optional arguments for the skill */
	args: z.string().optional(),
});

/**
 * Skill tool input type (inferred from Zod schema)
 */
export type SkillToolInput = z.infer<typeof SkillToolInputSchema>;

/**
 * Skill tool output schema (Zod)
 */
export const SkillToolOutputSchema = z.object({
	/** Result from skill execution */
	result: z.string().optional(),
	/** Error message if skill failed */
	error: z.string().optional(),
});

/**
 * Skill tool output type (inferred from Zod schema)
 */
export type SkillToolOutput = z.infer<typeof SkillToolOutputSchema>;

/**
 * Validates Skill tool input parameters using Zod
 */
export const validateSkillInput = createValidator(SkillToolInputSchema);

/**
 * Validates Skill tool output using Zod
 */
export const validateSkillOutput = createValidator(
	z.union([SkillToolOutputSchema, z.object({}).transform(() => ({}))]),
);

/**
 * Skill tool schema export
 */
export const SkillToolSchema: ToolSchema<SkillToolInput, SkillToolOutput> = {
	toolName: "Skill",
	inputSchema: SkillToolInputSchema,
	outputSchema: SkillToolOutputSchema,
	validateInput: validateSkillInput,
	validateOutput: validateSkillOutput,
};
