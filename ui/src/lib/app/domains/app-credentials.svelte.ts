import { createMutation, createQuery, queryOptions } from "@tanstack/svelte-query";
import type { QueryClient } from "@tanstack/svelte-query";

import { api } from "$lib/api-client";
import type { AppCredentials } from "$lib/app/app-context.types";
import { appQueryKeys } from "$lib/app/query/app-query-keys";
import type {
	AuthProvider,
	CodexAuthorizeResponse,
	CodexExchangeRequest,
	CodexExchangeResponse,
	CredentialAuthType,
	CredentialInfo,
	GitHubDeviceCodeRequest,
	GitHubDeviceCodeResponse,
	GitHubPollRequest,
	GitHubPollResponse,
	OAuthAuthorizeResponse,
	OAuthExchangeRequest,
	OAuthExchangeResponse,
	OAuthRefreshResponse,
} from "$lib/api-types";

type CreateAppCredentialsDomainArgs = {
	queryClient: QueryClient;
};

function authProvidersQueryOptions() {
	return queryOptions({
		queryKey: appQueryKeys.authProviders(),
		queryFn: async (): Promise<AuthProvider[]> => {
			const { authProviders } = await api.getAuthProviders();
			return authProviders;
		},
	});
}

function credentialsQueryOptions() {
	return queryOptions({
		queryKey: appQueryKeys.credentials(),
		queryFn: async (): Promise<CredentialInfo[]> => {
			const { credentials } = await api.getCredentials();
			return credentials;
		},
	});
}

export function createAppCredentialsDomain(
	args: CreateAppCredentialsDomainArgs,
): AppCredentials {
	const providersQuery = createQuery(() => authProvidersQueryOptions());
	const credentialsQuery = createQuery(() => credentialsQueryOptions());

	const createCredentialMutation = createMutation(() => ({
		mutationFn: async ({
			provider,
			authType,
			apiKey,
		}: {
			provider: string;
			authType: CredentialAuthType;
			apiKey: string;
		}) =>
			api.createCredential({
				provider,
				authType,
				apiKey,
			}),
		onSuccess: (credential) => {
			args.queryClient.setQueryData<CredentialInfo[]>(
				appQueryKeys.credentials(),
				(previous) => {
					const next = previous ? [...previous] : [];
					const existingIndex = next.findIndex((item) => item.provider === credential.provider);
					if (existingIndex >= 0) {
						next[existingIndex] = credential;
					} else {
						next.push(credential);
					}
					return next;
				},
			);
			void args.queryClient.invalidateQueries({ queryKey: appQueryKeys.models() });
		},
	}));

	const removeCredentialMutation = createMutation(() => ({
		mutationFn: async (provider: string) => {
			await api.deleteCredential(provider);
			return provider;
		},
		onSuccess: (provider) => {
			args.queryClient.setQueryData<CredentialInfo[]>(
				appQueryKeys.credentials(),
				(previous) => (previous ?? []).filter((credential) => credential.provider !== provider),
			);
			void args.queryClient.invalidateQueries({ queryKey: appQueryKeys.models() });
		},
	}));

	return {
		get list() {
			return credentialsQuery.data ?? [];
		},
		get providers() {
			return providersQuery.data ?? [];
		},
		get: (idOrProvider: string) =>
			(credentialsQuery.data ?? []).find(
				(credential) => credential.id === idOrProvider || credential.provider === idOrProvider,
			) ?? null,
		refresh: async () => {
			await Promise.all([providersQuery.refetch(), credentialsQuery.refetch()]);
		},
		create: async (provider: string, authType: CredentialAuthType, apiKey: string) => {
			return createCredentialMutation.mutateAsync({ provider, authType, apiKey });
		},
		update: async (provider: string, authType: CredentialAuthType, apiKey: string) => {
			return createCredentialMutation.mutateAsync({ provider, authType, apiKey });
		},
		remove: async (provider: string) => {
			await removeCredentialMutation.mutateAsync(provider);
		},
		refreshCredential: async (provider: string) => {
			const response = await api.refreshCredential(provider);
			await credentialsQuery.refetch();
			void args.queryClient.invalidateQueries({ queryKey: appQueryKeys.models() });
			return response;
		},
		anthropicAuthorize: (): Promise<OAuthAuthorizeResponse> => api.anthropicAuthorize(),
		anthropicExchange: async (data: OAuthExchangeRequest) => {
			const response = await api.anthropicExchange(data);
			await credentialsQuery.refetch();
			void args.queryClient.invalidateQueries({ queryKey: appQueryKeys.models() });
			return response;
		},
		githubDeviceCode: (data?: GitHubDeviceCodeRequest): Promise<GitHubDeviceCodeResponse> =>
			api.githubDeviceCode(data),
		githubPoll: async (data: GitHubPollRequest) => {
			const response = await api.githubPoll(data);
			if (response.status === "success") {
				await credentialsQuery.refetch();
				void args.queryClient.invalidateQueries({ queryKey: appQueryKeys.models() });
			}
			return response;
		},
		codexAuthorize: (): Promise<CodexAuthorizeResponse> => api.codexAuthorize(),
		codexExchange: async (data: CodexExchangeRequest) => {
			const response = await api.codexExchange(data);
			await credentialsQuery.refetch();
			void args.queryClient.invalidateQueries({ queryKey: appQueryKeys.models() });
			return response;
		},
	};
}
