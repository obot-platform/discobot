import type { DynamicToolPart } from "$lib/components/ai/types";

export type ToolRendererComponentProps = {
	toolPart: DynamicToolPart;
	sessionId?: string | null;
	threadId?: string | null;
	onToolApprovalResponse?: (payload: {
		id: string;
		approved: boolean;
		reason?: string;
	}) => void;
	isRaw?: boolean;
	onToggleRaw?: () => void;
};
