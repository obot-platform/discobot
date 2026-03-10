import { SessionStatus } from "$lib/api-constants";
import type {
	AgentModel,
	AuthProvider,
	CredentialInfo,
	SupportInfoResponse,
} from "$lib/api-types";
import type {
	EnvSetWithVars,
	HookRunStatus,
	IdeOption,
	PlanEntry,
	ServiceItem,
	SessionData,
	SessionSummary,
	ThreadSummary,
} from "$lib/shell-types";

export const windowControls = ["_", "□", "×"];

export const ideOptions: IdeOption[] = [
	{ id: "cursor", label: "Cursor" },
	{ id: "vscode", label: "VS Code" },
	{ id: "zed", label: "Zed" },
];

export const files = [
	"app-shell.svelte",
	"thread-list.svelte",
	"files-panel.svelte",
	"header.svelte",
	"session-view.svelte",
];

export const fileContents: Record<string, string> = {
	"app-shell.svelte": `<AppShell>
	<Header />
	<ThreadList />
	<ConversationPane />
	<FilePanel />
</AppShell>`,
	"thread-list.svelte": `<aside>
	{#each threads as thread}
		<ThreadItem {thread} />
	{/each}
</aside>`,
	"files-panel.svelte": `<section>
	<TabBar />
	<MonacoEditor />
</section>`,
	"header.svelte": `<Header>
	<ModeGroup />
	<IdeLaunchButton />
	<DiffStats />
</Header>`,
	"session-view.svelte": `<ConversationPane>
	<MessageTimeline />
	<Composer />
</ConversationPane>`,
};

export const services: ServiceItem[] = [
	{ id: "api", label: "API", target: "localhost:3001" },
	{ id: "db", label: "DB", target: "localhost:8080" },
];

export const suggestedPrompts = [
	"Tighten the header spacing",
	"Show a mobile drawer concept",
	"Compare with the current React shell",
];

export const envSets: EnvSetWithVars[] = [
	{
		id: "env-core",
		projectId: "local",
		name: "Core",
		createdAt: new Date(Date.UTC(2026, 1, 24, 14, 15)).toISOString(),
		updatedAt: new Date(Date.UTC(2026, 2, 3, 9, 40)).toISOString(),
		envVars: {
			NODE_ENV: "development",
			LOG_LEVEL: "debug",
		},
	},
	{
		id: "env-ui",
		projectId: "local",
		name: "UI Preview",
		createdAt: new Date(Date.UTC(2026, 1, 26, 10, 5)).toISOString(),
		updatedAt: new Date(Date.UTC(2026, 2, 5, 11, 20)).toISOString(),
		envVars: {
			VITE_FEATURE_FLAGS: "new-shell,chat-input",
			VITE_API_BASE: "http://localhost:3001",
		},
	},
	{
		id: "env-ci",
		projectId: "local",
		name: "CI Mirror",
		createdAt: new Date(Date.UTC(2026, 2, 1, 8, 30)).toISOString(),
		updatedAt: new Date(Date.UTC(2026, 2, 7, 13, 10)).toISOString(),
		envVars: {
			CI: "true",
			PNPM_CACHE_DIR: "/tmp/pnpm-cache",
		},
	},
];

export const workflowActions = ["Commit", "Rebase", "Create PR", "Merge"];

export const settingsModels: AgentModel[] = [
	{ id: "claude-sonnet-4.5", name: "Claude Sonnet 4.5", provider: "anthropic" },
	{ id: "claude-opus-4", name: "Claude Opus 4", provider: "anthropic" },
	{ id: "gpt-5.3-codex", name: "GPT-5.3 Codex", provider: "openai" },
	{ id: "gpt-5.3-mini", name: "GPT-5.3 Mini", provider: "openai" },
];

export const authProviders: AuthProvider[] = [
	{
		id: "anthropic",
		name: "Anthropic",
		description: "Claude API key",
		env: ["ANTHROPIC_API_KEY"],
		category: "llm",
	},
	{
		id: "openai",
		name: "OpenAI",
		description: "OpenAI API key",
		env: ["OPENAI_API_KEY"],
		category: "llm",
	},
	{
		id: "github",
		name: "GitHub",
		description: "Git operations token",
		env: ["GITHUB_TOKEN"],
		category: "vcs",
	},
];

export const credentialFixtures: CredentialInfo[] = [
	{
		id: "cred-anthropic",
		name: "Anthropic",
		provider: "anthropic",
		authType: "api_key",
		isConfigured: true,
		updatedAt: new Date(Date.UTC(2026, 2, 9, 11, 45)).toISOString(),
	},
	{
		id: "cred-github",
		name: "GitHub",
		provider: "github",
		authType: "oauth",
		isConfigured: true,
		expiresAt: new Date(Date.UTC(2026, 5, 1, 8, 0)).toISOString(),
		updatedAt: new Date(Date.UTC(2026, 2, 8, 9, 30)).toISOString(),
	},
];

export const supportInfoFixture: SupportInfoResponse = {
	version: "0.0.0-dev",
	runtime: {
		os: "linux",
		arch: "amd64",
		go_version: "go1.26.0",
		num_cpu: 8,
		num_goroutine: 142,
	},
	config: {
		port: 3001,
		database_driver: "sqlite",
		auth_enabled: true,
		workspace_dir: "/home/discobot/workspace",
		sandbox_image: "ghcr.io/obot-platform/discobot:latest",
		tauri_mode: false,
		ssh_enabled: true,
		ssh_port: 2222,
		dispatcher_enabled: true,
		available_providers: ["docker", "vz"],
	},
	server_log: "[info] server started\n[info] session runtime initialized\n",
	log_path: "/tmp/discobot/server.log",
	log_exists: true,
	system_info: {
		ok: true,
		messages: [],
	},
};

const allSessionStatuses: SessionData["status"][] = [
	SessionStatus.INITIALIZING,
	SessionStatus.REINITIALIZING,
	SessionStatus.CLONING,
	SessionStatus.PULLING_IMAGE,
	SessionStatus.CREATING_SANDBOX,
	SessionStatus.READY,
	SessionStatus.RUNNING,
	SessionStatus.STOPPED,
	SessionStatus.ERROR,
	SessionStatus.REMOVING,
	SessionStatus.REMOVED,
];

function toStatusLabel(status: SessionData["status"]): string {
	return status
		.split("_")
		.map((word) => word.charAt(0).toUpperCase() + word.slice(1))
		.join(" ");
}

function makeSessionFiles(paths: string[]): SessionData["files"] {
	return paths.map((path) => ({
		id: path,
		name: path.split("/").at(-1) ?? path,
		type: "file",
	}));
}

function makePlanEntries(statusLabel: string): PlanEntry[] {
	return [
		{
			content: `Review ${statusLabel.toLowerCase()} shell spacing`,
			activeForm: "Reviewing shell spacing",
			status: "completed",
			priority: "medium",
		},
		{
			content: "Align thread sidebar interactions",
			activeForm: "Aligning thread sidebar interactions",
			status: "in_progress",
			priority: "high",
		},
		{
			content: "Validate mobile composer parity",
			activeForm: "Validating mobile composer parity",
			status: "pending",
			priority: "low",
		},
	];
}

function makeConversation(statusLabel: string): SessionData["conversation"] {
	return [
		{
			id: `msg-${statusLabel}-1`,
			role: "user",
			text: `Can we make the ${statusLabel.toLowerCase()} session feel more assistant-led?`,
		},
		{
			id: `msg-${statusLabel}-2`,
			role: "assistant",
			text: "Yes — keep the timeline central and dock tools beside chat instead of replacing it.",
		},
		{
			id: `msg-${statusLabel}-3`,
			role: "assistant",
			text: "I can wire the composer, thread rail, and tool renderers to maintain flow while preserving context.",
		},
	];
}

function makeHooksStatus(index: number): SessionData["hooksStatus"] {
	const hookStates: HookRunStatus["lastResult"][] = [
		"success",
		"running",
		"failure",
		"pending",
	];

	const now = Date.now();
	const first = hookStates[index % hookStates.length];
	const second = hookStates[(index + 1) % hookStates.length];
	const third = hookStates[(index + 2) % hookStates.length];

	const hooks: HookRunStatus[] = [
		{
			hookId: "hook-format-check",
			hookName: "Format check",
			type: "pre_tool_use",
			command: "pnpm format --check",
			lastResult: first,
			lastRunAt: new Date(now - 90_000).toISOString(),
			lastExitCode: first === "success" ? 0 : 1,
			runCount: 8,
			failCount: first === "failure" ? 2 : 0,
		},
		{
			hookId: "hook-lint-summary",
			hookName: "Lint summary",
			type: "post_tool_use",
			command: "pnpm check:frontend",
			lastResult: second,
			lastRunAt: new Date(now - 55_000).toISOString(),
			lastExitCode: second === "success" ? 0 : 1,
			runCount: 6,
			failCount: second === "failure" ? 1 : 0,
		},
		{
			hookId: "hook-policy-guard",
			hookName: "Policy guard",
			type: "user_prompt_submit",
			command: "validate-user-intent --strict",
			lastResult: third,
			lastRunAt: new Date(now - 30_000).toISOString(),
			lastExitCode: third === "success" ? 0 : 1,
			runCount: 11,
			failCount: third === "failure" ? 3 : 0,
		},
	];

	return {
		hooks,
		pendingHookIds: hooks.filter((hook) => hook.lastResult === "pending").map((hook) => hook.hookId),
	};
}

function makeHookOutputById(statusLabel: string): Record<string, string> {
	return {
		"hook-format-check": `[format-check]\nSession: ${statusLabel}\nAll files are formatted.`,
		"hook-lint-summary": `[lint-summary]\nSession: ${statusLabel}\n0 errors, 5 warnings (legacy ignores).`,
		"hook-policy-guard": `[policy-guard]\nSession: ${statusLabel}\nPrompt safety checks completed successfully.`,
	};
}

const sessionFixtures: SessionData[] = allSessionStatuses.map((status, index) => {
	const statusLabel = toStatusLabel(status);
	const id = `session-${status}`;

	return {
		id,
		name: `${statusLabel} sandbox`,
		description: `Mock session fixture for ${statusLabel.toLowerCase()} lifecycle state`,
		timestamp: new Date(Date.UTC(2026, 2, index + 1, 9, 30)).toISOString(),
		status,
		files: makeSessionFiles(files),
		workspaceId: "workspace-local",
		agentId: "agent-claude-code",
		model: "claude-sonnet-4.5",
		reasoning: index % 2 === 0 ? "high" : undefined,
		mode: index % 3 === 0 ? "plan" : undefined,
		activeEnvSetIds: ["env-core", "env-ui"],
		baseBranch: index % 2 === 0 ? "discobot-session" : "feature/ui-shell",
		baseCommit: `${(index + 10).toString(16)}${(index + 11).toString(16)}${(index + 12).toString(16)}${(index + 13).toString(16)}${(index + 14).toString(16)}${(index + 15).toString(16)}${(index + 16).toString(16)}`,
		references: {
			issueReference: `#${142 + index}`,
			pullRequestReference: `PR #${188 + index}`,
		},
		threads: [
			{ id: `thread-${status}-main`, name: `${statusLabel} thread` },
			{ id: `thread-${status}-review`, name: "Review follow-up" },
		],
		conversation: makeConversation(statusLabel),
		planEntries: makePlanEntries(statusLabel),
		hooksStatus: makeHooksStatus(index),
		hookOutputById: makeHookOutputById(statusLabel),
		editorFiles: files,
		fileContents,
		services,
		suggestedPrompts,
	};
});

const recentSessionIds = new Set([
	"session-ready",
	"session-running",
	"session-error",
	"session-initializing",
]);

export const sessionSummaries: SessionSummary[] = sessionFixtures.map((session) => ({
	id: session.id,
	name: session.name,
	isRecent: recentSessionIds.has(session.id),
	status: session.status,
}));

export const sessionDataById: Record<string, SessionData> = Object.fromEntries(
	sessionFixtures.map((session) => [session.id, session]),
);

export const sessionThreadsById: Record<string, ThreadSummary[]> = Object.fromEntries(
	sessionFixtures.map((session) => [session.id, session.threads]),
);
