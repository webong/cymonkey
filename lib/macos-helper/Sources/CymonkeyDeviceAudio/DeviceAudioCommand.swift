import Foundation
import AVFoundation
import CoreMedia
import CoreGraphics
import CymonkeyMacOSRuntime

// stdout is a streaming float WAV for capture, never logs. ffmpeg in the Board
// parent normalizes this source's actual rate/channel layout to the wire codec.
final class AudioSink: DeviceMediaSink {
    private var format: AVAudioFormat?
    private let lock = NSLock()
    func screenSample(_ sample: CMSampleBuffer) {}
    func systemAudioSample(_ sample: CMSampleBuffer) {
        guard let description = CMSampleBufferGetFormatDescription(sample) else { exit(2) }
        let source = AVAudioFormat(cmAudioFormatDescription: description)
        guard let pcm = AVAudioPCMBuffer(pcmFormat: source, frameCapacity: AVAudioFrameCount(CMSampleBufferGetNumSamples(sample))) else { exit(2) }
        pcm.frameLength = pcm.frameCapacity
        guard CMSampleBufferCopyPCMDataIntoAudioBufferList(sample, at: 0, frameCount: Int32(pcm.frameLength), into: pcm.mutableAudioBufferList) == noErr else { exit(2) }
        write(pcm)
    }
    func microphoneSample(_ buffer: AVAudioPCMBuffer, at time: AVAudioTime) { write(buffer) }
    func mediaStopped(error: Error?) { exit(error == nil ? 0 : 2) }
    private func write(_ input: AVAudioPCMBuffer) {
        lock.lock(); defer { lock.unlock() }
        guard input.frameLength > 0, input.frameLength <= 192000,
              let outputFormat = AVAudioFormat(commonFormat: .pcmFormatFloat32, sampleRate: input.format.sampleRate, channels: input.format.channelCount, interleaved: false),
              let converter = AVAudioConverter(from: input.format, to: outputFormat),
              let output = AVAudioPCMBuffer(pcmFormat: outputFormat, frameCapacity: input.frameLength) else { exit(2) }
        if let format, format != outputFormat { exit(2) }
        var supplied = false
        var error: NSError?
        let status = converter.convert(to: output, error: &error) { _, state in
            if supplied { state.pointee = .noDataNow; return nil }
            supplied = true; state.pointee = .haveData; return input
        }
        guard status != .error, error == nil, let channels = output.floatChannelData else { exit(2) }
        if format == nil {
            format = outputFormat
            var header = Data()
            func word<T: FixedWidthInteger>(_ value: T) { var v = value.littleEndian; withUnsafeBytes(of: &v) { header.append(contentsOf: $0) } }
            header.append(contentsOf: "RIFF".utf8); word(UInt32.max); header.append(contentsOf: "WAVEfmt ".utf8)
            word(UInt32(16)); word(UInt16(3)); word(UInt16(outputFormat.channelCount))
            word(UInt32(outputFormat.sampleRate)); word(UInt32(outputFormat.sampleRate) * outputFormat.channelCount * 4)
            word(UInt16(outputFormat.channelCount * 4)); word(UInt16(32))
            header.append(contentsOf: "data".utf8); word(UInt32.max)
            do { try FileHandle.standardOutput.write(contentsOf: header) } catch { exit(2) }
        }
        var bytes = Data(capacity: Int(output.frameLength * outputFormat.channelCount * 4))
        for frame in 0..<Int(output.frameLength) {
            for channel in 0..<Int(outputFormat.channelCount) {
                var bits = channels[channel][frame].bitPattern.littleEndian
                withUnsafeBytes(of: &bits) { bytes.append(contentsOf: $0) }
            }
        }
        do { try FileHandle.standardOutput.write(contentsOf: bytes) } catch { exit(2) }
    }
}

@main struct DeviceAudioCommand {
    @MainActor static func main() async {
        do {
            let args = Array(CommandLine.arguments.dropFirst())
            if args.count == 2, args[0] == "pointer-session" { try NativePointer.session(source: args[1]); return }
            if args.count == 3, args[0] == "pointer" { try NativePointer.perform(source: args[1], json: args[2]); return }
            guard args.count == 2 else { throw Failure.invalidArguments }
            let mode = args[0], source = args[1]
            if mode == "describe" {
                let screenAllowed = CGPreflightScreenCaptureAccess()
                let microphone = AVCaptureDevice.authorizationStatus(for: .audio)
                var displays: [String] = []
                if screenAllowed { displays = try await DeviceMedia.displays().map { String($0.displayID) } }
                let report: [String: Any] = ["platform": "macos", "systemCapture": screenAllowed ? "authorized" : "permission-required",
                    "microphone": microphone == .authorized ? "authorized" : "permission-required",
                    "playback": "available", "systemSources": displays, "microphoneSources": ["default"], "playbackSources": ["default"]]
                try FileHandle.standardOutput.write(contentsOf: JSONSerialization.data(withJSONObject: report)); return
            }
            if mode == "playback" {
                guard source.isEmpty || source == "default" else { throw Failure.unsupportedSource }
                let player = try DevicePlayback(); defer { player.close() }
                var pending = Data()
                while let chunk = try FileHandle.standardInput.read(upToCount: 3840), !chunk.isEmpty {
                    pending.append(chunk)
                    let count = min(3840, pending.count / 4 * 4)
                    if count == 0 { continue }
                    let packet = Data(pending.prefix(count)); pending.removeFirst(count)
                    try player.write(packet)
                    try await Task.sleep(nanoseconds: UInt64(count) * 1_000_000_000 / 192000)
                }
                guard pending.isEmpty else { throw Failure.invalidArguments }
                return
            }
            let sink = AudioSink()
            let media = DeviceMedia(sink: sink)
            if mode == "capture-system" {
                guard CGPreflightScreenCaptureAccess(), let id = UInt32(source) else { throw Failure.permissionOrSource }
                try await media.start(displayID: id, systemAudio: true, framesPerSecond: 1)
            } else if mode == "capture-microphone" {
                guard source.isEmpty || source == "default" else { throw Failure.unsupportedSource }
                try media.startMicrophone()
            } else { throw Failure.invalidArguments }
            // The parent cancels the process on channel/session close. No daemon
            // or detached child survives its owning session.
            while true { try await Task.sleep(nanoseconds: 1_000_000_000); _ = media; _ = sink }
        } catch {
            try? FileHandle.standardError.write(contentsOf: Data("Native audio unavailable: \(error)\n".utf8)); exit(2)
        }
    }
    enum Failure: Error { case invalidArguments, unsupportedSource, permissionOrSource }
}
