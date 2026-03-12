import type { StartChatRequest } from "$lib/api-types";
import type { ComposerMode } from "$lib/components/ide/conversation-composer.types";

type BuildEmptySessionStartChatRequestArgs = {
	sessionId: string;
	workspaceId: string | null;
	mode: ComposerMode;
	modelId: string | null;
	reasoning: boolean;
};

function normalizeModelId(modelId: string | null): string | undefined {
	if (!modelId) {
		return undefined;
	}

	return modelId.endsWith(":thinking") ? modelId.slice(0, -":thinking".length) : modelId;
}

export function shouldSubmitComposerOnEnter(draft: string): boolean {
	return draft.trim().length > 0;
}

export function buildEmptySessionStartChatRequest(
	args: BuildEmptySessionStartChatRequestArgs,
): StartChatRequest {
	const model = normalizeModelId(args.modelId);

	return {
		sessionId: args.sessionId,
		messages: [],
		...(args.workspaceId ? { workspaceId: args.workspaceId } : {}),
		...(model ? { model } : {}),
		reasoning: args.reasoning ? "enabled" : "disabled",
		mode: args.mode === "plan" ? "plan" : "",
	};
}
