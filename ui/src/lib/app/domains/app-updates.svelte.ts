import type { AppUpdates, UpdateStatus } from "$lib/app/app-context.types";
import {
	checkForAppUpdate,
	closeAppUpdate,
	downloadAppUpdate,
	supportsAppUpdates,
	installAppUpdate,
	relaunchApp,
	getDesktopRuntimeKind,
	type DesktopDownloadEvent,
} from "$lib/shell";
import type { UIStateStore } from "$lib/store/ui-state.store.svelte";
import { env as publicEnv } from "$env/dynamic/public";

const UPDATE_CHECK_INTERVAL_MS = 60 * 60 * 1000;
const GITHUB_RELEASES_API_URL =
	publicEnv.PUBLIC_DISCOBOT_RELEASES_API_URL ?? "";

type PendingUpdate = {
	updateRid: number;
	bytesRid: number | null;
};

type CreateAppUpdatesDomainArgs = {
	uiStateStore: UIStateStore;
};

type GitHubReleaseAsset = {
	name: string;
	browser_download_url: string;
};

type GitHubRelease = {
	prerelease: boolean;
	draft: boolean;
	tag_name: string;
	assets: GitHubReleaseAsset[];
};

function prereleaseAssetNames(): string[] {
	if (getDesktopRuntimeKind() !== "electron") {
		return ["latest.json"];
	}
	const platform = navigator.platform.toLowerCase();
	if (platform.includes("mac")) {
		return ["latest-mac.yml", "latest.yml"];
	}
	if (platform.includes("win")) {
		return ["latest.yml"];
	}
	return ["latest-linux.yml", "latest.yml"];
}

export function createAppUpdatesDomain(
	args: CreateAppUpdatesDomainArgs,
): AppUpdates {
	const { uiStateStore } = args;
	let updateStatus = $state<UpdateStatus>("idle");
	let availableVersion = $state<string | null>(null);
	let updateError = $state<string | null>(null);
	let downloadedBytes = $state(0);
	let totalBytes = $state<number | null>(null);
	let ignoredUpdateVersion = $state<string | null>(
		uiStateStore.ignoredUpdateVersion,
	);
	let trackPrereleases = $state(uiStateStore.trackPrereleases);
	const canTrackPrereleases = GITHUB_RELEASES_API_URL.length > 0;

	const isUpdateIgnored = $derived.by(
		() =>
			availableVersion !== null && ignoredUpdateVersion === availableVersion,
	);
	const showUpdateBadge = $derived.by(
		() =>
			updateStatus === "ready" && availableVersion !== null && !isUpdateIgnored,
	);

	let updateCheckInFlight = false;
	let pendingUpdate: PendingUpdate | null = null;

	async function closePendingUpdate(): Promise<void> {
		if (!pendingUpdate) return;

		const update = pendingUpdate;
		pendingUpdate = null;

		try {
			await closeAppUpdate(update.updateRid, update.bytesRid);
		} catch {
			// Ignore cleanup failures.
		}
	}

	function resetProgress(): void {
		downloadedBytes = 0;
		totalBytes = null;
	}

	function logUpdateError(action: "check" | "install", error: unknown): void {
		console.error(`[updates] ${action} failed`, {
			error,
			message: error instanceof Error ? error.message : String(error),
			stack: error instanceof Error ? error.stack : undefined,
			status: updateStatus,
			availableVersion,
			runtime: getDesktopRuntimeKind(),
		});
	}

	async function resolveUpdateEndpoint(): Promise<string | null> {
		if (!trackPrereleases) {
			return null;
		}
		if (!canTrackPrereleases) {
			return null;
		}

		const response = await fetch(GITHUB_RELEASES_API_URL, {
			headers: {
				Accept: "application/vnd.github+json",
			},
		});
		if (!response.ok) {
			throw new Error(
				`Failed to query GitHub pre-releases: ${response.status} ${response.statusText}`,
			);
		}

		const releases = (await response.json()) as GitHubRelease[];
		const release = releases.find(
			(release) => release.prerelease && !release.draft,
		);
		if (!release) {
			throw new Error("No GitHub pre-release is available.");
		}

		const assetNames = prereleaseAssetNames();
		const releaseAsset = release.assets.find((asset) =>
			assetNames.includes(asset.name),
		);
		if (!releaseAsset) {
			throw new Error(
				`GitHub pre-release ${release.tag_name} does not include ${assetNames.join(" or ")}.`,
			);
		}

		return releaseAsset.browser_download_url;
	}

	$effect(() => {
		if (!supportsAppUpdates()) {
			return;
		}

		void checkForUpdates();

		const intervalId = window.setInterval(() => {
			void checkForUpdates();
		}, UPDATE_CHECK_INTERVAL_MS);

		return () => {
			window.clearInterval(intervalId);
		};
	});

	async function checkForUpdates(): Promise<void> {
		if (updateCheckInFlight) return;
		if (updateStatus === "downloading" || updateStatus === "installing") {
			return;
		}

		updateCheckInFlight = true;
		updateStatus = "checking";
		updateError = null;
		resetProgress();

		try {
			await closePendingUpdate();

			const nextUpdate = await checkForAppUpdate(await resolveUpdateEndpoint());
			if (!nextUpdate) {
				availableVersion = null;
				updateStatus = "idle";
				return;
			}

			availableVersion = nextUpdate.version;

			if (ignoredUpdateVersion === nextUpdate.version) {
				await closeAppUpdate(nextUpdate.rid, null);
				updateStatus = "ready";
				return;
			}

			pendingUpdate = {
				updateRid: nextUpdate.rid,
				bytesRid: null,
			};
			updateStatus = "downloading";
			const bytesRid = await downloadAppUpdate(
				nextUpdate.rid,
				(event: DesktopDownloadEvent) => {
					switch (event.event) {
						case "Started":
							totalBytes = event.data?.contentLength ?? null;
							downloadedBytes = 0;
							break;
						case "Progress":
							downloadedBytes += event.data?.chunkLength ?? 0;
							break;
						case "Finished":
							if (totalBytes !== null) {
								downloadedBytes = totalBytes;
							}
							break;
					}
				},
			);
			pendingUpdate.bytesRid = bytesRid;
			updateStatus = "ready";
		} catch (error) {
			logUpdateError("check", error);
			await closePendingUpdate();
			resetProgress();
			updateStatus = "error";
			updateError =
				error instanceof Error ? error.message : "Failed to check for updates";
		} finally {
			updateCheckInFlight = false;
		}
	}

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
		get canTrackPrereleases() {
			return canTrackPrereleases;
		},
		get trackPrereleases() {
			return canTrackPrereleases && trackPrereleases;
		},
		check: async () => {
			if (!supportsAppUpdates()) {
				updateStatus = "error";
				updateError = "App updates are only available in the desktop app.";
				return;
			}

			await checkForUpdates();
		},
		installAndRelaunch: async () => {
			if (updateStatus !== "ready" || !pendingUpdate) return;
			if (!supportsAppUpdates()) {
				updateStatus = "error";
				updateError = "App updates are only available in the desktop app.";
				return;
			}

			updateStatus = "installing";
			updateError = null;
			try {
				if (pendingUpdate.bytesRid === null) {
					throw new Error("Update download is not ready yet.");
				}

				await installAppUpdate(pendingUpdate.updateRid, pendingUpdate.bytesRid);
				await relaunchApp();
			} catch (error) {
				logUpdateError("install", error);
				updateStatus = "error";
				updateError = error instanceof Error ? error.message : "Install failed";
			} finally {
				await closePendingUpdate();
			}
		},
		ignore: () => {
			if (!availableVersion) return;
			void closePendingUpdate();
			resetProgress();
			uiStateStore.ignoreUpdateVersion(availableVersion);
			ignoredUpdateVersion = uiStateStore.ignoredUpdateVersion;
		},
		setTrackPrereleases: async (value: boolean) => {
			if (!canTrackPrereleases) return;
			if (trackPrereleases === value) return;

			trackPrereleases = value;
			uiStateStore.setTrackPrereleases(value);
			await closePendingUpdate();
			availableVersion = null;
			updateError = null;
			resetProgress();
			updateStatus = "idle";
			await checkForUpdates();
		},
	};
}
