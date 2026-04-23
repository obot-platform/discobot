import {
  app,
  BrowserWindow,
  clipboard,
  dialog,
  ipcMain,
  Menu,
  protocol,
  shell,
} from "electron";
import { access, mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import {
  createInitialServerState,
  getDesktopServerConfig,
  getElectronRendererURL,
  startBundledServer,
  stopBundledServer,
  type DesktopServerState,
} from "./server";
import { hideMainWindow, setupTray, showMainWindow } from "./tray";
import { configureUpdater, registerUpdaterHandlers } from "./updater";
import {
  applyWindowState,
  loadWindowState,
  restoreWindowState,
  trackWindowState,
} from "./window-state";

let mainWindow: BrowserWindow | null = null;
let serverState: DesktopServerState | null = null;
let mainTray: Electron.Tray | null = null;
let trayInitialized = false;

function uiBuildDir(): string {
  return path.join(app.getAppPath(), "server", "static", "ui", "dist");
}

function resolveAppAssetPath(requestURL: string): string | null {
  const requestedPath = decodeURIComponent(new URL(requestURL).pathname);
  const relativePath =
    requestedPath === "/" ? "index.html" : requestedPath.replace(/^\/+/, "");
  const buildDir = uiBuildDir();
  const filePath = path.normalize(path.join(buildDir, relativePath));
  const relative = path.relative(buildDir, filePath);
  if (relative.startsWith("..") || path.isAbsolute(relative)) {
    return null;
  }
  return filePath;
}

function contentTypeForAppAsset(filePath: string): string {
  switch (path.extname(filePath).toLowerCase()) {
    case ".html":
      return "text/html; charset=utf-8";
    case ".js":
      return "text/javascript; charset=utf-8";
    case ".css":
      return "text/css; charset=utf-8";
    case ".json":
      return "application/json; charset=utf-8";
    case ".svg":
      return "image/svg+xml";
    case ".png":
      return "image/png";
    case ".jpg":
    case ".jpeg":
      return "image/jpeg";
    case ".ico":
      return "image/x-icon";
    case ".woff":
      return "font/woff";
    case ".woff2":
      return "font/woff2";
    case ".ttf":
      return "font/ttf";
    default:
      return "application/octet-stream";
  }
}

async function resolveDownloadPath(filename: string): Promise<string> {
  const downloadsDir = app.getPath("downloads");
  await mkdir(downloadsDir, { recursive: true });

  const requestedName = path.basename(filename).trim();
  const safeName =
    requestedName && requestedName !== "." && requestedName !== ".."
      ? requestedName
      : "download";
  const parsed = path.parse(safeName);

  for (let index = 0; ; index += 1) {
    const candidateName =
      index === 0 ? safeName : `${parsed.name} (${index})${parsed.ext}`;
    const candidatePath = path.join(downloadsDir, candidateName);
    try {
      await access(candidatePath);
    } catch {
      return candidatePath;
    }
  }
}

protocol.registerSchemesAsPrivileged([
  {
    scheme: "app",
    privileges: {
      standard: true,
      secure: true,
      supportFetchAPI: true,
      corsEnabled: true,
    },
  },
]);

async function registerAppProtocol(): Promise<void> {
  if (!app.isPackaged) {
    return;
  }

  protocol.handle("app", async (request) => {
    const filePath = resolveAppAssetPath(request.url);
    if (!filePath) {
      return new Response("Not found", { status: 404 });
    }
    try {
      const asset = await readFile(filePath);
      return new Response(asset, {
        headers: {
          "content-type": contentTypeForAppAsset(filePath),
        },
      });
    } catch {
      return new Response("Not found", { status: 404 });
    }
  });
}

function currentWindow(event: Electron.IpcMainInvokeEvent): BrowserWindow {
  const window = BrowserWindow.fromWebContents(event.sender);
  if (!window) {
    throw new Error("Could not resolve the current Electron window");
  }
  return window;
}

function registerDesktopHandlers(): void {
  ipcMain.handle("desktop:init-server-config", async () =>
    serverState ? getDesktopServerConfig(serverState) : null,
  );
  ipcMain.handle("desktop:download-file", async (_event, payload) => {
    const targetPath = await resolveDownloadPath(payload.filename);
    await writeFile(targetPath, new Uint8Array(payload.bytes));
    return targetPath;
  });
  ipcMain.handle("desktop:clipboard-read", () => clipboard.readText());
  ipcMain.handle("desktop:clipboard-write", (_event, text: string) => {
    clipboard.writeText(text);
  });
  ipcMain.handle("desktop:open-external", (_event, url: string) =>
    shell.openExternal(url),
  );
  ipcMain.handle("desktop:pick-directory", async (event) => {
    const result = await dialog.showOpenDialog(currentWindow(event), {
      properties: ["openDirectory"],
    });
    return result.canceled ? null : (result.filePaths[0] ?? null);
  });
  ipcMain.handle("desktop:window-minimize", (event) =>
    currentWindow(event).minimize(),
  );
  ipcMain.handle("desktop:window-maximize", (event) =>
    currentWindow(event).maximize(),
  );
  ipcMain.handle("desktop:window-unmaximize", (event) =>
    currentWindow(event).unmaximize(),
  );
  ipcMain.handle("desktop:window-is-maximized", (event) =>
    currentWindow(event).isMaximized(),
  );
  ipcMain.handle("desktop:window-close", (event) =>
    currentWindow(event).close(),
  );
  ipcMain.handle("desktop:window-is-fullscreen", (event) =>
    currentWindow(event).isFullScreen(),
  );
  ipcMain.handle("desktop:relaunch", async () => {
    app.relaunch();
    app.quit();
  });
  registerUpdaterHandlers();
}

function preloadPath(): string {
  return app.isPackaged
    ? path.resolve(import.meta.dirname, "preload.js")
    : path.resolve(import.meta.dirname, "..", ".electron-dist", "preload.js");
}

function setupDevContextMenu(window: BrowserWindow): void {
  window.webContents.on("context-menu", (_event, params) => {
    const menu = Menu.buildFromTemplate([
      {
        label: "Reload",
        click: () => {
          window.webContents.reload();
        },
      },
      {
        label: "Inspect Element",
        click: () => {
          window.webContents.inspectElement(params.x, params.y);
          if (!window.webContents.isDevToolsOpened()) {
            window.webContents.openDevTools({ mode: "detach" });
          }
        },
      },
    ]);
    menu.popup({ window });
  });
}

async function createMainWindow(): Promise<BrowserWindow> {
  const windowState = await loadWindowState();
  const window = new BrowserWindow(
    applyWindowState(
      {
        show: false,
        autoHideMenuBar: true,
        title: "Discobot",
        backgroundColor: "#0b0b0d",
        titleBarStyle: process.platform === "darwin" ? "hiddenInset" : "hidden",
        frame: process.platform === "darwin",
        webPreferences: {
          contextIsolation: true,
          nodeIntegration: false,
          sandbox: true,
          preload: preloadPath(),
        },
      },
      windowState,
    ),
  );

  window.on("ready-to-show", () => window.show());
  setupDevContextMenu(window);
  window.on("close", (event) => {
    if (!app.isQuitting) {
      event.preventDefault();
      hideMainWindow(window);
    }
  });
  window.on("resize", () => {
    window.webContents.send("desktop:window-resized");
  });
  window.on("enter-full-screen", () => {
    window.webContents.send("desktop:window-resized");
  });
  window.on("leave-full-screen", () => {
    window.webContents.send("desktop:window-resized");
  });
  trackWindowState(window);
  restoreWindowState(window, windowState);

  await window.loadURL(getElectronRendererURL());
  return window;
}

async function bootstrap(): Promise<void> {
  await app.whenReady();
  serverState = await createInitialServerState();
  serverState.process = await startBundledServer(serverState);
  await registerAppProtocol();
  configureUpdater();
  registerDesktopHandlers();
  mainWindow = await createMainWindow();
  if (!trayInitialized) {
    mainTray = setupTray(mainWindow);
    trayInitialized = true;
  }

  app.on("activate", async () => {
    if (BrowserWindow.getAllWindows().length === 0) {
      mainWindow = await createMainWindow();
      if (!trayInitialized) {
        mainTray = setupTray(mainWindow);
        trayInitialized = true;
      }
      return;
    }
    if (mainWindow) {
      showMainWindow(mainWindow);
    }
  });
}

app.on("second-instance", () => {
  if (mainWindow) {
    showMainWindow(mainWindow);
  }
});

const singleInstance = app.requestSingleInstanceLock();
if (!singleInstance) {
  app.quit();
} else {
  void bootstrap();
}

app.on("before-quit", () => {
  app.isQuitting = true;
  mainTray?.destroy();
  mainTray = null;
  if (serverState) {
    stopBundledServer(serverState);
  }
});

declare global {
  namespace Electron {
    interface App {
      isQuitting?: boolean;
    }
  }
}
