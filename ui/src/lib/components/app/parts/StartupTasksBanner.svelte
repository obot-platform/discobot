<script lang="ts">
	import { Badge } from "$lib/components/ui/badge";
	import { Progress } from "$lib/components/ui/progress";
	import { Spinner } from "$lib/components/ui/spinner";
	import type { AppStartupStatus } from "$lib/app/app-context.types";
	import type { StartupTask } from "$lib/api-types";

	type Props = {
		startup: AppStartupStatus;
	};

	let { startup }: Props = $props();

	function getTaskStatusLabel(task: StartupTask) {
		switch (task.state) {
			case "pending":
				return "Pending";
			case "in_progress":
				return "In progress";
			case "failed":
				return "Failed";
			default:
				return "Completed";
		}
	}

	function getTaskBadgeVariant(
		task: StartupTask,
	): "secondary" | "destructive" | "outline" {
		switch (task.state) {
			case "failed":
				return "destructive";
			case "in_progress":
				return "secondary";
			default:
				return "outline";
		}
	}

	function getTaskProgress(task: StartupTask) {
		if (typeof task.progress === "number") {
			return task.progress;
		}

		if (
			typeof task.bytesDownloaded === "number" &&
			typeof task.totalBytes === "number" &&
			task.totalBytes > 0
		) {
			return Math.round((task.bytesDownloaded / task.totalBytes) * 100);
		}

		return null;
	}

	function formatBytes(value: number) {
		if (value < 1024) {
			return `${value} B`;
		}

		const units = ["KB", "MB", "GB", "TB"];
		let size = value;
		let unitIndex = -1;
		while (size >= 1024 && unitIndex < units.length - 1) {
			size /= 1024;
			unitIndex += 1;
		}

		return `${size.toFixed(size >= 10 ? 0 : 1)} ${units[unitIndex]}`;
	}

	function getTaskDetail(task: StartupTask) {
		if (task.error) {
			return task.error;
		}

		if (task.currentOperation) {
			return task.currentOperation;
		}

		if (
			typeof task.bytesDownloaded === "number" &&
			typeof task.totalBytes === "number" &&
			task.totalBytes > 0
		) {
			return `${formatBytes(task.bytesDownloaded)} of ${formatBytes(task.totalBytes)}`;
		}

		return null;
	}
</script>

{#if startup.visibleTasks.length > 0}
	<div class="border-b border-border bg-muted/30 px-3 py-2">
		<div class="flex items-center gap-2">
			{#if startup.hasActiveTasks}
				<Spinner class="size-3.5 text-muted-foreground" />
			{/if}
			<p class="text-sm font-medium">Startup tasks</p>
			<p class="text-xs text-muted-foreground">
				{startup.visibleTasks.length}
				{startup.visibleTasks.length === 1 ? " task" : " tasks"}
			</p>
		</div>

		<div class="mt-2 grid gap-2 md:grid-cols-2">
			{#each startup.visibleTasks as task (task.id)}
				<div class="rounded-md border border-border bg-background/80 px-3 py-2">
					<div class="flex items-start justify-between gap-3">
						<div class="min-w-0">
							<p class="truncate text-sm font-medium">{task.name}</p>
							{#if getTaskDetail(task)}
								<p
									class={`mt-0.5 line-clamp-2 text-xs ${task.state === "failed" ? "text-destructive" : "text-muted-foreground"}`}
								>
									{getTaskDetail(task)}
								</p>
							{/if}
						</div>
						<Badge variant={getTaskBadgeVariant(task)}>
							{getTaskStatusLabel(task)}
						</Badge>
					</div>

					{#if getTaskProgress(task) !== null}
						<div class="mt-2 flex items-center gap-2">
							<Progress
								value={getTaskProgress(task) ?? 0}
								class="h-1.5 flex-1"
							/>
							<span class="text-[11px] tabular-nums text-muted-foreground">
								{getTaskProgress(task)}%
							</span>
						</div>
					{/if}
				</div>
			{/each}
		</div>
	</div>
{/if}
