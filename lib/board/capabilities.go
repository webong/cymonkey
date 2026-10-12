package board

// Standard device operations. Providers advertise only operations their selected
// target implements; the host chooses the subset granted to each session.
const (
	PointerMove     Capability = "pointer.move"
	PointerClick    Capability = "pointer.click"
	PointerScroll   Capability = "pointer.scroll"
	PointerDown     Capability = "pointer.down"
	PointerUp       Capability = "pointer.up"
	PointerRelease  Capability = "pointer.release"
	DisplayDescribe Capability = "display.describe"
	DisplayCapture  Capability = "display.capture"
	DisplayVideo    Capability = "display.video"
	RecordingStart  Capability = "recording.start"
	RecordingStop   Capability = "recording.stop"
	RecordingRead   Capability = "recording.read"
	DeviceHealth    Capability = "device.health"
	PermissionOpen  Capability = "permission.open"
	AudioDescribe   Capability = "audio.describe"
	AudioCapture    Capability = "audio.capture"
	AudioPlayback   Capability = "audio.playback"
	AudioMicrophone Capability = "audio.microphone"
	AudioConfigure  Capability = "audio.configure"
	StorageInspect  Capability = "storage.inspect"
	StorageExport   Capability = "storage.export"
	StorageRestore  Capability = "storage.restore"
	StorageUpload   Capability = "storage.upload"
	RemoteConnect   Capability = "remote.connect"
	WebRTCSignal    Capability = "webrtc.signal"
	WebRTCPoll      Capability = "webrtc.poll"
)
