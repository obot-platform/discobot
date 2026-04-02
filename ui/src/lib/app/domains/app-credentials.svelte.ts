import { api } from "$lib/api-client";
import type { AppCredentials } from "$lib/app/app-context.types";
import type {
	CodexDeviceCodeResponse,
	CodexPollRequest,
	CodexPollResponse,
	CredentialAuthType,
	CredentialInfo,
	GitHubDeviceCodeRequest,
	GitHubDeviceCodeResponse,
	GitHubPollRequest,
	OAuthAuthorizeResponse,
	OAuthExchangeRequest,
	OAuthExchangeResponse,
	OAuthRefreshResponse,
} from "$lib/api-types";
import type { CredentialStore } from "$lib/store/credentials.store.svelte";

type CreateAppCredentialsDomainArgs = {
	store: CredentialStore;
	refreshModels: () => Promise<void>;
};

export function createAppCredentialsDomain(
	args: CreateAppCredentialsDomainArgs,
): AppCredentials {
	const { store } = args;

	const saveCredential = async (data: {
		provider?: string;
		credentialId?: string;
		name: string;
		description?: string;
		authType: CredentialAuthType;
		apiKey?: string;
		envVars?: { key: string; value: string }[];
		agentVisible?: boolean;
		inactive?: boolean;
	}): Promise<CredentialInfo> => {
		const credential = await store.save(data);
		void args.refreshModels();
		return credential;
	};

	return {
		get list() {
			return store.list;
		},
		get credentialTypes() {
			return store.credentialTypes;
		},
		get: (idOrProvider) => store.get(idOrProvider),
		refresh: () => store.fetch(),
		create: saveCredential,
		update: saveCredential,
		remove: async (provider) => {
			await store.remove(provider);
			void args.refreshModels();
		},
		refreshCredential: async (provider) => {
			const response = await api.refreshCredential(provider);
			await store.fetch();
			void args.refreshModels();
			return response as OAuthRefreshResponse;
		},
		anthropicAuthorize: (): Promise<OAuthAuthorizeResponse> =>
			api.anthropicAuthorize(),
		anthropicExchange: async (
			data: OAuthExchangeRequest,
		): Promise<OAuthExchangeResponse> => {
			const response = await api.anthropicExchange(data);
			await store.fetch();
			void args.refreshModels();
			return response;
		},
		githubDeviceCode: (
			data?: GitHubDeviceCodeRequest,
		): Promise<GitHubDeviceCodeResponse> => api.githubDeviceCode(data),
		githubPoll: async (data: GitHubPollRequest) => {
			const response = await api.githubPoll(data);
			if (response.status === "success") {
				await store.fetch();
				void args.refreshModels();
			}
			return response;
		},
		codexDeviceCode: (): Promise<CodexDeviceCodeResponse> =>
			api.codexDeviceCode(),
		codexPoll: async (data: CodexPollRequest): Promise<CodexPollResponse> => {
			const response = await api.codexPoll(data);
			if (response.status === "success") {
				await store.fetch();
				void args.refreshModels();
			}
			return response;
		},
	};
}
