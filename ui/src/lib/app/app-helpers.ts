import type { Session } from "$lib/api-types";
import { getApiBase } from "$lib/api-config";
import {
	getDesktopRuntimeKind,
	isDesktopShell,
	supportsAppUpdates,
	supportsNativeWindowControls,
} from "$lib/shell";
import { type SessionSummary, type WindowControlsSide } from "$lib/shell-types";

export function detectWindowControlsSide(): WindowControlsSide {
	if (typeof navigator === "undefined") {
		return "right";
	}

	const nav = navigator as Navigator & {
		userAgentData?: {
			platform?: string;
		};
	};
	const platform = nav.userAgentData?.platform || nav.platform || nav.userAgent;
	return /mac/i.test(platform) ? "left" : "right";
}

export function getCurrentTimestamp(): string {
	return new Date().toISOString();
}

export async function delay(ms: number) {
	await new Promise((resolve) => window.setTimeout(resolve, ms));
}

export function compareIsoDatesDesc(left: string, right: string) {
	const leftTime = new Date(left).getTime();
	const rightTime = new Date(right).getTime();
	if (Number.isNaN(leftTime) || Number.isNaN(rightTime)) {
		return 0;
	}
	return rightTime - leftTime;
}

export function toSessionSummaries(sessions: Session[]): SessionSummary[] {
	return [...sessions]
		.sort((a, b) => compareIsoDatesDesc(a.createdAt, b.createdAt))
		.map((session) => ({
			id: session.id,
			name: session.displayName || session.name,
			status: session.status,
			isRecent: false,
			workspaceId: session.workspaceId,
		}));
}

export function getAppEnvironment() {
	const runtime = getDesktopRuntimeKind();
	const isDesktop = isDesktopShell();
	return {
		apiBase: getApiBase(),
		runtime,
		isDesktop,
		supportsNativeWindowControls: supportsNativeWindowControls(),
		supportsAppUpdates: supportsAppUpdates(),
		windowControlsSide: detectWindowControlsSide(),
	};
}
