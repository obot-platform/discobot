import { generateId } from "ai";

import { api } from "$lib/api-client";
import type { Session, UpdateSessionRequest } from "$lib/api-types";
import type { AsyncStatus } from "$lib/shell-types";

export type CreateSessionData = {
	workspaceId?: string;
	model?: string;
	reasoning?: string;
};

export class SessionStore {
	#items = $state<Session[]>([]);
	#status = $state<AsyncStatus>("idle");
	#inflight = new Set<string>();

	get list(): Session[] {
		return this.#items;
	}

	get status(): AsyncStatus {
		return this.#status;
	}

	/** Returns the cached session. Triggers a background fetchOne on cache miss. */
	get(id: string): Session | null {
		const cached = this.#items.find((s) => s.id === id) ?? null;
		if (cached === null && !this.#inflight.has(id)) {
			this.#inflight.add(id);
			void this.fetchOne(id).finally(() => this.#inflight.delete(id));
		}
		return cached;
	}

	/** Removes an item from the local cache without an API call (e.g. server-pushed removal). */
	evict(id: string): void {
		this.#items = this.#items.filter((s) => s.id !== id);
	}

	async fetch(): Promise<void> {
		this.#status = "loading";
		try {
			const { sessions } = await api.getSessions();
			this.#items = sessions;
			this.#status = "ready";
		} catch {
			this.#status = "error";
		}
	}

	async fetchOne(id: string): Promise<void> {
		try {
			const session = await api.getSession(id);
			const idx = this.#items.findIndex((s) => s.id === id);
			if (idx === -1) {
				this.#items = [...this.#items, session];
			} else {
				this.#items = this.#items.map((s, i) => (i === idx ? session : s));
			}
			if (this.#status !== "ready") {
				this.#status = "ready";
			}
		} catch (error) {
			console.error("[SessionStore] Failed to fetch session:", id, error);
		}
	}

	async create(data: CreateSessionData = {}): Promise<Session> {
		const { id } = await api.createSession({ id: generateId(), ...data });
		await this.fetchOne(id);
		return this.#items.find((s) => s.id === id)!;
	}

	async update(id: string, data: UpdateSessionRequest): Promise<Session> {
		await api.updateSession(id, data);
		await this.fetchOne(id);
		return this.#items.find((s) => s.id === id)!;
	}

	async remove(id: string): Promise<void> {
		await api.deleteSession(id);
		this.#items = this.#items.filter((s) => s.id !== id);
	}
}
