export function shortenPath(path: string): string {
	return path.replace(/^\/home\/discobot/, "~");
}
