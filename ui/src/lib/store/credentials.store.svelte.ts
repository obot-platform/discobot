import { api } from "$lib/api-client";
import type {
	CredentialAuthType,
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

	async save(
		provider: string,
		authType: CredentialAuthType,
		apiKey: string,
	): Promise<CredentialInfo> {
		await api.createCredential({ provider, authType, apiKey });
		await this.fetch();
		return this.#items.find((c) => c.provider === provider)!;
	}

	async remove(provider: string): Promise<void> {
		await api.deleteCredential(provider);
		this.#items = this.#items.filter((c) => c.provider !== provider);
	}
}
