import { spawn, spawnSync } from "node:child_process";
import { createWriteStream, mkdirSync } from "node:fs";
import os from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = dirname(fileURLToPath(import.meta.url));
const projectRoot = dirname(__dirname);
const serverDir = join(projectRoot, "server");
const config = os.platform() === "win32" ? ".air.windows.toml" : ".air.toml";

function defaultStateHome() {
  if (process.env.XDG_STATE_HOME) {
    return process.env.XDG_STATE_HOME;
  }
  if (os.platform() === "win32") {
    return process.env.LOCALAPPDATA ?? join(os.homedir(), "AppData", "Local");
  }
  return join(os.homedir(), ".local", "state");
}

function resolveServerLogPath() {
  return (
    process.env.SERVER_LOG_PATH ??
    join(defaultStateHome(), "discobot", "logs", "server.log")
  );
}

const serverLogPath = resolveServerLogPath();
mkdirSync(dirname(serverLogPath), { recursive: true });
const serverLog = createWriteStream(serverLogPath, { flags: "w" });

console.log(`[dev-server] writing server log to ${serverLogPath}`);

const isWindows = os.platform() === "win32";

const child = spawn(
  "go",
  ["run", "github.com/air-verse/air@latest", "-c", config],
  {
    cwd: serverDir,
    stdio: ["inherit", "pipe", "pipe"],
    // Own process group, so the terminal's Ctrl-C does not race shutdown()'s
    // teardown of the tree.
    detached: !isWindows,
    env: {
      ...process.env,
      SERVER_LOG_PATH: serverLogPath,
      SUGGESTIONS_ENABLED: "true",
    },
  },
);

// Collect every transitive child PID of rootPid from the process table. air
// runs the built server binary in its own process group, so a process-group
// kill of `go run` misses it; we signal each descendant by PID instead.
function descendantPids(rootPid) {
  const result = spawnSync("ps", ["-Ao", "pid=,ppid="], { encoding: "utf8" });
  if (result.status !== 0 || typeof result.stdout !== "string") {
    return [];
  }
  const childrenByParent = new Map();
  for (const line of result.stdout.trim().split("\n")) {
    const [pid, ppid] = line.trim().split(/\s+/).map(Number);
    if (!Number.isInteger(pid) || !Number.isInteger(ppid)) {
      continue;
    }
    const siblings = childrenByParent.get(ppid) ?? [];
    siblings.push(pid);
    childrenByParent.set(ppid, siblings);
  }
  const descendants = [];
  const stack = [rootPid];
  while (stack.length > 0) {
    const current = stack.pop();
    for (const pid of childrenByParent.get(current) ?? []) {
      descendants.push(pid);
      stack.push(pid);
    }
  }
  return descendants;
}

// Exit code reflecting how the child died, matching shell convention
// (128 + signal number) so callers see the same code they would from go/air.
function signalExitCode(signal) {
  switch (signal) {
    case "SIGINT":
      return 130;
    case "SIGTERM":
      return 143;
    default:
      return 1;
  }
}

function isAlive(pid) {
  try {
    process.kill(pid, 0);
    return true;
  } catch {
    return false;
  }
}

function signalPids(pids, signal) {
  for (const pid of pids) {
    try {
      process.kill(pid, signal);
    } catch {
      // already gone
    }
  }
}

let shuttingDown = false;
function shutdown(signal) {
  if (shuttingDown || child.pid === undefined) {
    return;
  }
  shuttingDown = true;

  if (isWindows) {
    spawnSync("taskkill", ["/pid", String(child.pid), "/t", "/f"]);
    serverLog.end();
    process.exit(0);
    return;
  }

  // Snapshot the whole tree (go run -> air -> server binary) before signalling,
  // since killed processes reparent and would no longer be discoverable.
  const tree = [child.pid, ...descendantPids(child.pid)];
  signalPids(tree, signal);

  // Exit once the tree is gone; force-kill stragglers at the deadline (the
  // server can ignore SIGINT mid port-retry). The interval is intentionally not
  // unref'd, so this process stays alive long enough to do that.
  const deadline = Date.now() + 5000;
  const exitCode = signalExitCode(signal);
  const poll = setInterval(() => {
    const alive = tree.filter(isAlive);
    const expired = Date.now() >= deadline;
    if (alive.length > 0 && !expired) {
      return;
    }
    if (expired) {
      signalPids(alive, "SIGKILL");
    }
    clearInterval(poll);
    serverLog.end();
    process.exit(exitCode);
  }, 200);
}

process.on("SIGINT", () => shutdown("SIGINT"));
process.on("SIGTERM", () => shutdown("SIGTERM"));

child.stdout.on("data", (chunk) => {
  process.stdout.write(chunk);
  serverLog.write(chunk);
});

child.stderr.on("data", (chunk) => {
  process.stderr.write(chunk);
  serverLog.write(chunk);
});

child.on("error", (err) => {
  console.error(`[dev-server] failed to start: ${err.message}`);
  serverLog.end();
  process.exit(1);
});

child.on("exit", (code, signal) => {
  if (shuttingDown) {
    // shutdown() owns teardown and the process exit in this case.
    return;
  }
  serverLog.end();
  if (signal) {
    // Reflect the child's signal-death in our exit code; re-raising would
    // re-enter our own handler.
    process.exit(signalExitCode(signal));
  }
  process.exit(code ?? 0);
});
