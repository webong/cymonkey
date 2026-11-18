#if os(iOS)
import ReplayKit
import CoreMedia

/// The authenticated host owns encoding and transport. Consume the sample
/// synchronously or retain it explicitly; never queue unbounded video frames.
public protocol DeviceCaptureSink: AnyObject {
    func receive(_ sample: CMSampleBuffer, kind: RPSampleBufferType)
    func stopped(error: Error?)
}

/// App capture on iOS. Cross-app capture uses DeviceBroadcastHandler in an
/// RPBroadcast upload extension selected through RPSystemBroadcastPickerView.
@MainActor public final class DeviceCapture {
    private let recorder = RPScreenRecorder.shared()
    private weak var sink: DeviceCaptureSink?
    private var active = false
    public init(sink: DeviceCaptureSink) { self.sink = sink }
    public func start(systemAudio: Bool = false, microphone: Bool = false) async throws {
        guard !active, recorder.isAvailable, !recorder.isRecording else {
            throw NSError(domain: "CymonkeyDeviceCapture", code: 1,
                          userInfo: [NSLocalizedDescriptionKey: "Screen capture unavailable or already active"])
        }
        active = true
        recorder.isMicrophoneEnabled = microphone
        let sink = self.sink
        do {
            try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, Error>) in
                recorder.startCapture(handler: { sample, kind, error in
                    if let error { sink?.stopped(error: error); return }
                    let permitted = kind == .video || (kind == .audioApp && systemAudio) || (kind == .audioMic && microphone)
                    if permitted, CMSampleBufferDataIsReady(sample) { sink?.receive(sample, kind: kind) }
                }, completionHandler: { error in
                    if let error { continuation.resume(throwing: error) }
                    else { continuation.resume() }
                })
            }
            if !active {
                try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, Error>) in
                    recorder.stopCapture { error in
                        if let error { continuation.resume(throwing: error) }
                        else { continuation.resume() }
                    }
                }
                throw CancellationError()
            }
        } catch { active = false; throw error }
    }
    public func stop() async throws {
        guard active else { return }
        active = false
        try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, Error>) in
            recorder.stopCapture { error in
                if let error { continuation.resume(throwing: error) }
                else { continuation.resume() }
            }
        }
        sink?.stopped(error: nil)
    }
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
