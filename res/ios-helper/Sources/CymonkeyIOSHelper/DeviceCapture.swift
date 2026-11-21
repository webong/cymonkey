#if os(iOS)
import ReplayKit
import CoreMedia
import ExtSystem

/// The authenticated host owns encoding and transport. Consume the sample
/// synchronously or retain it explicitly; never queue unbounded video frames.
public protocol DeviceCaptureSink: AnyObject {
    func receive(_ sample: CMSampleBuffer, kind: RPSampleBufferType)
    func stopped(error: Error?)
}

/// App capture on iOS. Cross-app capture uses DeviceBroadcastHandler in an
/// RPBroadcast upload extension selected through RPSystemBroadcastPickerView.
@MainActor public final class DeviceCapture {
    private let bridge: CaptureSinkBridge
    private let native: SystemDeviceCapture

    public init(sink: DeviceCaptureSink) {
        let bridge = CaptureSinkBridge(sink: sink)
        self.bridge = bridge
        self.native = SystemDeviceCapture(sink: bridge)
    }

    public func start(systemAudio: Bool = false, microphone: Bool = false) async throws {
        try await native.start(systemAudio: systemAudio, microphone: microphone, maxDuration: 3600)
    }

    public func stop() async throws {
        try await native.stop()
    }
}

private final class CaptureSinkBridge: SystemDeviceCaptureSink {
    private weak var sink: DeviceCaptureSink?

    init(sink: DeviceCaptureSink) { self.sink = sink }
    func receive(_ sample: CMSampleBuffer, kind: RPSampleBufferType) { sink?.receive(sample, kind: kind) }
    func mediaStopped(error: Error?) { sink?.stopped(error: error) }
}

/// Subclass in the host's Broadcast Upload Extension and override makeSink.
/// The sink must authenticate its session before accepting any sample. No
/// arbitrary system input is exposed by this adapter.
open class DeviceBroadcastHandler: RPBroadcastSampleHandler {
    private var sink: DeviceCaptureSink?
    private var paused = false
    /// Override from the authenticated session grant, independently of OS consent.
    open func permits(_ kind: RPSampleBufferType) -> Bool { kind == .video }
    open func makeSink(setupInfo: [String: NSObject]?) throws -> DeviceCaptureSink {
        throw NSError(domain: "CymonkeyDeviceCapture", code: 2,
                      userInfo: [NSLocalizedDescriptionKey: "Host must supply an authenticated media sink"])
    }
    open override func broadcastStarted(withSetupInfo setupInfo: [String: NSObject]?) {
        do { sink = try makeSink(setupInfo: setupInfo) }
        catch { finishBroadcastWithError(error) }
    }
    open override func processSampleBuffer(_ sampleBuffer: CMSampleBuffer, with sampleBufferType: RPSampleBufferType) {
        guard !paused, permits(sampleBufferType), CMSampleBufferDataIsReady(sampleBuffer) else { return }
        sink?.receive(sampleBuffer, kind: sampleBufferType)
    }
    open override func broadcastPaused() { paused = true }
    open override func broadcastResumed() { paused = false }
    open override func broadcastFinished() { sink?.stopped(error: nil); sink = nil }
}
#endif
