export type HandlePosition = "left" | "right" | "top" | "bottom";

type BezierOptions = {
	sourceX: number;
	sourceY: number;
	targetX: number;
	targetY: number;
	sourcePosition?: HandlePosition;
	targetPosition?: HandlePosition;
};

function controlOffset(position: HandlePosition, amount: number) {
	switch (position) {
		case "left":
			return { x: -amount, y: 0 };
		case "right":
			return { x: amount, y: 0 };
		case "top":
			return { x: 0, y: -amount };
		case "bottom":
			return { x: 0, y: amount };
	}
}

export function getBezierPath({
	sourceX,
	sourceY,
	targetX,
	targetY,
	sourcePosition = "right",
	targetPosition = "left",
}: BezierOptions): string {
	const dx = targetX - sourceX;
	const dy = targetY - sourceY;
	const base = Math.max(Math.abs(dx), Math.abs(dy));
	const distance = Math.max(24, Math.min(180, base * 0.5));

	const sourceControl = controlOffset(sourcePosition, distance);
	const targetControl = controlOffset(targetPosition, distance);

	const c1x = sourceX + sourceControl.x;
	const c1y = sourceY + sourceControl.y;
	const c2x = targetX + targetControl.x;
	const c2y = targetY + targetControl.y;

	return `M ${sourceX} ${sourceY} C ${c1x} ${c1y}, ${c2x} ${c2y}, ${targetX} ${targetY}`;
}
