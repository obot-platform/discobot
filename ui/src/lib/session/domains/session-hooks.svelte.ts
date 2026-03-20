import { api } from "$lib/api-client";
import type { HooksStatusResponse } from "$lib/api-types";
import { mergeHookOutput, toHooksStatus } from "$lib/session/domains/session-domain.helpers";
import type { SessionHooksService } from "$lib/session/session-context.types";

type CreateSessionHooksDomainArgs = {
	sessionId: string;
	hasSession: () => boolean;
};

export function createSessionHooksDomain(
	args: CreateSessionHooksDomainArgs,
): SessionHooksService & { refresh: () => Promise<void> } {
	let outputById = $state<Record<string, string>>({});
	let hooksData = $state<HooksStatusResponse | null>(null);

	const status = $derived(toHooksStatus(hooksData));

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

		outputById = outputs.reduce<Record<string, string>>((nextOutputById, [hookId, response]) => {
			return mergeHookOutput(nextOutputById, hookId, response);
		}, {});
	}

	return {
		get status() {
			return status;
		},
		get outputById() {
			return outputById;
		},
		rerun: (hookId: string) => {
			if (!args.hasSession()) {
				return;
			}
			void (async () => {
				await api.rerunHook(args.sessionId, hookId);
				const [nextStatus, nextOutput] = await Promise.all([
					api.getHooksStatus(args.sessionId),
					api.getHookOutput(args.sessionId, hookId),
				]);
				hooksData = nextStatus;
				outputById = mergeHookOutput(outputById, hookId, nextOutput);
			})();
		},
		refresh: async () => {
			if (!args.hasSession()) {
				hooksData = null;
				outputById = {};
				return;
			}
			const nextStatus = await api.getHooksStatus(args.sessionId);
			hooksData = nextStatus;
			await loadOutputs(nextStatus);
		},
	};
}
