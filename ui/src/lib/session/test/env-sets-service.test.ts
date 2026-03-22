import assert from "node:assert/strict";
import test from "node:test";

import { getActiveEnvSets } from "../domains/session-domain.helpers";
import type { EnvSetWithVars } from "../../shell-types";

const envSets: EnvSetWithVars[] = [
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

test("getActiveEnvSets preserves the selected order", () => {
	assert.deepEqual(
		getActiveEnvSets(envSets, ["env-2", "env-1"]).map((envSet) => envSet.id),
		["env-2", "env-1"],
	);
});

test("getActiveEnvSets filters ids that are no longer present", () => {
	assert.deepEqual(
		getActiveEnvSets(envSets, ["env-2", "missing"]).map((envSet) => envSet.id),
		["env-2"],
	);
});

test("getActiveEnvSets returns an empty list when nothing is active", () => {
	assert.deepEqual(getActiveEnvSets(envSets, []), []);
});
