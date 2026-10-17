import Foundation

public struct ControlEndpoint: Sendable {
    public let url: URL
    private let token: String

    public init(url: URL, bearerToken: String) throws {
        guard url.user == nil, url.password == nil,
              let scheme = url.scheme?.lowercased(), scheme == "ws" || scheme == "wss",
              let host = url.host, !host.isEmpty,
              !bearerToken.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else {
            throw RuntimeError.invalidRequest("invalid authenticated Cymonkey control endpoint")
        }
        if scheme == "ws" {
            let loopback = host == "localhost" || host == "127.0.0.1" || host == "::1"
            guard loopback else {
                throw RuntimeError.denied("plaintext Cymonkey control endpoints must use loopback")
            }
        }
        self.url = url
        self.token = bearerToken
    }

    func request() -> URLRequest {
        var request = URLRequest(url: url)
        request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        request.setValue(cymonkeyProtocolVersion, forHTTPHeaderField: "X-Jangolova-Protocol")
        return request
    }
}

public final class WebSocketControlClient: @unchecked Sendable {
    private let endpoint: ControlEndpoint
    private let session: URLSession
    private let decoder = JSONDecoder()
    private let encoder = JSONEncoder()

    public init(endpoint: ControlEndpoint, session: URLSession = .shared) {
        self.endpoint = endpoint
        self.session = session
    }

    public func run(
        runtime: CymonkeyRuntime,
        onConnected: (@Sendable () async -> Void)? = nil
    ) async throws {
        let task = session.webSocketTask(with: endpoint.request())
        task.maximumMessageSize = 4 * 1024 * 1024
        task.resume()
        defer { task.cancel(with: .goingAway, reason: nil) }
        var didConnect = false
        while true {
            let message = try await task.receive()
            if !didConnect {
                didConnect = true
                await onConnected?()
            }
            let data: Data
            switch message {
            case .data(let value): data = value
            case .string(let value):
                guard let value = value.data(using: .utf8) else {
                    throw RuntimeError.invalidRequest("control message is not UTF-8")
                }
                data = value
            @unknown default:
                throw RuntimeError.invalidRequest("unsupported control message")
            }
            guard data.count <= 4 * 1024 * 1024 else {
                throw RuntimeError.denied("control message exceeds the size limit")
            }
            let request: ControlRequest
            do {
                request = try decoder.decode(ControlRequest.self, from: data)
            } catch {
                throw RuntimeError.invalidRequest("control message is not a Cymonkey request")
            }
            let response = await runtime.handle(request)
            let encoded = try encoder.encode(response)
            try await task.send(.data(encoded))
        }
    }
}
