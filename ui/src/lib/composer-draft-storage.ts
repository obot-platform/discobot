import { writeStorage } from "./app/app-helpers";

const COMPOSER_DRAFT_STORAGE_PREFIX = "discobot:composer-draft:";
export const PENDING_COMPOSER_DRAFT_STORAGE_KEY = `${COMPOSER_DRAFT_STORAGE_PREFIX}pending`;

export function resolveComposerDraftStorageKey({
	isPending,
	threadId,
}: {
	isPending: boolean;
	threadId: string | null | undefined;
}): string {
	if (isPending || !threadId) {
		return PENDING_COMPOSER_DRAFT_STORAGE_KEY;
	}

	return `${COMPOSER_DRAFT_STORAGE_PREFIX}${threadId}`;
}

export function readComposerDraft(storageKey: string): string {
	if (typeof window === "undefined") {
		return "";
	}

	return window.localStorage.getItem(storageKey) ?? "";
}

export function writeComposerDraft(storageKey: string, value: string): void {
	writeStorage(storageKey, value || null);
}

export function clearComposerDraft(storageKey: string): void {
	writeStorage(storageKey, null);
}
