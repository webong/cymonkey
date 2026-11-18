import AVFoundation
import Foundation

/// Bounded PCM16 little-endian stereo 48 kHz playback. The host must grant
/// audio.playback and, on iOS, configure its AVAudioSession before construction.
public final class DevicePlayback {
    private let engine = AVAudioEngine()
    private let player = AVAudioPlayerNode()
    private let lock = NSLock()
    private var pending = 0
    private var closed = false
    private let format = AVAudioFormat(standardFormatWithSampleRate: 48000, channels: 2)!
    public init() throws {
        engine.attach(player)
        engine.connect(player, to: engine.mainMixerNode, format: format)
        try engine.start(); player.play()
    }
    /// One packet is at most 20 ms; at most 12 packets may be queued.
    /// Calls, including close, must be serialized by the host session.
    public func write(_ pcm: Data) throws {
        guard !pcm.isEmpty, pcm.count % 4 == 0, pcm.count <= 3840 else {
            throw NSError(domain: "CymonkeyDevicePlayback", code: 1,
                          userInfo: [NSLocalizedDescriptionKey: "Invalid PCM packet"])
        }
        lock.lock()
        guard !closed, pending < 12 else {
            lock.unlock()
            throw NSError(domain: "CymonkeyDevicePlayback", code: 2,
                          userInfo: [NSLocalizedDescriptionKey: "Playback closed or buffer full"])
        }
        pending += 1; lock.unlock()
        let frames = pcm.count / 4
        let buffer = AVAudioPCMBuffer(pcmFormat: format, frameCapacity: AVAudioFrameCount(frames))!
        buffer.frameLength = AVAudioFrameCount(frames)
        pcm.withUnsafeBytes { (bytes: UnsafeRawBufferPointer) in
            for frame in 0..<frames {
                for channel in 0..<2 {
                    let offset = frame * 4 + channel * 2
                    let value = UInt16(bytes[offset]) | UInt16(bytes[offset + 1]) << 8
                    buffer.floatChannelData![channel][frame] = Float(Int16(bitPattern: value)) / 32768
                }
            }
        }
        player.scheduleBuffer(buffer, completionCallbackType: .dataPlayedBack) { [weak self] _ in
            guard let self else { return }
            self.lock.lock(); self.pending = max(0, self.pending - 1); self.lock.unlock()
        }
    }
    public func close() {
        lock.lock(); let wasClosed = closed; closed = true; lock.unlock()
        if !wasClosed { player.stop(); engine.stop() }
    }
    deinit { close() }
}
