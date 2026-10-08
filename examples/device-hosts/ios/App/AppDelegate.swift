import UIKit
import ReplayKit
import AVFoundation
import CymonkeyIOSHelper

@main final class AppDelegate: UIResponder, UIApplicationDelegate {
    var window: UIWindow?
    func application(_ application: UIApplication, didFinishLaunchingWithOptions options: [UIApplication.LaunchOptionsKey: Any]?) -> Bool {
        TargetTicket.clear()
        window = UIWindow(frame: UIScreen.main.bounds)
        window?.rootViewController = HostController(); window?.makeKeyAndVisible(); return true
    }
}

@MainActor final class HostController: UIViewController {
    private let endpoint = UITextField(), token = UITextField(), status = UILabel()
    private let audio = UISwitch(), microphone = UISwitch(), playback = UISwitch()
    private let mediaQueue = DispatchQueue(label: "Board reference playback")
    private var output: DevicePlayback?
    private var capture: DeviceCapture?
    private var transport: DeviceTargetTransport?
    private var generation = UUID()
    private var busy = false
    override func viewDidLoad() {
        super.viewDidLoad(); view.backgroundColor = .systemBackground
        let stack = UIStackView(); stack.axis = .vertical; stack.spacing = 12
        stack.translatesAutoresizingMaskIntoConstraints = false; view.addSubview(stack)
        NSLayoutConstraint.activate([stack.leadingAnchor.constraint(equalTo: view.safeAreaLayoutGuide.leadingAnchor, constant: 20), stack.trailingAnchor.constraint(equalTo: view.safeAreaLayoutGuide.trailingAnchor, constant: -20), stack.topAnchor.constraint(equalTo: view.safeAreaLayoutGuide.topAnchor, constant: 20)])
        func label(_ text: String) { let label = UILabel(); label.text = text; label.numberOfLines = 0; stack.addArrangedSubview(label) }
        label("Board reference • video target\nPaste only a fresh scoped target credential.")
        endpoint.placeholder = "wss://host/v1/device-targets/mobile-…"
        token.placeholder = "Scoped token"; token.isSecureTextEntry = true
        for field in [endpoint, token] { field.borderStyle = .roundedRect; field.autocapitalizationType = .none; field.autocorrectionType = .no; stack.addArrangedSubview(field) }
        for (name, toggle) in [("System audio", audio), ("Microphone", microphone), ("Playback (app capture only)", playback)] {
            let row = UIStackView(); let title = UILabel(); title.text = name; row.addArrangedSubview(title); row.addArrangedSubview(toggle); stack.addArrangedSubview(row)
        }
        for (title, action) in [("Start app capture", #selector(start)), ("Stage broadcast credential", #selector(stage)), ("Stop / clear staged credential", #selector(stop))] {
            let button = UIButton(type: .system); button.setTitle(title, for: .normal); button.addTarget(self, action: action, for: .touchUpInside); stack.addArrangedSubview(button)
        }
        let picker = RPSystemBroadcastPickerView(frame: CGRect(x: 0, y: 0, width: 48, height: 48))
        picker.preferredExtension = Bundle.main.object(forInfoDictionaryKey: "BoardBroadcastID") as? String
        picker.showsMicrophoneButton = true; stack.addArrangedSubview(picker)
        label("For cross-app capture: stage, then tap the broadcast picker. Stop broadcasting with the system indicator. Use the browser viewer on another device.")
        status.numberOfLines = 0; stack.addArrangedSubview(status)
        NotificationCenter.default.addObserver(self, selector: #selector(stopAppForBackground), name: UIApplication.didEnterBackgroundNotification, object: nil)
    }
    private func ticket() throws -> TargetTicket {
        guard let url = URL(string: endpoint.text ?? ""), url.scheme == "wss", token.text?.count == 64 else {
            throw NSError(domain: "BoardReference", code: 3, userInfo: [NSLocalizedDescriptionKey: "Enter WSS endpoint and scoped token"])
        }
        return TargetTicket(endpoint: url, token: token.text!, audio: audio.isOn, microphone: microphone.isOn, playback: playback.isOn, expires: Date().addingTimeInterval(120))
    }
    @objc private func start() {
        guard !busy else { return }; busy = true
        let current = UUID(); generation = current
        Task {
            do {
                let approved = try ticket(); token.text = ""
                if approved.playback {
                    try AVAudioSession.sharedInstance().setCategory(.playAndRecord, mode: .default, options: [.defaultToSpeaker])
                    try AVAudioSession.sharedInstance().setActive(true)
                    output = try mediaQueue.sync { try DevicePlayback() }
                }
                let player = output; let queue = mediaQueue
                let writer: ((Data) throws -> Void)? = player.map { value in { data in try queue.sync { try value.write(data) } } }
                let sink = try DeviceTargetTransport(url: approved.endpoint, token: approved.token, video: true,
                    audio: approved.audio, microphone: approved.microphone, playback: writer) { [weak self] _ in
                    Task { @MainActor in if self?.generation == current { self?.stop() } }
                }
                transport = sink; sink.connect()
                let source = DeviceCapture(sink: sink); capture = source
                try await source.start(systemAudio: approved.audio, microphone: approved.microphone)
                if generation == current { status.text = "App capture active" }
            } catch { if generation == current { stop(); status.text = "Capture unavailable; check consent, grants and connection." } }
        }
    }
    @objc private func stage() {
        guard !busy else { return }
        do {
            let approved = try ticket()
            guard !approved.playback else { status.text = "Disable playback for broadcast mode"; return }
            try approved.stage(); token.text = ""; status.text = "Staged for 2 minutes. Open the system broadcast picker."
            DispatchQueue.main.asyncAfter(deadline: .now() + 120) { TargetTicket.clear() }
        } catch { status.text = "Could not stage credential; check endpoint and App Group." }
    }
    @objc private func stopAppForBackground() { if busy { stop() } }
    @objc private func stop() {
        generation = UUID(); let ending = generation; TargetTicket.clear(); transport?.close(); transport = nil
        let source = capture; capture = nil
        let player = output; output = nil; mediaQueue.sync { player?.close() }
        status.text = "Stopped. Use the system indicator to end an active broadcast."
        Task { try? await source?.stop(); try? AVAudioSession.sharedInstance().setActive(false); if generation == ending { busy = false } }
    }
}
