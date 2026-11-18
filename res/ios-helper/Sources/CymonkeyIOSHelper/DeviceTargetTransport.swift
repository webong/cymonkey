#if os(iOS)
import Foundation
import ReplayKit
import AVFoundation
import CoreImage
import ImageIO

/// One host-issued target credential, one WebSocket, no operator credential.
/// The host passes this object to DeviceCapture or DeviceBroadcastHandler's sink.
public final class DeviceTargetTransport: DeviceCaptureSink {
    private let socket: URLSessionWebSocketTask
    private let queue = DispatchQueue(label: "dev.cymonkey.device.target")
    private var pending: [Data] = []
    private let budget = DispatchSemaphore(value: 8)
    private var sending = false
    private var closed = false
    private let onStop: (Error?) -> Void
    private let playback: ((Data) throws -> Void)?
    private let video: Bool, audio: Bool, microphone: Bool
    private var converters: [Int: AVAudioConverter] = [:]
    private let imageContext = CIContext()
    private var lastVideo: TimeInterval = 0
    private let sampleLock = NSLock()

    public init(url: URL, token: String, video: Bool, audio: Bool, microphone: Bool,
                playback: ((Data) throws -> Void)? = nil, onStop: @escaping (Error?) -> Void) throws {
        guard url.scheme == "wss" || (url.scheme == "ws" && ["localhost", "127.0.0.1", "::1"].contains(url.host ?? "")),
              url.user == nil, url.password == nil, url.query == nil, url.fragment == nil,
              token.count == 64 else { throw Failure.invalidEndpoint }
        var request = URLRequest(url: url)
        request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        socket = URLSession.shared.webSocketTask(with: request)
        self.video = video; self.audio = audio; self.microphone = microphone
        self.playback = playback; self.onStop = onStop
    }
    public func connect() { socket.resume(); receivePlayback() }
    public func close() { queue.async { self.finish(nil) } }
    private func finish(_ error: Error?) {
        guard !closed else { return }; closed = true
        for _ in pending { budget.signal() }; pending.removeAll()
        socket.cancel(with: .goingAway, reason: nil); onStop(error)
    }
    public func stopped(error: Error?) { queue.async { self.finish(error) } }
    private func publish(_ kind: UInt8, _ bytes: Data) {
        guard budget.wait(timeout: .now()) == .success else { stopped(error: Failure.backpressure); return }
        queue.async {
            guard !self.closed else { self.budget.signal(); return }
            guard self.pending.count < 8 else { self.finish(Failure.backpressure); return }
            var message = Data([kind]); message.append(bytes); self.pending.append(message); self.drain()
        }
    }
    private func drain() {
        guard !sending, !closed, !pending.isEmpty else { return }
        sending = true
        socket.send(.data(pending.removeFirst())) { error in
            self.queue.async { self.budget.signal(); self.sending = false; if let error { self.finish(error) } else { self.drain() } }
        }
    }
    private func receivePlayback() {
        socket.receive { result in
            self.queue.async {
                guard !self.closed else { return }
                do {
                    guard case .data(let data) = try result.get(), data.first == 4,
                          data.count > 1, data.count <= 3841, (data.count - 1) % 4 == 0,
                          let playback = self.playback else { throw Failure.invalidFrame }
                    try playback(Data(data.dropFirst())); self.receivePlayback()
                } catch { self.finish(error) }
            }
        }
    }
    public func receive(_ sample: CMSampleBuffer, kind: RPSampleBufferType) {
        sampleLock.lock(); defer { sampleLock.unlock() }
        do {
            if kind == .video {
                guard video, ProcessInfo.processInfo.systemUptime - lastVideo >= 0.1,
                      let pixels = CMSampleBufferGetImageBuffer(sample) else { return }
                lastVideo = ProcessInfo.processInfo.systemUptime
                var image = CIImage(cvPixelBuffer: pixels)
                if let value = CMGetAttachment(sample, key: RPVideoSampleOrientationKey as CFString, attachmentModeOut: nil) as? NSNumber,
                   let orientation = CGImagePropertyOrientation(rawValue: value.uint32Value) { image = image.oriented(orientation) }
                let scale = min(1, 1280 / max(image.extent.width, image.extent.height))
                image = image.transformed(by: CGAffineTransform(scaleX: scale, y: scale))
                guard let jpeg = imageContext.jpegRepresentation(of: image, colorSpace: CGColorSpaceCreateDeviceRGB(), options: [:]), jpeg.count <= 4 << 20 else { throw Failure.invalidFrame }
                publish(1, jpeg); return
            }
            guard (kind == .audioApp && audio) || (kind == .audioMic && microphone) else { return }
            guard let description = CMSampleBufferGetFormatDescription(sample) else { throw Failure.invalidFrame }
            let format = AVAudioFormat(cmAudioFormatDescription: description)
            let count = CMSampleBufferGetNumSamples(sample)
            guard count > 0, count <= 96000, format.sampleRate >= 8000,
                  let input = AVAudioPCMBuffer(pcmFormat: format, frameCapacity: AVAudioFrameCount(count)),
                  let target = AVAudioFormat(commonFormat: .pcmFormatInt16, sampleRate: 48000, channels: 2, interleaved: true) else { throw Failure.invalidFrame }
            input.frameLength = input.frameCapacity
            guard CMSampleBufferCopyPCMDataIntoAudioBufferList(sample, at: 0, frameCount: Int32(count), into: input.mutableAudioBufferList) == noErr else { throw Failure.invalidFrame }
            let key = kind.rawValue
            if converters[key]?.inputFormat != format { converters[key] = AVAudioConverter(from: format, to: target) }
            guard let converter = converters[key],
                  let output = AVAudioPCMBuffer(pcmFormat: target, frameCapacity: AVAudioFrameCount(ceil(Double(count) * 48000 / format.sampleRate) + 32)) else { throw Failure.invalidFrame }
            var used = false; var error: NSError?
            let status = converter.convert(to: output, error: &error) { _, state in
                if used { state.pointee = .noDataNow; return nil }; used = true; state.pointee = .haveData; return input
            }
            guard status != .error, error == nil, let bytes = output.audioBufferList.pointee.mBuffers.mData else { throw Failure.invalidFrame }
            let data = Data(bytes: bytes, count: Int(output.frameLength) * 4)
            for offset in stride(from: 0, to: data.count, by: 3840) { publish(kind == .audioApp ? 2 : 3, data.subdata(in: offset..<min(data.count, offset + 3840))) }
        } catch { stopped(error: error) }
    }
    public enum Failure: Error { case invalidEndpoint, invalidFrame, backpressure }
}
#endif
