import {
	delay,
	IGNORED_UPDATE_VERSION_STORAGE_KEY,
	readIgnoredUpdateVersion,
	writeStorage,
} from "$lib/app/app-helpers";
import type { AppUpdates, UpdateStatus } from "$lib/app/app-context.types";

export function createAppUpdatesDomain(): AppUpdates {
	let updateStatus = $state<UpdateStatus>("idle");
	let availableVersion = $state<string | null>(null);
	let updateError = $state<string | null>(null);
	let downloadedBytes = $state(0);
	let totalBytes = $state<number | null>(null);
	let ignoredUpdateVersion = $state<string | null>(readIgnoredUpdateVersion());

	const isUpdateIgnored = $derived.by(
		() =>
			availableVersion !== null && ignoredUpdateVersion === availableVersion,
	);
	const showUpdateBadge = $derived.by(
		() =>
			updateStatus === "ready" && availableVersion !== null && !isUpdateIgnored,
	);

	const updateVersion = "0.0.0-dev+1";
	let updateCheckInFlight = false;

	return {
		get status() {
			return updateStatus;
		},
		get availableVersion() {
			return availableVersion;
		},
		get error() {
			return updateError;
		},
		get downloadedBytes() {
			return downloadedBytes;
		},
		get totalBytes() {
			return totalBytes;
		},
		get isIgnored() {
			return isUpdateIgnored;
		},
		get showBadge() {
			return showUpdateBadge;
		},
		check: async () => {
			if (updateCheckInFlight) return;
			if (updateStatus === "downloading" || updateStatus === "installing")
				return;

			updateCheckInFlight = true;
			updateStatus = "checking";
			updateError = null;

			try {
				await delay(300);
				availableVersion = updateVersion;

				if (isUpdateIgnored) {
					updateStatus = "ready";
					totalBytes = null;
					downloadedBytes = 0;
					return;
				}

				updateStatus = "downloading";
				totalBytes = 24 * 1024 * 1024;
				downloadedBytes = 0;
				for (let step = 1; step <= 8; step += 1) {
					await delay(90);
					if (updateStatus !== "downloading") return;
					downloadedBytes = Math.round(((totalBytes ?? 0) / 8) * step);
				}

				updateStatus = "ready";
			} catch (error) {
				updateStatus = "error";
				updateError =
					error instanceof Error
						? error.message
						: "Failed to check for updates";
			} finally {
				updateCheckInFlight = false;
			}
		},
		installAndRelaunch: async () => {
			if (updateStatus !== "ready") return;

			updateStatus = "installing";
			updateError = null;
			try {
				await delay(700);
				updateStatus = "idle";
				availableVersion = null;
				totalBytes = null;
				downloadedBytes = 0;
				ignoredUpdateVersion = null;
				writeStorage(IGNORED_UPDATE_VERSION_STORAGE_KEY, null);
			} catch (error) {
				updateStatus = "error";
				updateError = error instanceof Error ? error.message : "Install failed";
			}
		},
		ignore: () => {
			if (!availableVersion) return;
			ignoredUpdateVersion = availableVersion;
			writeStorage(IGNORED_UPDATE_VERSION_STORAGE_KEY, availableVersion);
		},
	};
}
