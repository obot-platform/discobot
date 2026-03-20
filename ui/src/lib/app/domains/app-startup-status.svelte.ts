import type { AppStartupStatus } from "$lib/app/app-context.types";
import type { StartupTaskStore } from "$lib/store/startup-tasks.store.svelte";

function isTaskActive(task: { state: string }) {
	return task.state === "pending" || task.state === "in_progress";
}

type CreateAppStartupStatusDomainArgs = {
	store: StartupTaskStore;
};

export function createAppStartupStatusDomain(args: CreateAppStartupStatusDomainArgs): AppStartupStatus {
	const { store } = args;

	const visibleTasks = $derived.by(() => store.list.filter((task) => task.state !== "completed"));
	const hasActiveTasks = $derived.by(() => store.list.some(isTaskActive));

	return {
		get tasks() {
			return store.list;
		},
		get visibleTasks() {
			return visibleTasks;
		},
		get hasActiveTasks() {
			return hasActiveTasks;
		},
		refresh: () => store.fetch(),
	};
}
