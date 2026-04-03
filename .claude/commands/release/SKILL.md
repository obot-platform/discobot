---
name: release
description: Tag a new release version. Use when the user wants to create a git tag for a new release.
allowed-tools: Bash(git tag, git log, git describe, git push, git branch, git remote), Read, Glob, AskUserQuestion
metadata:
  argument-hint: "<version>"
---

# Release Tagging

Create and tag a new release version.

## Process

1. **Check current state**: Run `git describe --tags --abbrev=0 2>/dev/null || echo "no tags"` to find the latest tag
2. **List recent tags**: Run `git tag --sort=-v:refname | head -10` to show recent version history
3. **Determine version**:
   - If a version argument was provided, use it (ensure it starts with `v`, e.g., `v1.2.3`)
   - Otherwise, ask the user what version to tag using AskUserQuestion with options:
     - Patch bump (e.g., v1.0.0 -> v1.0.1)
     - Minor bump (e.g., v1.0.0 -> v1.1.0)
     - Major bump (e.g., v1.0.0 -> v2.0.0)
     - Custom version
4. **Show changes**: Run `git log --oneline $(git describe --tags --abbrev=0 2>/dev/null)..HEAD` to show commits since last tag
5. **Create tag**: Run `git tag -a <version> -m "Release <version>"`
6. **Select the publish remote**: Run `git remote -v` and choose the remote whose URL points to `obot-platform/discobot`. Do not assume the correct remote is named `origin` or `upstream`.
7. **Check if commit is on that remote**: Run `git branch -r --contains HEAD` to check whether the tagged commit has already been pushed to the selected remote's main branch
8. **Ask about push**: Ask the user whether to push the tag to the selected `obot-platform/discobot` remote. If the tagged commit is not yet on that remote's main branch, also offer to push main.
9. **Push if requested**: Run `git push <selected-remote> <version>` and `git push <selected-remote> main` if needed

## Version Format

This project uses semantic versioning with a `v` prefix:
- `v1.0.0` - Major.Minor.Patch
- `v0.1.0-alpha` - Pre-release versions are also supported

## Example Usage

```
/release           # Interactive - will prompt for version
/release v1.2.3    # Tag specific version
```
