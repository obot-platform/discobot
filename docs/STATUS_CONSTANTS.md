# Status Constants Synchronization

This document describes how status constants are synchronized between the Go backend and TypeScript frontend.

## Overview

Status constants (Session, Commit, and Workspace statuses) are defined in two places:

1. **Go Backend**: `server/internal/model/model.go`
2. **TypeScript Frontend**: `lib/api-constants.ts`

These constants MUST be kept in sync manually when changes are made.

## Status Types

### Session Status

Represents the lifecycle of a session (sandbox container state).

**Go constants** (server/internal/model/model.go):
```go
const (
	SessionStatusInitializing    = "initializing"
	SessionStatusReinitializing  = "reinitializing"
	SessionStatusCloning         = "cloning"
	SessionStatusPullingImage    = "pulling_image"
	SessionStatusCreatingSandbox = "creating_sandbox"
	SessionStatusReady           = "ready"
	SessionStatusStopped         = "stopped"
	SessionStatusError           = "error"
	SessionStatusRemoving        = "removing"
	SessionStatusRemoved         = "removed"
)
```

**TypeScript constants** (lib/api-constants.ts):
```typescript
export const SessionStatus = {
	INITIALIZING: "initializing",
	REINITIALIZING: "reinitializing",
	CLONING: "cloning",
	PULLING_IMAGE: "pulling_image",
	CREATING_SANDBOX: "creating_sandbox",
	READY: "ready",
	STOPPED: "stopped",
	ERROR: "error",
	REMOVING: "removing",
	REMOVED: "removed",
} as const;
```

### Commit Status

Represents the commit state of a session (orthogonal to session status).

**Go constants** (server/internal/model/model.go):
```go
const (
	CommitStatusNone       = ""           // No commit in progress (default)
	CommitStatusPending    = "pending"    // Commit requested, waiting to start
	CommitStatusCommitting = "committing" // Commit in progress
	CommitStatusCompleted  = "completed"  // Commit completed successfully
	CommitStatusFailed     = "failed"     // Commit failed
)
```

**TypeScript constants** (lib/api-constants.ts):
```typescript
export const CommitStatus = {
	NONE: "",
	PENDING: "pending",
	COMMITTING: "committing",
	COMPLETED: "completed",
	FAILED: "failed",
} as const;
```

### Workspace Status

Represents the lifecycle of a workspace.

**Go constants** (server/internal/model/model.go):
```go
const (
	WorkspaceStatusInitializing = "initializing"
	WorkspaceStatusCloning      = "cloning"
	WorkspaceStatusReady        = "ready"
	WorkspaceStatusError        = "error"
)
```

**TypeScript constants** (lib/api-constants.ts):
```typescript
export const WorkspaceStatus = {
	INITIALIZING: "initializing",
	CLONING: "cloning",
	READY: "ready",
	ERROR: "error",
} as const;
```

## Usage

### TypeScript

Import and use the constants instead of hardcoded strings:

```typescript
import { SessionStatus, WorkspaceStatus } from "@/lib/api-constants";

// Good ✓
if (session.status === SessionStatus.COMPLETED) {
  // ...
}

// Bad ✗
if (session.status === "completed") {
  // ...
}
```

### Go

Use the defined constants:

```go
import "discobot/internal/model"

// Good ✓
session.CommitStatus = model.CommitStatusCompleted

// Bad ✗
session.CommitStatus = "completed"
```

## Type Safety

The TypeScript types in `lib/api-types.ts` are derived from the constants:

```typescript
export type SessionStatus =
	| (typeof SessionStatusConstants)[keyof typeof SessionStatusConstants];

export type WorkspaceStatus =
	| (typeof WorkspaceStatusConstants)[keyof typeof WorkspaceStatusConstants];
```

This ensures that:
1. TypeScript will catch any mismatches at compile time
2. The type system reflects the actual valid values
3. Refactoring tools can safely rename constants

## When to Update

When adding, removing, or renaming a status value:

1. Update the Go constants in `server/internal/model/model.go`
2. Update the TypeScript constants in `lib/api-constants.ts`
3. Update any usages in the codebase
4. Run type checking and tests to verify synchronization

## Files Using Status Constants

### Frontend
- `components/ide/sidebar-tree.tsx` - Session status indicators in workspace tree
- `components/ide/layout/header.tsx` - Session status in header
- `components/ide/layout/bottom-panel.tsx` - Commit status checks
- `components/ide/chat-panel.tsx` - Session status banner and input locking

### Backend
- `server/internal/service/session.go` - Session lifecycle management
- `server/internal/service/perform_commit_test.go` - Commit status tests
- `server/internal/handler/chat.go` - Commit status validation
- `server/internal/events/events_test.go` - Event testing

## Migration Notes

If you encounter hardcoded status strings in the codebase:
1. Replace them with the appropriate constant
2. Import the constants from `@/lib/api-constants` (TypeScript) or `model` package (Go)
3. This improves type safety and makes refactoring easier
