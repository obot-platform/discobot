<script lang="ts">
	import {
		categoryLabels,
		type UiComponentCategory,
		type UiComponentFilter,
		uiComponentCatalog,
		uiComponentFilters,
	} from "$lib/component-catalog";
	import { Badge } from "$lib/components/ui/badge";
	import { Button } from "$lib/components/ui/button";
	import {
		Card,
		CardContent,
		CardDescription,
		CardHeader,
		CardTitle,
	} from "$lib/components/ui/card";
	import { Input } from "$lib/components/ui/input";
	import { Separator } from "$lib/components/ui/separator";
	import { Switch } from "$lib/components/ui/switch";
	import {
		Tabs,
		TabsContent,
		TabsList,
		TabsTrigger,
	} from "$lib/components/ui/tabs";
	import { Textarea } from "$lib/components/ui/textarea";
	import { getApiBase, isTauriShell } from "$lib/environment";
	import { getTheme, toggleTheme } from "$lib/theme";

	let theme = getTheme();
	let search = "";
	let activeCategory: UiComponentFilter = "all";
	let previewSurface = "foundations";
	let compactPreview = false;
	let selectedComponentName = uiComponentCatalog[0]?.name ?? "button";
	const formPreviewNotes = `Workspace goals:\n- mobile ready\n- shell first\n- preserve backend contracts`;

	$: normalizedSearch = search.trim().toLowerCase();
	$: filteredCatalog = uiComponentCatalog.filter((component) => {
		const matchesCategory =
			activeCategory === "all" || component.category === activeCategory;
		const haystack = [
			component.name,
			component.label,
			component.description,
			categoryLabels[component.category],
		]
			.join(" ")
			.toLowerCase();
		const matchesSearch =
			!normalizedSearch || haystack.includes(normalizedSearch);
		return matchesCategory && matchesSearch;
	});
	$: categorySummary = uiComponentFilters
		.filter((filter): filter is UiComponentCategory => filter !== "all")
		.map((category) => ({
			category,
			count: uiComponentCatalog.filter(
				(component) => component.category === category,
			).length,
			label: categoryLabels[category],
		}));
	$: if (
		filteredCatalog.length > 0 &&
		!filteredCatalog.some(
			(component) => component.name === selectedComponentName,
		)
	) {
		selectedComponentName = filteredCatalog[0].name;
	}
	$: selectedComponent =
		filteredCatalog.find(
			(component) => component.name === selectedComponentName,
		) ??
		uiComponentCatalog.find(
			(component) => component.name === selectedComponentName,
		) ??
		null;

	function handleThemeToggle() {
		theme = toggleTheme();
	}

	function setCategory(category: UiComponentFilter) {
		activeCategory = category;
	}

	function selectComponent(name: string) {
		selectedComponentName = name;
	}
</script>

<svelte:head>
	<title>Discobot UI Component Gallery</title>
</svelte:head>

<div class="min-h-screen bg-background text-foreground">
	<div
		class="mx-auto flex min-h-screen max-w-7xl flex-col gap-8 px-6 py-8 lg:px-10"
	>
		<header
			class="flex flex-col gap-6 rounded-3xl border border-border bg-card/80 p-6 shadow-sm backdrop-blur"
		>
			<div
				class="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between"
			>
				<div class="space-y-3">
					<div class="flex flex-wrap items-center gap-2">
						<Badge variant="secondary">Svelte 5 redesign</Badge>
						<Badge variant="outline"
							>{uiComponentCatalog.length} components installed</Badge
						>
					</div>
					<div class="space-y-2">
						<p
							class="text-sm font-medium uppercase tracking-[0.24em] text-muted-foreground"
						>
							Component gallery
						</p>
						<h1 class="text-3xl font-semibold tracking-tight sm:text-4xl">
							Browse the installed shadcn-svelte system
						</h1>
						<p
							class="max-w-3xl text-sm leading-6 text-muted-foreground sm:text-base"
						>
							Use this page to search, filter, and compare the full component
							set while we shape the new Discobot shell.
						</p>
					</div>
				</div>

				<div class="flex flex-wrap items-center gap-3 self-start lg:self-auto">
					<Button variant="outline" href="/gallery/brand">Brand preview</Button>
					<Button variant="outline" href="/gallery/ai">AI gallery</Button>
					<Button variant="outline" href="/gallery/ai/conversation-pane">
						Conversation pane sandbox
					</Button>
					<Button variant="outline" href="/gallery/startup">
						Startup preview
					</Button>
					<Button variant="outline" href="/">Back to home</Button>
					<Button variant="outline" onclick={handleThemeToggle}>
						Theme: {theme}
					</Button>
				</div>
			</div>

			<div class="grid gap-4 md:grid-cols-[minmax(0,1fr)_auto] md:items-center">
				<Input
					bind:value={search}
					placeholder="Search components, categories, or use cases"
				/>
				<div
					class="flex items-center gap-3 rounded-2xl border border-border bg-background/70 px-4 py-3 text-sm"
				>
					<div class="space-y-0.5">
						<p class="font-medium">Compact preview</p>
						<p class="text-xs text-muted-foreground">
							Tighten spacing in the sample panel
						</p>
					</div>
					<Switch bind:checked={compactPreview} />
				</div>
			</div>

			<div class="flex flex-wrap gap-2">
				{#each uiComponentFilters as filter}
					<Button
						variant={activeCategory === filter ? "default" : "outline"}
						size="sm"
						onclick={() => setCategory(filter)}
					>
						{filter === "all" ? "All" : categoryLabels[filter]}
					</Button>
				{/each}
			</div>
		</header>

		<section class="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
			{#each categorySummary as summary}
				<Card>
					<CardHeader class="gap-1 pb-3">
						<CardDescription>{summary.label}</CardDescription>
						<CardTitle class="text-2xl">{summary.count}</CardTitle>
					</CardHeader>
					<CardContent>
						<p class="text-sm leading-6 text-muted-foreground">
							{summary.label} ready to mix into the redesign.
						</p>
					</CardContent>
				</Card>
			{/each}
		</section>

		<main
			class="grid flex-1 gap-6 xl:grid-cols-[minmax(0,1.45fr)_minmax(19rem,0.75fr)]"
		>
			<section class="space-y-6">
				<Card>
					<CardHeader class={compactPreview ? "gap-2 pb-3" : undefined}>
						<div class="flex flex-wrap items-center justify-between gap-3">
							<div>
								<CardTitle>Live preview surface</CardTitle>
								<CardDescription>
									A starter playground built from the installed component set.
								</CardDescription>
							</div>
							<Badge variant="outline"
								>Current runtime: {isTauriShell() ? "Tauri" : "Browser"}</Badge
							>
						</div>
					</CardHeader>
					<CardContent class={compactPreview ? "space-y-4" : "space-y-6"}>
						<Tabs bind:value={previewSurface}>
							<TabsList>
								<TabsTrigger value="foundations">Foundations</TabsTrigger>
								<TabsTrigger value="forms">Forms</TabsTrigger>
								<TabsTrigger value="workflow">Workflow</TabsTrigger>
							</TabsList>

							<TabsContent value="foundations" class="mt-4 space-y-4">
								<div class="flex flex-wrap gap-2">
									<Button>Primary action</Button>
									<Button variant="secondary">Secondary</Button>
									<Button variant="outline">Outline</Button>
									<Button variant="ghost">Ghost</Button>
								</div>
								<div class="flex flex-wrap gap-2">
									<Badge>Default</Badge>
									<Badge variant="secondary">Secondary</Badge>
									<Badge variant="outline">Outline</Badge>
									<Badge variant="destructive">Destructive</Badge>
								</div>
								<div class="grid gap-4 md:grid-cols-2">
									<Card class="bg-background/70">
										<CardHeader>
											<CardTitle>Workspace card</CardTitle>
											<CardDescription>
												A calm surface for repo, agent, or session metadata.
											</CardDescription>
										</CardHeader>
										<CardContent>
											<p class="text-sm leading-6 text-muted-foreground">
												Use cards to break the shell into readable, movable
												sections.
											</p>
										</CardContent>
									</Card>
									<Card class="bg-background/70">
										<CardHeader>
											<CardTitle>Status grouping</CardTitle>
											<CardDescription>
												Compact building blocks for dense desktop UI.
											</CardDescription>
										</CardHeader>
										<CardContent class="space-y-3">
											<div
												class="flex items-center justify-between rounded-xl border border-border px-3 py-2 text-sm"
											>
												<span>Backend connection</span>
												<Badge variant="secondary">Healthy</Badge>
											</div>
											<div
												class="flex items-center justify-between rounded-xl border border-border px-3 py-2 text-sm"
											>
												<span>Preview API base</span>
												<span class="font-mono text-xs text-muted-foreground"
													>{getApiBase()}</span
												>
											</div>
										</CardContent>
									</Card>
								</div>
							</TabsContent>

							<TabsContent value="forms" class="mt-4 space-y-4">
								<div class="grid gap-4 md:grid-cols-2">
									<div
										class="space-y-3 rounded-2xl border border-border bg-background/70 p-4"
									>
										<p class="text-sm font-medium">Search & filters</p>
										<Input value="Build a mobile-first shell" />
										<Textarea rows={4} value={formPreviewNotes} />
									</div>
									<div
										class="space-y-3 rounded-2xl border border-border bg-background/70 p-4"
									>
										<p class="text-sm font-medium">Preferences</p>
										<div
											class="flex items-center justify-between rounded-xl border border-border px-3 py-2"
										>
											<div>
												<p class="text-sm font-medium">Dense navigation</p>
												<p class="text-xs text-muted-foreground">
													Fit more panes on desktop
												</p>
											</div>
											<Switch checked />
										</div>
										<div
											class="flex items-center justify-between rounded-xl border border-border px-3 py-2"
										>
											<div>
												<p class="text-sm font-medium">Animated transitions</p>
												<p class="text-xs text-muted-foreground">
													Subtle motion only
												</p>
											</div>
											<Switch />
										</div>
									</div>
								</div>
							</TabsContent>

							<TabsContent value="workflow" class="mt-4 space-y-4">
								<div
									class="space-y-4 rounded-2xl border border-border bg-background/70 p-4"
								>
									<div class="flex flex-wrap gap-2">
										<Badge>Shell</Badge>
										<Badge variant="secondary">Chat</Badge>
										<Badge variant="outline">Files</Badge>
										<Badge variant="outline">Diff</Badge>
									</div>
									<Separator />
									<div class="grid gap-3 md:grid-cols-3">
										<div class="rounded-xl border border-border p-3">
											<p class="text-sm font-medium">1. Explore</p>
											<p class="mt-1 text-sm text-muted-foreground">
												Search the catalog for the right primitives.
											</p>
										</div>
										<div class="rounded-xl border border-border p-3">
											<p class="text-sm font-medium">2. Compose</p>
											<p class="mt-1 text-sm text-muted-foreground">
												Combine cards, sidebars, sheets, and menus into screens.
											</p>
										</div>
										<div class="rounded-xl border border-border p-3">
											<p class="text-sm font-medium">3. Refine</p>
											<p class="mt-1 text-sm text-muted-foreground">
												Tune states, spacing, and interaction density for
												desktop and mobile.
											</p>
										</div>
									</div>
								</div>
							</TabsContent>
						</Tabs>
					</CardContent>
				</Card>

				<Card>
					<CardHeader>
						<div class="flex flex-wrap items-center justify-between gap-3">
							<div>
								<CardTitle>Installed components</CardTitle>
								<CardDescription>
									{filteredCatalog.length} matching components ready to browse.
								</CardDescription>
							</div>
							{#if normalizedSearch}
								<Badge variant="outline">Search: {search}</Badge>
							{/if}
						</div>
					</CardHeader>
					<CardContent>
						{#if filteredCatalog.length === 0}
							<div
								class="rounded-2xl border border-dashed border-border px-4 py-8 text-center"
							>
								<p class="font-medium">No components match that filter yet.</p>
								<p class="mt-2 text-sm text-muted-foreground">
									Try a broader search or switch back to the full catalog.
								</p>
							</div>
						{:else}
							<div class="grid gap-3 md:grid-cols-2 2xl:grid-cols-3">
								{#each filteredCatalog as component}
									<button
										type="button"
										on:click={() => selectComponent(component.name)}
										class={`rounded-2xl border p-4 text-left transition ${selectedComponentName === component.name ? "border-primary bg-primary/10 shadow-sm" : "border-border bg-background/70 hover:bg-accent"}`}
									>
										<div class="flex flex-wrap items-center gap-2">
											<p class="font-medium">{component.label}</p>
											<Badge variant="outline"
												>{categoryLabels[component.category]}</Badge
											>
											{#if component.featured}
												<Badge>Featured</Badge>
											{/if}
										</div>
										<p class="mt-3 text-sm leading-6 text-muted-foreground">
											{component.description}
										</p>
										<p class="mt-3 font-mono text-xs text-muted-foreground">
											{component.importPath}
										</p>
									</button>
								{/each}
							</div>
						{/if}
					</CardContent>
				</Card>
			</section>

			<aside class="space-y-6">
				<Card>
					<CardHeader>
						<CardTitle>Selected component</CardTitle>
						<CardDescription>
							Inspect a component before we start designing real screens.
						</CardDescription>
					</CardHeader>
					<CardContent class="space-y-4">
						{#if selectedComponent}
							<div class="space-y-3">
								<div class="flex flex-wrap items-center gap-2">
									<Badge>{selectedComponent.label}</Badge>
									<Badge variant="outline">
										{categoryLabels[selectedComponent.category]}
									</Badge>
									{#if selectedComponent.featured}
										<Badge variant="secondary">Featured starter</Badge>
									{/if}
								</div>
								<div>
									<h2 class="text-2xl font-semibold tracking-tight">
										{selectedComponent.label}
									</h2>
									<p class="mt-2 text-sm leading-6 text-muted-foreground">
										{selectedComponent.description}
									</p>
								</div>
							</div>

							<Separator />

							<div class="space-y-2">
								<p class="text-sm font-medium">Import</p>
								<pre
									class="overflow-x-auto rounded-2xl border border-border bg-background/70 p-3 text-xs leading-6 text-muted-foreground"><code
										>{`import { ${selectedComponent.exportName} } from "${selectedComponent.importPath}";`}</code
									></pre>
							</div>

							<div class="grid gap-3">
								<div
									class="rounded-2xl border border-border bg-background/70 p-4"
								>
									<p class="text-sm font-medium">When to reach for it</p>
									<p class="mt-2 text-sm leading-6 text-muted-foreground">
										Start with {selectedComponent.label.toLowerCase()} when the screen
										needs
										{selectedComponent.category === "layout"
											? " structure and composition"
											: selectedComponent.category === "form"
												? " user input or settings"
												: selectedComponent.category === "navigation"
													? " movement between related actions or destinations"
													: selectedComponent.category === "overlay"
														? " transient UI layered over the main content"
														: selectedComponent.category === "data"
															? " structured information display"
															: selectedComponent.category === "feedback"
																? " status or progress communication"
																: " compact UI affordances"}
										.
									</p>
								</div>
								<div
									class="rounded-2xl border border-border bg-background/70 p-4"
								>
									<p class="text-sm font-medium">Design note</p>
									<p class="mt-2 text-sm leading-6 text-muted-foreground">
										Use the gallery to shortlist primitives first, then we can
										compose them into Discobot-specific views like the shell,
										session lists, and chat panes.
									</p>
								</div>
							</div>
						{:else}
							<p class="text-sm text-muted-foreground">
								Select a component card to inspect it.
							</p>
						{/if}
					</CardContent>
				</Card>

				<Card>
					<CardHeader>
						<CardTitle>Selection heuristics</CardTitle>
						<CardDescription>
							A lightweight guide for composing the first real screens.
						</CardDescription>
					</CardHeader>
					<CardContent>
						<div class="space-y-3 text-sm leading-6 text-muted-foreground">
							<p>
								<span class="font-medium text-foreground">Layout:</span> card, sidebar,
								sheet, resizable, scroll-area.
							</p>
							<p>
								<span class="font-medium text-foreground">Forms:</span> input, textarea,
								select, switch, field, form.
							</p>
							<p>
								<span class="font-medium text-foreground">Navigation:</span> tabs,
								dropdown-menu, command, breadcrumb, navigation-menu.
							</p>
							<p>
								<span class="font-medium text-foreground">Feedback:</span> alert,
								sonner, progress, skeleton, spinner, empty.
							</p>
						</div>
					</CardContent>
				</Card>
			</aside>
		</main>
	</div>
</div>
