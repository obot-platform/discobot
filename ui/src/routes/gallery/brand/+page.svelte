<script lang="ts">
	import brandBlackUrl from "$lib/components/app/parts/branding-assets/discobot-brand-black.svg";
	import brandGradientUrl from "$lib/components/app/parts/branding-assets/discobot-brand-gradient.svg";
	import brandWhiteUrl from "$lib/components/app/parts/branding-assets/discobot-brand-white.svg";
	import logoBlackUrl from "$lib/components/app/parts/branding-assets/discobot-logo-black.svg";
	import logoPurpleUrl from "$lib/components/app/parts/branding-assets/discobot-logo-purple.svg";
	import logoWhiteUrl from "$lib/components/app/parts/branding-assets/discobot-logo-white.svg";
	import { Badge } from "$lib/components/ui/badge";
	import { Button } from "$lib/components/ui/button";
	import {
		Card,
		CardContent,
		CardDescription,
		CardHeader,
		CardTitle,
	} from "$lib/components/ui/card";
	import DiscobotBrand from "$lib/components/app/parts/DiscobotBrand.svelte";
	import DiscobotLogo from "$lib/components/app/parts/DiscobotLogo.svelte";
	import { getTheme, toggleTheme } from "$lib/theme";

	let theme = getTheme();

	const lockups = [
		{ label: "Gradient lockup", src: brandGradientUrl, bg: "bg-white" },
		{ label: "Black lockup", src: brandBlackUrl, bg: "bg-white" },
		{ label: "White lockup", src: brandWhiteUrl, bg: "bg-[#111111]" },
	];

	const marks = [
		{ label: "Purple logo", src: logoPurpleUrl, bg: "bg-white" },
		{ label: "Black logo", src: logoBlackUrl, bg: "bg-white" },
		{ label: "White logo", src: logoWhiteUrl, bg: "bg-[#111111]" },
	];

	function handleThemeToggle() {
		theme = toggleTheme();
	}
</script>

<svelte:head>
	<title>Discobot Brand Gallery</title>
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
						<Badge variant="secondary">Brand system</Badge>
						<Badge variant="outline">Lockups + logo marks</Badge>
					</div>
					<div class="space-y-2">
						<p
							class="text-sm font-medium uppercase tracking-[0.24em] text-muted-foreground"
						>
							Brand gallery
						</p>
						<h1 class="text-3xl font-semibold tracking-tight sm:text-4xl">
							Review Discobot branding assets
						</h1>
						<p
							class="max-w-3xl text-sm leading-6 text-muted-foreground sm:text-base"
						>
							Compare the combined lockups, logo-only marks, and app-facing
							treatments before wiring them into Tauri icons and product
							surfaces.
						</p>
					</div>
				</div>

				<div class="flex flex-wrap items-center gap-3 self-start lg:self-auto">
					<Button variant="outline" href="/gallery">Back to gallery</Button>
					<Button variant="outline" href="/">Back to home</Button>
					<Button variant="outline" onclick={handleThemeToggle}>
						Theme: {theme}
					</Button>
				</div>
			</div>
		</header>

		<section class="grid gap-6 lg:grid-cols-2">
			<Card>
				<CardHeader>
					<CardTitle>Live component usage</CardTitle>
					<CardDescription>
						How the current Svelte components render when they import the SVG
						assets directly.
					</CardDescription>
				</CardHeader>
				<CardContent class="space-y-6">
					<div
						class="space-y-3 rounded-2xl border border-border bg-background p-5"
					>
						<p class="text-sm font-medium text-muted-foreground">
							Combined brand
						</p>
						<DiscobotBrand heightClass="h-8" />
					</div>
					<div
						class="space-y-3 rounded-2xl border border-border bg-background p-5"
					>
						<p class="text-sm font-medium text-muted-foreground">Logo mark</p>
						<DiscobotLogo size={56} />
					</div>
				</CardContent>
			</Card>

			<Card>
				<CardHeader>
					<CardTitle>App icon previews</CardTitle>
					<CardDescription>
						A quick look at the mark by itself on common launcher-style tiles.
					</CardDescription>
				</CardHeader>
				<CardContent class="grid gap-4 sm:grid-cols-3">
					<div
						class="space-y-3 rounded-2xl border border-border bg-white p-5 text-center"
					>
						<div
							class="mx-auto flex size-20 items-center justify-center rounded-2xl bg-white shadow-sm ring-1 ring-black/5"
						>
							<img
								src={logoPurpleUrl}
								alt="Discobot purple app icon"
								class="size-14"
							/>
						</div>
						<p class="text-xs text-muted-foreground">Light launcher</p>
					</div>
					<div
						class="space-y-3 rounded-2xl border border-border bg-[#111111] p-5 text-center"
					>
						<div
							class="mx-auto flex size-20 items-center justify-center rounded-2xl bg-[#1a1a1a] shadow-sm ring-1 ring-white/10"
						>
							<img
								src={logoWhiteUrl}
								alt="Discobot white app icon"
								class="size-14"
							/>
						</div>
						<p class="text-xs text-white/70">Dark launcher</p>
					</div>
					<div
						class="space-y-3 rounded-2xl border border-border bg-background p-5 text-center"
					>
						<div
							class="mx-auto flex size-20 items-center justify-center rounded-2xl bg-muted shadow-sm ring-1 ring-border"
						>
							<img
								src={logoBlackUrl}
								alt="Discobot black monochrome app icon"
								class="size-14"
							/>
						</div>
						<p class="text-xs text-muted-foreground">Monochrome</p>
					</div>
				</CardContent>
			</Card>
		</section>

		<section class="space-y-6">
			<div class="space-y-2">
				<h2 class="text-2xl font-semibold tracking-tight">Combined lockups</h2>
				<p class="text-sm leading-6 text-muted-foreground">
					These are the full brand lockups with the spacing preserved from the
					source SVG assets.
				</p>
			</div>

			<div class="grid gap-4 lg:grid-cols-3">
				{#each lockups as lockup}
					<Card>
						<CardHeader class="pb-3">
							<CardTitle class="text-base">{lockup.label}</CardTitle>
						</CardHeader>
						<CardContent>
							<div
								class={`flex min-h-40 items-center justify-center rounded-2xl border border-border p-6 ${lockup.bg}`}
							>
								<img
									src={lockup.src}
									alt={lockup.label}
									class="h-10 w-auto max-w-full"
								/>
							</div>
						</CardContent>
					</Card>
				{/each}
			</div>
		</section>

		<section class="space-y-6">
			<div class="space-y-2">
				<h2 class="text-2xl font-semibold tracking-tight">Logo-only marks</h2>
				<p class="text-sm leading-6 text-muted-foreground">
					These are the standalone marks intended for icons, avatars, and
					tighter UI placements.
				</p>
			</div>

			<div class="grid gap-4 lg:grid-cols-3">
				{#each marks as mark}
					<Card>
						<CardHeader class="pb-3">
							<CardTitle class="text-base">{mark.label}</CardTitle>
						</CardHeader>
						<CardContent>
							<div
								class={`flex min-h-40 items-center justify-center rounded-2xl border border-border p-6 ${mark.bg}`}
							>
								<img src={mark.src} alt={mark.label} class="h-24 w-auto" />
							</div>
						</CardContent>
					</Card>
				{/each}
			</div>
		</section>
	</div>
</div>
