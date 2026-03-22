<script lang="ts">
	import ChevronDownIcon from "@lucide/svelte/icons/chevron-down";
	import type { DropdownMenu as DropdownMenuPrimitive } from "bits-ui";
	import type { ButtonProps, ButtonSize, ButtonVariant } from "$lib/components/ui/button";
	import { Button } from "$lib/components/ui/button";
	import {
		DropdownMenu,
		DropdownMenuContent,
		DropdownMenuTrigger,
	} from "$lib/components/ui/dropdown-menu";
	import { cn } from "$lib/utils";

	type Props = {
		label: string;
		menuAriaLabel: string;
		class?: string;
		contentClass?: string;
		children?: () => any;
		onclick?: ButtonProps["onclick"];
		variant?: ButtonVariant;
		size?: ButtonSize;
		type?: ButtonProps["type"];
		disabled?: boolean;
		primaryDisabled?: boolean;
		align?: DropdownMenuPrimitive.ContentProps["align"];
		sideOffset?: DropdownMenuPrimitive.ContentProps["sideOffset"];
	};

	let {
		label,
		menuAriaLabel,
		class: className,
		contentClass,
		children,
		onclick,
		variant = "outline",
		size = "xs",
		type = "button",
		disabled = false,
		primaryDisabled = false,
		align = "end",
		sideOffset = 8,
	}: Props = $props();

	const iconClass = $derived(size === "xs" || size === "icon-xs" ? "size-3.5" : "size-4");
	const useSharedOutlineBorder = $derived(variant === "outline");
	const groupClass = $derived(
		useSharedOutlineBorder
			? "inline-flex items-center overflow-hidden rounded-md border border-border bg-background p-0.5 shadow-xs"
			: "inline-flex items-center",
	);
	const primaryButtonClass = $derived(
		useSharedOutlineBorder
			? "rounded-l-[calc(var(--radius)-1px)] rounded-r-none border-0 bg-transparent shadow-none dark:bg-transparent"
			: "rounded-r-none",
	);
	const triggerButtonClass = $derived(
		useSharedOutlineBorder
			? "rounded-r-[calc(var(--radius)-1px)] rounded-l-none border-0 border-l border-border bg-transparent px-2 shadow-none dark:bg-transparent"
			: "rounded-l-none border-l-0 px-2",
	);
</script>

<DropdownMenu>
	<div class={cn(groupClass, className)}>
		<Button
			{variant}
			{size}
			{type}
			disabled={disabled || primaryDisabled}
			{onclick}
			class={primaryButtonClass}
		>
			{label}
		</Button>
		<DropdownMenuTrigger>
			<Button
				{variant}
				{size}
				{type}
				{disabled}
				class={triggerButtonClass}
				aria-label={menuAriaLabel}
			>
				<ChevronDownIcon class={iconClass} />
			</Button>
		</DropdownMenuTrigger>
	</div>
	<DropdownMenuContent {align} {sideOffset} class={contentClass}>
		{@render children?.()}
	</DropdownMenuContent>
</DropdownMenu>
