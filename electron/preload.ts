import { contextBridge, ipcRenderer } from "electron";
import type {
  DesktopDownloadEvent,
  DesktopRendererBridge,
  DesktopServerConfig,
} from "../ui/src/lib/desktop/types";

type Unsubscribe = () => void;

const desktopAPI: DesktopRendererBridge = {
  kind: "electron",
  initServerConfig: () =>
    ipcRenderer.invoke(
      "desktop:init-server-config",
    ) as Promise<DesktopServerConfig | null>,
  downloadFile: (filename, bytes) =>
    ipcRenderer.invoke("desktop:download-file", {
      filename,
      bytes: Array.from(bytes),
    }) as Promise<string>,
  readClipboardText: () => ipcRenderer.invoke("desktop:clipboard-read"),
  writeClipboardText: (text) =>
    ipcRenderer.invoke("desktop:clipboard-write", text),
  openExternalUrl: (url) => ipcRenderer.invoke("desktop:open-external", url),
  pickDirectory: () => ipcRenderer.invoke("desktop:pick-directory"),
  windowMinimize: () => ipcRenderer.invoke("desktop:window-minimize"),
  windowMaximize: () => ipcRenderer.invoke("desktop:window-maximize"),
  windowUnmaximize: () => ipcRenderer.invoke("desktop:window-unmaximize"),
  windowIsMaximized: () => ipcRenderer.invoke("desktop:window-is-maximized"),
  windowClose: () => ipcRenderer.invoke("desktop:window-close"),
  windowIsFullscreen: () => ipcRenderer.invoke("desktop:window-is-fullscreen"),
  checkForAppUpdate: (endpoint) =>
    ipcRenderer.invoke("desktop:update-check", endpoint ?? null),
  downloadAppUpdate: async (rid, onEvent) => {
    const eventName = `desktop:update-download:${rid}`;
    const listener = (_event: unknown, payload: DesktopDownloadEvent) => {
      onEvent(payload);
    };
    ipcRenderer.on(eventName, listener);
    try {
      return await ipcRenderer.invoke("desktop:update-download", rid);
    } finally {
      ipcRenderer.removeListener(eventName, listener);
    }
  },
  installAppUpdate: (updateRid, bytesRid) =>
    ipcRenderer.invoke("desktop:update-install", { updateRid, bytesRid }),
  closeAppUpdate: (updateRid, bytesRid) =>
    ipcRenderer.invoke("desktop:update-close", {
      updateRid: updateRid ?? null,
      bytesRid: bytesRid ?? null,
    }),
  relaunchApp: () => ipcRenderer.invoke("desktop:relaunch"),
  onWindowResized: (listener): Unsubscribe => {
    const eventName = "desktop:window-resized";
    const wrapped = () => listener();
    ipcRenderer.on(eventName, wrapped);
    return () => {
      ipcRenderer.removeListener(eventName, wrapped);
    };
  },
};

contextBridge.exposeInMainWorld("__DISCOBOT_DESKTOP__", desktopAPI);
