import assert from "node:assert";
import { afterEach, describe, test } from "node:test";
import { cleanup, fireEvent, render, waitFor } from "@testing-library/react";
import {
	PromptInput,
	PromptInputProvider,
	PromptInputSubmit,
	PromptInputTextarea,
} from "./prompt-input";

describe("PromptInput empty-submit behavior", () => {
	afterEach(() => {
		cleanup();
	});

	test("ignores Enter when the textarea is blank", async () => {
		let submitCount = 0;
		let createSessionCount = 0;

		const { container } = render(
			<PromptInput
				onSubmit={() => {
					submitCount += 1;
				}}
			>
				<PromptInputTextarea />
				<PromptInputSubmit
					onCreateSession={() => {
						createSessionCount += 1;
					}}
				/>
			</PromptInput>,
		);

		const textarea = container.querySelector("textarea");
		assert.ok(textarea);

		fireEvent.keyDown(textarea, { key: "Enter" });
		await new Promise((resolve) => setTimeout(resolve, 0));

		assert.strictEqual(submitCount, 0);
		assert.strictEqual(createSessionCount, 0);
	});

	test("creates an empty session when the submit button is clicked with blank input", () => {
		let submitCount = 0;
		let createSessionCount = 0;

		const { container } = render(
			<PromptInput
				onSubmit={() => {
					submitCount += 1;
				}}
			>
				<PromptInputTextarea />
				<PromptInputSubmit
					onCreateSession={() => {
						createSessionCount += 1;
					}}
				/>
			</PromptInput>,
		);

		const button = container.querySelector('button[type="submit"]');
		assert.ok(button);

		fireEvent.click(button);

		assert.strictEqual(submitCount, 0);
		assert.strictEqual(createSessionCount, 1);
	});

	test("still submits on Enter when the textarea has content", async () => {
		let submittedText = "";

		const { container } = render(
			<PromptInputProvider>
				<PromptInput
					onSubmit={(message) => {
						submittedText = message.text;
					}}
				>
					<PromptInputTextarea />
					<PromptInputSubmit />
				</PromptInput>
			</PromptInputProvider>,
		);

		const textarea = container.querySelector("textarea");
		assert.ok(textarea);

		fireEvent.change(textarea, { target: { value: "hello" } });
		fireEvent.keyDown(textarea, { key: "Enter" });

		await waitFor(() => {
			assert.strictEqual(submittedText, "hello");
		});
	});
});
