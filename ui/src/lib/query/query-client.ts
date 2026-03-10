import { QueryClient } from "@tanstack/svelte-query";

let queryClient: QueryClient | null = null;

export function getQueryClient(): QueryClient {
	if (queryClient) {
		return queryClient;
	}

	queryClient = new QueryClient({
		defaultOptions: {
			queries: {
				refetchOnWindowFocus: false,
				retry: 1,
			},
		},
	});

	return queryClient;
}
