export { createMutation } from "./create-mutation.svelte";
export { createQuery } from "./create-query.svelte";
export { QueryClient, getQueryClient } from "./query-client";
export type {
	MutationOptions,
	MutationResult,
	QueryClientConfig,
	QueryFilters,
	QueryFunction,
	QueryFunctionContext,
	QueryKey,
	QueryOptions,
	QueryResult,
	QueryStatus,
} from "./query-types";

import type { QueryKey, QueryOptions } from "./query-types";

/** Identity helper that adds type inference to query option objects — mirrors TanStack's `queryOptions`. */
export function queryOptions<TData, TKey extends QueryKey = QueryKey>(
	options: QueryOptions<TData, TKey>,
): QueryOptions<TData, TKey> {
	return options;
}
