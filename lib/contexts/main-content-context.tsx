import { generateId } from "ai";
import * as React from "react";
import type { Session } from "@/lib/api-types";
import {
	STORAGE_KEYS,
	usePersistedState,
} from "@/lib/hooks/use-persisted-state";
import { invalidateSessionFiles } from "@/lib/hooks/use-session-files";
import { invalidateSession, useSession } from "@/lib/hooks/use-sessions";
import { useWorkspaces } from "@/lib/hooks/use-workspaces";

export type MainContentView =
	| { type: "new-session"; workspaceId?: string; sessionId: string }
	| { type: "session"; sessionId: string }
	| { type: "workspace-sessions"; workspaceId: string };

export interface MainContentContextValue {
	// Current view state
	view: MainContentView;

	// Helper methods to get current IDs
	getSelectedSessionId: () => string | null;
	getSelectedWorkspaceId: () => string | null;
	// Get session ID including temporary ID for new sessions
	getSessionIdForView: () => string | null;
	// Check if current view is a new session
	isNewSession: () => boolean;

	// Session data and loading state
	selectedSession: Session | null | undefined;
	isSessionLoading: boolean;

	// UI Settings
	chatWidthMode: "full" | "constrained";
	setChatWidthMode: (mode: "full" | "constrained") => void;

	// Actions to change the view
	showNewSession: (options?: { workspaceId?: string }) => void;
	showSession: (sessionId: string) => void;
	showWorkspaceSessions: (workspaceId: string) => void;
	// Handle session creation - updates view with workspace/agent IDs if current session matches
	sessionCreated: (
		sessionId: string,
		workspaceId: string,
		agentId: string,
	) => void;
}

const MainContentContext = React.createContext<MainContentContextValue | null>(
	null,
);

export function useMainContentContext() {
	const context = React.useContext(MainContentContext);
	if (!context) {
		throw new Error(
			"useMainContentContext must be used within a MainContentProvider",
		);
	}
	return context;
}

interface MainContentProviderProps {
	children: React.ReactNode;
}

export function MainContentProvider({ children }: MainContentProviderProps) {
	const [view, setView] = React.useState<MainContentView>({
		type: "new-session",
		sessionId: generateId(), // Generate on initial mount
	});

	// Fetch workspaces to restore persisted selection
	const { workspaces } = useWorkspaces();

	// Get persisted workspace ID
	const [persistedWorkspaceId] = usePersistedState<string | null>(
		STORAGE_KEYS.SELECTED_WORKSPACE_ID,
		null,
	);

	// Chat width mode state - persisted in localStorage
	const [chatWidthMode, setChatWidthMode] = usePersistedState<
		"full" | "constrained"
	>(STORAGE_KEYS.CHAT_WIDTH_MODE, "constrained");

	// Restore persisted workspace on mount
	React.useEffect(() => {
		// Only restore if we're on new-session view with no workspace selected
		if (
			view.type === "new-session" &&
			!view.workspaceId &&
			persistedWorkspaceId &&
			workspaces.length > 0
		) {
			// Check if the persisted workspace still exists
			const workspaceExists = workspaces.some(
				(w) => w.id === persistedWorkspaceId,
			);
			if (workspaceExists) {
				// Update view to include the persisted workspace (keep same session ID)
				setView({
					type: "new-session",
					workspaceId: persistedWorkspaceId,
					sessionId: view.sessionId,
				});
			}
		}
	}, [view, persistedWorkspaceId, workspaces]);

	// Fetch session data when viewing a session
	const selectedSessionId = view.type === "session" ? view.sessionId : null;
	const { session: selectedSession, isLoading: isSessionLoading } =
		useSession(selectedSessionId);

	// Helper methods to get current IDs
	const getSelectedSessionId = React.useCallback(() => {
		return view.type === "session" ? view.sessionId : null;
	}, [view]);

	const getSessionIdForView = React.useCallback(() => {
		if (view.type === "session") {
			return view.sessionId;
		}
		if (view.type === "new-session") {
			return view.sessionId;
		}
		return null;
	}, [view]);

	const isNewSession = React.useCallback(() => {
		return view.type === "new-session";
	}, [view]);

	const getSelectedWorkspaceId = React.useCallback(() => {
		if (view.type === "workspace-sessions") {
			return view.workspaceId;
		}
		if (view.type === "new-session" && view.workspaceId) {
			return view.workspaceId;
		}
		// When viewing a session, return the workspace ID from the session
		if (view.type === "session") {
			return selectedSession?.workspaceId ?? null;
		}
		return null;
	}, [view, selectedSession]);

	// Actions
	const showNewSession = React.useCallback(
		(options?: { workspaceId?: string }) => {
			const sessionId = generateId();
			setView({
				type: "new-session",
				workspaceId: options?.workspaceId,
				sessionId,
			});
		},
		[],
	);

	const showSession = React.useCallback((sessionId: string) => {
		setView({ type: "session", sessionId });
	}, []);

	const showWorkspaceSessions = React.useCallback((workspaceId: string) => {
		setView({ type: "workspace-sessions", workspaceId });
	}, []);

	const sessionCreated = React.useCallback(
		(sessionId: string, _workspaceId: string, _agentId: string) => {
			// Invalidate SWR caches to trigger refetch of session data
			// This must happen regardless of view state because the session was created
			// on the backend and any hooks watching this session should refetch
			// This is crucial because hooks may have gotten 404 errors before
			// the session was created and stopped retrying
			invalidateSession(sessionId);
			invalidateSessionFiles(sessionId);

			// Only update view if we're currently in new-session view and the session ID matches
			if (view.type === "new-session" && view.sessionId === sessionId) {
				setView((prev) => {
					// ensure we aren't dealing with a stale callback. this happens when the user
					// clicks away from a new session before sessionCreated is called.
					if (prev.type === "new-session" && prev.sessionId === sessionId) {
						return {
							type: "session",
							sessionId,
						};
					}
					return prev;
				});
			}
		},
		[view],
	);

	const value = React.useMemo<MainContentContextValue>(
		() => ({
			view,
			getSelectedSessionId,
			getSelectedWorkspaceId,
			getSessionIdForView,
			isNewSession,
			selectedSession,
			isSessionLoading,
			chatWidthMode,
			setChatWidthMode,
			showNewSession,
			showSession,
			showWorkspaceSessions,
			sessionCreated,
		}),
		[
			view,
			getSelectedSessionId,
			getSelectedWorkspaceId,
			getSessionIdForView,
			isNewSession,
			selectedSession,
			isSessionLoading,
			chatWidthMode,
			setChatWidthMode,
			showNewSession,
			showSession,
			showWorkspaceSessions,
			sessionCreated,
		],
	);

	return (
		<MainContentContext.Provider value={value}>
			{children}
		</MainContentContext.Provider>
	);
}
