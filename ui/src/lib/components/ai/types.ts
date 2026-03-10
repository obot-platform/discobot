export type ToolState =
	| "input-streaming"
	| "input-available"
	| "approval-requested"
	| "approval-responded"
	| "output-available"
	| "output-error"
	| "output-denied";

export type ToolApproval =
	| {
			id: string;
			approved?: never;
			reason?: never;
	  }
	| {
			id: string;
			approved: boolean;
			reason?: string;
	  }
	| undefined;

export type LanguageModelUsage = {
	inputTokens?: number;
	outputTokens?: number;
	reasoningTokens?: number;
	cachedInputTokens?: number;
};

export type AttachmentMediaCategory =
	| "image"
	| "video"
	| "audio"
	| "document"
	| "source"
	| "unknown";

export type AttachmentVariant = "grid" | "inline" | "list";

export type AttachmentFileData = {
	id: string;
	type: "file";
	filename?: string;
	mediaType?: string;
	url?: string;
};

export type AttachmentSourceDocumentData = {
	id: string;
	type: "source-document";
	title?: string;
	filename?: string;
	mediaType?: string;
	url?: string;
};

export type AttachmentData = AttachmentFileData | AttachmentSourceDocumentData;

export type MessageRole = "system" | "user" | "assistant" | "tool";

export type DynamicToolPart = {
	type: "dynamic-tool";
	toolCallId: string;
	toolName: string;
	state: ToolState;
	input: unknown;
	approval?: ToolApproval;
	output?: unknown;
	errorText?: string;
	title?: string;
};
