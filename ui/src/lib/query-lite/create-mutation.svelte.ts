import type { MutationOptions, MutationResult } from "./query-types";

export function createMutation<TData, TVariables>(
	getOptions: () => MutationOptions<TData, TVariables>,
): MutationResult<TData, TVariables> {
	let isPending = $state(false);
	let error = $state<Error | null>(null);

	return {
		get isPending() {
			return isPending;
		},
		get error() {
			return error;
		},
		async mutateAsync(variables: TVariables): Promise<TData> {
			const options = getOptions();
			isPending = true;
			error = null;
			try {
				const data = await options.mutationFn(variables);
				await options.onSuccess?.(data, variables);
				await options.onSettled?.(data, null, variables);
				return data;
			} catch (err) {
				const caught = err instanceof Error ? err : new Error(String(err));
				error = caught;
				await options.onError?.(err, variables);
				await options.onSettled?.(null, err, variables);
				throw caught;
			} finally {
				isPending = false;
			}
		},
	};
}
