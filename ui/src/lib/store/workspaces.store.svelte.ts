import { SvelteSet } from "svelte/reactivity";

import { api } from "$lib/api-client";
import type {
	CreateWorkspaceRequest,
	Workspace,
	WorkspaceValidationResult,
} from "$lib/api-types";
import type { AsyncStatus } from "$lib/shell-types";

import { RequestCoalescer } from "./request-coalescer";

export class WorkspaceStore {
	#items = $state<Workspace[]>([]);
	#status = $state<AsyncStatus>("idle");
	#inflight = new SvelteSet<string>();
	#fetchRequests = new RequestCoalescer<"list">();
	#fetchOneRequests = new RequestCoalescer<string>();

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
		return this.#fetchRequests.run("list", async () => {
			this.#status = "loading";
			try {
				const { workspaces } = await api.getWorkspaces();
				this.#items = workspaces;
				this.#status = "ready";
			} catch {
				this.#status = "error";
			}
		});
	}

	async fetchOne(id: string): Promise<void> {
		return this.#fetchOneRequests.run(id, async () => {
			try {
				const workspace = await api.getWorkspace(id);
				const idx = this.#items.findIndex((w) => w.id === id);
				if (idx === -1) {
					this.#items.push(workspace);
				} else {
					this.#items[idx] = workspace;
				}
				if (this.#status !== "ready") {
					this.#status = "ready";
				}
			} catch (error) {
				console.error("[WorkspaceStore] Failed to fetch workspace:", id, error);
			}
		});
	}

	async validate(
		path: string,
		sourceType: "local" | "git",
	): Promise<WorkspaceValidationResult> {
		return api.validateWorkspace({ path, sourceType });
	}

	async create(data: CreateWorkspaceRequest): Promise<Workspace> {
		const created = await api.createWorkspace(data);
		await this.fetchOne(created.id);
		return this.#items.find((w) => w.id === created.id)!;
	}

	async update(
		id: string,
		data: { path?: string; displayName?: string | null },
	): Promise<Workspace> {
		const updated = await api.updateWorkspace(id, data);
		const idx = this.#items.findIndex((workspace) => workspace.id === id);
		if (idx === -1) {
			this.#items.push(updated);
		} else {
			this.#items[idx] = updated;
		}
		return updated;
	}

	async remove(id: string, deleteFiles = false): Promise<void> {
		await api.deleteWorkspace(id, deleteFiles);
		this.#items = this.#items.filter((workspace) => workspace.id !== id);
	}
}
