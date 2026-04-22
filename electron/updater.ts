import { app, ipcMain, type IpcMainInvokeEvent } from "electron";
import electronUpdater from "electron-updater";
import type { UpdateInfo } from "electron-updater";
import type {
  DesktopDownloadEvent,
  DesktopUpdateMetadata,
} from "../ui/src/lib/desktop/types";

const { autoUpdater } = electronUpdater;

type PendingUpdate = {
  metadata: DesktopUpdateMetadata;
  info: UpdateInfo;
};

let nextRid = 1;
const pendingUpdates = new Map<number, PendingUpdate>();

function currentVersion(): string {
  return app.getVersion();
}

function toMetadata(info: UpdateInfo): DesktopUpdateMetadata {
  const rid = nextRid++;
  return {
    rid,
    currentVersion: currentVersion(),
    version: info.version,
    date: typeof info.releaseDate === "string" ? info.releaseDate : undefined,
    body: info.releaseNotes
      ? typeof info.releaseNotes === "string"
        ? info.releaseNotes
        : info.releaseNotes.map((note) => note.note).join("\n\n")
      : undefined,
    rawJson: info as unknown as Record<string, unknown>,
  };
}

function senderEventName(rid: number): string {
  return `desktop:update-download:${rid}`;
}

function sendDownloadEvent(
  event: IpcMainInvokeEvent,
  rid: number,
  payload: DesktopDownloadEvent,
): void {
  event.sender.send(senderEventName(rid), payload);
}

function resolveFeedUrl(endpoint?: string | null): string | null {
  if (!endpoint) {
    return null;
  }
  try {
    const url = new URL(endpoint);
    const pathname = url.pathname;
    if (!pathname.endsWith(".yml")) {
      return null;
    }
    return endpoint.slice(
      0,
      endpoint.length - pathname.split("/").pop()!.length,
    );
  } catch {
    return null;
  }
}

export function configureUpdater(): void {
  autoUpdater.autoDownload = false;
  autoUpdater.autoInstallOnAppQuit = false;
  autoUpdater.allowPrerelease = true;
  if (!app.isPackaged) {
    autoUpdater.forceDevUpdateConfig = true;
  }
}

export function registerUpdaterHandlers(): void {
  ipcMain.handle(
    "desktop:update-check",
    async (_event, endpoint?: string | null) => {
      if (!app.isPackaged) {
        return null;
      }

      const feedUrl = resolveFeedUrl(endpoint);
      if (feedUrl) {
        autoUpdater.setFeedURL({
          provider: "generic",
          url: feedUrl,
          channel: "latest",
        });
      }

      const result = await autoUpdater.checkForUpdates();
      const info = result?.updateInfo;
      if (!info || info.version === currentVersion()) {
        return null;
      }

      const metadata = toMetadata(info);
      pendingUpdates.set(metadata.rid, {
        metadata,
        info,
      });
      return metadata;
    },
  );

  ipcMain.handle("desktop:update-download", async (event, rid: number) => {
    const pending = pendingUpdates.get(rid);
    if (!pending) {
      throw new Error(`Unknown update resource id: ${rid}`);
    }

    sendDownloadEvent(event, rid, {
      event: "Started",
      data: {
        contentLength:
          typeof pending.info.files?.[0]?.size === "number"
            ? pending.info.files[0].size
            : undefined,
      },
    });

    let downloadedBytes = 0;
    const progress = (info: {
      bytesPerSecond: number;
      percent: number;
      transferred: number;
      total: number;
    }) => {
      const chunkLength = Math.max(0, info.transferred - downloadedBytes);
      downloadedBytes = info.transferred;
      sendDownloadEvent(event, rid, {
        event: "Progress",
        data: {
          chunkLength,
        },
      });
    };
    const downloaded = () => {
      sendDownloadEvent(event, rid, { event: "Finished" });
    };

    autoUpdater.on("download-progress", progress);
    autoUpdater.once("update-downloaded", downloaded);

    try {
      await autoUpdater.downloadUpdate();
      return rid;
    } finally {
      autoUpdater.removeListener("download-progress", progress);
      autoUpdater.removeListener("update-downloaded", downloaded);
    }
  });

  ipcMain.handle("desktop:update-install", async (_event, payload) => {
    const pending = pendingUpdates.get(payload.updateRid);
    if (!pending) {
      throw new Error(`Unknown update resource id: ${payload.updateRid}`);
    }
    autoUpdater.quitAndInstall();
  });

  ipcMain.handle("desktop:update-close", async (_event, payload) => {
    if (typeof payload?.updateRid === "number") {
      pendingUpdates.delete(payload.updateRid);
    }
  });
}
