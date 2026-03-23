// API Constants - shared string constants that must match the public REST API

// Session status constants representing the lifecycle of a session plus
// commit/rebase progress states surfaced through the public status field.
export const SessionStatus = {
	INITIALIZING: "initializing",
	REINITIALIZING: "reinitializing",
	CLONING: "cloning",
	PULLING_IMAGE: "pulling_image",
	CREATING_SANDBOX: "creating_sandbox",
	READY: "ready",
	STOPPED: "stopped",
	PENDING: "pending",
	COMMITTING: "committing",
	COMPLETED: "completed",
	ERROR: "error",
	REMOVING: "removing",
	REMOVED: "removed",
} as const;

// Workspace status constants representing the lifecycle of a workspace
export const WorkspaceStatus = {
	INITIALIZING: "initializing",
	CLONING: "cloning",
	READY: "ready",
	ERROR: "error",
} as const;
