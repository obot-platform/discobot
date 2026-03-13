import type { QueryKey, QueryKeyPart } from "./query-types";

function isPlainObject(value: unknown): value is Record<string, unknown> {
	return typeof value === "object" && value !== null && !Array.isArray(value);
}

function normalizeValue(value: QueryKeyPart): QueryKeyPart {
	if (Array.isArray(value)) {
		return value.map((item) => normalizeValue(item)) as QueryKeyPart[];
	}
	if (isPlainObject(value)) {
		return Object.keys(value)
			.sort()
			.reduce<Record<string, QueryKeyPart>>((result, key) => {
				result[key] = normalizeValue(value[key] as QueryKeyPart);
				return result;
			}, {});
	}
	return value;
}

export function normalizeQueryKey(queryKey: QueryKey): QueryKey {
	return queryKey.map((part) => normalizeValue(part)) as QueryKey;
}

export function stableSerializeQueryKey(queryKey: QueryKey): string {
	return JSON.stringify(normalizeQueryKey(queryKey));
}

export function isEqualQueryKey(left: QueryKey, right: QueryKey): boolean {
	return stableSerializeQueryKey(left) === stableSerializeQueryKey(right);
}

export function isPartialQueryKeyMatch(candidate: QueryKey, prefix: QueryKey): boolean {
	if (prefix.length > candidate.length) {
		return false;
	}
	for (let index = 0; index < prefix.length; index += 1) {
		const left = stableSerializeQueryKey([candidate[index] as QueryKeyPart]);
		const right = stableSerializeQueryKey([prefix[index] as QueryKeyPart]);
		if (left !== right) {
			return false;
		}
	}
	return true;
}
