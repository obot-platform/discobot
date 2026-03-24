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

export type NumberedToolOutputLine = {
	lineNumber: string;
	text: string;
};

export type ParsedNumberedToolOutput = {
	isTruncated: boolean;
	truncationFilePath?: string;
	lines: NumberedToolOutputLine[];
};

const TRUNCATED_OUTPUT_PREFIX =
	/^\[Output too long \([^\]]+\)\. Full output written to: (.+)\]$/u;
const NUMBERED_TOOL_OUTPUT_LINE = /^\s*(\d+)→(.*)$/u;

export function parseNumberedToolOutput(
	value: string,
): ParsedNumberedToolOutput {
	if (!value) {
		return {
			isTruncated: false,
			lines: [],
		};
	}

	const rawLines = value.split(/\r\n|\n|\r/u);
	let startIndex = 0;
	let isTruncated = false;
	let truncationFilePath: string | undefined;

	const firstNonEmptyLineIndex = rawLines.findIndex(
		(line) => line.trim() !== "",
	);
	if (firstNonEmptyLineIndex >= 0) {
		const truncationMatch = rawLines[firstNonEmptyLineIndex]?.match(
			TRUNCATED_OUTPUT_PREFIX,
		);
		if (truncationMatch) {
			isTruncated = true;
			truncationFilePath = truncationMatch[1]?.trim();
			startIndex = firstNonEmptyLineIndex + 1;
			while (rawLines[startIndex]?.trim() === "") {
				startIndex += 1;
			}
		}
	}

	const candidateLines = rawLines.slice(startIndex);
	while (candidateLines.at(-1) === "") {
		candidateLines.pop();
	}

	if (candidateLines.length === 0) {
		return {
			isTruncated,
			truncationFilePath,
			lines: [],
		};
	}

	const parsedLines: NumberedToolOutputLine[] = [];
	for (const line of candidateLines) {
		const match = line.match(NUMBERED_TOOL_OUTPUT_LINE);
		if (!match) {
			return {
				isTruncated,
				truncationFilePath,
				lines: [],
			};
		}

		parsedLines.push({
			lineNumber: match[1],
			text: match[2],
		});
	}

	return {
		isTruncated,
		truncationFilePath,
		lines: parsedLines,
	};
}
