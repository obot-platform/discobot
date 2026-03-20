// API Constants - shared string constants that must match server/internal/model/model.go

// Session status constants representing the lifecycle of a session
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

// Commit status constants representing the commit state of a session (orthogonal to session status)
export const CommitStatus = {
	NONE: "",
	PENDING: "pending",
	COMMITTING: "committing",
	COMPLETED: "completed",
	FAILED: "failed",
} as const;

// Workspace status constants representing the lifecycle of a workspace
export const WorkspaceStatus = {
	INITIALIZING: "initializing",
	CLONING: "cloning",
	READY: "ready",
	ERROR: "error",
} as const;
