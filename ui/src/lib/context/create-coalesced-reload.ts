export function createCoalescedReload(
	run: () => Promise<void>,
): () => Promise<void> {
	let inFlight: Promise<void> | null = null;
	let rerunRequested = false;

	return async () => {
		rerunRequested = true;

		if (!inFlight) {
			inFlight = (async () => {
				try {
					do {
						rerunRequested = false;
						await run();
					} while (rerunRequested);
				} finally {
					inFlight = null;
				}
			})();
		}

		return inFlight;
	};
}

export function createBackgroundRefresh(
	run: () => Promise<void>,
	errorMessage: string,
): () => Promise<void> {
	return async () => {
		void run().catch((error) => {
			console.error(errorMessage, error);
		});
	};
}
