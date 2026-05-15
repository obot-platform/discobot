import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import test from "node:test";

const THREAD_WORKSPACE_COMPONENT = path.resolve(
	import.meta.dirname,
	"../app/ThreadWorkspace.svelte",
);
const THREAD_WORKSPACE_ACTIVE_COMPONENT = path.resolve(
	import.meta.dirname,
	"../app/ThreadWorkspaceActive.svelte",
);

function readThreadWorkspaceSource() {
	return readFileSync(THREAD_WORKSPACE_COMPONENT, "utf-8");
}

function readThreadWorkspaceActiveSource() {
	return readFileSync(THREAD_WORKSPACE_ACTIVE_COMPONENT, "utf-8");
}

test("thread workspace always renders the active conversation view", () => {
	const source = readThreadWorkspaceSource();

	assert.doesNotMatch(source, /let sessionsMenuOpen = \$state\(false\)/);
	assert.doesNotMatch(source, /<SessionSidebar/);
	assert.doesNotMatch(source, /<Popover\.Root/);
	assert.match(source, /type Props = \{/);
	assert.match(source, /threadId: string;/);
	assert.match(source, /sidebarOpen\?: boolean;/);
	assert.match(source, /let \{/);
	assert.match(source, /\}: Props =\s*\$props\(\);/);
	assert.match(
		source,
		/const thread = session\.ensureThread\(untrack\(\(\) => threadId\)\);/,
	);
	assert.match(source, /setThreadContext\(thread\);/);
	assert.match(
		source,
		/<ThreadWorkspaceActive \{visible\} \{reserveSidebarSpace\} \{mode\} \/>/,
	);
	assert.doesNotMatch(source, /const hasSelectedThread = \$derived\.by/);
	assert.doesNotMatch(source, /const hasConversationMessages = \$derived\.by/);
	assert.doesNotMatch(source, /const showActiveConversation = \$derived\.by/);
	assert.doesNotMatch(source, /const isLoadingThread = \$derived\.by/);
	assert.doesNotMatch(
		source,
		/import \{ isSessionTransitioningStatus \} from "\$lib\/api-constants"/,
	);
	assert.doesNotMatch(source, /canLoadSessionThreads/);
	assert.doesNotMatch(source, /session\.threads\.status === "idle"/);
	assert.doesNotMatch(source, /session\.threads\.status === "loading"/);
	assert.doesNotMatch(
		source,
		/const showThreadSelectionPrompt = \$derived\.by/,
	);
	assert.doesNotMatch(source, /Select a thread to continue\./);
	assert.doesNotMatch(source, /<ConversationComposerSessionSetupStatus/);
	assert.doesNotMatch(
		source,
		/import Loader2Icon from "@lucide\/svelte\/icons\/loader-2"/,
	);
	assert.doesNotMatch(source, /<Loader2Icon class="size-4 animate-spin" \/>/);
	assert.doesNotMatch(
		source,
		/title=\{isLoadingThread \? "Loading thread" : ""\}/,
	);
	assert.doesNotMatch(
		source,
		/Loading the selected thread while the session starts\./,
	);
});

test("thread context stops sandbox refreshes when a session is not ready", () => {
	const source = readFileSync(
		path.resolve(import.meta.dirname, "../../context/thread-context.svelte.ts"),
		"utf-8",
	);

	assert.match(
		source,
		/const refreshSessionState = async \(\) => \{\s*if \(!hasSession\) \{/,
	);
	assert.match(
		source,
		/retryScheduler\.dispose\(\);\s*conversation\.disconnect\(\);/,
	);
	assert.match(source, /afterTurn: async \(\) => \{\s*if \(!hasSession\) \{/);
});

test("active thread workspace keeps the stream live while inactive conversation nodes are unmounted", () => {
	const source = readThreadWorkspaceActiveSource();

	assert.match(source, /if \(!props\.visible \|\| !session\.current\) \{/);
	assert.match(source, /void thread\.connect\(\);/);
	assert.match(
		source,
		/\$effect\(\(\) => \{[\s\S]*void thread\.connect\(\);[\s\S]*\}\);/,
	);
	assert.match(source, /const headerTitle = \$derived\.by/);
	assert.match(source, /if \(session\.isPending\) \{\s*return "";/);
	assert.match(
		source,
		/isSessionTransitioningStatus\(session\.current\?\.sandboxStatus\)[\s\S]*\? "Loading thread"[\s\S]*: ""/,
	);
	assert.match(source, /title=\{headerTitle\}/);
	assert.doesNotMatch(
		source,
		/title=\{session\.threads\.selected\?\.name \?\?/,
	);
	assert.equal(
		source.match(/<ConversationPane visible=\{props\.visible\} \/>/g)?.length,
		2,
	);
});
