export type BottomSpacerHeightArgs = {
	contentHeight: number;
	existingSpacerHeight: number;
	anchorOffsetTop: number;
	contentTopPadding: number;
	viewportClientHeight: number;
	viewportPaddingBottom: number;
	viewportPaddingTop: number;
};

export function getBottomSpacerHeight({
	contentHeight,
	existingSpacerHeight,
	anchorOffsetTop,
	contentTopPadding,
	viewportClientHeight,
	viewportPaddingBottom,
	viewportPaddingTop,
}: BottomSpacerHeightArgs): number {
	const viewportContentHeight = Math.max(
		0,
		viewportClientHeight - viewportPaddingTop - viewportPaddingBottom,
	);
	const contentHeightWithoutSpacer = Math.max(
		0,
		contentHeight - existingSpacerHeight,
	);
	const distanceFromAnchorTopToEnd = Math.max(
		0,
		contentHeightWithoutSpacer - anchorOffsetTop,
	);
	const availableViewportHeight = Math.max(
		0,
		viewportContentHeight - Math.max(0, contentTopPadding),
	);
	const nextSpacerHeight = availableViewportHeight - distanceFromAnchorTopToEnd;

	return nextSpacerHeight <= 0 ? 0 : Math.floor(nextSpacerHeight + 0.01);
}
