import { spawn, type ChildProcessWithoutNullStreams } from "node:child_process";
import { accessSync, constants } from "node:fs";
import { mkdir } from "node:fs/promises";
import { createServer } from "node:net";
import path from "node:path";
import { app } from "electron";

export type DesktopServerState = {
  port: number;
  secret: string;
  sshPort: number;
  process: ChildProcessWithoutNullStreams | null;
};

function findAvailablePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const probe = createServer();
    probe.once("error", reject);
    probe.listen(0, "127.0.0.1", () => {
      const address = probe.address();
      probe.close((error) => {
        if (error) {
          reject(error);
          return;
        }
        if (!address || typeof address === "string") {
          reject(new Error("Failed to determine an available port"));
          return;
        }
        resolve(address.port);
      });
    });
  });
}

function preferredSSHPort(): Promise<number> {
  return new Promise((resolve) => {
    const probe = createServer();
    probe.once("error", async () => {
      resolve(await findAvailablePort());
    });
    probe.listen(3333, "127.0.0.1", () => {
      probe.close(() => resolve(3333));
    });
  });
}

function generateSecret(): string {
  const alphabet =
    "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789";
  return Array.from(
    { length: 32 },
    () => alphabet[Math.floor(Math.random() * alphabet.length)],
  ).join("");
}

async function getLogFilePath(): Promise<string> {
  const logDir = path.join(app.getPath("logs"), "discobot");
  await mkdir(logDir, { recursive: true });
  return path.join(logDir, "server.log");
}

function getDevRendererOrigin(): string {
  return "http://localhost:3100";
}

function getProductionRendererOrigin(): string {
  return "app://discobot";
}

function resolveServerBinaryPath(): string {
  const platform = process.platform;
  const arch = process.arch;
  const targetTriple =
    platform === "darwin"
      ? arch === "arm64"
        ? "aarch64-apple-darwin"
        : "x86_64-apple-darwin"
      : platform === "linux"
        ? arch === "arm64"
          ? "aarch64-unknown-linux-gnu"
          : "x86_64-unknown-linux-gnu"
        : arch === "arm64"
          ? "aarch64-pc-windows-msvc"
          : "x86_64-pc-windows-msvc";
  const executable = `discobot-server-${targetTriple}${platform === "win32" ? ".exe" : ""}`;
  const candidates = [
    path.join(process.resourcesPath, "binaries", executable),
    path.join(app.getAppPath(), "src-tauri", "binaries", executable),
    path.join(process.cwd(), "src-tauri", "binaries", executable),
  ];

  for (const candidate of candidates) {
    try {
      accessSync(candidate, constants.X_OK);
      return candidate;
    } catch {
      // keep searching
    }
  }

  throw new Error(`Could not locate bundled server binary: ${executable}`);
}

function resolveBundledVZEnv(): Record<string, string> {
  if (process.platform !== "darwin") {
    return {};
  }

  const candidates = [
    path.join(process.resourcesPath, "vz"),
    path.join(app.getAppPath(), "src-tauri", "resources"),
    path.join(app.getAppPath(), "src-tauri", "resources", "vz"),
    path.join(process.cwd(), "src-tauri", "resources"),
    path.join(process.cwd(), "src-tauri", "resources", "vz"),
  ];

  for (const vzDir of candidates) {
    const kernelPath = path.join(vzDir, "vmlinux");
    const rootfsPath = path.join(vzDir, "discobot-rootfs.squashfs");
    try {
      accessSync(kernelPath, constants.R_OK);
      accessSync(rootfsPath, constants.R_OK);
      return {
        VZ_KERNEL_PATH: kernelPath,
        VZ_BASE_DISK_PATH: rootfsPath,
      };
    } catch {
      // keep searching
    }
  }

  return {};
}

export async function createInitialServerState(): Promise<DesktopServerState> {
  if (!app.isPackaged) {
    return {
      port: 3001,
      secret: "",
      sshPort: 3333,
      process: null,
    };
  }

  return {
    port: await findAvailablePort(),
    secret: generateSecret(),
    sshPort: await preferredSSHPort(),
    process: null,
  };
}

export async function startBundledServer(
  state: DesktopServerState,
): Promise<ChildProcessWithoutNullStreams | null> {
  if (!app.isPackaged) {
    return null;
  }

  const child = spawn(resolveServerBinaryPath(), [], {
    cwd: app.getPath("userData"),
    env: {
      ...process.env,
      PORT: String(state.port),
      SSH_PORT: String(state.sshPort),
      CORS_ORIGINS: [
        getProductionRendererOrigin(),
        getDevRendererOrigin(),
      ].join(","),
      DISCOBOT_DESKTOP_RUNTIME: "electron",
      DISCOBOT_DESKTOP_SECRET: state.secret,
      SUGGESTIONS_ENABLED: "true",
      STDIN_KEEPALIVE: "true",
      LOG_FILE: await getLogFilePath(),
      ...resolveBundledVZEnv(),
    },
    stdio: "pipe",
  });

  child.stdout.on("data", (chunk) => {
    process.stdout.write(`[discobot-server] ${chunk}`);
  });
  child.stderr.on("data", (chunk) => {
    process.stderr.write(`[discobot-server] ${chunk}`);
  });

  return child;
}

export function stopBundledServer(state: DesktopServerState): void {
  state.process?.kill();
  state.process = null;
}

export function getDesktopServerConfig(
  state: DesktopServerState,
): { port: number; secret: string } | null {
  return app.isPackaged ? { port: state.port, secret: state.secret } : null;
}

export function getElectronRendererURL(): string {
  return app.isPackaged
    ? "app://discobot/index.html"
    : `${getDevRendererOrigin()}/`;
}

export function getElectronRendererOrigin(): string {
  return app.isPackaged
    ? getProductionRendererOrigin()
    : getDevRendererOrigin();
}
