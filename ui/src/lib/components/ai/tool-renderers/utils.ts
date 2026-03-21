export function shortenPath(path: string): string {
	return path.replace(/^\/home\/discobot/, "~");
}

export function renderToolValue(value: unknown): string {
	if (typeof value === "string") {
		return value;
	}
	if (value && typeof value === "object") {
		return JSON.stringify(value, null, 2);
	}
	if (value !== undefined && value !== null) {
		return String(value);
	}
	return "";
}

export function countLines(value: string): number {
	if (!value) {
		return 0;
	}
	return value.split("\n").length;
}
