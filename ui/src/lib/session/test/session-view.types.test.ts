import assert from "node:assert/strict";
import test from "node:test";

import {
	getDefaultActiveView,
	getSelectedFileFromView,
	getSelectedServiceIdFromView,
} from "../session-view.types";

test("getDefaultActiveView prefers the first file when present", () => {
		assert.deepEqual(getDefaultActiveView(["src/app.ts"]), {
			kind: "file",
			path: "src/app.ts",
		});
});

test("getDefaultActiveView falls back to chat without files", () => {
		assert.deepEqual(getDefaultActiveView([]), { kind: "chat" });
});

test("view helpers expose selected file and service ids", () => {
		assert.equal(getSelectedFileFromView({ kind: "file", path: "src/app.ts" }), "src/app.ts");
		assert.equal(getSelectedFileFromView({ kind: "chat" }), "");
	assert.equal(getSelectedServiceIdFromView({ kind: "services" }), null);
		assert.equal(getSelectedServiceIdFromView({ kind: "terminal" }), null);
});
