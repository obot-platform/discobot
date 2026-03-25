import assert from "node:assert/strict";
import test from "node:test";

import { RequestCoalescer } from "./request-coalescer";

type Deferred<T> = {
	promise: Promise<T>;
	resolve: (value: T | PromiseLike<T>) => void;
	reject: (reason?: unknown) => void;
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

test("RequestCoalescer gives later callers the next run for the same key", async () => {
	const coalescer = new RequestCoalescer<string, string>();
	const first = createDeferred<string>();
	const second = createDeferred<string>();
	const started: string[] = [];
	let latestValue = "initial";
	let callCount = 0;

	const runTask = () => {
		callCount += 1;
		started.push(`run-${callCount}`);
		const current = callCount === 1 ? first.promise : second.promise;
		return current.then((value) => {
			latestValue = value;
			return value;
		});
	};

	const firstResult = coalescer.run("sessions", runTask);
	const secondResult = coalescer.run("sessions", runTask);
	const thirdResult = coalescer.run("sessions", runTask);

	assert.equal(callCount, 1);
	assert.notEqual(firstResult, secondResult);
	assert.strictEqual(secondResult, thirdResult);

	first.resolve("stale");
	assert.equal(await firstResult, "stale");
	assert.deepEqual(started, ["run-1", "run-2"]);
	assert.equal(latestValue, "stale");

	second.resolve("fresh");
	assert.equal(await secondResult, "fresh");
	assert.equal(await thirdResult, "fresh");
	assert.equal(callCount, 2);
	assert.equal(latestValue, "fresh");
});

test("RequestCoalescer dedupes queued callers into a single follow-up run", async () => {
	const coalescer = new RequestCoalescer<string, number>();
	const first = createDeferred<number>();
	const second = createDeferred<number>();
	let callCount = 0;

	const runTask = () => {
		callCount += 1;
		return callCount === 1 ? first.promise : second.promise;
	};

	const firstResult = coalescer.run("workspaces", runTask);
	const queuedResults = Array.from({ length: 5 }, () =>
		coalescer.run("workspaces", runTask),
	);

	assert.equal(callCount, 1);
	assert.ok(queuedResults.every((result) => result === queuedResults[0]));

	first.resolve(1);
	await firstResult;
	assert.equal(callCount, 2);

	second.resolve(2);
	assert.deepEqual(await Promise.all(queuedResults), [2, 2, 2, 2, 2]);
	assert.equal(callCount, 2);
});

test("RequestCoalescer keeps keys independent", async () => {
	const coalescer = new RequestCoalescer<string, string>();
	const sessionFirst = createDeferred<string>();
	const sessionSecond = createDeferred<string>();
	const workspaceFirst = createDeferred<string>();
	let sessionCalls = 0;
	let workspaceCalls = 0;

	const sessionTask = () => {
		sessionCalls += 1;
		return sessionCalls === 1 ? sessionFirst.promise : sessionSecond.promise;
	};
	const workspaceTask = () => {
		workspaceCalls += 1;
		return workspaceFirst.promise;
	};

	const firstSession = coalescer.run("session:1", sessionTask);
	const queuedSession = coalescer.run("session:1", sessionTask);
	const firstWorkspace = coalescer.run("workspace:1", workspaceTask);

	assert.equal(sessionCalls, 1);
	assert.equal(workspaceCalls, 1);

	sessionFirst.resolve("session-stale");
	workspaceFirst.resolve("workspace-fresh");
	assert.equal(await firstSession, "session-stale");
	assert.equal(await firstWorkspace, "workspace-fresh");
	assert.equal(sessionCalls, 2);
	assert.equal(workspaceCalls, 1);

	sessionSecond.resolve("session-fresh");
	assert.equal(await queuedSession, "session-fresh");
});
