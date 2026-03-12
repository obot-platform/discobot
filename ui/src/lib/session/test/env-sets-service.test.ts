import assert from "node:assert/strict";
import test from "node:test";

import {
	createSessionEnvSetsService,
	createThreadEnvSetsService,
} from "../services/env-sets-service";
import type { EnvSetWithVars, SessionData } from "../../shell-types";

function makeSession(activeEnvSetIds: string[] = []): SessionData {
	return {
		id: "session-1",
		name: "Session",
		description: "",
		timestamp: "2026-03-11T00:00:00.000Z",
		status: "ready",
		files: [],
		baseBranch: "main",
		baseCommit: "abcdef0",
		references: {
			issueReference: "",
			pullRequestReference: "",
		},
		threads: [{ id: "thread-1", name: "Thread 1" }],
		activeEnvSetIds,
		editorFiles: ["src/app.ts"],
		fileContents: {
			"src/app.ts": "export const ok = true;",
		},
		services: [],
	};
}

test("session env set service removes deleted env sets from every session", () => {
		let envSets: EnvSetWithVars[] = [
			{
				id: "env-1",
				projectId: "local",
				name: "Core",
				createdAt: "2026-03-11T00:00:00.000Z",
				updatedAt: "2026-03-11T00:00:00.000Z",
				envVars: { TOKEN: "abc" },
			},
			{
				id: "env-2",
				projectId: "local",
				name: "UI",
				createdAt: "2026-03-11T00:00:00.000Z",
				updatedAt: "2026-03-11T00:00:00.000Z",
				envVars: { COLOR: "blue" },
			},
		];
		let sessionDataById: Record<string, SessionData> = {
			"session-1": makeSession(["env-1", "env-2"]),
			"session-2": { ...makeSession(["env-2"]), id: "session-2" },
		};

		const service = createSessionEnvSetsService({
			getSessionDataById: () => sessionDataById,
			setSessionDataById: (value) => {
				sessionDataById = value;
			},
			getList: () => envSets,
			setList: (value) => {
				envSets = value;
			},
			createEnvSetId: () => "env-new",
			nowIsoString: () => "2026-03-11T00:00:00.000Z",
		});

		service.remove("env-2");

		assert.deepEqual(envSets.map((envSet) => envSet.id), ["env-1"]);
		assert.deepEqual(sessionDataById["session-1"].activeEnvSetIds, ["env-1"]);
		assert.deepEqual(sessionDataById["session-2"].activeEnvSetIds, []);
});

test("thread env set service toggles active ids for the selected session", () => {
		const envSets: EnvSetWithVars[] = [
			{
				id: "env-1",
				projectId: "local",
				name: "Core",
				createdAt: "2026-03-11T00:00:00.000Z",
				updatedAt: "2026-03-11T00:00:00.000Z",
				envVars: { TOKEN: "abc" },
			},
		];
		let sessionDataById: Record<string, SessionData> = {
			"session-1": makeSession([]),
		};

		const service = createThreadEnvSetsService({
			getSessionId: () => "session-1",
			getSessionDataById: () => sessionDataById,
			setSessionDataById: (value) => {
				sessionDataById = value;
			},
			getList: () => envSets,
		});

		service.toggle("env-1");
		assert.deepEqual(sessionDataById["session-1"].activeEnvSetIds, ["env-1"]);
		assert.deepEqual(service.active.map((envSet) => envSet.id), ["env-1"]);

		service.toggle("env-1");
		assert.deepEqual(sessionDataById["session-1"].activeEnvSetIds, []);
		assert.deepEqual(service.active, []);
});
