export type ParsedDiffLine = {
	left: number | null;
	right: number | null;
	marker: " " | "+" | "-";
	content: string;
};

export type EditDiffSegment = {
	text: string;
	changed: boolean;
};

export type EditDiffRow = {
	oldLineNumber: number | null;
	newLineNumber: number | null;
	kind: "context" | "add" | "remove";
	segments: EditDiffSegment[];
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

function buildLcsMatrix(left: string[], right: string[]): number[][] {
	const matrix = Array.from({ length: left.length + 1 }, () =>
		Array(right.length + 1).fill(0),
	);

	for (let leftIndex = 1; leftIndex <= left.length; leftIndex += 1) {
		for (let rightIndex = 1; rightIndex <= right.length; rightIndex += 1) {
			if (left[leftIndex - 1] === right[rightIndex - 1]) {
				matrix[leftIndex][rightIndex] =
					matrix[leftIndex - 1][rightIndex - 1] + 1;
				continue;
			}

			matrix[leftIndex][rightIndex] = Math.max(
				matrix[leftIndex - 1][rightIndex],
				matrix[leftIndex][rightIndex - 1],
			);
		}
	}

	return matrix;
}

function getCommonPrefixLength(left: string, right: string): number {
	const limit = Math.min(left.length, right.length);
	let index = 0;

	while (index < limit && left[index] === right[index]) {
		index += 1;
	}

	return index;
}

function getCommonSuffixLength(
	left: string,
	right: string,
	prefixLength: number,
): number {
	const maxLength = Math.min(left.length, right.length) - prefixLength;
	let index = 0;

	while (
		index < maxLength &&
		left[left.length - index - 1] === right[right.length - index - 1]
	) {
		index += 1;
	}

	return index;
}

function buildSegments(
	value: string,
	prefixLength: number,
	suffixLength: number,
): EditDiffSegment[] {
	const segments: EditDiffSegment[] = [];
	const suffixStart =
		suffixLength > 0 ? value.length - suffixLength : value.length;
	const prefix = value.slice(0, prefixLength);
	const changed = value.slice(prefixLength, suffixStart);
	const suffix = value.slice(suffixStart);

	if (prefix.length > 0) {
		segments.push({ text: prefix, changed: false });
	}
	if (changed.length > 0) {
		segments.push({ text: changed, changed: true });
	}
	if (suffix.length > 0) {
		segments.push({ text: suffix, changed: false });
	}

	if (segments.length === 0) {
		segments.push({ text: "", changed: false });
	}

	return segments;
}

function buildChangedLineSegments(
	left: string,
	right: string,
): { left: EditDiffSegment[]; right: EditDiffSegment[] } {
	const prefixLength = getCommonPrefixLength(left, right);
	const suffixLength = getCommonSuffixLength(left, right, prefixLength);

	return {
		left: buildSegments(left, prefixLength, suffixLength),
		right: buildSegments(right, prefixLength, suffixLength),
	};
}

export function buildEditDiffRows(
	oldContent: string,
	newContent: string,
): EditDiffRow[] {
	const oldLines = toLines(oldContent);
	const newLines = toLines(newContent);
	const matrix = buildLcsMatrix(oldLines, newLines);
	const operations: Array<{
		kind: "context" | "add" | "remove";
		line: string;
		oldLineNumber: number | null;
		newLineNumber: number | null;
	}> = [];

	let oldIndex = oldLines.length;
	let newIndex = newLines.length;

	while (oldIndex > 0 && newIndex > 0) {
		if (oldLines[oldIndex - 1] === newLines[newIndex - 1]) {
			operations.push({
				kind: "context",
				line: oldLines[oldIndex - 1] ?? "",
				oldLineNumber: oldIndex,
				newLineNumber: newIndex,
			});
			oldIndex -= 1;
			newIndex -= 1;
			continue;
		}

		if (matrix[oldIndex - 1][newIndex] >= matrix[oldIndex][newIndex - 1]) {
			operations.push({
				kind: "remove",
				line: oldLines[oldIndex - 1] ?? "",
				oldLineNumber: oldIndex,
				newLineNumber: null,
			});
			oldIndex -= 1;
			continue;
		}

		operations.push({
			kind: "add",
			line: newLines[newIndex - 1] ?? "",
			oldLineNumber: null,
			newLineNumber: newIndex,
		});
		newIndex -= 1;
	}

	while (oldIndex > 0) {
		operations.push({
			kind: "remove",
			line: oldLines[oldIndex - 1] ?? "",
			oldLineNumber: oldIndex,
			newLineNumber: null,
		});
		oldIndex -= 1;
	}

	while (newIndex > 0) {
		operations.push({
			kind: "add",
			line: newLines[newIndex - 1] ?? "",
			oldLineNumber: null,
			newLineNumber: newIndex,
		});
		newIndex -= 1;
	}

	operations.reverse();

	const rows: EditDiffRow[] = [];
	let index = 0;

	while (index < operations.length) {
		const operation = operations[index];

		if (!operation || operation.kind === "context") {
			if (operation) {
				rows.push({
					kind: "context",
					oldLineNumber: operation.oldLineNumber,
					newLineNumber: operation.newLineNumber,
					segments: [{ text: operation.line, changed: false }],
				});
			}
			index += 1;
			continue;
		}

		const removed: typeof operations = [];
		const added: typeof operations = [];

		while (index < operations.length && operations[index]?.kind !== "context") {
			const changedOperation = operations[index];
			if (!changedOperation) {
				break;
			}
			if (changedOperation.kind === "remove") {
				removed.push(changedOperation);
			} else {
				added.push(changedOperation);
			}
			index += 1;
		}

		const pairCount = Math.max(removed.length, added.length);

		for (let pairIndex = 0; pairIndex < pairCount; pairIndex += 1) {
			const removedOperation = removed[pairIndex];
			const addedOperation = added[pairIndex];
			const pairedSegments =
				removedOperation && addedOperation
					? buildChangedLineSegments(removedOperation.line, addedOperation.line)
					: null;

			if (removedOperation) {
				rows.push({
					kind: "remove",
					oldLineNumber: removedOperation.oldLineNumber,
					newLineNumber: null,
					segments: pairedSegments?.left ?? [
						{ text: removedOperation.line, changed: true },
					],
				});
			}

			if (addedOperation) {
				rows.push({
					kind: "add",
					oldLineNumber: null,
					newLineNumber: addedOperation.newLineNumber,
					segments: pairedSegments?.right ?? [
						{ text: addedOperation.line, changed: true },
					],
				});
			}
		}
	}

	return rows;
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

export function reconstructOriginalFromPatch(
	currentContent: string,
	patch: string,
): string {
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
		if (
			inHunk &&
			(line.startsWith(" ") || line.startsWith("+") || line.startsWith("-"))
		) {
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
