import { getContext, setContext } from "svelte";
import type {
	AttachmentData,
	AttachmentMediaCategory,
	AttachmentVariant,
} from "$lib/components/ai/types";

const ATTACHMENTS_CONTEXT_KEY = Symbol.for("discobot-ui-ai-attachments-context");
const ATTACHMENT_CONTEXT_KEY = Symbol.for("discobot-ui-ai-attachment-context");

export type AttachmentsContextValue = {
	variant: AttachmentVariant;
};

export type AttachmentContextValue = {
	data: AttachmentData;
	mediaCategory: AttachmentMediaCategory;
	onRemove?: () => void;
	variant: AttachmentVariant;
};

export function setAttachmentsContext(
	value: AttachmentsContextValue,
): AttachmentsContextValue {
	return setContext(ATTACHMENTS_CONTEXT_KEY, value);
}

export function useAttachmentsContext(): AttachmentsContextValue {
	return (
		getContext<AttachmentsContextValue | undefined>(ATTACHMENTS_CONTEXT_KEY) ?? {
			variant: "grid",
		}
	);
}

export function setAttachmentContext(
	value: AttachmentContextValue,
): AttachmentContextValue {
	return setContext(ATTACHMENT_CONTEXT_KEY, value);
}

export function useAttachmentContext(): AttachmentContextValue {
	const context = getContext<AttachmentContextValue | undefined>(
		ATTACHMENT_CONTEXT_KEY,
	);
	if (!context) {
		throw new Error("Attachment components must be used within <Attachment>");
	}
	return context;
}
