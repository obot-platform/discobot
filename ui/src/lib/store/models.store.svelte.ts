import { api } from "$lib/api-client";
import type { ModelInfo } from "$lib/api-types";
import type { AsyncStatus } from "$lib/shell-types";

export class ModelStore {
	#items = $state<ModelInfo[]>([]);
	#status = $state<AsyncStatus>("idle");

	get list(): ModelInfo[] {
		return this.#items;
	}

	get status(): AsyncStatus {
		return this.#status;
	}

	/** Returns the cached model. Triggers a background fetch of the full list on cache miss. */
	get(id: string): ModelInfo | null {
		const cached = this.#items.find((m) => m.id === id) ?? null;
		if (cached === null && this.#status === "idle") {
			void this.fetch();
		}
		return cached;
	}

	async fetch(): Promise<void> {
		this.#status = "loading";
		try {
			const { models } = await api.getProjectModels();
			this.#items = models;
			this.#status = "ready";
		} catch {
			this.#status = "error";
		}
	}
}
