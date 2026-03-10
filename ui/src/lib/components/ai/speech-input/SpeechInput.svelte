<script lang="ts">
	import LoaderIcon from "@lucide/svelte/icons/loader";
	import MicIcon from "@lucide/svelte/icons/mic";
	import SquareIcon from "@lucide/svelte/icons/square";
	import { Button } from "$lib/components/ui/button";
	import { cn } from "$lib/utils";
	import { onDestroy, onMount } from "svelte";

	interface SpeechRecognition extends EventTarget {
		continuous: boolean;
		interimResults: boolean;
		lang: string;
		start(): void;
		stop(): void;
		onstart: ((this: SpeechRecognition, ev: Event) => void) | null;
		onend: ((this: SpeechRecognition, ev: Event) => void) | null;
		onresult:
			| ((this: SpeechRecognition, ev: SpeechRecognitionEvent) => void)
			| null;
		onerror:
			| ((this: SpeechRecognition, ev: SpeechRecognitionErrorEvent) => void)
			| null;
	}

	interface SpeechRecognitionEvent extends Event {
		results: SpeechRecognitionResultList;
		resultIndex: number;
	}

	interface SpeechRecognitionResultList {
		readonly length: number;
		item(index: number): SpeechRecognitionResult;
		[index: number]: SpeechRecognitionResult;
	}

	interface SpeechRecognitionResult {
		readonly length: number;
		item(index: number): SpeechRecognitionAlternative;
		[index: number]: SpeechRecognitionAlternative;
		isFinal: boolean;
	}

	interface SpeechRecognitionAlternative {
		transcript: string;
		confidence: number;
	}

interface SpeechRecognitionErrorEvent extends Event {
	error: string;
}

	type SpeechRecognitionConstructor = new () => SpeechRecognition;

	type SpeechRecognitionWindow = Window &
		typeof globalThis & {
			SpeechRecognition?: SpeechRecognitionConstructor;
			webkitSpeechRecognition?: SpeechRecognitionConstructor;
		};

	type SpeechInputMode = "speech-recognition" | "media-recorder" | "none";

	type Props = {
		onTranscriptionChange?: (text: string) => void;
		onAudioRecorded?: (audioBlob: Blob) => Promise<string>;
		lang?: string;
		class?: string;
		disabled?: boolean;
	};

	let {
		onTranscriptionChange,
		onAudioRecorded,
		lang = "en-US",
		class: className,
		disabled = false,
		...restProps
	}: Props = $props();

	let isListening = $state(false);
	let isProcessing = $state(false);
	let mode = $state<SpeechInputMode>("none");
	let recognition = $state<SpeechRecognition | null>(null);
	let mediaRecorder = $state<MediaRecorder | null>(null);
	let audioChunks = $state<Blob[]>([]);

	function getSpeechRecognitionConstructor():
		| SpeechRecognitionConstructor
		| undefined {
		const speechWindow = window as SpeechRecognitionWindow;
		return speechWindow.SpeechRecognition ?? speechWindow.webkitSpeechRecognition;
	}

	function detectSpeechInputMode(): SpeechInputMode {
		if (typeof window === "undefined") {
			return "none";
		}
		if (getSpeechRecognitionConstructor()) {
			return "speech-recognition";
		}
		if (typeof MediaRecorder !== "undefined" && navigator.mediaDevices) {
			return "media-recorder";
		}
		return "none";
	}

	function setupSpeechRecognition() {
		if (mode !== "speech-recognition") {
			return;
		}

		const SpeechRecognitionConstructor = getSpeechRecognitionConstructor();
		if (!SpeechRecognitionConstructor) {
			return;
		}

		const speechRecognition = new SpeechRecognitionConstructor();
		speechRecognition.continuous = true;
		speechRecognition.interimResults = true;
		speechRecognition.lang = lang;

		speechRecognition.onstart = () => {
			isListening = true;
		};

		speechRecognition.onend = () => {
			isListening = false;
		};

		speechRecognition.onresult = (event: SpeechRecognitionEvent) => {
			let finalTranscript = "";
			for (let i = event.resultIndex; i < event.results.length; i += 1) {
				const result = event.results[i];
				if (result.isFinal) {
					finalTranscript += result[0]?.transcript ?? "";
				}
			}
			if (finalTranscript) {
				onTranscriptionChange?.(finalTranscript);
			}
		};

		speechRecognition.onerror = () => {
			isListening = false;
		};

		recognition = speechRecognition;
	}

	async function startMediaRecorder() {
		if (!onAudioRecorded) {
			return;
		}

		try {
			const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
			const recorder = new MediaRecorder(stream);
			audioChunks = [];

			recorder.ondataavailable = (event) => {
				if (event.data.size > 0) {
					audioChunks = [...audioChunks, event.data];
				}
			};

			recorder.onstop = async () => {
				for (const track of stream.getTracks()) {
					track.stop();
				}

				const audioBlob = new Blob(audioChunks, { type: "audio/webm" });
				if (audioBlob.size > 0) {
					isProcessing = true;
					try {
						const transcript = await onAudioRecorded(audioBlob);
						if (transcript) {
							onTranscriptionChange?.(transcript);
						}
					} finally {
						isProcessing = false;
					}
				}
			};

			recorder.onerror = () => {
				isListening = false;
				for (const track of stream.getTracks()) {
					track.stop();
				}
			};

			mediaRecorder = recorder;
			recorder.start();
			isListening = true;
		} catch {
			isListening = false;
		}
	}

	function stopMediaRecorder() {
		if (mediaRecorder?.state === "recording") {
			mediaRecorder.stop();
		}
		isListening = false;
	}

	function toggleListening() {
		if (mode === "speech-recognition" && recognition) {
			if (isListening) {
				recognition.stop();
			} else {
				recognition.start();
			}
			return;
		}

		if (mode === "media-recorder") {
			if (isListening) {
				stopMediaRecorder();
			} else {
				void startMediaRecorder();
			}
		}
	}

	onMount(() => {
		mode = detectSpeechInputMode();
		setupSpeechRecognition();
	});

	$effect(() => {
		if (mode === "speech-recognition" && recognition) {
			recognition.lang = lang;
		}
	});

	onDestroy(() => {
		recognition?.stop();
		if (mediaRecorder?.state === "recording") {
			mediaRecorder.stop();
		}
	});

	const isDisabled = $derived.by(
		() =>
			disabled ||
			mode === "none" ||
			(mode === "speech-recognition" && !recognition) ||
			(mode === "media-recorder" && !onAudioRecorded) ||
			isProcessing,
	);
</script>

<div class="relative inline-flex items-center justify-center">
	{#if isListening}
		{#each [0, 1, 2] as ring}
			<div
				class="absolute inset-0 animate-ping rounded-full border-2 border-red-400/30"
				style={`animation-delay: ${ring * 0.3}s; animation-duration: 2s;`}
			></div>
		{/each}
	{/if}

	<Button
		class={cn(
			"relative z-10 rounded-full transition-all duration-300",
			isListening
				? "bg-destructive text-white hover:bg-destructive/80 hover:text-white"
				: "bg-primary text-primary-foreground hover:bg-primary/80 hover:text-primary-foreground",
			className,
		)}
		disabled={isDisabled}
		onclick={toggleListening}
		{...restProps}
	>
		{#if isProcessing}
			<LoaderIcon class="size-4 animate-spin" />
		{:else if isListening}
			<SquareIcon class="size-4" />
		{:else}
			<MicIcon class="size-4" />
		{/if}
	</Button>
</div>
