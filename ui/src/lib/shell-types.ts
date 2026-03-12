import type { Session, SessionStatus } from "$lib/api-types";

export type CenterPanel =
	| "chat"
	| "terminal"
	| "desktop"
	| "files"
	| "diff-review"
	| `service:${string}`;
export type PreferredIde = "cursor" | "vscode" | "zed";
export type WindowControlsSide = "left" | "right";

export type AsyncStatus = "idle" | "loading" | "ready" | "error";
export type WorkspaceSourceType = "local" | "git";
export type WorkspaceStatus = "ready" | "loading" | "error";
export type SessionStatusValue = SessionStatus;
export type ConversationRole = "user" | "assistant";
export type SessionFileState = "active" | "edited" | "linked";
export type PlanEntryStatus = "pending" | "in_progress" | "completed";
export type HookLastResult = "pending" | "running" | "success" | "failure";

export type IdeOption = {
	id: PreferredIde;
	label: string;
};

export type ServiceItem = {
	id: string;
	label: string;
	target: string;
};

export type EnvSetInfo = {
	id: string;
	projectId: string;
	name: string;
	createdAt: string;
	updatedAt: string;
};

export type EnvSetWithVars = EnvSetInfo & {
	envVars: Record<string, string>;
};

export type SessionSummary = {
	id: string;
	name: string;
	isRecent: boolean;
	status: SessionStatusValue;
};

export type WorkspaceSummary = {
	id: string;
	target: string;
	sourceType: WorkspaceSourceType;
	status: WorkspaceStatus;
	baseBranch: string;
	baseCommit: string;
};

export type SessionReferences = {
	issueReference: string;
	pullRequestReference: string;
};

export type ThreadSummary = {
	id: string;
	name: string;
};

export type PlanEntry = {
	content: string;
	status: PlanEntryStatus;
	activeForm: string;
	priority?: "low" | "medium" | "high";
};

export type HookRunStatus = {
	hookId: string;
	hookName: string;
	type: "pre_tool_use" | "post_tool_use" | "user_prompt_submit";
	command?: string;
	lastResult: HookLastResult;
	lastRunAt?: string;
	lastExitCode?: number;
	runCount: number;
	failCount: number;
};

export type HooksStatus = {
	hooks: HookRunStatus[];
	pendingHookIds: string[];
};

export type SessionDiffSummary = {
	added: number;
	modified: number;
	removed: number;
};

export type SessionConversationMessage = {
	id: string;
	role: ConversationRole;
	text: string;
};

export type SessionFile = {
	path: string;
	state: SessionFileState;
};

export type SessionDetail = {
	id: string;
	name: string;
	status: SessionStatusValue;
	baseBranch: string;
	baseCommit: string;
	references: SessionReferences;
	diffSummary: SessionDiffSummary;
	services: ServiceItem[];
	conversation: SessionConversationMessage[];
};

export type SessionEditorData = {
	files: SessionFile[];
	openTabs: string[];
	fileContents: Record<string, string>;
};

export type SessionData = {
	id: Session["id"];
	name: Session["name"];
	description: Session["description"];
	timestamp: Session["timestamp"];
	status: Session["status"];
	files: Session["files"];
	workspaceId?: Session["workspaceId"];
	model?: Session["model"];	reasoning?: Session["reasoning"];
	mode?: Session["mode"];
	activeEnvSetIds?: Session["activeEnvSetIds"];
	baseBranch: string;
	baseCommit: string;
	references: SessionReferences;
	threads: ThreadSummary[];
	conversation?: SessionConversationMessage[];
	planEntries?: PlanEntry[];
	hooksStatus?: HooksStatus;
	hookOutputById?: Record<string, string>;
	editorFiles: string[];
	fileContents: Record<string, string>;
	services: ServiceItem[];
};
