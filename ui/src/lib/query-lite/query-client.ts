import { isPartialQueryKeyMatch, stableSerializeQueryKey } from "./query-key";
import { registerFocusClient } from "./query-focus";
import type {
	QueryClientConfig,
	QueryFilters,
	QueryKey,
	QueryOptions,
	QueryRecord,
	ResolvedQueryOptions,
} from "./query-types";

type StoredQueryOptions = ResolvedQueryOptions<any, any>;

type InternalQueryEntry = {
	abortController: AbortController | null;
	activeCount: number;
	data: unknown;
	error: Error | null;
	fetchId: number;
	fetchPromise: Promise<unknown> | null;
	fetchStatus: "idle" | "fetching";
	invalidated: boolean;
	options: StoredQueryOptions | null;
	queryKey: QueryKey;
	serializedKey: string;
	status: import("./query-types").QueryStatus;
	updatedAt: number;
};

type InternalFetchOptions = {
	background?: boolean;
	force?: boolean;
};

type CacheEntry = InternalQueryEntry & {
	subscribers: Set<() => void>;
};

const DEFAULT_STALE_TIME = 0;
const DEFAULT_RETRY = 1;
const DEFAULT_REFETCH_ON_WINDOW_FOCUS = false;
const DEFAULT_REFETCH_ON_VISIBILITY = false;

function applyDefaults<TData, TKey extends QueryKey>(
	options: QueryOptions<TData, TKey>,
	defaults: QueryClientConfig["defaultOptions"],
): ResolvedQueryOptions<TData, TKey> {
	const q = defaults?.queries;
	return {
		queryKey: options.queryKey,
		queryFn: options.queryFn,
		staleTime: options.staleTime ?? q?.staleTime ?? DEFAULT_STALE_TIME,
		retry: options.retry ?? q?.retry ?? DEFAULT_RETRY,
		refetchOnWindowFocus:
			options.refetchOnWindowFocus ?? q?.refetchOnWindowFocus ?? DEFAULT_REFETCH_ON_WINDOW_FOCUS,
		refetchOnVisibility:
			options.refetchOnVisibility ?? q?.refetchOnVisibility ?? DEFAULT_REFETCH_ON_VISIBILITY,
		initialData: options.initialData,
		queryClient: options.queryClient,
	};
}

export class QueryClient {
	private readonly cache = new Map<string, CacheEntry>();
	private readonly config: QueryClientConfig;
	private readonly unregisterFocus: () => void;

	constructor(config?: QueryClientConfig) {
		this.config = config ?? {};
		this.unregisterFocus = registerFocusClient(this);
	}

	destroy(): void {
		this.unregisterFocus();
	}

	private createEntry(queryKey: QueryKey): CacheEntry {
		return {
			abortController: null,
			activeCount: 0,
			data: undefined,
			error: null,
			fetchId: 0,
			fetchPromise: null,
			fetchStatus: "idle",
			invalidated: false,
			options: null,
			queryKey,
			serializedKey: stableSerializeQueryKey(queryKey),
			status: "idle",
			subscribers: new Set(),
			updatedAt: 0,
		};
	}

	getOrCreate(queryKey: QueryKey): CacheEntry {
		const key = stableSerializeQueryKey(queryKey);
		let entry = this.cache.get(key);
		if (!entry) {
			entry = this.createEntry(queryKey);
			this.cache.set(key, entry);
		}
		return entry;
	}

	private notify(entry: CacheEntry): void {
		for (const subscriber of entry.subscribers) {
			subscriber();
		}
	}

	async fetchEntry<TData, TKey extends QueryKey>(
		entry: CacheEntry,
		options: ResolvedQueryOptions<TData, TKey>,
		opts?: InternalFetchOptions,
	): Promise<TData> {
		const force = opts?.force ?? false;
		const background = opts?.background ?? false;

		// Deduplicate in-flight fetches
		if (entry.fetchStatus === "fetching" && entry.fetchPromise) {
			return entry.fetchPromise as Promise<TData>;
		}

		// Return non-stale cached data when not forcing
		if (!force && !entry.invalidated && entry.status === "success" && entry.updatedAt > 0) {
			const age = Date.now() - entry.updatedAt;
			if (age < options.staleTime) {
				return entry.data as TData;
			}
		}

		// Cancel any previous in-flight request
		if (entry.abortController) {
			entry.abortController.abort();
		}

		const fetchId = entry.fetchId + 1;
		const controller = new AbortController();
		entry.fetchId = fetchId;
		entry.abortController = controller;
		entry.fetchStatus = "fetching";

		// Show pending state only when there's no existing data to show
		if (!background && entry.status !== "success") {
			entry.status = "pending";
			this.notify(entry);
		}

		let attempt = 0;

		const attemptFetch = async (): Promise<TData> => {
			try {
				const result = await options.queryFn({
					queryKey: options.queryKey,
					signal: controller.signal,
				});

				// If a newer fetch has superseded this one, resolve silently
				if (entry.fetchId !== fetchId) {
					return result;
				}

				entry.data = result;
				entry.error = null;
				entry.status = "success";
				entry.updatedAt = Date.now();
				entry.invalidated = false;
				entry.fetchStatus = "idle";
				entry.fetchPromise = null;
				entry.abortController = null;
				this.notify(entry);
				return result;
			} catch (err) {
				if (controller.signal.aborted) {
					throw err;
				}
				if (attempt < options.retry) {
					attempt += 1;
					return attemptFetch();
				}
				if (entry.fetchId !== fetchId) {
					throw err;
				}
				entry.error = err instanceof Error ? err : new Error(String(err));
				entry.status = "error";
				entry.fetchStatus = "idle";
				entry.fetchPromise = null;
				entry.abortController = null;
				this.notify(entry);
				throw err;
			}
		};

		const promise = attemptFetch();
		entry.fetchPromise = promise;
		return promise;
	}

	subscribe(entry: CacheEntry, subscriber: () => void): void {
		entry.subscribers.add(subscriber);
		entry.activeCount += 1;
	}

	unsubscribe(entry: CacheEntry, subscriber: () => void): void {
		entry.subscribers.delete(subscriber);
		entry.activeCount = Math.max(0, entry.activeCount - 1);
	}

	getQueryData<TData>(queryKey: QueryKey): TData | undefined {
		const key = stableSerializeQueryKey(queryKey);
		const entry = this.cache.get(key);
		return entry?.data as TData | undefined;
	}

	setQueryData<TData>(
		queryKey: QueryKey,
		updater: TData | ((previous: TData | undefined) => TData),
	): TData {
		const entry = this.getOrCreate(queryKey);
		const previous = entry.data as TData | undefined;
		const next =
			typeof updater === "function"
				? (updater as (prev: TData | undefined) => TData)(previous)
				: updater;
		entry.data = next;
		entry.status = "success";
		entry.updatedAt = Date.now();
		entry.error = null;
		this.notify(entry);
		return next;
	}

	async fetchQuery<TData, TKey extends QueryKey>(options: QueryOptions<TData, TKey>): Promise<TData> {
		const resolved = this.resolveOptions(options);
		const entry = this.getOrCreate(resolved.queryKey);
		entry.options = resolved;
		return this.fetchEntry(entry, resolved);
	}

	resolveOptions<TData, TKey extends QueryKey>(
		options: QueryOptions<TData, TKey>,
	): ResolvedQueryOptions<TData, TKey> {
		return applyDefaults(options, this.config.defaultOptions);
	}

	private matchEntries(filters?: QueryFilters): CacheEntry[] {
		const entries = Array.from(this.cache.values());
		if (!filters) return entries;

		return entries.filter((entry) => {
			if (filters.queryKey) {
				if (!isPartialQueryKeyMatch(entry.queryKey, filters.queryKey)) return false;
			}
			if (filters.predicate) {
				const record: QueryRecord & { queryKey: QueryKey } = {
					queryKey: entry.queryKey,
					data: entry.data,
					error: entry.error,
					status: entry.status,
					updatedAt: entry.updatedAt,
				};
				return filters.predicate(record);
			}
			return true;
		});
	}

	async invalidateQueries(filters?: QueryFilters): Promise<void> {
		const entries = this.matchEntries(filters);
		const refetches: Promise<unknown>[] = [];

		for (const entry of entries) {
			entry.invalidated = true;
			this.notify(entry);

			if (entry.activeCount > 0 && entry.options) {
				refetches.push(
					this.fetchEntry(entry, entry.options, {
						force: true,
						background: true,
					}).catch(() => {}),
				);
			}
		}

		await Promise.all(refetches);
	}

	removeQueries(filters?: QueryFilters): void {
		const entries = this.matchEntries(filters);
		for (const entry of entries) {
			entry.abortController?.abort();
			this.cache.delete(entry.serializedKey);
			this.notify(entry);
		}
	}

	async cancelQueries(filters?: QueryFilters): Promise<void> {
		const entries = this.matchEntries(filters);
		const waits: Promise<void>[] = [];

		for (const entry of entries) {
			if (entry.abortController) {
				entry.abortController.abort();
			}
			if (entry.fetchPromise) {
				waits.push(entry.fetchPromise.then(() => {}, () => {}));
			}
			entry.fetchStatus = "idle";
			entry.fetchPromise = null;
			entry.abortController = null;
			if (entry.status === "pending") {
				entry.status = "idle";
			}
			this.notify(entry);
		}

		await Promise.all(waits);
	}

	revalidateOnFocus(): void {
		for (const entry of this.cache.values()) {
			if (
				entry.activeCount > 0 &&
				entry.options?.refetchOnWindowFocus &&
				entry.fetchStatus === "idle"
			) {
				void this.fetchEntry(entry, entry.options, { force: true, background: true }).catch(
					() => {},
				);
			}
		}
	}

	revalidateOnVisibility(): void {
		for (const entry of this.cache.values()) {
			if (
				entry.activeCount > 0 &&
				entry.options?.refetchOnVisibility &&
				entry.fetchStatus === "idle"
			) {
				void this.fetchEntry(entry, entry.options, { force: true, background: true }).catch(
					() => {},
				);
			}
		}
	}
}

let defaultClient: QueryClient | null = null;

export function getQueryClient(): QueryClient {
	if (!defaultClient) {
		defaultClient = new QueryClient();
	}
	return defaultClient;
}
