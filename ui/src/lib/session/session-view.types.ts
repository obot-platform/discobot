export type SessionActiveView =
	| { kind: "chat" }
	| { kind: "terminal" }
	| { kind: "desktop" }
	| { kind: "diff-review" }
	| { kind: "file"; path: string }
	| { kind: "services" };

export type EnvSetEditorMode = "list" | "create" | "edit";

export function getDefaultActiveView(files: string[]): SessionActiveView {
	if (files.length > 0) {
		return {
			kind: "file",
			path: files[0],
		};
	}

	return {
		kind: "chat",
	};
}

export function getSelectedFileFromView(activeView: SessionActiveView): string {
	return activeView.kind === "file" ? activeView.path : "";
}

export function getSelectedServiceIdFromView(
	_activeView: SessionActiveView,
): string | null {
	return null;
}
