import { delay } from "$lib/app/app-helpers";
import { ignoreVersionPreference } from "$lib/app/services/app-preferences-service";
import type { AppStore } from "$lib/app/store/app-store.svelte";

export type AppUpdateService = {
	checkForUpdate: () => Promise<void>;
	installAndRelaunch: () => Promise<void>;
	ignoreVersion: () => void;
};

type CreateAppUpdateServiceArgs = {
	store: AppStore;
};

export function createAppUpdateService(args: CreateAppUpdateServiceArgs): AppUpdateService {
	const updateVersion = "0.0.0-dev+1";
	let updateCheckInFlight = false;

	return {
		checkForUpdate: async () => {
			if (updateCheckInFlight) {
				return;
			}
			if (args.store.updateStatus === "downloading" || args.store.updateStatus === "installing") {
				return;
			}

			updateCheckInFlight = true;
			args.store.updateStatus = "checking";
			args.store.updateError = null;

			try {
				await delay(300);
				args.store.availableVersion = updateVersion;

				if (args.store.isUpdateIgnored) {
					args.store.updateStatus = "ready";
					args.store.totalBytes = null;
					args.store.downloadedBytes = 0;
					return;
				}

				args.store.updateStatus = "downloading";
				args.store.totalBytes = 24 * 1024 * 1024;
				args.store.downloadedBytes = 0;
				for (let step = 1; step <= 8; step += 1) {
					await delay(90);
					if (args.store.updateStatus !== "downloading") {
						return;
					}
					args.store.downloadedBytes = Math.round((args.store.totalBytes / 8) * step);
				}

				args.store.updateStatus = "ready";
			} catch (error) {
				args.store.updateStatus = "error";
				args.store.updateError =
					error instanceof Error ? error.message : "Failed to check for updates";
			} finally {
				updateCheckInFlight = false;
			}
		},
		installAndRelaunch: async () => {
			if (args.store.updateStatus !== "ready") {
				return;
			}

			args.store.updateStatus = "installing";
			args.store.updateError = null;
			try {
				await delay(700);
				args.store.updateStatus = "idle";
				args.store.availableVersion = null;
				args.store.totalBytes = null;
				args.store.downloadedBytes = 0;
				args.store.ignoredUpdateVersion = null;
				ignoreVersionPreference(null);
			} catch (error) {
				args.store.updateStatus = "error";
				args.store.updateError = error instanceof Error ? error.message : "Install failed";
			}
		},
		ignoreVersion: () => {
			if (!args.store.availableVersion) {
				return;
			}
			args.store.ignoredUpdateVersion = args.store.availableVersion;
			ignoreVersionPreference(args.store.availableVersion);
		},
	};
}
