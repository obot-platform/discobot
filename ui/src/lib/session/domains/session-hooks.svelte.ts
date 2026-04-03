import { api } from "$lib/api-client";
import type { HooksStatusResponse } from "$lib/api-types";
import {
	mergeHookOutput,
	toHooksStatus,
} from "$lib/session/domains/session-domain.helpers";
import type {
	HookOutputState,
	SessionHooksService,
} from "$lib/session/session-context.types";

type CreateSessionHooksDomainArgs = {
	sessionId: string;
	hasSession: () => boolean;
};

export function createSessionHooksDomain(
	args: CreateSessionHooksDomainArgs,
): SessionHooksService & { refresh: () => Promise<void> } {
	let outputById = $state<Record<string, HookOutputState>>({});
	let hooksData = $state<HooksStatusResponse | null>(null);
	let rerunningHookIds = $state<string[]>([]);

	const status = $derived.by(() => {
		const baseStatus = toHooksStatus(hooksData);
		if (rerunningHookIds.length === 0) {
			return baseStatus;
		}

		return {
			...baseStatus,
			pendingHookIds: baseStatus.pendingHookIds.filter(
				(hookId) => !rerunningHookIds.includes(hookId),
			),
			hooks: baseStatus.hooks.map((hook) => {
				if (!rerunningHookIds.includes(hook.hookId)) {
					return hook;
				}
				return {
					...hook,
					lastResult: "running" as const,
				};
			}),
		};
	});

	async function loadOutputs(nextStatus: HooksStatusResponse | null) {
		if (!nextStatus) {
			outputById = {};
			return;
		}

		const outputs = await Promise.all(
			Object.keys(nextStatus.hooks).map(async (hookId) => {
				const response = await api.getHookOutput(args.sessionId, hookId);
				return [hookId, response] as const;
			}),
		);

		outputById = outputs.reduce<Record<string, HookOutputState>>(
			(nextOutputById, [hookId, response]) => {
				return mergeHookOutput(nextOutputById, hookId, response);
			},
			{},
		);
	}

	async function refresh() {
		if (!args.hasSession()) {
			hooksData = null;
			outputById = {};
			rerunningHookIds = [];
			return;
		}
		const nextStatus = await api.getHooksStatus(args.sessionId);
		hooksData = nextStatus;
		await loadOutputs(nextStatus);
	}

	async function applyStatusUpdate(nextStatus: HooksStatusResponse) {
		if (!args.hasSession()) {
			return;
		}
		hooksData = nextStatus;
		await loadOutputs(nextStatus);
	}

	return {
		get status() {
			return status;
		},
		get outputById() {
			return outputById;
		},
		rerun: (hookId: string) => {
			if (!args.hasSession() || rerunningHookIds.includes(hookId)) {
				return;
			}
			rerunningHookIds = [...rerunningHookIds, hookId];
			void (async () => {
				try {
					await api.rerunHook(args.sessionId, hookId);
				} finally {
					rerunningHookIds = rerunningHookIds.filter((id) => id !== hookId);
					await refresh();
				}
			})();
		},
		refresh,
		applyStatusUpdate,
	};
}
