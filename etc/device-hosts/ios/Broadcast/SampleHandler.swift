import ReplayKit
import CymonkeyIOSHelper

final class SampleHandler: DeviceBroadcastHandler {
    private var ticket: TargetTicket?
    override func makeSink(setupInfo: [String: NSObject]?) throws -> DeviceCaptureSink {
        let approved = try TargetTicket.consume(); ticket = approved
        let transport = try DeviceTargetTransport(url: approved.endpoint, token: approved.token,
            video: true, audio: approved.audio, microphone: approved.microphone) { [weak self] error in
                guard let self, let error else { return }
                self.finishBroadcastWithError(error)
            }
        transport.connect()
        return transport
    }
    override func permits(_ kind: RPSampleBufferType) -> Bool {
        kind == .video || (kind == .audioApp && ticket?.audio == true) || (kind == .audioMic && ticket?.microphone == true)
    }
}
