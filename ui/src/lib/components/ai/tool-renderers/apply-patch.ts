export type ApplyPatchLineKind = "context" | "add" | "remove";

export type ApplyPatchLine = {
	kind: ApplyPatchLineKind;
	marker: " " | "+" | "-";
	content: string;
};

export type ApplyPatchChunk = {
	context: string | null;
	lines: ApplyPatchLine[];
	isEndOfFile: boolean;
};

export type ApplyPatchOperationKind = "add" | "delete" | "update";

export type ApplyPatchOperation = {
	kind: ApplyPatchOperationKind;
	path: string;
	movePath: string | null;
	addLines: string[];
	chunks: ApplyPatchChunk[];
	stats: {
		additions: number;
		removals: number;
		context: number;
	};
};

export type ApplyPatchParseResult = {
	raw: string;
	operations: ApplyPatchOperation[];
	stats: {
		files: number;
		additions: number;
		removals: number;
		addedFiles: number;
		deletedFiles: number;
		updatedFiles: number;
	};
	incomplete: boolean;
	error?: string;
};

export type ApplyPatchOutputEntry = {
	status: "added" | "modified" | "deleted";
	marker: "A" | "M" | "D";
	path: string;
};

export type ApplyPatchOutputResult = {
	raw: string;
	success: boolean;
	entries: ApplyPatchOutputEntry[];
};

const BEGIN_PATCH_MARKER = "*** Begin Patch";
const END_PATCH_MARKER = "*** End Patch";
const ADD_FILE_MARKER = "*** Add File: ";
const DELETE_FILE_MARKER = "*** Delete File: ";
const UPDATE_FILE_MARKER = "*** Update File: ";
const MOVE_TO_MARKER = "*** Move to: ";
const END_OF_FILE_MARKER = "*** End of File";

export function extractApplyPatchInput(input: unknown): string {
	if (typeof input === "string") {
		return input;
	}

	if (!input || typeof input !== "object") {
		return "";
	}

	const candidate = input as Record<string, unknown>;

	if (typeof candidate.raw === "string") {
		return candidate.raw;
	}

	if (typeof candidate.input === "string") {
		return candidate.input;
	}

	return "";
}

export function parseApplyPatchInput(input: unknown): ApplyPatchParseResult {
	return parseApplyPatch(extractApplyPatchInput(input));
}

export function parseApplyPatch(rawPatch: string): ApplyPatchParseResult {
	const raw = normalizeNewlines(rawPatch);
	const trimmed = raw.trim();

	if (!trimmed) {
		return emptyParseResult(raw, "Patch input is empty.");
	}

	const lines = raw.split("\n");
	const beginIndex = findFirstNonEmptyLine(lines);
	if (beginIndex < 0 || lines[beginIndex]?.trim() !== BEGIN_PATCH_MARKER) {
		return emptyParseResult(
			raw,
			`Patch must start with '${BEGIN_PATCH_MARKER}'.`,
		);
	}

	const endIndex = findPatchEndIndex(lines, beginIndex + 1);
	const incomplete = endIndex < 0;
	const bodyEnd = incomplete ? lines.length : endIndex;
	const operations: ApplyPatchOperation[] = [];

	let cursor = beginIndex + 1;
	let error: string | undefined;

	while (cursor < bodyEnd) {
		const trimmedLine = lines[cursor]?.trim() ?? "";
		if (!trimmedLine) {
			cursor += 1;
			continue;
		}

		const parsed = parseOperation(lines, cursor, bodyEnd);
		if (parsed.operation) {
			operations.push(parsed.operation);
			cursor = parsed.nextCursor;
		}
		if (parsed.error) {
			error = parsed.error;
			break;
		}
		if (!parsed.operation) {
			break;
		}
	}

	return {
		raw,
		operations,
		stats: summarizeOperations(operations),
		incomplete,
		...(error ? { error } : {}),
	};
}

export function parseApplyPatchOutput(output: unknown): ApplyPatchOutputResult {
	const raw = typeof output === "string" ? normalizeNewlines(output) : "";
	const trimmed = raw.trim();
	if (!trimmed) {
		return {
			raw,
			success: false,
			entries: [],
		};
	}

	const prefix = "Success. Updated the following files:";
	if (!trimmed.startsWith(prefix)) {
		return {
			raw,
			success: false,
			entries: [],
		};
	}

	const entries = trimmed
		.slice(prefix.length)
		.split("\n")
		.map((line) => line.trim())
		.filter(Boolean)
		.reduce<ApplyPatchOutputEntry[]>((accumulator, line) => {
			const marker = line[0];
			const path = line.slice(1).trim();
			if (!path) {
				return accumulator;
			}
			switch (marker) {
				case "A":
					accumulator.push({ status: "added", marker: "A", path });
					break;
				case "M":
					accumulator.push({ status: "modified", marker: "M", path });
					break;
				case "D":
					accumulator.push({ status: "deleted", marker: "D", path });
					break;
			}
			return accumulator;
		}, []);

	return {
		raw,
		success: true,
		entries,
	};
}

export function summarizeApplyPatchTitle(input: unknown): string | undefined {
	const rawPatch = normalizeNewlines(extractApplyPatchInput(input));
	if (!rawPatch.trim()) {
		return undefined;
	}

	const lines = rawPatch.split("\n");
	let firstPath: string | undefined;
	let operationCount = 0;
	let pendingUpdatePath: string | undefined;

	for (const line of lines) {
		if (line.startsWith(ADD_FILE_MARKER)) {
			operationCount += 1;
			const path = line.slice(ADD_FILE_MARKER.length).trim();
			if (!firstPath && path) {
				firstPath = path;
			}
			pendingUpdatePath = undefined;
			continue;
		}

		if (line.startsWith(DELETE_FILE_MARKER)) {
			operationCount += 1;
			const path = line.slice(DELETE_FILE_MARKER.length).trim();
			if (!firstPath && path) {
				firstPath = path;
			}
			pendingUpdatePath = undefined;
			continue;
		}

		if (line.startsWith(UPDATE_FILE_MARKER)) {
			operationCount += 1;
			const path = line.slice(UPDATE_FILE_MARKER.length).trim();
			if (!firstPath && path) {
				firstPath = path;
			}
			pendingUpdatePath = path || undefined;
			continue;
		}

		if (line.startsWith(MOVE_TO_MARKER) && pendingUpdatePath) {
			const movePath = line.slice(MOVE_TO_MARKER.length).trim();
			if (movePath && firstPath === pendingUpdatePath) {
				firstPath = movePath;
			}
			pendingUpdatePath = undefined;
		}
	}

	if (!firstPath || operationCount === 0) {
		return undefined;
	}

	const fileName = firstPath.split("/").at(-1) ?? firstPath;
	if (operationCount === 1) {
		return fileName;
	}
	return `${fileName} (+${operationCount - 1})`;
}

export function getApplyPatchDisplayPath(
	operation: ApplyPatchOperation,
): string {
	return operation.movePath || operation.path;
}

function emptyParseResult(raw: string, error: string): ApplyPatchParseResult {
	return {
		raw,
		operations: [],
		stats: summarizeOperations([]),
		incomplete: false,
		error,
	};
}

function normalizeNewlines(value: string): string {
	return value.replaceAll("\r\n", "\n").replaceAll("\r", "\n");
}

function findFirstNonEmptyLine(lines: string[]): number {
	for (let index = 0; index < lines.length; index += 1) {
		if (lines[index]?.trim()) {
			return index;
		}
	}
	return -1;
}

function findPatchEndIndex(lines: string[], startIndex: number): number {
	for (let index = startIndex; index < lines.length; index += 1) {
		if (lines[index]?.trim() === END_PATCH_MARKER) {
			return index;
		}
	}
	return -1;
}

function summarizeOperations(
	operations: ApplyPatchOperation[],
): ApplyPatchParseResult["stats"] {
	return operations.reduce<ApplyPatchParseResult["stats"]>(
		(accumulator, operation) => {
			accumulator.files += 1;
			accumulator.additions += operation.stats.additions;
			accumulator.removals += operation.stats.removals;
			if (operation.kind === "add") {
				accumulator.addedFiles += 1;
			}
			if (operation.kind === "delete") {
				accumulator.deletedFiles += 1;
			}
			if (operation.kind === "update") {
				accumulator.updatedFiles += 1;
			}
			return accumulator;
		},
		{
			files: 0,
			additions: 0,
			removals: 0,
			addedFiles: 0,
			deletedFiles: 0,
			updatedFiles: 0,
		},
	);
}

function parseOperation(
	lines: string[],
	cursor: number,
	bodyEnd: number,
): {
	operation?: ApplyPatchOperation;
	nextCursor: number;
	error?: string;
} {
	const header = lines[cursor]?.trim() ?? "";

	if (header.startsWith(ADD_FILE_MARKER)) {
		const path = header.slice(ADD_FILE_MARKER.length).trim();
		const addLines: string[] = [];
		let nextCursor = cursor + 1;
		for (; nextCursor < bodyEnd; nextCursor += 1) {
			const nextLine = lines[nextCursor] ?? "";
			if (!nextLine.startsWith("+")) {
				break;
			}
			addLines.push(nextLine.slice(1));
		}
		return {
			operation: {
				kind: "add",
				path,
				movePath: null,
				addLines,
				chunks: [],
				stats: {
					additions: addLines.length,
					removals: 0,
					context: 0,
				},
			},
			nextCursor,
		};
	}

	if (header.startsWith(DELETE_FILE_MARKER)) {
		const path = header.slice(DELETE_FILE_MARKER.length).trim();
		return {
			operation: {
				kind: "delete",
				path,
				movePath: null,
				addLines: [],
				chunks: [],
				stats: {
					additions: 0,
					removals: 0,
					context: 0,
				},
			},
			nextCursor: cursor + 1,
		};
	}

	if (header.startsWith(UPDATE_FILE_MARKER)) {
		const path = header.slice(UPDATE_FILE_MARKER.length).trim();
		let nextCursor = cursor + 1;
		let movePath: string | null = null;
		if ((lines[nextCursor]?.trim() ?? "").startsWith(MOVE_TO_MARKER)) {
			movePath = (lines[nextCursor]?.trim() ?? "")
				.slice(MOVE_TO_MARKER.length)
				.trim();
			nextCursor += 1;
		}

		const chunks: ApplyPatchChunk[] = [];
		let chunkError: string | undefined;
		for (; nextCursor < bodyEnd; ) {
			const nextTrimmed = lines[nextCursor]?.trim() ?? "";
			if (!nextTrimmed) {
				nextCursor += 1;
				continue;
			}
			if (nextTrimmed.startsWith("*** ")) {
				break;
			}

			const parsedChunk = parseChunk(
				lines,
				nextCursor,
				bodyEnd,
				chunks.length === 0,
			);
			if (parsedChunk.error) {
				chunkError = parsedChunk.error;
				break;
			}
			if (!parsedChunk.chunk) {
				break;
			}
			chunks.push(parsedChunk.chunk);
			nextCursor = parsedChunk.nextCursor;
		}

		if (chunks.length === 0) {
			return {
				nextCursor,
				error: chunkError ?? `Update file hunk for '${path}' is empty.`,
			};
		}

		return {
			operation: {
				kind: "update",
				path,
				movePath,
				addLines: [],
				chunks,
				stats: summarizeChunks(chunks),
			},
			nextCursor,
			...(chunkError ? { error: chunkError } : {}),
		};
	}

	return {
		nextCursor: cursor,
		error: `Unexpected patch header: '${header}'.`,
	};
}

function parseChunk(
	lines: string[],
	cursor: number,
	bodyEnd: number,
	allowMissingContext: boolean,
): {
	chunk?: ApplyPatchChunk;
	nextCursor: number;
	error?: string;
} {
	let nextCursor = cursor;
	let context: string | null = null;
	const firstTrimmed = lines[nextCursor]?.trim() ?? "";
	if (firstTrimmed === "@@") {
		nextCursor += 1;
	} else if (firstTrimmed.startsWith("@@ ")) {
		context = firstTrimmed.slice(3);
		nextCursor += 1;
	} else if (!allowMissingContext) {
		return {
			nextCursor,
			error: `Expected update hunk to start with '@@', got '${lines[nextCursor] ?? ""}'.`,
		};
	}

	const chunkLines: ApplyPatchLine[] = [];
	let isEndOfFile = false;

	for (; nextCursor < bodyEnd; nextCursor += 1) {
		const line = lines[nextCursor] ?? "";
		const trimmed = line.trim();
		if (trimmed === END_OF_FILE_MARKER) {
			isEndOfFile = true;
			nextCursor += 1;
			break;
		}
		if (
			chunkLines.length > 0 &&
			(trimmed === "@@" ||
				trimmed.startsWith("@@ ") ||
				trimmed.startsWith("*** "))
		) {
			break;
		}
		if (line === "") {
			chunkLines.push({ kind: "context", marker: " ", content: "" });
			continue;
		}

		switch (line[0]) {
			case " ":
				chunkLines.push({
					kind: "context",
					marker: " ",
					content: line.slice(1),
				});
				break;
			case "+":
				chunkLines.push({ kind: "add", marker: "+", content: line.slice(1) });
				break;
			case "-":
				chunkLines.push({
					kind: "remove",
					marker: "-",
					content: line.slice(1),
				});
				break;
			default:
				if (chunkLines.length === 0) {
					return {
						nextCursor,
						error: `Unexpected line in update hunk: '${line}'.`,
					};
				}
				return {
					chunk: {
						context,
						lines: chunkLines,
						isEndOfFile,
					},
					nextCursor,
				};
		}
	}

	if (chunkLines.length === 0) {
		return {
			nextCursor,
			error: "Update hunk does not contain any lines.",
		};
	}

	return {
		chunk: {
			context,
			lines: chunkLines,
			isEndOfFile,
		},
		nextCursor,
	};
}

function summarizeChunks(
	chunks: ApplyPatchChunk[],
): ApplyPatchOperation["stats"] {
	return chunks.reduce<ApplyPatchOperation["stats"]>(
		(accumulator, chunk) => {
			for (const line of chunk.lines) {
				if (line.kind === "add") {
					accumulator.additions += 1;
				}
				if (line.kind === "remove") {
					accumulator.removals += 1;
				}
				if (line.kind === "context") {
					accumulator.context += 1;
				}
			}
			return accumulator;
		},
		{
			additions: 0,
			removals: 0,
			context: 0,
		},
	);
}
