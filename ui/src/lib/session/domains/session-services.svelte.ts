import { api } from "$lib/api-client";
import type { Service } from "$lib/api-types";
import { toServiceItem } from "$lib/session/domains/session-domain.helpers";
import type { SessionServicesDomain } from "$lib/session/session-context.types";

type CreateSessionServicesDomainArgs = {
	sessionId: string;
	hasSession: () => boolean;
	getActiveServiceId: () => string | null;
	openService: (serviceId: string) => void;
};

export function createSessionServicesDomain(
	args: CreateSessionServicesDomainArgs,
): SessionServicesDomain {
	let rawServices = $state<Service[]>([]);

	$effect(() => {
		if (
			typeof window === "undefined" ||
			!rawServices.some(
				(service) =>
					service.status === "starting" || service.status === "stopping",
			)
		) {
			return;
		}

		const timeout = window.setTimeout(() => {
			void refresh();
		}, 5000);

		return () => {
			window.clearTimeout(timeout);
		};
	});

	async function refresh() {
		if (!args.hasSession()) {
			rawServices = [];
			return;
		}
		const { services } = await api.getServices(args.sessionId);
		rawServices = services;
	}

	const list = $derived(rawServices.map(toServiceItem));
	const active = $derived(
		list.find((service) => service.id === args.getActiveServiceId()) ?? null,
	);

	return {
		get list() {
			return list;
		},
		get active() {
			return active;
		},
		open: args.openService,
		start: async (serviceId: string) => {
			if (!args.hasSession()) {
				return;
			}
			await api.startService(args.sessionId, serviceId);
			await refresh();
		},
		stop: async (serviceId: string) => {
			if (!args.hasSession()) {
				return;
			}
			try {
				await api.stopService(args.sessionId, serviceId);
			} finally {
				await refresh();
			}
		},
		refresh,
	};
}
