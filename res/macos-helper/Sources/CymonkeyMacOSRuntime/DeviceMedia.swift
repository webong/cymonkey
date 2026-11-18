import AVFoundation
import CoreMedia
import ScreenCaptureKit

/// Native capture owns no network credentials. The host encodes these samples
/// and attaches them to an explicitly granted device session.
public protocol DeviceMediaSink: AnyObject {
    func screenSample(_ sample: CMSampleBuffer)
    func systemAudioSample(_ sample: CMSampleBuffer)
    func microphoneSample(_ buffer: AVAudioPCMBuffer, at time: AVAudioTime)
    func mediaStopped(error: Error?)
}

@available(macOS 13.0, *)
public final class DeviceMedia: NSObject, SCStreamOutput, SCStreamDelegate {
    public weak var sink: DeviceMediaSink?
    private var stream: SCStream?
    private var starting = false
    private var generation: UInt = 0
    private var microphone: AVAudioEngine?
    private let queue = DispatchQueue(label: "dev.cymonkey.device.capture")
    public init(sink: DeviceMediaSink) { self.sink = sink }

    /// Enumerate after host permission checks. The caller explicitly chooses a
    /// display ID; IDs received over an untrusted session are not host approval.
    public static func displays() async throws -> [SCDisplay] {
        try await SCShareableContent.excludingDesktopWindows(false, onScreenWindowsOnly: true).displays
    }
    @MainActor public func start(displayID: CGDirectDisplayID, systemAudio: Bool, framesPerSecond: Int = 30) async throws {
        guard stream == nil, !starting, (1...60).contains(framesPerSecond) else {
            throw NSError(domain: "CymonkeyDeviceMedia", code: 1)
        }
        starting = true
        generation &+= 1
        let currentGeneration = generation
        defer { starting = false }
        let content = try await SCShareableContent.excludingDesktopWindows(false, onScreenWindowsOnly: true)
        guard currentGeneration == generation else { throw CancellationError() }
        guard let display = content.displays.first(where: { $0.displayID == displayID }) else {
            throw NSError(domain: "CymonkeyDeviceMedia", code: 2,
                          userInfo: [NSLocalizedDescriptionKey: "Approved display is unavailable"])
        }
        let config = SCStreamConfiguration()
        config.width = display.width; config.height = display.height
        config.minimumFrameInterval = CMTime(value: 1, timescale: CMTimeScale(framesPerSecond))
        config.queueDepth = 3
        config.capturesAudio = systemAudio
        config.excludesCurrentProcessAudio = true
        config.sampleRate = 48000; config.channelCount = 2
        let capture = SCStream(filter: SCContentFilter(display: display, excludingWindows: []), configuration: config, delegate: self)
        try capture.addStreamOutput(self, type: .screen, sampleHandlerQueue: queue)
        if systemAudio { try capture.addStreamOutput(self, type: .audio, sampleHandlerQueue: queue) }
        stream = capture
        do {
            try await capture.startCapture()
            if currentGeneration != generation {
                try await capture.stopCapture()
                throw CancellationError()
            }
        } catch { if stream === capture { stream = nil }; throw error }
    }
    @MainActor public func startMicrophone() throws {
        guard microphone == nil, AVCaptureDevice.authorizationStatus(for: .audio) == .authorized else {
            throw NSError(domain: "CymonkeyDeviceMedia", code: 3,
                          userInfo: [NSLocalizedDescriptionKey: "Microphone grant and OS permission required"])
        }
        let engine = AVAudioEngine()
        let input = engine.inputNode
        let format = input.outputFormat(forBus: 0)
        guard format.sampleRate > 0, format.channelCount > 0 else { throw NSError(domain: "CymonkeyDeviceMedia", code: 4) }
        input.installTap(onBus: 0, bufferSize: 960, format: format) { [weak self] buffer, time in
            self?.sink?.microphoneSample(buffer, at: time)
        }
        do { try engine.start(); microphone = engine }
        catch { input.removeTap(onBus: 0); throw error }
    }
    @MainActor public func stop() async throws {
        generation &+= 1;
        microphone?.inputNode.removeTap(onBus: 0); microphone?.stop(); microphone = nil
        let capture = stream; stream = nil
        try await capture?.stopCapture()
        sink?.mediaStopped(error: nil)
    }
    public func stream(_ stream: SCStream, didOutputSampleBuffer sampleBuffer: CMSampleBuffer, of type: SCStreamOutputType) {
        guard CMSampleBufferDataIsReady(sampleBuffer) else { return }
        switch type {
        case .screen: sink?.screenSample(sampleBuffer)
        case .audio: sink?.systemAudioSample(sampleBuffer)
        default: break
        }
    }
    public func stream(_ stream: SCStream, didStopWithError error: Error) {
        Task { @MainActor [weak self] in
            guard let self, self.stream === stream else { return }
            self.stream = nil
            self.microphone?.inputNode.removeTap(onBus: 0); self.microphone?.stop(); self.microphone = nil
            self.sink?.mediaStopped(error: error)
        }
    }
}
