import { api } from "$lib/api-client";
import type {
	CredentialAuthType,
	CredentialEnvVar,
	CredentialInfo,
	CredentialType,
} from "$lib/api-types";
import type { AsyncStatus } from "$lib/shell-types";

import { RequestCoalescer } from "./request-coalescer";

export class CredentialStore {
	#items = $state<CredentialInfo[]>([]);
	#credentialTypes = $state<CredentialType[]>([]);
	#status = $state<AsyncStatus>("idle");
	#fetchRequests = new RequestCoalescer<"list">();

	get list(): CredentialInfo[] {
		return this.#items;
	}

	get credentialTypes(): CredentialType[] {
		return this.#credentialTypes;
	}

	get status(): AsyncStatus {
		return this.#status;
	}

	/** Returns the cached credential. Triggers a background fetch of the full list on cache miss. */
	get(idOrProvider: string): CredentialInfo | null {
		const cached =
			this.#items.find(
				(c) => c.id === idOrProvider || c.provider === idOrProvider,
			) ?? null;
		if (cached === null && this.#status === "idle") {
			void this.fetch();
		}
		return cached;
	}

	async fetch(): Promise<void> {
		return this.#fetchRequests.run("list", async () => {
			this.#status = "loading";
			try {
				const [{ credentialTypes }, { credentials }] = await Promise.all([
					api.getCredentialTypes(),
					api.getCredentials(),
				]);
				this.#credentialTypes = credentialTypes;
				this.#items = credentials;
				this.#status = "ready";
			} catch {
				this.#status = "error";
			}
		});
	}

	async save(data: {
		provider?: string;
		credentialId?: string;
		name: string;
		description?: string;
		authType: CredentialAuthType;
		apiKey?: string;
		envVars?: CredentialEnvVar[];
		agentVisible?: boolean;
	}): Promise<CredentialInfo> {
		await api.createCredential(data);
		await this.fetch();
		return (
			(data.credentialId
				? this.#items.find((c) => c.id === data.credentialId)
				: null) ??
			(data.provider
				? this.#items.find((c) => c.provider === data.provider)
				: null) ??
			this.#items[this.#items.length - 1]!
		);
	}

	async getOne(identifier: string): Promise<CredentialInfo> {
		return api.getCredential(identifier);
	}

	async remove(identifier: string): Promise<void> {
		try {
			await api.deleteCredential(identifier);
			this.#items = this.#items.filter(
				(c) => c.id !== identifier && c.provider !== identifier,
			);
			return;
		} catch (error) {
			await this.fetch();
			const stillExists = this.#items.some(
				(c) => c.id === identifier || c.provider === identifier,
			);
			if (!stillExists) {
				return;
			}
			throw error;
		}
	}
}
