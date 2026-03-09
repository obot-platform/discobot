export type CenterPanel = "chat" | "terminal" | "desktop" | "files" | `service:${string}`;
export type PreferredIde = "cursor" | "vscode" | "zed";
export type WindowControlsSide = "left" | "right";

export type IdeOption = {
	id: PreferredIde;
	label: string;
};

export type ServiceItem = {
	id: string;
	label: string;
	target: string;
};
