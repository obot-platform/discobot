<script lang="ts">
	import { cn } from "$lib/utils";

	type Props = {
		base64?: string;
		uint8Array?: Uint8Array;
		mediaType: string;
		alt?: string;
		class?: string;
	};

	let {
		base64,
		uint8Array,
		mediaType,
		alt = "",
		class: className,
	}: Props = $props();

	const src = $derived.by(() => {
		if (base64) {
			return `data:${mediaType};base64,${base64}`;
		}
		if (uint8Array) {
			let binary = "";
			for (const byte of uint8Array) {
				binary += String.fromCharCode(byte);
			}
			return `data:${mediaType};base64,${btoa(binary)}`;
		}
		return "";
	});
</script>

<img
	{alt}
	class={cn("h-auto max-w-full overflow-hidden rounded-md", className)}
	{src}
/>
