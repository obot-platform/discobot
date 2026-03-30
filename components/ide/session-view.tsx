import * as React from "react";
import { ChatPanel } from "@/components/ide/chat-panel";
import { ConsolidatedDiffView } from "@/components/ide/consolidated-diff-view";
import { DesktopView } from "@/components/ide/desktop-view";
import { DiffContent } from "@/components/ide/diff-content";
import { FilePanel } from "@/components/ide/file-panel";
import { ResizeHandle } from "@/components/ide/resize-handle";
import { ServiceView } from "@/components/ide/service-view";
import { SessionViewHeader } from "@/components/ide/session-view-header";
import { TerminalView } from "@/components/ide/terminal-view";
import { useSessionViewContext } from "@/lib/contexts/session-view-context";
import {
	getSessionStorageKey,
	STORAGE_KEYS,
	usePersistedState,
} from "@/lib/hooks/use-persisted-state";
import { cn } from "@/lib/utils";

const RIGHT_SIDEBAR_DEFAULT_WIDTH = 224;
const RIGHT_SIDEBAR_MIN_WIDTH = 160;
const RIGHT_SIDEBAR_MAX_WIDTH = 400;

const CHAT_DEFAULT_WIDTH_PERCENT = 25; // 1/4 of the view
const CHAT_MIN_WIDTH = 300;
const CHAT_MAX_WIDTH = 800;

interface SessionViewProps {
	sessionId: string | null;
	isNew: boolean;
	initialWorkspaceId: string | null;
	onSessionCreated?: (
		sessionId: string,
		workspaceId: string,
		agentId: string,
	) => void;
}

export function SessionView({
	isNew,
	initialWorkspaceId,
	onSessionCreated,
}: SessionViewProps) {
	const {
		selectedSessionId,
		selectedSession,
		activeView,
		setActiveView,
		activeFilePathFromView,
		openFiles,
		terminalMounted,
		terminalRoot,
		terminalRef,
		setTerminalStatus,
		desktopMounted,
		services,
		activeServiceId,
		mountedServices,
		rightSidebarOpen,
		refreshDiffData,
		registerChatResumeStream,
		handleFileSelect,
	} = useSessionViewContext();

	// Manage right sidebar width state
	const [rightSidebarWidth, setRightSidebarWidth] = usePersistedState(
		STORAGE_KEYS.RIGHT_SIDEBAR_WIDTH,
		RIGHT_SIDEBAR_DEFAULT_WIDTH,
	);

	// Manage chat width state (per session)
	const chatWidthKey = getSessionStorageKey(
		STORAGE_KEYS.CHAT_WIDTH,
		selectedSessionId,
	);
	const [chatWidth, setChatWidth] = usePersistedState(
		chatWidthKey,
		CHAT_DEFAULT_WIDTH_PERCENT,
	);

	const handleRightSidebarResize = React.useCallback(
		(delta: number) => {
			// Delta is positive when moving right, but we want to grow when moving left
			setRightSidebarWidth((prev) =>
				Math.min(
					RIGHT_SIDEBAR_MAX_WIDTH,
					Math.max(RIGHT_SIDEBAR_MIN_WIDTH, prev - delta),
				),
			);
		},
		[setRightSidebarWidth],
	);

	// Ref to track the container width for calculating chat width percentage
	const containerRef = React.useRef<HTMLDivElement>(null);

	const handleChatResize = React.useCallback(
		(delta: number) => {
			if (!containerRef.current) return;

			const containerWidth = containerRef.current.offsetWidth;
			const currentChatWidthPx = (chatWidth / 100) * containerWidth;
			const newChatWidthPx = Math.min(
				CHAT_MAX_WIDTH,
				Math.max(CHAT_MIN_WIDTH, currentChatWidthPx + delta),
			);
			const newChatWidthPercent = (newChatWidthPx / containerWidth) * 100;

			setChatWidth(newChatWidthPercent);
		},
		[chatWidth, setChatWidth],
	);

	// Handle chat completion to refresh file data
	const handleChatComplete = React.useCallback(() => {
		refreshDiffData();
	}, [refreshDiffData]);

	// Show file panel when session is selected
	const showFilePanel = selectedSessionId !== null;

	// Determine if chat should be full-width (only when chat is active AND file panel is closed)
	const isChatFullWidth = activeView === "chat" && !rightSidebarOpen;

	return (
		<div className="flex flex-col overflow-hidden flex-1">
			<SessionViewHeader />
			<div ref={containerRef} className="flex-1 overflow-hidden flex">
				{/* Left - Chat panel (always visible when session is selected) */}
				{selectedSessionId && (
					<>
						<div
							className="relative flex flex-col overflow-hidden transition-[width] duration-300 ease-in-out"
							style={{ width: isChatFullWidth ? "100%" : `${chatWidth}%` }}
						>
							<ChatPanel
								key={selectedSessionId}
								sessionId={selectedSessionId}
								initialMessages={!isNew ? [] : undefined}
								initialWorkspaceId={initialWorkspaceId}
								onSessionCreated={onSessionCreated}
								onChatComplete={handleChatComplete}
								onRegisterResumeStream={registerChatResumeStream}
								className="h-full"
							/>

							{/* Resize handle between chat and content - only show when not full-width */}
							{!isChatFullWidth && (
								<ResizeHandle
									orientation="vertical"
									onResize={handleChatResize}
								/>
							)}
						</div>

						{/* Right - Content area (terminal/changes/files) - only show when not full-width */}
						{!isChatFullWidth && (
							<div className="flex-1 overflow-hidden flex">
								<div
									className={cn(
										"flex-1 overflow-hidden relative",
										!selectedSessionId && "flex items-center justify-center",
									)}
								>
									{/* Terminal - lazy mounted, stays mounted once viewed */}
									{terminalMounted && (
										<div
											className={cn(
												"absolute inset-0",
												activeView !== "terminal" && "hidden",
											)}
										>
											<TerminalView
												ref={terminalRef}
												sessionId={selectedSessionId}
												root={terminalRoot}
												className="h-full"
												onToggleChat={() => setActiveView("chat")}
												hideHeader
												onConnectionStatusChange={setTerminalStatus}
											/>
										</div>
									)}
									{/* Desktop - lazy mounted, stays mounted once viewed */}
									{desktopMounted && selectedSessionId && (
										<div
											className={cn(
												"absolute inset-0",
												activeView !== "desktop" && "hidden",
											)}
										>
											<DesktopView
												sessionId={selectedSessionId}
												className="h-full"
											/>
										</div>
									)}
									{/* Consolidated diff view - GitHub-style stacked diffs */}
									{selectedSessionId && (
										<div
											className={cn(
												"absolute inset-0",
												activeView !== "consolidated-diff" && "hidden",
											)}
										>
											<ConsolidatedDiffView />
										</div>
									)}
									{/* Service views - lazy mounted, stay mounted once viewed */}
									{selectedSessionId &&
										services
											.filter((s) => mountedServices.has(s.id))
											.map((service) => (
												<div
													key={service.id}
													className={cn(
														"absolute inset-0",
														activeServiceId !== service.id && "hidden",
													)}
												>
													<ServiceView
														sessionId={selectedSessionId}
														service={service}
														className="h-full"
													/>
												</div>
											))}
									{/* File diff content - all open files stay mounted, only active is visible */}
									{openFiles.map((file) => (
										<div
											key={file.id}
											className={cn(
												"absolute inset-0 flex flex-col",
												activeFilePathFromView !== file.id && "hidden",
											)}
										>
											<DiffContent file={file} />
										</div>
									))}
								</div>

								{/* Right - File panel (only show when session is selected) */}
								{showFilePanel && rightSidebarOpen && (
									<div className="relative">
										<ResizeHandle
											orientation="vertical"
											side="left"
											onResize={handleRightSidebarResize}
										/>
										<FilePanel
											sessionId={selectedSessionId}
											sessionStatus={selectedSession?.status}
											activeView={activeView}
											onFileSelect={handleFileSelect}
											className="overflow-hidden"
											style={{ width: rightSidebarWidth }}
										/>
									</div>
								)}
							</div>
						)}
					</>
				)}
			</div>
		</div>
	);
}
