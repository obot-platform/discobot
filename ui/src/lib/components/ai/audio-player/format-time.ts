export function formatTime(value: number): string {
	if (!Number.isFinite(value) || value < 0) {
		return "0:00";
	}

	const totalSeconds = Math.floor(value);
	const minutes = Math.floor(totalSeconds / 60);
	const seconds = totalSeconds % 60;

	return `${minutes}:${String(seconds).padStart(2, "0")}`;
}
