import assert from "node:assert/strict";
import { after, before, describe, test } from "node:test";

import { QueryClient } from "../query-client";

// Helpers

function defer<T>() {
	let resolve!: (value: T) => void;
	let reject!: (reason: unknown) => void;
	const promise = new Promise<T>((res, rej) => {
		resolve = res;
		reject = rej;
	});
	return { promise, resolve, reject };
}

function makeFn<T>(value: T, delay = 0): () => Promise<T> {
	return () =>
		delay > 0
			? new Promise((res) => setTimeout(() => res(value), delay))
			: Promise.resolve(value);
}

// ---- getQueryData / setQueryData ----

describe("setQueryData", () => {
	test("stores a value and returns it", () => {
		const client = new QueryClient();
		const result = client.setQueryData<string[]>(["items"], ["a", "b"]);
		assert.deepEqual(result, ["a", "b"]);
	});

	test("getQueryData retrieves the stored value", () => {
		const client = new QueryClient();
		client.setQueryData(["items"], ["a", "b"]);
		assert.deepEqual(client.getQueryData(["items"]), ["a", "b"]);
	});

	test("getQueryData returns undefined for unknown keys", () => {
		const client = new QueryClient();
		assert.equal(client.getQueryData(["missing"]), undefined);
	});

	test("updater function receives the previous value", () => {
		const client = new QueryClient();
		client.setQueryData<number[]>(["nums"], [1, 2]);
		const result = client.setQueryData<number[]>(["nums"], (prev) => [...(prev ?? []), 3]);
		assert.deepEqual(result, [1, 2, 3]);
	});

	test("updater function receives undefined for a new key", () => {
		const client = new QueryClient();
		const result = client.setQueryData<string[]>(["new"], (prev) => [...(prev ?? []), "first"]);
		assert.deepEqual(result, ["first"]);
	});

	test("notifies subscribers when data changes", () => {
		const client = new QueryClient();
		const entry = client.getOrCreate(["items"]);
		let notifyCount = 0;
		client.subscribe(entry, () => notifyCount++);

		client.setQueryData(["items"], "hello");
		assert.equal(notifyCount, 1);
	});
});

// ---- fetchEntry / fetchQuery ----

describe("fetchQuery", () => {
	test("fetches and caches the result", async () => {
		const client = new QueryClient();
		const result = await client.fetchQuery({
			queryKey: ["users"],
			queryFn: makeFn([{ id: 1 }]),
		});
		assert.deepEqual(result, [{ id: 1 }]);
		assert.deepEqual(client.getQueryData(["users"]), [{ id: 1 }]);
	});

	test("re-fetches on second call when staleTime is 0 (default)", async () => {
		let calls = 0;
		const client = new QueryClient();
		await client.fetchQuery({
			queryKey: ["x"],
			queryFn: async () => {
				calls++;
				return calls;
			},
		});
		const second = await client.fetchQuery({ queryKey: ["x"], queryFn: async () => ++calls });
		assert.equal(second, 2);
		assert.equal(calls, 2);
	});

	test("deduplicates concurrent in-flight fetches", async () => {
		let calls = 0;
		const { promise, resolve } = defer<string>();
		const client = new QueryClient();
		const opts = {
			queryKey: ["y"] as const,
			queryFn: () => {
				calls++;
				return promise;
			},
		};

		const p1 = client.fetchQuery(opts);
		const p2 = client.fetchQuery(opts);
		resolve("done");
		const [r1, r2] = await Promise.all([p1, p2]);

		assert.equal(r1, "done");
		assert.equal(r2, "done");
		assert.equal(calls, 1);
	});

	test("sets status to success after fetch", async () => {
		const client = new QueryClient();
		await client.fetchQuery({ queryKey: ["k"], queryFn: makeFn("value") });
		const entry = client.getOrCreate(["k"]);
		assert.equal(entry.status, "success");
	});

	test("sets status to error and rethrows on failure", async () => {
		const client = new QueryClient();
		await assert.rejects(
			() =>
				client.fetchQuery({
					queryKey: ["fail"],
					queryFn: async () => {
						throw new Error("boom");
					},
					retry: 0,
				}),
			/boom/,
		);
		const entry = client.getOrCreate(["fail"]);
		assert.equal(entry.status, "error");
		assert.equal(entry.error?.message, "boom");
	});

	test("retries the specified number of times before failing", async () => {
		let attempts = 0;
		const client = new QueryClient();
		await assert.rejects(() =>
			client.fetchQuery({
				queryKey: ["retry"],
				queryFn: async () => {
					attempts++;
					throw new Error("fail");
				},
				retry: 2,
			}),
		);
		assert.equal(attempts, 3); // 1 initial + 2 retries
	});
});

// ---- staleTime / SWR ----

describe("staleTime", () => {
	test("returns cached data without re-fetching when within staleTime", async () => {
		let calls = 0;
		const client = new QueryClient();
		const opts = {
			queryKey: ["stale"] as const,
			queryFn: async () => {
				calls++;
				return "data";
			},
			staleTime: 60_000,
			retry: 0,
		};

		await client.fetchQuery(opts);
		await client.fetchQuery(opts);

		assert.equal(calls, 1);
	});

	test("refetches when staleTime has elapsed", async () => {
		let calls = 0;
		const client = new QueryClient();
		await client.fetchQuery({
			queryKey: ["stale2"],
			queryFn: async () => {
				calls++;
				return "v" + calls;
			},
			staleTime: 0,
			retry: 0,
		});

		// Force updatedAt to appear old
		const entry = client.getOrCreate(["stale2"]);
		entry.updatedAt = Date.now() - 100;

		await client.fetchQuery({
			queryKey: ["stale2"],
			queryFn: async () => {
				calls++;
				return "v" + calls;
			},
			staleTime: 0,
			retry: 0,
		});

		assert.equal(calls, 2);
	});
});

// ---- invalidateQueries ----

describe("invalidateQueries", () => {
	test("marks matching entries as invalidated", async () => {
		const client = new QueryClient();
		await client.fetchQuery({ queryKey: ["sessions", "a"], queryFn: makeFn("a"), retry: 0 });
		await client.fetchQuery({ queryKey: ["sessions", "b"], queryFn: makeFn("b"), retry: 0 });

		await client.invalidateQueries({ queryKey: ["sessions"] });

		const a = client.getOrCreate(["sessions", "a"]);
		const b = client.getOrCreate(["sessions", "b"]);
		// Invalidated flag is cleared after a refetch, but data was refreshed
		assert.equal(a.status, "success");
		assert.equal(b.status, "success");
	});

	test("notifies subscribers on invalidation", async () => {
		const client = new QueryClient();
		const entry = client.getOrCreate(["notified"]);
		let count = 0;
		client.subscribe(entry, () => count++);

		await client.invalidateQueries({ queryKey: ["notified"] });
		assert.ok(count > 0);
	});

	test("invalidates all entries when no filter is provided", async () => {
		const client = new QueryClient();
		await client.fetchQuery({ queryKey: ["a"], queryFn: makeFn(1), retry: 0 });
		await client.fetchQuery({ queryKey: ["b"], queryFn: makeFn(2), retry: 0 });

		// Patch both entries to look stale so we can observe refetch
		let refetchCount = 0;
		const patchFn = async () => {
			refetchCount++;
			return "new";
		};
		const ea = client.getOrCreate(["a"]);
		const eb = client.getOrCreate(["b"]);
		// Set active count so invalidation triggers refetch
		ea.activeCount = 1;
		ea.options = { queryKey: ["a"], queryFn: patchFn, staleTime: 0, retry: 0, refetchOnWindowFocus: false, refetchOnVisibility: false };
		eb.activeCount = 1;
		eb.options = { queryKey: ["b"], queryFn: patchFn, staleTime: 0, retry: 0, refetchOnWindowFocus: false, refetchOnVisibility: false };

		await client.invalidateQueries();
		assert.equal(refetchCount, 2);
	});

	test("predicate filter applies correctly", async () => {
		const client = new QueryClient();
		await client.fetchQuery({ queryKey: ["sessions", "x"], queryFn: makeFn("x"), retry: 0 });
		await client.fetchQuery({ queryKey: ["workspaces", "y"], queryFn: makeFn("y"), retry: 0 });

		let invalidatedCount = 0;
		const ex = client.getOrCreate(["sessions", "x"]);
		client.subscribe(ex, () => invalidatedCount++);

		await client.invalidateQueries({
			predicate: (q) => Array.isArray(q.queryKey) && q.queryKey[0] === "sessions",
		});

		assert.ok(invalidatedCount > 0);
	});
});

// ---- removeQueries ----

describe("removeQueries", () => {
	test("removes matching entries from the cache", () => {
		const client = new QueryClient();
		client.setQueryData(["sessions", "a"], "a");
		client.setQueryData(["sessions", "b"], "b");
		client.setQueryData(["workspaces"], "ws");

		client.removeQueries({ queryKey: ["sessions"] });

		assert.equal(client.getQueryData(["sessions", "a"]), undefined);
		assert.equal(client.getQueryData(["sessions", "b"]), undefined);
		assert.equal(client.getQueryData(["workspaces"]), "ws");
	});

	test("notifies subscribers of removed entries", () => {
		const client = new QueryClient();
		const entry = client.getOrCreate(["target"]);
		let notified = false;
		client.subscribe(entry, () => (notified = true));

		client.removeQueries({ queryKey: ["target"] });
		assert.ok(notified);
	});
});

// ---- cancelQueries ----

describe("cancelQueries", () => {
	test("aborts an in-flight fetch and clears fetchStatus", async () => {
		const { promise, resolve } = defer<string>();
		const client = new QueryClient();

		// Start a long-running fetch in the background
		const fetchPromise = client
			.fetchQuery({
				queryKey: ["slow"],
				queryFn: ({ signal }) =>
					new Promise<string>((res, rej) => {
						signal.addEventListener("abort", () => rej(new DOMException("Aborted", "AbortError")));
						promise.then(res);
					}),
				retry: 0,
			})
			.catch(() => {});

		const entry = client.getOrCreate(["slow"]);
		assert.equal(entry.fetchStatus, "fetching");

		await client.cancelQueries({ queryKey: ["slow"] });
		assert.equal(entry.fetchStatus, "idle");

		resolve("too late");
		await fetchPromise;
	});

	test("resets status from pending to idle and notifies subscribers", async () => {
		const { promise, resolve } = defer<string>();
		const client = new QueryClient();

		const fetchPromise = client
			.fetchQuery({
				queryKey: ["pending-cancel"],
				queryFn: ({ signal }) =>
					new Promise<string>((res, rej) => {
						signal.addEventListener("abort", () => rej(new DOMException("Aborted", "AbortError")));
						promise.then(res);
					}),
				retry: 0,
			})
			.catch(() => {});

		const entry = client.getOrCreate(["pending-cancel"]);
		assert.equal(entry.status, "pending");

		let notified = false;
		client.subscribe(entry, () => (notified = true));

		await client.cancelQueries({ queryKey: ["pending-cancel"] });

		assert.equal(entry.status, "idle");
		assert.ok(notified);

		resolve("too late");
		await fetchPromise;
	});
});

// ---- subscribe / unsubscribe ----

describe("subscribe / unsubscribe", () => {
	test("subscriber is called on setQueryData", () => {
		const client = new QueryClient();
		const entry = client.getOrCreate(["k"]);
		let calls = 0;
		client.subscribe(entry, () => calls++);
		client.setQueryData(["k"], 1);
		client.setQueryData(["k"], 2);
		assert.equal(calls, 2);
	});

	test("subscriber is not called after unsubscribe", () => {
		const client = new QueryClient();
		const entry = client.getOrCreate(["k"]);
		let calls = 0;
		const sub = () => calls++;
		client.subscribe(entry, sub);
		client.setQueryData(["k"], 1);
		client.unsubscribe(entry, sub);
		client.setQueryData(["k"], 2);
		assert.equal(calls, 1);
	});

	test("activeCount increments on subscribe and decrements on unsubscribe", () => {
		const client = new QueryClient();
		const entry = client.getOrCreate(["k"]);
		const sub = () => {};
		client.subscribe(entry, sub);
		assert.equal(entry.activeCount, 1);
		client.unsubscribe(entry, sub);
		assert.equal(entry.activeCount, 0);
	});

	test("activeCount never goes below zero", () => {
		const client = new QueryClient();
		const entry = client.getOrCreate(["k"]);
		client.unsubscribe(entry, () => {});
		assert.equal(entry.activeCount, 0);
	});
});

// ---- revalidateOnFocus / revalidateOnVisibility ----

describe("revalidateOnFocus", () => {
	test("triggers a background refetch for active entries with refetchOnWindowFocus", async () => {
		let calls = 0;
		const client = new QueryClient();
		const entry = client.getOrCreate(["focused"]);
		entry.status = "success";
		entry.data = "old";
		entry.updatedAt = Date.now();
		entry.activeCount = 1;
		entry.options = {
			queryKey: ["focused"],
			queryFn: async () => {
				calls++;
				return "new";
			},
			staleTime: 0,
			retry: 0,
			refetchOnWindowFocus: true,
			refetchOnVisibility: false,
		};

		client.revalidateOnFocus();
		// Wait for the background fetch to complete
		await new Promise((res) => setTimeout(res, 20));
		assert.equal(calls, 1);
		assert.equal(client.getQueryData(["focused"]), "new");
	});

	test("does not refetch when refetchOnWindowFocus is false", async () => {
		let calls = 0;
		const client = new QueryClient();
		const entry = client.getOrCreate(["unfocused"]);
		entry.status = "success";
		entry.activeCount = 1;
		entry.options = {
			queryKey: ["unfocused"],
			queryFn: async () => {
				calls++;
				return "v";
			},
			staleTime: 0,
			retry: 0,
			refetchOnWindowFocus: false,
			refetchOnVisibility: false,
		};

		client.revalidateOnFocus();
		await new Promise((res) => setTimeout(res, 20));
		assert.equal(calls, 0);
	});
});

describe("revalidateOnVisibility", () => {
	test("triggers a background refetch for active entries with refetchOnVisibility", async () => {
		let calls = 0;
		const client = new QueryClient();
		const entry = client.getOrCreate(["visible"]);
		entry.status = "success";
		entry.data = "old";
		entry.updatedAt = Date.now();
		entry.activeCount = 1;
		entry.options = {
			queryKey: ["visible"],
			queryFn: async () => {
				calls++;
				return "new";
			},
			staleTime: 0,
			retry: 0,
			refetchOnWindowFocus: false,
			refetchOnVisibility: true,
		};

		client.revalidateOnVisibility();
		await new Promise((res) => setTimeout(res, 20));
		assert.equal(calls, 1);
	});
});

// ---- defaultOptions ----

describe("defaultOptions", () => {
	test("applies default staleTime to all queries", async () => {
		let calls = 0;
		const client = new QueryClient({ defaultOptions: { queries: { staleTime: 60_000, retry: 0 } } });
		const fn = async () => {
			calls++;
			return calls;
		};
		await client.fetchQuery({ queryKey: ["d"], queryFn: fn });
		await client.fetchQuery({ queryKey: ["d"], queryFn: fn });
		assert.equal(calls, 1);
	});

	test("resolveOptions applies defaultOptions from the client", () => {
		const client = new QueryClient({
			defaultOptions: { queries: { staleTime: 30_000, retry: 3, refetchOnWindowFocus: true, refetchOnVisibility: true } },
		});
		const resolved = client.resolveOptions({
			queryKey: ["r"],
			queryFn: async () => "x",
		});
		assert.equal(resolved.staleTime, 30_000);
		assert.equal(resolved.retry, 3);
		assert.equal(resolved.refetchOnWindowFocus, true);
		assert.equal(resolved.refetchOnVisibility, true);
	});

	test("resolveOptions lets per-query options override defaultOptions", () => {
		const client = new QueryClient({
			defaultOptions: { queries: { staleTime: 30_000, retry: 3 } },
		});
		const resolved = client.resolveOptions({
			queryKey: ["r"],
			queryFn: async () => "x",
			staleTime: 0,
			retry: 0,
		});
		assert.equal(resolved.staleTime, 0);
		assert.equal(resolved.retry, 0);
	});
});
