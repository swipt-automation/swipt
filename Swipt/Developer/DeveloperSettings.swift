import Foundation
import LibSwipt
import Perception

@Perceptible
final class DeveloperSettings {
    static let shared = DeveloperSettings()

    static let portRange = 1...65535

    private enum Key {
        static let localNetworkEnabled = "developerLocalNetworkEnabled"
        static let localPort = "developerLocalPort"
        static let localAuthenticates = "developerLocalAuthenticates"
        static let localToken = "developerLocalToken"
        static let cloudflareEnabled = "developerCloudflareEnabled"
        static let cloudflareToken = "developerCloudflareToken"
        static let cloudflarePath = "developerCloudflarePath"
        static let cloudflareMode = "developerCloudflareMode"
        static let cloudflareAccount = "developerCloudflareAccount"
        static let cloudflareAccountName = "developerCloudflareAccountName"
        static let cloudflareZone = "developerCloudflareZone"
        static let cloudflareHostname = "developerCloudflareHostname"
        static let cloudflareQuickTunnel = "developerCloudflareQuickTunnel"
        static let cloudflareQuickHostname = "developerCloudflareQuickHostname"
        static let cloudflareNamedTunnel = "developerCloudflareNamedTunnel"
        static let tailscaleEnabled = "developerTailscaleEnabled"
        static let tailscaleAuthenticates = "developerTailscaleAuthenticates"
        static let tailscaleToken = "developerTailscaleToken"
    }

    var isLocalNetworkEnabled: Bool {
        didSet { store.set(isLocalNetworkEnabled, for: Key.localNetworkEnabled) }
    }

    var localPort: Int {
        didSet { store.set(localPort, for: Key.localPort) }
    }

    var localAuthenticates: Bool {
        didSet { store.set(localAuthenticates, for: Key.localAuthenticates) }
    }

    var localToken: String {
        didSet { store.set(localToken, for: Key.localToken) }
    }

    var cloudflareToken: String {
        didSet { store.set(cloudflareToken, for: Key.cloudflareToken) }
    }

    var isCloudflareEnabled: Bool {
        didSet { store.set(isCloudflareEnabled, for: Key.cloudflareEnabled) }
    }

    var cloudflarePath: String {
        didSet { store.set(cloudflarePath, for: Key.cloudflarePath) }
    }

    var cloudflareMode: CloudflareMode {
        didSet { store.set(cloudflareMode.rawValue, for: Key.cloudflareMode) }
    }

    var cloudflareAccount: String {
        didSet { store.set(cloudflareAccount, for: Key.cloudflareAccount) }
    }

    var cloudflareAccountName: String {
        didSet { store.set(cloudflareAccountName, for: Key.cloudflareAccountName) }
    }

    var cloudflareZone: String {
        didSet { store.set(cloudflareZone, for: Key.cloudflareZone) }
    }

    var cloudflareHostname: String {
        didSet { store.set(cloudflareHostname, for: Key.cloudflareHostname) }
    }

    var cloudflareQuickTunnel: String {
        didSet { store.set(cloudflareQuickTunnel, for: Key.cloudflareQuickTunnel) }
    }

    var cloudflareQuickHostname: String {
        didSet { store.set(cloudflareQuickHostname, for: Key.cloudflareQuickHostname) }
    }

    var cloudflareNamedTunnel: String {
        didSet { store.set(cloudflareNamedTunnel, for: Key.cloudflareNamedTunnel) }
    }

    var isTailscaleEnabled: Bool {
        didSet { store.set(isTailscaleEnabled, for: Key.tailscaleEnabled) }
    }

    var tailscaleAuthenticates: Bool {
        didSet { store.set(tailscaleAuthenticates, for: Key.tailscaleAuthenticates) }
    }

    var tailscaleToken: String {
        didSet { store.set(tailscaleToken, for: Key.tailscaleToken) }
    }

    private let store = PreferenceStore.shared

    init() {
        let storedLocalToken = store.value(String.self, for: Key.localToken)
        localToken = storedLocalToken ?? Self.randomHex(32)
        let storedCloudflareToken = store.value(String.self, for: Key.cloudflareToken)
        cloudflareToken = storedCloudflareToken ?? Self.randomHex(32)
        let storedTailscaleToken = store.value(String.self, for: Key.tailscaleToken)
        tailscaleToken = storedTailscaleToken ?? Self.randomHex(32)
        let publishesLocalNetwork = ProcessInfo.processInfo.arguments.contains("--publish-local-network")
        isLocalNetworkEnabled = publishesLocalNetwork || store.value(Bool.self, for: Key.localNetworkEnabled) ?? false
        let storedPort = store.value(Int.self, for: Key.localPort)
        localPort = storedPort ?? Int.random(in: 10000...49151)
        localAuthenticates = store.value(Bool.self, for: Key.localAuthenticates) ?? false
        isCloudflareEnabled = store.value(Bool.self, for: Key.cloudflareEnabled) ?? false
        cloudflarePath = store.value(String.self, for: Key.cloudflarePath) ?? "/"
        cloudflareMode = store.value(Int32.self, for: Key.cloudflareMode).flatMap(CloudflareMode.init(rawValue:)) ?? .quickTunnel
        cloudflareAccount = store.value(String.self, for: Key.cloudflareAccount) ?? ""
        cloudflareAccountName = store.value(String.self, for: Key.cloudflareAccountName) ?? ""
        cloudflareZone = store.value(String.self, for: Key.cloudflareZone) ?? ""
        cloudflareHostname = store.value(String.self, for: Key.cloudflareHostname) ?? ""
        cloudflareQuickTunnel = store.value(String.self, for: Key.cloudflareQuickTunnel) ?? ""
        cloudflareQuickHostname = store.value(String.self, for: Key.cloudflareQuickHostname) ?? ""
        cloudflareNamedTunnel = store.value(String.self, for: Key.cloudflareNamedTunnel) ?? ""
        isTailscaleEnabled = store.value(Bool.self, for: Key.tailscaleEnabled) ?? false
        tailscaleAuthenticates = store.value(Bool.self, for: Key.tailscaleAuthenticates) ?? false
        if publishesLocalNetwork {
            store.set(true, for: Key.localNetworkEnabled)
        }
        if storedLocalToken == nil {
            store.set(localToken, for: Key.localToken)
        }
        if storedCloudflareToken == nil {
            store.set(cloudflareToken, for: Key.cloudflareToken)
        }
        if storedTailscaleToken == nil {
            store.set(tailscaleToken, for: Key.tailscaleToken)
        }
        if storedPort == nil {
            store.set(localPort, for: Key.localPort)
        }
    }

    static func randomHex(_ length: Int) -> String {
        String((0..<length).map { _ in "0123456789abcdef".randomElement()! })
    }

    var isPublishing: Bool {
        isLocalNetworkEnabled || isCloudflareEnabled || isTailscaleEnabled
    }

    var isCloudflareComplete: Bool {
        cloudflareMode == .quickTunnel || (!cloudflareAccount.isEmpty && !cloudflareHostname.isEmpty)
    }

    var cloudflarePathPrefix: String {
        cloudflarePath == "/" ? "" : cloudflarePath
    }

    var cloudflarePublicHostname: String {
        switch cloudflareMode {
        case .quickTunnel: isCloudflareEnabled ? cloudflareQuickHostname : ""
        case .tunnel: cloudflareHostname
        }
    }
}

enum CloudflareMode: RawRepresentable {
    case quickTunnel
    case tunnel

    init?(rawValue: Int32) {
        switch rawValue {
        case LibSwiptCloudflareModeQuickTunnel: self = .quickTunnel
        case LibSwiptCloudflareModeTunnel: self = .tunnel
        default: return nil
        }
    }

    var rawValue: Int32 {
        switch self {
        case .quickTunnel: LibSwiptCloudflareModeQuickTunnel
        case .tunnel: LibSwiptCloudflareModeTunnel
        }
    }

    var name: String {
        switch self {
        case .quickTunnel: "Quick Tunnel"
        case .tunnel: "Tunnel"
        }
    }
}
