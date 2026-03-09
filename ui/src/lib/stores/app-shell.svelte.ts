export type PrimaryPane = "chat" | "diff" | "terminal";

class AppShellStore {
	leftSidebarOpen = $state(true);
	rightSidebarOpen = $state(true);
	mobileNavOpen = $state(false);
	primaryPane = $state<PrimaryPane>("chat");

	setPrimaryPane(pane: PrimaryPane) {
		this.primaryPane = pane;
	}

	toggleLeftSidebar() {
		this.leftSidebarOpen = !this.leftSidebarOpen;
	}

	toggleRightSidebar() {
		this.rightSidebarOpen = !this.rightSidebarOpen;
	}

	toggleMobileNav() {
		this.mobileNavOpen = !this.mobileNavOpen;
	}

	resetLayout() {
		this.leftSidebarOpen = true;
		this.rightSidebarOpen = true;
		this.mobileNavOpen = false;
		this.primaryPane = "chat";
	}
}

export const appShellStore = new AppShellStore();
