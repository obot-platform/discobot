import type { DynamicToolPart } from "$lib/components/ai/types";

export type ToolRendererComponentProps = {
	toolPart: DynamicToolPart;
	sessionId?: string | null;
	threadId?: string | null;
};
