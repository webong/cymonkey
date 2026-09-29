import CymonkeyMacOSRuntime
import Darwin
import Foundation

@main
struct CymonkeyMacOSHelperCommand {
    static func main() async {
        do {
            let environment = ProcessInfo.processInfo.environment
            guard let endpointValue = environment["JANGOLOVA_CYMONKEY_CONTROL_URL"],
                  let endpointURL = URL(string: endpointValue),
                  let token = environment["JANGOLOVA_CYMONKEY_CONTROL_TOKEN"],
                  environment["JANGOLOVA_CYMONKEY_PROTOCOL"] == cymonkeyProtocolVersion,
                  let configPath = environment["JANGOLOVA_CYMONKEY_CONFIG"],
                  NSString(string: configPath).isAbsolutePath else {
                throw RuntimeError.invalidRequest("required Cymonkey helper launch configuration is missing")
            }
            let configuration = try HelperConfiguration.load(from: URL(fileURLWithPath: configPath))
            let endpoint = try ControlEndpoint(url: endpointURL, bearerToken: token)
            let runtime = try CymonkeyRuntime(configuration: configuration)
            try await WebSocketControlClient(endpoint: endpoint).run(runtime: runtime)
        } catch {
            // Do not echo endpoints, tokens, configuration contents, native
            // descriptor payloads, or target application data to stderr.
            FileHandle.standardError.write(Data("Cymonkey macOS helper stopped: \(safeMessage(error))\n".utf8))
            exit(EXIT_FAILURE)
        }
    }

    private static func safeMessage(_ error: Error) -> String {
        if let runtimeError = error as? RuntimeError { return runtimeError.description }
        return "native control connection failed"
    }
}
