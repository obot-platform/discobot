import { createQueryLocalState } from "./query-local-state.svelte";
import { getQueryClient, type QueryClient } from "./query-client";
import type { QueryKey, QueryOptions, QueryResult } from "./query-types";

export function createQuery<TData, TKey extends QueryKey = QueryKey>(
	getOptions: () => QueryOptions<TData, TKey>,
): QueryResult<TData> {
	// Resolve client once at construction time (the client is expected to be stable)
	const initialOpts = getOptions();
	const client = (initialOpts.queryClient as QueryClient | undefined) ?? getQueryClient();

	const local = createQueryLocalState<TData>(initialOpts.initialData);

	$effect(() => {
		const resolved = client.resolveOptions(getOptions());
		const entry = client.getOrCreate(resolved.queryKey);

		// Store latest options on the entry so focus/visibility revalidation can use them
		entry.options = resolved;

		// Sync local reactive state from the current cache entry
		local.data = entry.data as TData | undefined;
		local.error = entry.error;
		local.status = entry.status;

		// Subscriber: called whenever the cache entry changes
		const subscriber = () => {
			local.data = entry.data as TData | undefined;
			local.error = entry.error;
			local.status = entry.status;
		};

		client.subscribe(entry, subscriber);

		// Trigger an initial fetch; use background mode if we already have data
		void client
			.fetchEntry(entry, resolved, { background: entry.status === "success" })
			.catch(() => {});

		return () => {
			client.unsubscribe(entry, subscriber);
		};
	});

	return {
		get data() {
			return local.data;
		},
		get error() {
			return local.error;
		},
		get isPending() {
			return local.status === "pending";
		},
		get isError() {
			return local.status === "error";
		},
		get isSuccess() {
			return local.status === "success";
		},
		async refetch(): Promise<TData> {
			const resolved = client.resolveOptions(getOptions());
			const entry = client.getOrCreate(resolved.queryKey);
			return client.fetchEntry(entry, resolved, { force: true }) as Promise<TData>;
		},
	};
}
