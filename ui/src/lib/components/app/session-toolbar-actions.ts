import type { CommitOperation, Session } from "../../api-types";
import { CommitStatus } from "../../api-constants";

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
	const isPending = args.session?.commitStatus === CommitStatus.PENDING;
	const isCommitting = args.session?.commitStatus === CommitStatus.COMMITTING;
	const showBusy = args.startingOperation !== null || isPending || isCommitting;
	const activeOperation = args.startingOperation ?? args.session?.commitOperation ?? "commit";
	const progressLabel = activeOperation === "rebase" ? "Rebasing..." : "Committing...";
	const primaryAction = hasChanges ? "commit" : "rebase";
	const primaryLabel = hasChanges ? "Commit" : "Rebase";

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
