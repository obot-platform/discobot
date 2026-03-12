import assert from "node:assert";
import { describe, it } from "node:test";
import {
	buildCreateOnlyChatRequest,
	getHookResumeDecision,
	isChatStreamActive,
} from "./chat-panel";

describe("chat-panel hook resume logic", () => {
	it("treats submitted and streaming chats as active streams", () => {
		assert.strictEqual(isChatStreamActive("submitted"), true);
		assert.strictEqual(isChatStreamActive("streaming"), true);
		assert.strictEqual(isChatStreamActive("ready"), false);
		assert.strictEqual(isChatStreamActive("error"), false);
	});

	it("queues a reconnect when hooks change during an active stream", () => {
		const decision = getHookResumeDecision({
			previousLastEvaluatedAt: "2026-03-07T23:00:00.000Z",
			nextLastEvaluatedAt: "2026-03-07T23:00:02.000Z",
			chatStatus: "streaming",
			pendingResume: false,
		});

		assert.deepStrictEqual(decision, {
			nextPendingResume: true,
			shouldResume: false,
		});
	});

	it("reconnects once the queued hook-triggered resume becomes idle", () => {
		const decision = getHookResumeDecision({
			previousLastEvaluatedAt: "2026-03-07T23:00:02.000Z",
			nextLastEvaluatedAt: "2026-03-07T23:00:02.000Z",
			chatStatus: "ready",
			pendingResume: true,
		});

		assert.deepStrictEqual(decision, {
			nextPendingResume: true,
			shouldResume: true,
		});
	});

	it("reconnects immediately when hooks change while the chat is already idle", () => {
		const decision = getHookResumeDecision({
			previousLastEvaluatedAt: "2026-03-07T23:00:00.000Z",
			nextLastEvaluatedAt: "2026-03-07T23:00:02.000Z",
			chatStatus: "ready",
			pendingResume: false,
		});

		assert.deepStrictEqual(decision, {
			nextPendingResume: true,
			shouldResume: true,
		});
	});

	it("builds a create-only request for blank new sessions", () => {
		const request = buildCreateOnlyChatRequest({
			sessionId: "session-new",
			workspaceId: "ws-1",
			agentId: "agent-1",
			modelId: "claude-sonnet-4:thinking",
			mode: "build",
		});

		assert.deepStrictEqual(request, {
			sessionId: "session-new",
			messages: [],
			workspaceId: "ws-1",
			agentId: "agent-1",
			model: "claude-sonnet-4",
			reasoning: "enabled",
			mode: "",
		});
		assert.ok(!("text" in request));
	});
});
