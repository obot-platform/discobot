import { mkdirSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { build } from "esbuild";

const __dirname = dirname(fileURLToPath(import.meta.url));
const projectRoot = dirname(__dirname);
const outputDir = join(projectRoot, ".electron-dist");

mkdirSync(outputDir, { recursive: true });

await Promise.all([
  build({
    entryPoints: {
      main: join(projectRoot, "electron", "main.ts"),
    },
    outdir: outputDir,
    bundle: true,
    format: "esm",
    platform: "node",
    target: "node24",
    sourcemap: true,
    external: ["electron", "electron-updater"],
  }),
  build({
    entryPoints: {
      preload: join(projectRoot, "electron", "preload.ts"),
    },
    outdir: outputDir,
    bundle: true,
    format: "cjs",
    platform: "node",
    target: "node24",
    sourcemap: true,
    external: ["electron"],
  }),
]);
