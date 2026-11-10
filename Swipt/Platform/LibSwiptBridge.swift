import Foundation
import LibSwipt

nonisolated struct SwipletService: Equatable, Sendable {
    enum Status: String, Sendable {
        case starting
        case running
        case stopped
        case failed
    }

    var enabled = false
    var status = Status.stopped
    var error = ""
    var since: Date?

    init() {}

    init(enabled: Bool, status: String, error: String, since: Int64) {
        self.enabled = enabled
        self.status = Status(rawValue: status) ?? .stopped
        self.error = error
        self.since = since > 0 ? Date(timeIntervalSince1970: TimeInterval(since) / 1000) : nil
    }
}

nonisolated enum PlatformEvent: Sendable {
    case dialog(id: Int64, request: String?)
    case service(swiplet: String, SwipletService)
    case databaseChanged(swiplet: String, database: String, path: String, table: String, rowID: Int64)
    case swipletsChanged
    case apiFailed(String)
    case shutdownRequested
    case recordingFailed(String)
}

nonisolated final class PlatformEvents: NSObject, LibSwiptPlatformHandlerProtocol, @unchecked Sendable {
    private let deliver = LockedState<(@Sendable (PlatformEvent) -> Void)?>(initialState: nil)
    private let deliverBackdrop: @Sendable (Data?) -> Void
    private let isActive: @Sendable () -> Bool

    init(backdrop deliverBackdrop: @escaping @Sendable (Data?) -> Void, active isActive: @escaping @Sendable () -> Bool) {
        self.deliverBackdrop = deliverBackdrop
        self.isActive = isActive
    }

    func connect(_ deliver: @escaping @Sendable (PlatformEvent) -> Void) {
        self.deliver.withLock { $0 = deliver }
    }

    func onDialog(_ dialogID: Int64, request: String?) {
        deliver.withLock { $0 }?(.dialog(id: dialogID, request: request ?? ""))
    }

    func onDialogWithdrawn(_ dialogID: Int64) {
        deliver.withLock { $0 }?(.dialog(id: dialogID, request: nil))
    }

    func onServiceChanged(_ swiplet: String?, enabled: Bool, status: String?, message: String?, since: Int64) {
        deliver.withLock { $0 }?(.service(swiplet: swiplet ?? "", SwipletService(enabled: enabled, status: status ?? "", error: message ?? "", since: since)))
    }

    func onDatabaseChanged(_ swiplet: String?, database: String?, path: String?, table: String?, rowID: Int64) {
        deliver.withLock { $0 }?(.databaseChanged(swiplet: swiplet ?? "", database: database ?? "", path: path ?? "", table: table ?? "", rowID: rowID))
    }

    func onSwipletsChanged() {
        deliver.withLock { $0 }?(.swipletsChanged)
    }

    func onAPIFailed(_ message: String?) {
        deliver.withLock { $0 }?(.apiFailed(message ?? ""))
    }

    func onShutdownRequested() {
        deliver.withLock { $0 }?(.shutdownRequested)
    }

    func onRecordingFailed(_ message: String?) {
        deliver.withLock { $0 }?(.recordingFailed(message ?? ""))
    }

    func onBackdrop(_ image: Data?) {
        deliverBackdrop(image)
    }

    func appActive() -> Bool {
        isActive()
    }
}

nonisolated struct DeviceStateChange: Sendable {
    var state: Int32
    var errorCode: Int32
    var message: String
}

nonisolated final class DeviceEvents: NSObject, LibSwiptDeviceHandlerProtocol, @unchecked Sendable {
    private let deliver = LockedState<(@Sendable (DeviceStateChange) -> Void)?>(initialState: nil)

    func connect(_ deliver: @escaping @Sendable (DeviceStateChange) -> Void) {
        self.deliver.withLock { $0 = deliver }
    }

    func onStateChanged(_ state: Int32, errorCode: Int32, message: String?) {
        deliver.withLock { $0 }?(DeviceStateChange(state: state, errorCode: errorCode, message: message ?? ""))
    }

    func backlightState() -> Int32 {
        LocalBacklight.state
    }
}

nonisolated final class CloudflareTunnelEvents: NSObject, LibSwiptCloudflareTunnelHandlerProtocol, @unchecked Sendable {
    private let deliver: @Sendable (_ tunnel: String, _ hostname: String) -> Void
    private let deliverError: @Sendable (String) -> Void

    init(_ deliver: @escaping @Sendable (_ tunnel: String, _ hostname: String) -> Void, error deliverError: @escaping @Sendable (String) -> Void) {
        self.deliver = deliver
        self.deliverError = deliverError
    }

    func onState(_ tunnel: String?, hostname: String?) {
        deliver(tunnel ?? "", hostname ?? "")
    }

    func onError(_ message: String?) {
        deliverError(message ?? "")
    }
}

nonisolated struct TailscaleStatus: Equatable, Sendable {
    enum State: Sendable {
        case starting
        case stopped
        case needsLogin
        case needsMachineAuth
        case running
    }

    var state: State
    var loginName: String
    var tailnet: String
    var dnsName: String
    var addresses: [String]

    var isLoggedIn: Bool {
        switch state {
        case .stopped, .needsMachineAuth, .running: true
        case .starting, .needsLogin: false
        }
    }

    init(_ status: LibSwiptTailscaleStatus) {
        state =
            switch status.state {
            case LibSwiptTailscaleStateStopped: .stopped
            case LibSwiptTailscaleStateNeedsLogin: .needsLogin
            case LibSwiptTailscaleStateNeedsMachineAuth: .needsMachineAuth
            case LibSwiptTailscaleStateRunning: .running
            default: .starting
            }
        loginName = status.loginName
        tailnet = status.tailnet
        dnsName = status.dnsName
        addresses = status.addresses?.toArray() ?? []
    }
}

nonisolated final class TailscaleEvents: NSObject, LibSwiptTailscaleHandlerProtocol, @unchecked Sendable {
    private let deliver: @Sendable (TailscaleStatus) -> Void
    private let deliverError: @Sendable (String) -> Void

    init(_ deliver: @escaping @Sendable (TailscaleStatus) -> Void, error deliverError: @escaping @Sendable (String) -> Void) {
        self.deliver = deliver
        self.deliverError = deliverError
    }

    func onStatus(_ status: LibSwiptTailscaleStatus?) {
        guard let status else { return }
        deliver(TailscaleStatus(status))
    }

    func onError(_ message: String?) {
        deliverError(message ?? "")
    }
}

nonisolated final class ActionEvents: NSObject, LibSwiptActionObserverProtocol, @unchecked Sendable {
    private let deliverDialog: @Sendable (Int64, String?) -> Void

    init(dialog deliverDialog: @escaping @Sendable (Int64, String?) -> Void) {
        self.deliverDialog = deliverDialog
    }

    func onDialog(_ dialogID: Int64, request: String?) {
        deliverDialog(dialogID, request ?? "")
    }

    func onDialogWithdrawn(_ dialogID: Int64) {
        deliverDialog(dialogID, nil)
    }
}

nonisolated enum PairingEvent: Sendable {
    case pin(String)
    case paired(Data)
    case failed(String)
}

nonisolated final class PairingEvents: NSObject, LibSwiptPairingHandlerProtocol, @unchecked Sendable {
    private let deliver: @Sendable (PairingEvent) -> Void

    init(_ deliver: @escaping @Sendable (PairingEvent) -> Void) {
        self.deliver = deliver
    }

    func onPIN(_ pin: String?) {
        deliver(.pin(pin ?? ""))
    }

    func onPaired(_ record: Data?) {
        deliver(.paired(record ?? Data()))
    }

    func onPairingFailed(_ message: String?) {
        deliver(.failed(message ?? ""))
    }
}
