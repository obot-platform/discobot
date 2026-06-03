import { spawn, type ChildProcessWithoutNullStreams } from "node:child_process";
import { accessSync, constants, createWriteStream } from "node:fs";
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
    path.join(app.getAppPath(), "electron", "binaries", executable),
    path.join(process.cwd(), "electron", "binaries", executable),
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
    path.join(app.getAppPath(), "electron", "resources"),
    path.join(app.getAppPath(), "electron", "resources", "vz"),
    path.join(process.cwd(), "electron", "resources"),
    path.join(process.cwd(), "electron", "resources", "vz"),
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

function resolveBundledWSLEnv(): Record<string, string> {
  if (process.platform !== "win32") {
    return {};
  }

  const env: Record<string, string> = {};
  const rootfsCandidates = [
    path.join(process.resourcesPath, "wsl", "discobot-rootfs.tar.zst"),
    path.join(
      app.getAppPath(),
      "electron",
      "resources",
      "wsl",
      "discobot-rootfs.tar.zst",
    ),
    path.join(
      process.cwd(),
      "electron",
      "resources",
      "wsl",
      "discobot-rootfs.tar.zst",
    ),
  ];
  const iconCandidates = [
    path.join(process.resourcesPath, "icon.ico"),
    path.join(app.getAppPath(), "electron", "assets", "icons", "icon.ico"),
    path.join(process.cwd(), "electron", "assets", "icons", "icon.ico"),
  ];
  for (const rootfsPath of rootfsCandidates) {
    try {
      accessSync(rootfsPath, constants.R_OK);
      env.WSL_ROOTFS_ARCHIVE_PATH = rootfsPath;
      break;
    } catch {
      // keep searching
    }
  }
  for (const iconPath of iconCandidates) {
    try {
      accessSync(iconPath, constants.R_OK);
      env.DISCOBOT_DESKTOP_ICON_PATH = iconPath;
      break;
    } catch {
      // keep searching
    }
  }
  return env;
}

function resolveBundledHCSEnv(): Record<string, string> {
  if (process.platform !== "win32") {
    return {};
  }

  const candidates = [
    path.join(process.resourcesPath, "hcs"),
    path.join(app.getAppPath(), "electron", "resources", "hcs"),
    path.join(process.cwd(), "electron", "resources", "hcs"),
  ];

  for (const hcsDir of candidates) {
    const launcherPath = path.join(hcsDir, "HcsLinuxVmLauncher.exe");
    const kernelPath = path.join(hcsDir, "wsl-kernel");
    const rootDiskPath = path.join(hcsDir, "discobot-rootfs.vhd");
    try {
      accessSync(launcherPath, constants.X_OK);
      accessSync(kernelPath, constants.R_OK);
      accessSync(rootDiskPath, constants.R_OK);
      return {
        HCS_LAUNCHER_PATH: launcherPath,
        HCS_KERNEL_PATH: kernelPath,
        HCS_ROOT_DISK_PATH: rootDiskPath,
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

  const serverBinaryPath = resolveServerBinaryPath();
  const serverLogPath = await getLogFilePath();
  const serverLog = createWriteStream(serverLogPath, { flags: "a" });
  const child = spawn(serverBinaryPath, [], {
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
      LOG_FILE: serverLogPath,
      SERVER_LOG_PATH: serverLogPath,
      ...resolveBundledVZEnv(),
      ...resolveBundledWSLEnv(),
      ...resolveBundledHCSEnv(),
    },
    stdio: "pipe",
  });

  child.stdout.on("data", (chunk) => {
    process.stdout.write(`[discobot-server] ${chunk}`);
    serverLog.write(chunk);
  });
  child.stderr.on("data", (chunk) => {
    process.stderr.write(`[discobot-server] ${chunk}`);
    serverLog.write(chunk);
  });
  child.on("close", () => {
    serverLog.end();
  });
  child.on("error", (error) => {
    serverLog.write(`[discobot-server] failed to start: ${error.message}\n`);
    serverLog.end();
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
  return app.isPackaged ? "app://discobot/" : `${getDevRendererOrigin()}/`;
}

export function getElectronRendererOrigin(): string {
  return app.isPackaged
    ? getProductionRendererOrigin()
    : getDevRendererOrigin();
}
