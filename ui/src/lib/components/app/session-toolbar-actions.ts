import type { CommitOperation, Session } from "../../api-types";
import { SessionStatus } from "../../api-constants";

export type SessionToolbarOperationState = {
	hasChanges: boolean;
	showSplitButton: boolean;
	primaryAction: CommitOperation;
	primaryLabel: string;
	secondaryAction: CommitOperation | null;
	secondaryLabel: string | null;
	activeOperation: CommitOperation;
	showPending: boolean;
	showBusy: boolean;
	buttonLabel: string;
};

export function getSessionToolbarOperationState(args: {
	filesChanged: number;
	session: Session | null;
	startingOperation: CommitOperation | null;
}): SessionToolbarOperationState {
	const hasChanges = args.filesChanged > 0;
	const primaryAction = hasChanges ? "commit" : "rebase";
	const primaryLabel = hasChanges ? "Commit" : "Rebase";
	const isPending = args.session?.status === SessionStatus.PENDING;
	const isCommitting = args.session?.status === SessionStatus.COMMITTING;
	const showBusy = args.startingOperation !== null || isPending || isCommitting;
	const activeOperation = args.startingOperation ?? primaryAction;
	const progressLabel =
		args.startingOperation === "rebase"
			? "Rebasing..."
			: args.startingOperation === "commit"
				? "Committing..."
				: "Working...";

	return {
		hasChanges,
		showSplitButton: hasChanges,
		primaryAction,
		primaryLabel,
		secondaryAction: hasChanges ? "rebase" : null,
		secondaryLabel: hasChanges ? "Rebase" : null,
		activeOperation,
		showPending: isPending,
		showBusy,
		buttonLabel: isPending ? "Pending..." : showBusy ? progressLabel : primaryLabel,
	};
}
