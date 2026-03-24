<script lang="ts">
	import { Popover } from "$lib/components/ui/popover";
	import { onMount } from "svelte";
	import {
		enumerateAudioInputs,
		requestMicrophonePermission,
	} from "./audio-devices";
	import { setMicSelectorContext } from "./context";

	type Props = {
		value?: string;
		open?: boolean;
		onValueChange?: (value: string | undefined) => void;
		onOpenChange?: (open: boolean) => void;
		children?: () => any;
	};

	let {
		value = $bindable(undefined),
		open = $bindable(false),
		onValueChange,
		onOpenChange,
		children,
		...restProps
	}: Props = $props();

	let devices = $state<MediaDeviceInfo[]>([]);
	let loading = $state(false);
	let error = $state<string | null>(null);
	let hasPermission = $state(false);
	let width = $state(200);

	async function loadDevicesWithoutPermission() {
		if (typeof navigator === "undefined" || !navigator.mediaDevices) {
			return;
		}
		try {
			loading = true;
			error = null;
			devices = await enumerateAudioInputs();
		} catch (loadError) {
			error =
				loadError instanceof Error
					? loadError.message
					: "Failed to get audio devices";
		} finally {
			loading = false;
		}
	}

	async function loadDevicesWithPermission() {
		if (
			typeof navigator === "undefined" ||
			!navigator.mediaDevices ||
			loading
		) {
			return;
		}
		try {
			loading = true;
			error = null;
			await requestMicrophonePermission();
			devices = await enumerateAudioInputs();
			hasPermission = true;
		} catch (loadError) {
			error =
				loadError instanceof Error
					? loadError.message
					: "Failed to get audio devices";
		} finally {
			loading = false;
		}
	}

	const micSelector = $state({
		data: [] as MediaDeviceInfo[],
		loading: false,
		error: null as string | null,
		hasPermission: false,
		loadDevices: loadDevicesWithPermission,
		value: undefined as string | undefined,
		setValue: (nextValue: string | undefined) => {
			value = nextValue;
			onValueChange?.(nextValue);
		},
		open: false,
		setOpen: (nextOpen: boolean) => {
			open = nextOpen;
			onOpenChange?.(nextOpen);
		},
		width: 200,
		setWidth: (nextWidth: number) => {
			width = nextWidth;
		},
	});

	$effect(() => {
		micSelector.data = devices;
		micSelector.loading = loading;
		micSelector.error = error;
		micSelector.hasPermission = hasPermission;
		micSelector.value = value;
		micSelector.open = open;
		micSelector.width = width;
	});

	$effect(() => {
		if (open && !hasPermission && !loading) {
			void loadDevicesWithPermission();
		}
	});

	onMount(() => {
		void loadDevicesWithoutPermission();

		if (typeof navigator === "undefined" || !navigator.mediaDevices) {
			return;
		}

		const handleDeviceChange = () => {
			if (micSelector.hasPermission) {
				void loadDevicesWithPermission();
			} else {
				void loadDevicesWithoutPermission();
			}
		};

		navigator.mediaDevices.addEventListener("devicechange", handleDeviceChange);
		return () => {
			navigator.mediaDevices.removeEventListener(
				"devicechange",
				handleDeviceChange,
			);
		};
	});

	setMicSelectorContext(micSelector);
</script>

<Popover bind:open {...restProps}>
	{@render children?.()}
</Popover>
