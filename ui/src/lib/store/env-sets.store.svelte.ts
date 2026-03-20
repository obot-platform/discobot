import { api } from "$lib/api-client";
import type { EnvSetWithVars } from "$lib/api-types";
import type { AsyncStatus } from "$lib/shell-types";

export class EnvSetStore {
	#items = $state<EnvSetWithVars[]>([]);
	#status = $state<AsyncStatus>("idle");
	#inflight = new Set<string>();

	get list(): EnvSetWithVars[] {
		return this.#items;
	}

	get status(): AsyncStatus {
		return this.#status;
	}

	/** Returns the cached env set. Triggers a background fetchOne on cache miss. */
	get(id: string): EnvSetWithVars | null {
		const cached = this.#items.find((e) => e.id === id) ?? null;
		if (cached === null && !this.#inflight.has(id)) {
			this.#inflight.add(id);
			void this.fetchOne(id).finally(() => this.#inflight.delete(id));
		}
		return cached;
	}

	async fetch(): Promise<void> {
		this.#status = "loading";
		try {
			const { envSets } = await api.listEnvSets();
			this.#items = await Promise.all(envSets.map((e) => api.getEnvSet(e.id)));
			this.#status = "ready";
		} catch {
			this.#status = "error";
		}
	}

	async fetchOne(id: string): Promise<void> {
		try {
			const envSet = await api.getEnvSet(id);
			const idx = this.#items.findIndex((e) => e.id === id);
			if (idx === -1) {
				this.#items = [...this.#items, envSet];
			} else {
				this.#items = this.#items.map((e, i) => (i === idx ? envSet : e));
			}
			if (this.#status !== "ready") {
				this.#status = "ready";
			}
		} catch (error) {
			console.error("[EnvSetStore] Failed to fetch env set:", id, error);
		}
	}

	async create(name: string, envVars: Record<string, string>): Promise<EnvSetWithVars> {
		const created = await api.createEnvSet(name.trim(), envVars);
		await this.fetchOne(created.id);
		return this.#items.find((e) => e.id === created.id)!;
	}

	async update(id: string, name: string, envVars: Record<string, string>): Promise<EnvSetWithVars> {
		await api.updateEnvSet(id, name.trim(), envVars);
		await this.fetchOne(id);
		return this.#items.find((e) => e.id === id)!;
	}

	async remove(id: string): Promise<void> {
		await api.deleteEnvSet(id);
		this.#items = this.#items.filter((e) => e.id !== id);
	}
}
