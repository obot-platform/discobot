<script lang="ts">
	import { Badge } from "$lib/components/ui/badge";
	import { Button } from "$lib/components/ui/button";

	type Props = {
		fileContents: Record<string, string>;
		files: string[];
		onClose: () => void;
		onSelectFile: (file: string) => void;
		selectedFile: string;
	};

	let { fileContents, files, onClose, onSelectFile, selectedFile }: Props = $props();

	function fileStatus(index: number) {
		if (index === 0) return "active";
		if (index < 3) return "edited";
		return "linked";
	}

	function fileLabel(file: string) {
		return file.split("/").at(-1) ?? file;
	}
</script>

<div class="space-y-3">
	<div class="flex items-center justify-between gap-3">
		<div>
			<p class="text-sm font-medium">File editor</p>
			<p class="mt-1 text-xs text-muted-foreground">
				A tabs-style editor panel that can later become Monaco or VS Code Web.
			</p>
		</div>
		<Button variant="outline" size="sm" onclick={onClose}>Close panel</Button>
	</div>
	<div class="grid overflow-hidden rounded-md border border-border bg-background xl:grid-cols-[minmax(0,1fr)_13rem]">
		<div class="min-w-0 border-b border-border xl:border-r xl:border-b-0">
			<div class="flex flex-wrap items-center gap-1 border-b border-border px-2 py-2">
				{#each files.slice(0, 4) as file}
					<button
						type="button"
						onclick={() => onSelectFile(file)}
						class={`rounded-md px-3 py-1.5 text-sm transition ${selectedFile === file ? "bg-card text-foreground" : "text-muted-foreground hover:bg-accent"}`}
					>
						{fileLabel(file)}
					</button>
				{/each}
			</div>
			<div class="space-y-3 p-4">
				<div class="flex items-center justify-between gap-3">
					<div>
						<p class="font-mono text-sm text-foreground">{selectedFile}</p>
						<p class="mt-1 text-xs text-muted-foreground">Mock editor buffer · ready for Monaco</p>
					</div>
					<div class="flex gap-2">
						<Badge variant="outline">tab</Badge>
						<Badge variant="outline">editor</Badge>
					</div>
				</div>
				<pre class="overflow-x-auto rounded-md border border-border bg-card p-4 font-mono text-sm leading-6 text-muted-foreground"><code>{fileContents[selectedFile]}</code></pre>
			</div>
		</div>
		<div class="border-t border-border p-3 xl:border-t-0 xl:border-l">
			<p class="text-xs font-medium uppercase tracking-[0.16em] text-muted-foreground">
				Files
			</p>
			<div class="mt-3 space-y-1.5">
				{#each files as file, index}
					<button
						type="button"
						onclick={() => onSelectFile(file)}
						class={`block w-full rounded-md border px-3 py-2.5 text-left ${selectedFile === file ? "border-primary bg-primary/10" : "border-border bg-card"}`}
					>
						<div class="flex items-center justify-between gap-3">
							<div class="min-w-0">
								<p class="truncate font-mono text-xs text-foreground">{fileLabel(file)}</p>
								<p class="mt-1 text-xs capitalize text-muted-foreground">{fileStatus(index)}</p>
							</div>
							<Badge variant={selectedFile === file ? "default" : "outline"}>
								{selectedFile === file ? "Open" : "File"}
							</Badge>
						</div>
					</button>
				{/each}
			</div>
		</div>
	</div>
</div>
