export type QueryPrimitive = string | number | boolean | null | undefined;
export type QueryKeyPart = QueryPrimitive | QueryKeyPart[] | { [key: string]: QueryKeyPart };
export type QueryKey = readonly QueryKeyPart[];

export type QueryStatus = "idle" | "pending" | "success" | "error";

export type QueryFunctionContext<TKey extends QueryKey = QueryKey> = {
	queryKey: TKey;
	signal: AbortSignal;
};

export type QueryFunction<TData, TKey extends QueryKey = QueryKey> = (
	context: QueryFunctionContext<TKey>,
) => Promise<TData>;

export type QueryOptions<TData, TKey extends QueryKey = QueryKey> = {
	initialData?: TData;
	queryClient?: QueryClient;
	queryFn: QueryFunction<TData, TKey>;
	queryKey: TKey;
	refetchOnVisibility?: boolean;
	refetchOnWindowFocus?: boolean;
	retry?: number;
	staleTime?: number;
};

export type ResolvedQueryOptions<TData, TKey extends QueryKey = QueryKey> = Required<
	Pick<
		QueryOptions<TData, TKey>,
		"queryFn" | "queryKey" | "retry" | "staleTime" | "refetchOnWindowFocus" | "refetchOnVisibility"
	>
> &
	Pick<QueryOptions<TData, TKey>, "initialData" | "queryClient">;

export type QueryResult<TData> = {
	data: TData | undefined;
	error: Error | null;
	isError: boolean;
	isPending: boolean;
	isSuccess: boolean;
	refetch: () => Promise<TData>;
};

export type MutationOptions<TData, TVariables> = {
	mutationFn: (variables: TVariables) => Promise<TData>;
	onError?: (error: unknown, variables: TVariables) => void | Promise<void>;
	onSettled?: (
		data: TData | null,
		error: unknown,
		variables: TVariables,
	) => void | Promise<void>;
	onSuccess?: (data: TData, variables: TVariables) => void | Promise<void>;
};

export type MutationResult<TData, TVariables> = {
	error: Error | null;
	isPending: boolean;
	mutateAsync: (variables: TVariables) => Promise<TData>;
};

export type QueryRecord<TData = unknown> = {
	data: TData | undefined;
	error: Error | null;
	status: QueryStatus;
	updatedAt: number;
};

export type QueryFilters = {
	predicate?: (query: QueryRecord & { queryKey: QueryKey }) => boolean;
	queryKey?: QueryKey;
};

export type QueryClientConfig = {
	defaultOptions?: {
		queries?: Pick<
			QueryOptions<unknown>,
			"refetchOnVisibility" | "refetchOnWindowFocus" | "retry" | "staleTime"
		>;
	};
};

export type QueryClient = import("./query-client").QueryClient;
