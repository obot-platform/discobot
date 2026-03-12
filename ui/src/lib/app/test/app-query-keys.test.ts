import assert from "node:assert/strict";
import test from "node:test";

import { appQueryKeys } from "../query/app-query-keys";

test("app query keys are stable and scoped", () => {
	assert.deepEqual(appQueryKeys.all(), ["app"]);
	assert.deepEqual(appQueryKeys.workspaces(), ["app", "workspaces"]);
	assert.deepEqual(appQueryKeys.models(), ["app", "models"]);
	assert.deepEqual(appQueryKeys.agents(), ["app", "agents"]);
	assert.deepEqual(appQueryKeys.supportInfo(), ["app", "support-info"]);
	assert.deepEqual(appQueryKeys.sessions(), ["app", "sessions"]);
});
