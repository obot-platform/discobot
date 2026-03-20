import { api } from "$lib/api-client";
import type { CreateWorkspaceRequest, Workspace, WorkspaceValidationResult } from "$lib/api-types";
import type { AsyncStatus } from "$lib/shell-types";

export class WorkspaceStore {
	#items = $state<Workspace[]>([]);
	#status = $state<AsyncStatus>("idle");
	#inflight = new Set<string>();

	get list(): Workspace[] {
		return this.#items;
	}

	get status(): AsyncStatus {
		return this.#status;
	}

	/** Returns the cached workspace. Triggers a background fetchOne on cache miss. */
	get(id: string): Workspace | null {
		const cached = this.#items.find((w) => w.id === id) ?? null;
		if (cached === null && !this.#inflight.has(id)) {
			this.#inflight.add(id);
			void this.fetchOne(id).finally(() => this.#inflight.delete(id));
		}
		return cached;
	}

	async fetch(): Promise<void> {
		this.#status = "loading";
		try {
			const { workspaces } = await api.getWorkspaces();
			this.#items = workspaces;
			this.#status = "ready";
		} catch {
			this.#status = "error";
		}
	}

	async fetchOne(id: string): Promise<void> {
		try {
			const workspace = await api.getWorkspace(id);
			const idx = this.#items.findIndex((w) => w.id === id);
			if (idx === -1) {
				this.#items = [...this.#items, workspace];
			} else {
				this.#items = this.#items.map((w, i) => (i === idx ? workspace : w));
			}
			if (this.#status !== "ready") {
				this.#status = "ready";
			}
		} catch (error) {
			console.error("[WorkspaceStore] Failed to fetch workspace:", id, error);
		}
	}

	async validate(path: string, sourceType: "local" | "git"): Promise<WorkspaceValidationResult> {
		return api.validateWorkspace({ path, sourceType });
	}

	async create(data: CreateWorkspaceRequest): Promise<Workspace> {
		const created = await api.createWorkspace(data);
		await this.fetchOne(created.id);
		return this.#items.find((w) => w.id === created.id)!;
	}
}
