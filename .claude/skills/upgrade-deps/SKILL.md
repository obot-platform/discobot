---
name: upgrade-deps
description: Upgrade dependencies and runtimes safely, run CI, and report higher-risk options.
argument-hint: '[optional scope, e.g. "frontend only" or "include runtimes"]'
---

Run a dependency and runtime upgrade pass for this repository.

User scope or notes: $ARGUMENTS

Follow this workflow exactly:

1. Baseline and inventory
   - Check `git status --short` first and protect any pre-existing user changes.
   - Identify package managers, lockfiles, runtime version declarations,
     language-specific manifests, CI workflow files, Dockerfiles, Compose files,
     container build scripts, and checked-in automation that pins tools or images.
   - For this repository, expect pnpm, Go modules/workspaces, Node runtime pins,
     Go runtime/toolchain declarations, GitHub Actions, Dockerfiles, Compose files,
     and scripts.

2. Discover available updates
   - Check outdated JavaScript dependencies with pnpm tooling.
   - Check Go module updates with `go list -m -u` in each relevant module/workspace.
   - Check runtime updates for Node.js and Go by inspecting checked-in version pins
     and current upstream stable releases when needed.
   - Check CI workflow maintenance items, including pinned GitHub Actions versions,
     setup-node/setup-go versions, workflow tool versions, cache keys, package-manager
     setup steps, and runtime versions duplicated in workflow files.
   - Check Docker and container maintenance items, including base image tags,
     runtime/package-manager install steps, image build arguments, Compose image
     tags, and scripts that build or publish container artifacts.
   - Classify each available update as:
     - Low impact: patch/minor dependency updates within the declared compatible
       range, lockfile refreshes, non-breaking tool/action patch updates, and
       Docker base image patch or digest refreshes that stay on the same runtime
       and distro family.
     - Higher risk: major dependency updates, framework/compiler/runtime version
       changes, package-manager major changes, Go/Node major or policy changes,
       GitHub Actions major changes, CI environment or permission changes, Docker
       base image major/distro-family changes, generated-code-affecting upgrades,
       or anything with visible migration notes.

3. Auto-adopt only low-impact updates
   - Apply low-impact updates directly.
   - Keep changes minimal and scoped to dependency/runtime metadata, lockfiles,
     workflow files, Docker/container metadata, and scripts that only pin versions.
   - Run the package-manager/go tidy commands needed to make lockfiles and sums
     consistent.
   - Keep runtime pins aligned across manifests, CI workflows, Dockerfiles, and
     scripts when adopting a runtime or package-manager version.
   - Do not auto-apply higher-risk updates.

4. Validate
   - Run the repository CI using `/ci` after low-impact changes are applied.
   - When workflow or Docker/container files changed, also run the relevant local
     validation commands available in the repository, such as workflow formatting,
     shell checks, Docker build checks, or targeted tests for build scripts.
   - If CI fails, diagnose and fix issues caused by the adopted upgrades, then rerun
     `/ci` until it passes or a clear blocker remains.

5. Report and ask about higher-risk upgrades
   - Summarize the low-impact changes that were applied, including files changed.
   - State the CI result.
   - Explicitly call out any CI workflow, Dockerfile, Compose, or automation changes,
     including which tool/image/action pins moved and whether runtime pins remain
     consistent across the repo.
   - List higher-risk upgrades that are available but not applied. For each, include:
     current version, available version, why it is higher risk, expected migration or
     validation work, and a recommendation.
   - Ask the user which, if any, higher-risk upgrades they want to accept next.
   - Do not commit changes unless the user explicitly asks.

Important constraints:
- Preserve unrelated user changes.
- Prefer pnpm over npm/yarn.
- Use Go 1.26 style guidance from AGENTS.md when Go files must change.
- Update relevant docs only if runtime/dependency policy or developer commands change.
