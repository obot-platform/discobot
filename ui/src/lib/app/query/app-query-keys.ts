export const APP_QUERY_SCOPE = "app" as const;

export const appQueryKeys = {
	all: () => [APP_QUERY_SCOPE] as const,
	workspaces: () => [APP_QUERY_SCOPE, "workspaces"] as const,
	models: () => [APP_QUERY_SCOPE, "models"] as const,
	agents: () => [APP_QUERY_SCOPE, "agents"] as const,
	authProviders: () => [APP_QUERY_SCOPE, "auth-providers"] as const,
	credentials: () => [APP_QUERY_SCOPE, "credentials"] as const,
	supportInfo: () => [APP_QUERY_SCOPE, "support-info"] as const,
	sessions: () => [APP_QUERY_SCOPE, "sessions"] as const,
};
