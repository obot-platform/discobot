import type { CenterPanel } from "$lib/shell-types";
import type { Getter, Setter } from "$lib/session/runtime/modules/module-context";
import type { SessionUiModule } from "$lib/session/runtime/session-runtime.types";

type CreateSessionUiModuleArgs = {
	getCenterPanel: Getter<CenterPanel>;
	setCenterPanel: Setter<CenterPanel>;
	getSelectedFile: Getter<string>;
	setSelectedFileState: Setter<string>;
	getFiles: Getter<string[]>;
	getIdeMenuOpen: Getter<boolean>;
	setIdeMenuOpen: Setter<boolean>;
	getComposerDraft: Getter<string>;
	setComposerDraftState: Setter<string>;
};

export function createSessionUiModule(args: CreateSessionUiModuleArgs): SessionUiModule {
	const openChat = () => {
		args.setCenterPanel("chat");
	};

	const openTerminal = () => {
		args.setCenterPanel("terminal");
	};

	const openDesktop = () => {
		args.setCenterPanel("desktop");
	};

	const openFiles = (file?: string) => {
		if (file) {
			args.setSelectedFileState(file);
		} else if (!args.getSelectedFile()) {
			args.setSelectedFileState(args.getFiles()[0] ?? "");
		}
		args.setCenterPanel("files");
	};

	const openDiffReview = () => {
		args.setCenterPanel("diff-review");
	};

	const openService = (serviceId: string) => {
		args.setCenterPanel(`service:${serviceId}`);
	};

	const toggleIdeMenu = () => {
		args.setIdeMenuOpen(!args.getIdeMenuOpen());
	};

	const setComposerDraft = (value: string) => {
		args.setComposerDraftState(value);
	};

	return {
		get centerPanel() {
			return args.getCenterPanel();
		},
		get selectedFile() {
			return args.getSelectedFile();
		},
		get ideMenuOpen() {
			return args.getIdeMenuOpen();
		},
		get composerDraft() {
			return args.getComposerDraft();
		},
		openChat,
		openTerminal,
		openDesktop,
		openFiles,
		openDiffReview,
		openService,
		toggleIdeMenu,
		setComposerDraft,
	};
}
