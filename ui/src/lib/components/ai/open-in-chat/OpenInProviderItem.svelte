<script lang="ts">
	import { DropdownMenuItem } from "$lib/components/ui/dropdown-menu";
	import { openUrl } from "$lib/tauri";
	import { useOpenInChatContext } from "./context";
	import { ExternalLinkIcon, providers, type OpenInProvider } from "./providers";

	type Props = {
		provider: OpenInProvider;
	};

	let { provider, ...restProps }: Props = $props();
	const openIn = useOpenInChatContext();
	const providerMeta = $derived.by(() => providers[provider]);
	const ProviderIcon = $derived.by(() => providerMeta.icon);

	async function handleOpen() {
		const url = providerMeta.createUrl(openIn.query);
		await openUrl(url);
	}
</script>

<DropdownMenuItem onclick={handleOpen} class="flex items-center gap-2" {...restProps}>
	<ProviderIcon class="size-4 shrink-0" />
	<span class="flex-1">{providerMeta.title}</span>
	<ExternalLinkIcon class="size-4 shrink-0" />
</DropdownMenuItem>
