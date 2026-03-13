import type { QueryStatus } from "./query-types";

export type QueryLocalState<TData> = {
	data: TData | undefined;
	error: Error | null;
	status: QueryStatus;
};

export function createQueryLocalState<TData>(initialData?: TData): QueryLocalState<TData> {
	let data = $state<TData | undefined>(initialData);
	let error = $state<Error | null>(null);
	let status = $state<QueryStatus>(initialData !== undefined ? "success" : "idle");

	return {
		get data() {
			return data;
		},
		set data(value) {
			data = value;
		},
		get error() {
			return error;
		},
		set error(value) {
			error = value;
		},
		get status() {
			return status;
		},
		set status(value) {
			status = value;
		},
	};
}
