export type ParsedDiffLine = {
	left: number | null;
	right: number | null;
	marker: " " | "+" | "-";
	content: string;
};

export type ParsedDiffHunk = {
	header: string;
	oldStart: number;
	oldLines: number;
	newStart: number;
	newLines: number;
	lines: ParsedDiffLine[];
};

const HUNK_HEADER_RE = /^@@\s+-(\d+)(?:,(\d+))?\s+\+(\d+)(?:,(\d+))?\s+@@/;

function toLines(content: string): string[] {
	if (content.length === 0) {
		return [];
	}
	return content.split("\n");
}

export function parseUnifiedDiff(patch: string): ParsedDiffHunk[] {
	const lines = patch.split("\n");
	const hunks: ParsedDiffHunk[] = [];
	let index = 0;

	while (index < lines.length) {
		const line = lines[index] ?? "";
		const match = HUNK_HEADER_RE.exec(line);
		if (!match) {
			index += 1;
			continue;
		}

		const oldStart = Number.parseInt(match[1] ?? "0", 10);
		const oldLines = Number.parseInt(match[2] ?? "1", 10);
		const newStart = Number.parseInt(match[3] ?? "0", 10);
		const newLines = Number.parseInt(match[4] ?? "1", 10);

		let left = oldStart;
		let right = newStart;
		const hunkLines: ParsedDiffLine[] = [];
		index += 1;

		while (index < lines.length) {
			const hunkLine = lines[index] ?? "";
			if (HUNK_HEADER_RE.test(hunkLine)) {
				break;
			}
			if (hunkLine === "\\ No newline at end of file") {
				index += 1;
				continue;
			}
			if (hunkLine.startsWith("+")) {
				hunkLines.push({
					left: null,
					right,
					marker: "+",
					content: hunkLine.slice(1),
				});
				right += 1;
				index += 1;
				continue;
			}
			if (hunkLine.startsWith("-")) {
				hunkLines.push({
					left,
					right: null,
					marker: "-",
					content: hunkLine.slice(1),
				});
				left += 1;
				index += 1;
				continue;
			}
			if (hunkLine.startsWith(" ")) {
				hunkLines.push({
					left,
					right,
					marker: " ",
					content: hunkLine.slice(1),
				});
				left += 1;
				right += 1;
				index += 1;
				continue;
			}
			index += 1;
		}

		hunks.push({
			header: line,
			oldStart,
			oldLines,
			newStart,
			newLines,
			lines: hunkLines,
		});
	}

	return hunks;
}

export function reconstructOriginalFromPatch(currentContent: string, patch: string): string {
	const hunks = parseUnifiedDiff(patch);
	if (hunks.length === 0) {
		return currentContent;
	}

	const currentLines = toLines(currentContent);
	const originalLines: string[] = [];
	let currentLine = 1;

	for (const hunk of hunks) {
		while (currentLine < hunk.newStart) {
			const unchangedLine = currentLines[currentLine - 1];
			if (unchangedLine !== undefined) {
				originalLines.push(unchangedLine);
			}
			currentLine += 1;
		}

		for (const line of hunk.lines) {
			if (line.marker === " ") {
				const unchangedLine = currentLines[currentLine - 1];
				originalLines.push(unchangedLine ?? line.content);
				currentLine += 1;
				continue;
			}
			if (line.marker === "+") {
				currentLine += 1;
				continue;
			}
			originalLines.push(line.content);
		}
	}

	while (currentLine <= currentLines.length) {
		const unchangedLine = currentLines[currentLine - 1];
		if (unchangedLine !== undefined) {
			originalLines.push(unchangedLine);
		}
		currentLine += 1;
	}

	return originalLines.join("\n");
}

export function countDiffLinesFast(patch: string): number {
	let count = 0;
	let inHunk = false;

	for (const line of patch.split("\n")) {
		if (line.startsWith("@@")) {
			inHunk = true;
			continue;
		}
		if (inHunk && (line.startsWith(" ") || line.startsWith("+") || line.startsWith("-"))) {
			count += 1;
		}
	}

	return count;
}

export async function hashString(value: string): Promise<string> {
	if (typeof crypto !== "undefined" && typeof crypto.subtle !== "undefined") {
		const encoded = new TextEncoder().encode(value);
		const buffer = await crypto.subtle.digest("SHA-256", encoded);
		const bytes = Array.from(new Uint8Array(buffer));
		return bytes.map((byte) => byte.toString(16).padStart(2, "0")).join("");
	}

	let hash = 2166136261;
	for (let index = 0; index < value.length; index += 1) {
		hash ^= value.charCodeAt(index);
		hash = Math.imul(hash, 16777619);
	}
	return (hash >>> 0).toString(16);
}
