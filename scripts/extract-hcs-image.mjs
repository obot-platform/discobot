#!/usr/bin/env node
/**
 * Extract HCS image files from Docker registry for Electron bundling.
 *
 * This script uses `go tool crane` (from go-containerregistry) to pull an HCS
 * Docker image from the registry and extract the root VHD, WSL2 kernel, HCS
 * launcher, gvproxy, and gvforwarder files to electron/resources/hcs/ for
 * bundling into the Windows app.
 *
 * Usage: node scripts/extract-hcs-image.mjs [image-ref] [arch]
 *   image-ref: Docker image reference (defaults to ghcr.io/obot-platform/discobot-hcs:main)
 *   arch: Architecture (amd64 or arm64, defaults to host arch)
 */

import { execFileSync, execSync } from "node:child_process";
import { existsSync, mkdtempSync, mkdirSync, rmSync, statSync } from "node:fs";
import os from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = dirname(fileURLToPath(import.meta.url));
const projectRoot = join(__dirname, "..");
const resourcesDir = join(projectRoot, "electron", "resources", "hcs");

const imageRef = process.argv[2] || "ghcr.io/obot-platform/discobot-hcs:main";
const arch = process.argv[3] || (process.arch === "arm64" ? "arm64" : "amd64");

mkdirSync(resourcesDir, { recursive: true });

function maybeLoginToGhcr() {
  const token = process.env.GITHUB_TOKEN || process.env.GH_TOKEN;
  if (!token) {
    return;
  }

  const username =
    process.env.GITHUB_ACTOR || process.env.GH_USERNAME || "github";
  console.log("Authenticating go tool crane with ghcr.io...");
  execSync(
    `go tool crane auth login ghcr.io -u "${username}" --password-stdin`,
    {
      cwd: projectRoot,
      input: token,
      stdio: ["pipe", "inherit", "inherit"],
    },
  );
}

function resolveTarCommand() {
  if (process.platform !== "win32") {
    return "tar";
  }

  const systemRoot = process.env.SystemRoot || "C:\\Windows";
  const nativeTar = join(systemRoot, "System32", "tar.exe");
  if (existsSync(nativeTar)) {
    return nativeTar;
  }
  return "tar.exe";
}

function extractTarEntries(tarPath, outputDir, files) {
  execFileSync(
    resolveTarCommand(),
    ["xf", tarPath, "-C", outputDir, ...files],
    {
      cwd: projectRoot,
      stdio: "inherit",
    },
  );
}

function extractionHint(error) {
  const message = error instanceof Error ? error.message : String(error);
  if (message.includes("DENIED") || message.includes("UNAUTHORIZED")) {
    return " The image ref may not exist yet, or ghcr.io may require credentials for it. Override HCS_IMAGE_REF or set GH_TOKEN/GITHUB_TOKEN if needed.";
  }
  return "";
}

console.log(`Extracting HCS image files for ${arch}...`);
console.log(`Image: ${imageRef}`);
console.log(`Output directory: ${resourcesDir}`);

const extractFiles = [
  "discobot-rootfs.vhd",
  "wsl-kernel",
  "kernel-version",
  "wsl-kernel-ref",
  "HcsLinuxVmLauncher.exe",
  "gvproxy.exe",
  "gvforwarder",
];
const tempDir = mkdtempSync(join(os.tmpdir(), "discobot-hcs-image-"));
const tempTarPath = join(tempDir, "image.tar");

try {
  maybeLoginToGhcr();
  console.log(`Exporting image with go tool crane (platform linux/${arch})...`);
  execSync(
    `go tool crane export --platform "linux/${arch}" "${imageRef}" "${tempTarPath}"`,
    { cwd: projectRoot, stdio: "inherit" },
  );
  extractTarEntries(tempTarPath, resourcesDir, extractFiles);

  console.log("HCS image files extracted successfully:");
  for (const file of extractFiles) {
    const filePath = join(resourcesDir, file);
    try {
      const stats = statSync(filePath);
      const sizeMB = (stats.size / 1024 / 1024).toFixed(1);
      console.log(`  ${file}: ${sizeMB} MB`);
    } catch {
      console.log(`  ${file} (size unknown)`);
    }
  }
} catch (error) {
  console.error(`Failed to extract HCS image:${extractionHint(error)}`);
  console.error(error instanceof Error ? error.message : String(error));
  process.exit(1);
} finally {
  rmSync(tempDir, { force: true, recursive: true });
}
