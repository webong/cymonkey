import ExtAudio

/// The host still owns the playback grant, AVAudioSession, and session lifetime.
/// PCM framing and bounded native playback come from the pinned ext iOS SDK.
public typealias DevicePlayback = ExtAudio.SystemAudioPlayback
