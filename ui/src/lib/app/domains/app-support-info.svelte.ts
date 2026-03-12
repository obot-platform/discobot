import { createQuery, queryOptions } from "@tanstack/svelte-query";

import { api } from "$lib/api-client";
import type { AppSupportInfo } from "$lib/app/app-context.types";
import { appQueryKeys } from "$lib/app/query/app-query-keys";
import type { SupportInfoResponse } from "$lib/api-types";

function supportInfoQueryOptions() {
	return queryOptions({
		queryKey: appQueryKeys.supportInfo(),
		queryFn: async (): Promise<SupportInfoResponse> => {
			return api.getSupportInfo();
		},
		enabled: false,
	});
}

export function createAppSupportInfoDomain(): AppSupportInfo {
	const supportInfoQuery = createQuery(() => supportInfoQueryOptions());
	const status = $derived.by(() => {
		if (supportInfoQuery.isPending) {
			return "loading" as const;
		}
		if (supportInfoQuery.isError) {
			return "error" as const;
		}
		if (supportInfoQuery.isSuccess) {
			return "ready" as const;
		}
		return "idle" as const;
	});
	const error = $derived.by(() =>
		supportInfoQuery.error instanceof Error ? supportInfoQuery.error.message : null,
	);

	return {
		get data() {
			return supportInfoQuery.data ?? null;
		},
		get status() {
			return status;
		},
		get error() {
			return error;
		},
		fetch: async () => {
			await supportInfoQuery.refetch();
		},
	};
}
