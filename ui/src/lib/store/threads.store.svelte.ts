import { api } from "$lib/api-client";
import type {
	CreateThreadRequest,
	Thread,
	UpdateThreadRequest,
} from "$lib/api-types";
import type { AsyncStatus } from "$lib/shell-types";

import { RequestCoalescer } from "./request-coalescer";

export class ThreadStore {
	#items = $state<Thread[]>([]);
	#status = $state<AsyncStatus>("idle");
	#fetchRequests = new RequestCoalescer<string>();
	#fetchOneRequests = new RequestCoalescer<string>();

	get list(): Thread[] {
		return this.#items;
	}

	get status(): AsyncStatus {
		return this.#status;
	}

	/** Returns the cached thread, or null. Use fetchOne to populate on a cache miss. */
	get(id: string): Thread | null {
		return this.#items.find((t) => t.id === id) ?? null;
	}

	reset(): void {
		this.#items = [];
		this.#status = "idle";
	}

	async fetch(sessionId: string): Promise<void> {
		return this.#fetchRequests.run(sessionId, async () => {
			this.#status = "loading";
			try {
				const { threads } = await api.getThreads(sessionId);
				this.#items = threads;
				this.#status = "ready";
			} catch (error) {
				this.#status = "error";
				throw error;
			}
		});
	}

	async fetchOne(sessionId: string, threadId: string): Promise<void> {
		return this.#fetchOneRequests.run(`${sessionId}:${threadId}`, async () => {
			try {
				const thread = await api.getThread(sessionId, threadId);
				const idx = this.#items.findIndex((t) => t.id === threadId);
				if (idx === -1) {
					this.#items = [...this.#items, thread];
				} else {
					this.#items = this.#items.map((t, i) => (i === idx ? thread : t));
				}
				if (this.#status !== "ready") {
					this.#status = "ready";
				}
			} catch (error) {
				console.error("[ThreadStore] Failed to fetch thread:", threadId, error);
			}
		});
	}

	async create(sessionId: string, data: CreateThreadRequest): Promise<Thread> {
		const created = await api.createThread(sessionId, data);
		await this.fetchOne(sessionId, created.id);
		return this.#items.find((t) => t.id === created.id)!;
	}

	async update(
		sessionId: string,
		threadId: string,
		data: UpdateThreadRequest,
	): Promise<Thread> {
		await api.updateThread(sessionId, threadId, data);
		await this.fetchOne(sessionId, threadId);
		return this.#items.find((t) => t.id === threadId)!;
	}

	async remove(sessionId: string, threadId: string): Promise<void> {
		await api.deleteThread(sessionId, threadId);
		this.#items = this.#items.filter((t) => t.id !== threadId);
		if (this.#status !== "ready") {
			this.#status = "ready";
		}
	}
}
