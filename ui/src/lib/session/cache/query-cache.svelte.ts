import type { QueryClient, QueryKey } from "@tanstack/svelte-query";

export const SESSION_QUERY_SCOPE = "session" as const;

export type SessionQueryKey = readonly [
	typeof SESSION_QUERY_SCOPE,
	sessionId: string,
	domain: string,
	...parts: string[],
];

export type SessionQueryCache = {
	key: (domain: string, ...parts: string[]) => SessionQueryKey;
	getData: <T>(key: QueryKey) => T | undefined;
	setData: <T>(key: QueryKey, updater: T | ((previous: T | undefined) => T)) => void;
	invalidateAll: () => Promise<void>;
	invalidateDomain: (domain: string) => Promise<void>;
	removeAll: () => void;
	cancelAll: () => Promise<void>;
};

function isSessionKeyFor(sessionId: string, key: QueryKey): boolean {
	return (
		Array.isArray(key) &&
		key.length >= 3 &&
		key[0] === SESSION_QUERY_SCOPE &&
		key[1] === sessionId
	);
}

export function createSessionQueryCache(
	queryClient: QueryClient,
	sessionId: string,
): SessionQueryCache {
	const key = (domain: string, ...parts: string[]): SessionQueryKey => {
		return [SESSION_QUERY_SCOPE, sessionId, domain, ...parts];
	};

	const getData = <T>(queryKey: QueryKey): T | undefined => {
		return queryClient.getQueryData<T>(queryKey);
	};

	const setData = <T>(
		queryKey: QueryKey,
		updater: T | ((previous: T | undefined) => T),
	) => {
		queryClient.setQueryData<T>(queryKey, updater);
	};

	const invalidateAll = async () => {
		await queryClient.invalidateQueries({
			predicate: (query) => isSessionKeyFor(sessionId, query.queryKey),
		});
	};

	const invalidateDomain = async (domain: string) => {
		await queryClient.invalidateQueries({
			predicate: (query) => {
				const queryKey = query.queryKey;
				return isSessionKeyFor(sessionId, queryKey) && queryKey[2] === domain;
			},
		});
	};

	const removeAll = () => {
		queryClient.removeQueries({
			predicate: (query) => isSessionKeyFor(sessionId, query.queryKey),
		});
	};

	const cancelAll = async () => {
		await queryClient.cancelQueries({
			predicate: (query) => isSessionKeyFor(sessionId, query.queryKey),
		});
	};

	return {
		key,
		getData,
		setData,
		invalidateAll,
		invalidateDomain,
		removeAll,
		cancelAll,
	};
}
