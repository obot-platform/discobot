export async function enumerateAudioInputs(): Promise<MediaDeviceInfo[]> {
	const deviceList = await navigator.mediaDevices.enumerateDevices();
	return deviceList.filter((device) => device.kind === "audioinput");
}

export async function requestMicrophonePermission(): Promise<void> {
	const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
	for (const track of stream.getTracks()) {
		track.stop();
	}
}
