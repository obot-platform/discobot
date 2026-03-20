import { api } from "$lib/api-client";
import type { AppSupportInfo } from "$lib/app/app-context.types";
import type { SupportInfoResponse } from "$lib/api-types";

export function createAppSupportInfoDomain(): AppSupportInfo {
	let data = $state<SupportInfoResponse | null>(null);
	let status = $state<"idle" | "loading" | "ready" | "error">("idle");
	let error = $state<string | null>(null);

	return {
		get data() {
			return data;
		},
		get status() {
			return status;
		},
		get error() {
			return error;
		},
		fetch: async () => {
			status = "loading";
			error = null;
			try {
				data = await api.getSupportInfo();
				status = "ready";
			} catch (err) {
				error = err instanceof Error ? err.message : "Failed to load support info";
				status = "error";
			}
		},
	};
}
