import assert from "node:assert/strict";
import test from "node:test";

import { canOpenAttachmentFullscreen } from "../ai/attachments/utils";

test("canOpenAttachmentFullscreen accepts image file attachments with a URL", () => {
	assert.equal(
		canOpenAttachmentFullscreen({
			id: "image-1",
			type: "file",
			filename: "preview.png",
			mediaType: "image/png",
			url: "data:image/png;base64,abc123",
		}),
		true,
	);
});

test("canOpenAttachmentFullscreen rejects non-image attachments", () => {
	assert.equal(
		canOpenAttachmentFullscreen({
			id: "doc-1",
			type: "file",
			filename: "notes.txt",
			mediaType: "text/plain",
			url: "data:text/plain;base64,abc123",
		}),
		false,
	);
});

test("canOpenAttachmentFullscreen rejects image attachments without a URL", () => {
	assert.equal(
		canOpenAttachmentFullscreen({
			id: "image-2",
			type: "file",
			filename: "preview.png",
			mediaType: "image/png",
		}),
		false,
	);
});
