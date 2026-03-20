import assert from "node:assert/strict";
import test from "node:test";

import { createUserMessage } from "../domains/session-domain.helpers";

test("createUserMessage leaves provisional unset by default", () => {
	const message = createUserMessage("hello");

	assert.equal(message.role, "user");
	assert.deepEqual(message.parts, [{ type: "text", text: "hello" }]);
	assert.equal(message.provisional, undefined);
});

test("createUserMessage can mark a message as provisional", () => {
	const message = createUserMessage("hello", { provisional: true });

	assert.equal(message.role, "user");
	assert.deepEqual(message.parts, [{ type: "text", text: "hello" }]);
	assert.equal(message.provisional, true);
});
