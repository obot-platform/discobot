import assert from "node:assert/strict";
import test from "node:test";

import type { Workspace } from "$lib/api-types";

import { upsertWorkspace } from "../domains/app-workspaces.helpers";

test("upsertWorkspace replaces an existing workspace in place", () => {
	const existingWorkspaces: Workspace[] = [
		{
			id: "workspace-1",
			path: "/tmp/one",
			sourceType: "local",
			status: "ready",
		},
		{
			id: "workspace-2",
			path: "/tmp/two",
			sourceType: "git",
			status: "cloning",
		},
	];

	const nextWorkspace: Workspace = {
		...existingWorkspaces[1],
		status: "error",
		errorMessage: "failed",
	};

	assert.deepEqual(upsertWorkspace(existingWorkspaces, nextWorkspace), [
		existingWorkspaces[0],
		nextWorkspace,
	]);
});

test("upsertWorkspace appends a newly seen workspace", () => {
	const existingWorkspaces: Workspace[] = [
		{
			id: "workspace-1",
			path: "/tmp/one",
			sourceType: "local",
			status: "ready",
		},
	];

	const nextWorkspace: Workspace = {
		id: "workspace-2",
		path: "/tmp/two",
		sourceType: "git",
		status: "cloning",
	};

	assert.deepEqual(upsertWorkspace(existingWorkspaces, nextWorkspace), [
		existingWorkspaces[0],
		nextWorkspace,
	]);
});
