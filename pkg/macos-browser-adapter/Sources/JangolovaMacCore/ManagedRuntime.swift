import CymonkeyMacOSRuntime
import Foundation

public actor ManagedRuntimeController {
    public private(set) var status: ManagedRuntimeStatus = .stopped
    private var task: Task<Void, Never>?

    public init() {}

    public func start(configuration: ManagedRuntimeConfiguration) async throws {
        guard configuration.protocolVersion == cymonkeyProtocolVersion else {
            throw MacExtensionError.protocolMismatch
        }
        stop()
        let helper = try HelperConfiguration.load(from: configuration.helperConfiguration)
        let endpoint = try ControlEndpoint(url: configuration.endpoint, bearerToken: configuration.bearerToken)
        let runtime = try CymonkeyRuntime(configuration: helper)
        status = .connecting
        task = Task { [weak self] in
            guard let self else { return }
            do {
                try await WebSocketControlClient(endpoint: endpoint).run(
                    runtime: runtime,
                    onConnected: { await self.markConnected() }
                )
                await self.markStopped()
            } catch is CancellationError {
                await self.markStopped()
            } catch {
                await self.markFailed()
            }
        }
    }

    public func stop() {
        task?.cancel()
        task = nil
        status = .stopped
    }

    private func markConnected() { status = .connected }
    private func markStopped() { status = .stopped; task = nil }
    private func markFailed() { status = .failed("managed Cymonkey control connection failed"); task = nil }
}
