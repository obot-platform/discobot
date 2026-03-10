<script lang="ts">
	import { cn } from "$lib/utils";

	type Props = {
		value?: string;
		class?: string;
		children?: () => any;
	};

	let { value, class: className, children, ...restProps }: Props = $props();

	const accentCodes: Record<string, string> = {
		american: "US",
		british: "UK",
		australian: "AU",
		canadian: "CA",
		irish: "IE",
		scottish: "SC",
		indian: "IN",
		"south-african": "ZA",
		"new-zealand": "NZ",
		spanish: "ES",
		french: "FR",
		german: "DE",
		italian: "IT",
		portuguese: "PT",
		brazilian: "BR",
		mexican: "MX",
		argentinian: "AR",
		japanese: "JP",
		chinese: "CN",
		korean: "KR",
		russian: "RU",
		arabic: "AR",
		dutch: "NL",
		swedish: "SE",
		norwegian: "NO",
		danish: "DK",
		finnish: "FI",
		polish: "PL",
		turkish: "TR",
		greek: "GR",
	};

	const accentLabel = $derived.by(() => {
		if (!value) {
			return null;
		}
		return accentCodes[value] ?? value;
	});
</script>

<span class={cn("text-muted-foreground text-xs", className)} {...restProps}>
	{#if children}
		{@render children()}
	{:else}
		{accentLabel}
	{/if}
</span>
