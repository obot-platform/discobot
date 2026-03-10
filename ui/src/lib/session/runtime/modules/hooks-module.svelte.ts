import type { SessionData } from "$lib/shell-types";
import type { Getter, Setter } from "$lib/session/runtime/modules/module-context";
import type { SessionHooksModule } from "$lib/session/runtime/session-runtime.types";

type CreateSessionHooksModuleArgs = {
	getSessionId: Getter<string | null>;
	getSessionDataById: Getter<Record<string, SessionData>>;
	setSessionDataById: Setter<Record<string, SessionData>>;
	getStatus: Getter<NonNullable<SessionData["hooksStatus"]>>;
	getOutputById: Getter<Record<string, string>>;
	nowIsoString: () => string;
};

export function createSessionHooksModule(args: CreateSessionHooksModuleArgs): SessionHooksModule {
	const rerun = (hookId: string) => {
		const sessionId = args.getSessionId();
		if (!sessionId) {
			return;
		}
		const stableSessionId = sessionId;

		const sessionDataById = args.getSessionDataById();
		const currentSession = sessionDataById[stableSessionId];
		if (!currentSession?.hooksStatus) {
			return;
		}

		const targetHook = currentSession.hooksStatus.hooks.find((hook) => hook.hookId === hookId);
		if (!targetHook || targetHook.lastResult === "running") {
			return;
		}

		const startedAt = args.nowIsoString();
		const previousOutput = currentSession.hookOutputById?.[hookId] ?? "";

		args.setSessionDataById({
			...sessionDataById,
			[stableSessionId]: {
				...currentSession,
				hooksStatus: {
					...currentSession.hooksStatus,
					pendingHookIds: currentSession.hooksStatus.pendingHookIds.filter((id) => id !== hookId),
					hooks: currentSession.hooksStatus.hooks.map((hook) =>
						hook.hookId === hookId
							? {
								...hook,
								lastResult: "running",
								lastRunAt: startedAt,
								lastExitCode: undefined,
								runCount: hook.runCount + 1,
							}
							: hook,
					),
				},
				hookOutputById: {
					...(currentSession.hookOutputById ?? {}),
					[hookId]: `${previousOutput}\n\n[rerun] ${startedAt} — rerun requested...`.trim(),
				},
			},
		});

		setTimeout(() => {
			const latestData = args.getSessionDataById();
			const latestSession = latestData[stableSessionId];
			if (!latestSession?.hooksStatus) {
				return;
			}

			const latestHook = latestSession.hooksStatus.hooks.find((hook) => hook.hookId === hookId);
			if (!latestHook) {
				return;
			}

			const finishedAt = args.nowIsoString();
			const latestOutput = latestSession.hookOutputById?.[hookId] ?? "";

			args.setSessionDataById({
				...latestData,
				[stableSessionId]: {
					...latestSession,
					hooksStatus: {
						...latestSession.hooksStatus,
						pendingHookIds: latestSession.hooksStatus.pendingHookIds.filter((id) => id !== hookId),
						hooks: latestSession.hooksStatus.hooks.map((hook) =>
							hook.hookId === hookId
								? {
									...hook,
									lastResult: "success",
									lastRunAt: finishedAt,
									lastExitCode: 0,
								}
								: hook,
						),
					},
					hookOutputById: {
						...(latestSession.hookOutputById ?? {}),
						[hookId]: `${latestOutput}\n[rerun] ${finishedAt} — completed successfully (exit 0).`.trim(),
					},
				},
			});
		}, 900);
	};

	return {
		get status() {
			return args.getStatus();
		},
		get outputById() {
			return args.getOutputById();
		},
		rerun,
	};
}
