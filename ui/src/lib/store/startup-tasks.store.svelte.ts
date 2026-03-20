import { api } from "$lib/api-client";
import type { StartupTask } from "$lib/api-types";
import type { AsyncStatus } from "$lib/shell-types";

export class StartupTaskStore {
	#items = $state<StartupTask[]>([]);
	#status = $state<AsyncStatus>("idle");

	get list(): StartupTask[] {
		return this.#items;
	}

	get status(): AsyncStatus {
		return this.#status;
	}

	/** Returns the cached task. Triggers a background fetch of the full list on cache miss. */
	get(id: string): StartupTask | null {
		const cached = this.#items.find((t) => t.id === id) ?? null;
		if (cached === null && this.#status === "idle") {
			void this.fetch();
		}
		return cached;
	}

	async fetch(): Promise<void> {
		this.#status = "loading";
		try {
			const status = await api.getSystemStatus();
			this.#items = status.startupTasks ?? [];
			this.#status = "ready";
		} catch {
			this.#status = "error";
		}
	}
}
