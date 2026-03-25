type Deferred<T> = {
	promise: Promise<T>;
	resolve: (value: T | PromiseLike<T>) => void;
	reject: (reason?: unknown) => void;
};

type RequestState<TResult> = {
	current: Promise<TResult> | null;
	next: Deferred<TResult> | null;
	nextTask: (() => Promise<TResult>) | null;
};

function createDeferred<T>(): Deferred<T> {
	let resolve!: Deferred<T>["resolve"];
	let reject!: Deferred<T>["reject"];
	const promise = new Promise<T>((nextResolve, nextReject) => {
		resolve = nextResolve;
		reject = nextReject;
	});
	return { promise, resolve, reject };
}

export class RequestCoalescer<TKey, TResult = void> {
	#states = new Map<TKey, RequestState<TResult>>();

	run(key: TKey, task: () => Promise<TResult>): Promise<TResult> {
		const state = this.#getState(key);
		if (state.current === null) {
			const current = this.#start(key, state, task);
			state.current = current;
			return current;
		}

		if (state.next === null) {
			state.next = createDeferred<TResult>();
		}
		state.nextTask = task;
		return state.next.promise;
	}

	#getState(key: TKey): RequestState<TResult> {
		const existing = this.#states.get(key);
		if (existing) {
			return existing;
		}

		const created: RequestState<TResult> = {
			current: null,
			next: null,
			nextTask: null,
		};
		this.#states.set(key, created);
		return created;
	}

	#start(
		key: TKey,
		state: RequestState<TResult>,
		task: () => Promise<TResult>,
	): Promise<TResult> {
		const current = (async () => task())();
		void current.finally(() => {
			if (state.current !== current) {
				return;
			}

			const queued = state.next;
			const queuedTask = state.nextTask;
			if (queued && queuedTask) {
				state.next = null;
				state.nextTask = null;
				const next = this.#start(key, state, queuedTask);
				state.current = next;
				void next.then(queued.resolve, queued.reject);
				return;
			}

			state.current = null;
			state.next = null;
			state.nextTask = null;
			this.#states.delete(key);
		});
		return current;
	}
}
