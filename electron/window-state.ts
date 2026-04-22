import { readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import {
  app,
  type BrowserWindow,
  type BrowserWindowConstructorOptions,
} from "electron";

type PersistedWindowState = {
  width: number;
  height: number;
  x?: number;
  y?: number;
  isMaximized?: boolean;
  isFullscreen?: boolean;
};

const DEFAULT_WINDOW_STATE: PersistedWindowState = {
  width: 1200,
  height: 1200,
};

function windowStatePath(): string {
  return path.join(app.getPath("userData"), "window-state.json");
}

export async function loadWindowState(): Promise<PersistedWindowState> {
  try {
    const persisted = JSON.parse(
      await readFile(windowStatePath(), "utf-8"),
    ) as PersistedWindowState;
    return {
      width:
        typeof persisted.width === "number"
          ? persisted.width
          : DEFAULT_WINDOW_STATE.width,
      height:
        typeof persisted.height === "number"
          ? persisted.height
          : DEFAULT_WINDOW_STATE.height,
      x: typeof persisted.x === "number" ? persisted.x : undefined,
      y: typeof persisted.y === "number" ? persisted.y : undefined,
      isMaximized: persisted.isMaximized === true,
      isFullscreen: persisted.isFullscreen === true,
    };
  } catch {
    return DEFAULT_WINDOW_STATE;
  }
}

async function saveWindowState(window: BrowserWindow): Promise<void> {
  if (window.isDestroyed()) {
    return;
  }

  const bounds =
    window.isMaximized() || window.isFullScreen()
      ? window.getNormalBounds()
      : window.getBounds();
  const state: PersistedWindowState = {
    width: bounds.width,
    height: bounds.height,
    x: bounds.x,
    y: bounds.y,
    isMaximized: window.isMaximized(),
    isFullscreen: window.isFullScreen(),
  };

  await writeFile(windowStatePath(), JSON.stringify(state, null, "\t"));
}

export function applyWindowState(
  options: BrowserWindowConstructorOptions,
  state: PersistedWindowState,
): BrowserWindowConstructorOptions {
  return {
    ...options,
    width: state.width,
    height: state.height,
    ...(typeof state.x === "number" ? { x: state.x } : {}),
    ...(typeof state.y === "number" ? { y: state.y } : {}),
  };
}

export function trackWindowState(window: BrowserWindow): void {
  const persist = () => {
    void saveWindowState(window);
  };

  window.on("move", persist);
  window.on("resize", persist);
  window.on("maximize", persist);
  window.on("unmaximize", persist);
  window.on("enter-full-screen", persist);
  window.on("leave-full-screen", persist);
  window.on("close", persist);
}

export function restoreWindowState(
  window: BrowserWindow,
  state: PersistedWindowState,
): void {
  if (state.isFullscreen) {
    window.setFullScreen(true);
    return;
  }
  if (state.isMaximized) {
    window.maximize();
  }
}
