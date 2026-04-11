import * as Sentry from "@sentry/sveltekit";

const dsn = import.meta.env.PUBLIC_SENTRY_DSN;
const release = import.meta.env.PUBLIC_SENTRY_RELEASE;
const dist = import.meta.env.PUBLIC_SENTRY_DIST;
const gitCommit = import.meta.env.PUBLIC_SENTRY_GIT_COMMIT;
const gitTag = import.meta.env.PUBLIC_SENTRY_GIT_TAG;
const appVersion = import.meta.env.PUBLIC_SENTRY_APP_VERSION;
const sentryEnabled = Boolean(dsn);

const tags = {
	...(appVersion ? { app_version: appVersion } : {}),
	...(gitCommit ? { git_commit: gitCommit } : {}),
	...(gitTag ? { git_tag: gitTag } : {}),
};

Sentry.init({
	dsn,
	enabled: sentryEnabled,
	environment: import.meta.env.MODE,
	...(release ? { release } : {}),
	...(dist ? { dist } : {}),
	initialScope: {
		tags,
	},
});

console.info("[sentry] initialized", {
	enabled: sentryEnabled,
	environment: import.meta.env.MODE,
	...(release ? { release } : {}),
	...(dist ? { dist } : {}),
});

export const handleError = Sentry.handleErrorWithSentry();
