import type { IdeOption, ServiceItem } from "$lib/shell-types";

export const PREFERRED_IDE_STORAGE_KEY = "preferred.ide";

export const windowControls = ["_", "□", "×"];

export const ideOptions: IdeOption[] = [
	{ id: "cursor", label: "Cursor" },
	{ id: "vscode", label: "VS Code" },
	{ id: "zed", label: "Zed" },
];

export const sessionStatus = "Running";
export const workspaceTarget = "gh: obot-platform/discobot";
export const baseBranch = "discobot-session";
export const baseCommit = "a1b2c3d";
export const issueReference = "#142";
export const pullRequestReference = "PR #188";

export const recentThreads = ["Latest run", "Header polish", "Terminal follow-up"];
export const allThreads = ["Landing Shell Study", "Sidebar concepts", "Mobile layout"];

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

export const contextChips = ["mobile target", "shell layout", "session aware", "api stable"];

export const suggestedPrompts = [
	"Tighten the header spacing",
	"Show a mobile drawer concept",
	"Compare with the current React shell",
];

export const workflowActions = ["Commit", "Rebase", "Create PR", "Merge"];
