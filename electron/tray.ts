import { nativeImage, type BrowserWindow, Menu, Tray, app } from "electron";
import path from "node:path";

function trayIconPath(): string {
  return path.join(
    app.getAppPath(),
    "src-tauri",
    "icons",
    "tray-icon.png",
  );
}

function trayIcon(): Electron.NativeImage {
  const icon = nativeImage.createFromPath(trayIconPath());
  if (process.platform === "darwin") {
    const resized = icon.resize({ width: 18, height: 18 });
    resized.setTemplateImage(true);
    return resized;
  }
  if (process.platform === "linux") {
    return icon.resize({ width: 22, height: 22 });
  }
  if (process.platform === "win32") {
    return icon.resize({ width: 16, height: 16 });
  }
  return icon;
}

export function showMainWindow(window: BrowserWindow): void {
  if (process.platform === "darwin") {
    app.dock.show();
  }
  window.show();
  if (window.isMinimized()) {
    window.restore();
  }
  window.focus();
}

export function hideMainWindow(window: BrowserWindow): void {
  window.hide();
  if (process.platform === "darwin") {
    app.dock.hide();
  }
}

export function setupTray(window: BrowserWindow): Tray {
  const tray = new Tray(trayIcon());
  tray.setToolTip("Discobot");
  tray.setContextMenu(
    Menu.buildFromTemplate([
      { label: "Show", click: () => showMainWindow(window) },
      { type: "separator" },
      { label: "Quit", click: () => app.quit() },
    ]),
  );
  tray.on("click", () => {
    if (window.isVisible() && window.isFocused()) {
      hideMainWindow(window);
      return;
    }
    showMainWindow(window);
  });
  return tray;
}
