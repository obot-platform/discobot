import type { ToolState } from "../types";

export function getToolStatusLabel(state: ToolState): string {
	switch (state) {
		case "input-streaming":
			return "Preparing";
		case "input-available":
			return "Running";
		case "approval-requested":
			return "Awaiting Approval";
		case "approval-responded":
			return "Responded";
		case "output-available":
			return "Completed";
		case "output-error":
			return "Error";
		case "output-denied":
			return "Denied";
	}
}

export function isToolRunningState(state: ToolState): boolean {
	return state === "input-streaming" || state === "input-available";
}

export function isToolPreparingState(state: ToolState): boolean {
	return state === "input-streaming";
}
