import { getAppEnvironment } from "$lib/app/app-helpers";
import type {
	AppContextBootstrap,
	AppEnvironment,
} from "$lib/app/app-context.types";

type CreateAppEnvironmentDomainArgs = {
	bootstrap: AppContextBootstrap;
};

export function createAppEnvironmentDomain(
	args: CreateAppEnvironmentDomainArgs,
): AppEnvironment {
	const env = getAppEnvironment();
	return {
		apiBase: env.apiBase,
		runtime: env.runtime,
		isDesktop: env.isDesktop,
		supportsNativeWindowControls: env.supportsNativeWindowControls,
		supportsAppUpdates: env.supportsAppUpdates,
		windowControlsSide: env.windowControlsSide,
		windowControls: args.bootstrap.windowControls,
	};
}
