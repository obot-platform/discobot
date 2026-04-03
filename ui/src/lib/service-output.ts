const ESCAPE = "\u001b";
const CSI_8BIT = "\u009b";

export function renderServiceOutputText(value: string): string {
	return value
		.replaceAll("\r\n", "\n")
		.split("\n")
		.map(renderServiceOutputLine)
		.join("\n");
}

function renderServiceOutputLine(value: string): string {
	const buffer: string[] = [];
	let cursor = 0;
	let index = 0;

	while (index < value.length) {
		const char = value[index] ?? "";

		if (char === "\r") {
			cursor = 0;
			index += 1;
			continue;
		}

		if (char === "\b") {
			cursor = Math.max(0, cursor - 1);
			index += 1;
			continue;
		}

		if (char === ESCAPE || char === CSI_8BIT) {
			const nextState = consumeEscapeSequence(value, index, buffer, cursor);
			index = nextState.index;
			cursor = nextState.cursor;
			continue;
		}

		if (isIgnoredControlCharacter(char)) {
			index += 1;
			continue;
		}

		if (cursor === buffer.length) {
			buffer.push(char);
		} else {
			buffer[cursor] = char;
		}
		cursor += 1;
		index += 1;
	}

	return buffer.join("");
}

function consumeEscapeSequence(
	value: string,
	index: number,
	buffer: string[],
	cursor: number,
): { index: number; cursor: number } {
	const char = value[index];
	const next = value[index + 1] ?? "";

	if (char === CSI_8BIT) {
		return consumeCsiSequence(value, index + 1, buffer, cursor);
	}

	if (next === "[") {
		return consumeCsiSequence(value, index + 2, buffer, cursor);
	}

	if (next === "]") {
		return consumeOscSequence(value, index + 2, cursor);
	}

	return { index: Math.min(value.length, index + 2), cursor };
}

function consumeCsiSequence(
	value: string,
	index: number,
	buffer: string[],
	cursor: number,
): { index: number; cursor: number } {
	let cursorIndex = index;
	while (cursorIndex < value.length) {
		const code = value.charCodeAt(cursorIndex);
		if (code >= 0x40 && code <= 0x7e) {
			const command = value[cursorIndex] ?? "";
			const params = value.slice(index, cursorIndex);
			if (command === "K") {
				return {
					index: cursorIndex + 1,
					cursor: applyEraseInLine(params, buffer, cursor),
				};
			}
			return { index: cursorIndex + 1, cursor };
		}
		cursorIndex += 1;
	}

	return { index: value.length, cursor };
}

function consumeOscSequence(
	value: string,
	index: number,
	cursor: number,
): { index: number; cursor: number } {
	let cursorIndex = index;
	while (cursorIndex < value.length) {
		const char = value[cursorIndex] ?? "";
		if (char === "\u0007") {
			return { index: cursorIndex + 1, cursor };
		}
		if (char === ESCAPE && value[cursorIndex + 1] === "\\") {
			return { index: cursorIndex + 2, cursor };
		}
		cursorIndex += 1;
	}

	return { index: value.length, cursor };
}

function applyEraseInLine(
	params: string,
	buffer: string[],
	cursor: number,
): number {
	const modeToken = params.split(";").at(-1) ?? "0";
	const mode = Number.parseInt(modeToken || "0", 10);

	if (mode === 1) {
		buffer.splice(0, Math.min(buffer.length, cursor + 1));
		return 0;
	}

	if (mode === 2) {
		buffer.length = 0;
		return 0;
	}

	buffer.length = Math.min(buffer.length, cursor);
	return cursor;
}

function isIgnoredControlCharacter(char: string): boolean {
	if (char === "\t") {
		return false;
	}

	const code = char.charCodeAt(0);
	return (code >= 0 && code < 0x20) || code === 0x7f;
}
