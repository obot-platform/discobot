type QueryClientLike = {
	revalidateOnFocus: () => void;
	revalidateOnVisibility: () => void;
};

const clients = new Set<QueryClientLike>();
let isListening = false;

function handleFocus() {
	for (const client of clients) {
		client.revalidateOnFocus();
	}
}

function handleVisibilityChange() {
	if (typeof document === "undefined" || document.visibilityState !== "visible") {
		return;
	}
	for (const client of clients) {
		client.revalidateOnVisibility();
	}
}

function ensureListeners() {
	if (isListening || typeof window === "undefined" || typeof document === "undefined") {
		return;
	}
	window.addEventListener("focus", handleFocus);
	document.addEventListener("visibilitychange", handleVisibilityChange);
	isListening = true;
}

export function registerFocusClient(client: QueryClientLike): () => void {
	clients.add(client);
	ensureListeners();
	return () => {
		clients.delete(client);
	};
}
