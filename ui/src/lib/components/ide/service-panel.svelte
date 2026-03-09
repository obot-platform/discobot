<script lang="ts">
	import { Button } from "$lib/components/ui/button";
	import type { ServiceItem } from "$lib/shell-types";

	type Props = {
		onClose: () => void;
		service: ServiceItem;
	};

	let { onClose, service }: Props = $props();
</script>

<div class="space-y-4">
	<div class="flex items-center justify-between gap-3">
		<div>
			<p class="text-sm font-medium">{service.label} service</p>
			<p class="mt-1 text-xs text-muted-foreground">
				User-defined service exposed at {service.target}
			</p>
		</div>
		<Button variant="outline" size="sm" onclick={onClose}>Close panel</Button>
	</div>
	<div class="space-y-3">
		<div class="rounded-xl border border-border bg-background/70 px-3 py-3 text-sm">
			<p class="font-medium">Target</p>
			<p class="mt-1 text-muted-foreground">{service.target}</p>
		</div>
		<div class="rounded-xl border border-border bg-background/70 px-3 py-3 text-sm">
			<p class="font-medium">Status</p>
			<p class="mt-1 text-muted-foreground">Healthy and reachable</p>
		</div>
		<div class="rounded-2xl border border-border bg-background/70 p-4 font-mono text-sm leading-6 text-muted-foreground">
			<p>&gt; opening user-defined service: {service.label}</p>
			<p>&gt; proxy ready at {service.target}</p>
			<p>&gt; rendering service preview...</p>
		</div>
	</div>
</div>
