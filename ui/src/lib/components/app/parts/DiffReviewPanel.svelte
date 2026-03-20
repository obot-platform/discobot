<script lang="ts">
	import { Badge } from "$lib/components/ui/badge";
	import { Button } from "$lib/components/ui/button";
	import type { SessionDiffFileEntry, SessionDiffStats } from "$lib/api-types";

	type Props = {
		onClose: () => void;
		diff: SessionDiffFileEntry[];
		fileContents: Record<string, string>;
		diffStats: SessionDiffStats;
	};

	type DiffStatus = "added" | "modified" | "deleted";
	type DiffLine = {
		left: number | null;
		right: number | null;
		marker: " " | "+" | "-";
		content: string;
	};
	type DiffFilePreview = {
		path: string;
		status: DiffStatus;
		additions: number;
		deletions: number;
		lines: DiffLine[];
	};

	let { onClose, diff, fileContents, diffStats }: Props = $props();

	function statusForFile(path: string): DiffStatus {
		const diffEntry = diff.find((file) => file.path === path);
		if (diffEntry?.status === "added") return "added";
		if (diffEntry?.status === "deleted") return "deleted";
		return "modified";
	}

	function sourceLinesForFile(path: string): string[] {
		const source = fileContents[path] ?? "";
		const lines = source
			.split("\n")
			.map((line) => line.replace(/\t/g, "  "))
			.filter((line) => line.trim().length > 0)
			.slice(0, 4);
		if (lines.length > 0) {
			return lines;
		}
		return [`// ${path}`];
	}

	function buildPreview(path: string, status: DiffStatus, sourceLines: string[]): DiffFilePreview {
		if (status === "added") {
			return {
				path,
				status,
				additions: sourceLines.length,
				deletions: 0,
				lines: sourceLines.map((content, index) => ({
					left: null,
					right: index + 1,
					marker: "+",
					content,
				})),
			};
		}

		if (status === "deleted") {
			return {
				path,
				status,
				additions: 0,
				deletions: sourceLines.length,
				lines: sourceLines.map((content, index) => ({
					left: index + 1,
					right: null,
					marker: "-",
					content,
				})),
			};
		}

		const contextLineA = sourceLines[0] ?? "function renderPanel() {";
		const originalLine = sourceLines[1] ?? "  return panel;";
		const contextLineB = sourceLines[2] ?? "}";

		return {
			path,
			status,
			additions: 1,
			deletions: 1,
			lines: [
				{ left: 21, right: 21, marker: " ", content: contextLineA },
				{ left: 22, right: null, marker: "-", content: originalLine },
				{ left: null, right: 22, marker: "+", content: `${originalLine} // updated` },
				{ left: 23, right: 23, marker: " ", content: contextLineB },
			],
		};
	}

	const reviewFiles = $derived.by(() =>
		diff
			.slice(0, 4)
			.map((file) => buildPreview(file.path, statusForFile(file.path), sourceLinesForFile(file.path))),
	);

	const totals = $derived.by(() => diffStats);
</script>

<div class="space-y-3">
	<div class="flex items-center justify-between gap-3">
		<div>
			<p class="text-sm font-medium">Diff review</p>
			<p class="mt-1 text-xs text-muted-foreground">GitHub-style stacked file diffs for reviewing this thread.</p>
		</div>
		<Button variant="outline" size="sm" onclick={onClose}>Close panel</Button>
	</div>

	{#if reviewFiles.length === 0}
		<div class="rounded-md border border-border bg-card p-4 text-sm text-muted-foreground">
			No changed files yet.
		</div>
	{:else}
		<div class="overflow-hidden rounded-md border border-border bg-card">
			<div class="flex items-center justify-between gap-3 border-b border-border px-3 py-2">
				<p class="text-xs font-medium uppercase tracking-[0.16em] text-muted-foreground">
					{reviewFiles.length} changed {reviewFiles.length === 1 ? "file" : "files"}
				</p>
				<div class="flex items-center gap-3 text-xs font-medium">
					<span class="text-green-500">+{totals.additions}</span>
					<span class="text-red-500">-{totals.deletions}</span>
				</div>
			</div>

			<div class="divide-y divide-border">
				{#each reviewFiles as file}
					<section>
						<div class="flex items-center justify-between gap-3 bg-muted/40 px-3 py-2">
							<div class="flex min-w-0 items-center gap-2">
								<Badge
									variant="outline"
									class={
										file.status === "added"
											? "text-green-600"
											: file.status === "deleted"
												? "text-red-500"
												: "text-yellow-600"
									}
								>
									{file.status.toUpperCase()}
								</Badge>
								<p class="truncate font-mono text-xs text-foreground">{file.path}</p>
							</div>
							<div class="flex items-center gap-2 text-xs font-medium">
								{#if file.additions > 0}
									<span class="text-green-500">+{file.additions}</span>
								{/if}
								{#if file.deletions > 0}
									<span class="text-red-500">-{file.deletions}</span>
								{/if}
							</div>
						</div>
						<div class="overflow-x-auto">
							<table class="w-full border-collapse font-mono text-xs">
								<tbody>
									{#each file.lines as line}
										<tr class={line.marker === "+" ? "bg-diff-add" : line.marker === "-" ? "bg-diff-remove" : ""}>
											<td class="w-12 border-r border-border/60 px-2 py-1 text-right text-[11px] text-muted-foreground">
												{line.left ?? ""}
											</td>
											<td class="w-12 border-r border-border/60 px-2 py-1 text-right text-[11px] text-muted-foreground">
												{line.right ?? ""}
											</td>
											<td class="whitespace-pre px-3 py-1.5 text-foreground">
												<span
													class={`inline-block w-4 ${line.marker === "+" ? "text-green-500" : line.marker === "-" ? "text-red-500" : "text-muted-foreground"}`}
												>
													{line.marker}
												</span>
												{line.content}
											</td>
										</tr>
									{/each}
								</tbody>
							</table>
						</div>
					</section>
				{/each}
			</div>
		</div>
	{/if}
</div>
