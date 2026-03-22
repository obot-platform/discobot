import assert from "node:assert/strict";
import test from "node:test";

import { getBottomSpacerHeight } from "../app/conversation-pane-layout";

test("getBottomSpacerHeight rounds down fractional spacer values to avoid short-conversation overflow", () => {
	assert.equal(
		getBottomSpacerHeight({
			contentHeight: 123.8,
			existingSpacerHeight: 0,
			anchorOffsetTop: 0.2,
			contentTopPadding: 0,
			viewportClientHeight: 400,
			viewportPaddingBottom: 16,
			viewportPaddingTop: 16,
		}),
		244,
	);
});

test("getBottomSpacerHeight excludes the existing spacer before measuring the current turn", () => {
	assert.equal(
		getBottomSpacerHeight({
			contentHeight: 500,
			existingSpacerHeight: 120,
			anchorOffsetTop: 200,
			contentTopPadding: 24,
			viewportClientHeight: 440,
			viewportPaddingBottom: 16,
			viewportPaddingTop: 16,
		}),
		204,
	);
});
