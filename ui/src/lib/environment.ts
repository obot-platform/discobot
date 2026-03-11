import { getApiBase as getConfiguredApiBase, isTauri } from "$lib/api-config";

export function isTauriShell(): boolean {
	return isTauri();
}

export function getApiBase(): string {
	return getConfiguredApiBase();
}
