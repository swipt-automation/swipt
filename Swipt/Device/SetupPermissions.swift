import CoreLocation
import CoreTelephony
import Network
import UserNotifications

enum SetupPermissions {
    static func request() async {
        async let location: Void = LocationPermission().request()
        async let notifications: Void = requestNotifications()
        async let localNetwork: Void = requestLocalNetwork()
        async let cellularData: Void = requestCellularData()
        _ = await (location, notifications, localNetwork, cellularData)
    }

    private static func requestNotifications() async {
        _ = try? await UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound])
    }

    private static func requestLocalNetwork() async {
        let type = "_swipt._tcp"
        let name = UUID().uuidString
        guard let listener = try? NWListener(using: .tcp) else { return }
        listener.service = NWListener.Service(name: name, type: type)
        listener.newConnectionHandler = { $0.cancel() }
        let browser = NWBrowser(for: .bonjour(type: type, domain: "local."), using: .tcp)
        await withCheckedContinuation { (continuation: CheckedContinuation<Void, Never>) in
            let resolution = Resolution(continuation)
            browser.browseResultsChangedHandler = { results, _ in
                let found = results.contains { result in
                    if case let .service(service, _, _, _) = result.endpoint { service == name } else { false }
                }
                if found {
                    Task { @MainActor in resolution.resume() }
                }
            }
            browser.stateUpdateHandler = { state in
                if case .failed = state {
                    Task { @MainActor in resolution.resume() }
                }
            }
            listener.stateUpdateHandler = { state in
                if case .failed = state {
                    Task { @MainActor in resolution.resume() }
                }
            }
            listener.start(queue: .main)
            browser.start(queue: .main)
        }
        browser.cancel()
        listener.cancel()
    }

    private static func requestCellularData() async {
        let data = CTCellularData()
        await withCheckedContinuation { (continuation: CheckedContinuation<Void, Never>) in
            let resolution = Resolution(continuation)
            data.cellularDataRestrictionDidUpdateNotifier = { state in
                if state != .restrictedStateUnknown {
                    Task { @MainActor in resolution.resume() }
                }
            }
            if data.restrictedState != .restrictedStateUnknown {
                resolution.resume()
            }
        }
        data.cellularDataRestrictionDidUpdateNotifier = nil
    }
}

private final class Resolution {
    private var continuation: CheckedContinuation<Void, Never>?

    init(_ continuation: CheckedContinuation<Void, Never>) {
        self.continuation = continuation
    }

    func resume() {
        continuation?.resume()
        continuation = nil
    }
}

private final class LocationPermission: NSObject, CLLocationManagerDelegate {
    private let manager = CLLocationManager()
    private var resolution: Resolution?
    private var fallback: Task<Void, Never>?

    func request() async {
        switch manager.authorizationStatus {
        case .notDetermined:
            await withCheckedContinuation { (continuation: CheckedContinuation<Void, Never>) in
                resolution = Resolution(continuation)
                manager.delegate = self
                manager.requestWhenInUseAuthorization()
            }
        case .authorizedWhenInUse:
            await withCheckedContinuation { (continuation: CheckedContinuation<Void, Never>) in
                resolution = Resolution(continuation)
                manager.delegate = self
                requestAlways()
            }
        default:
            break
        }
    }

    private func requestAlways() {
        manager.requestAlwaysAuthorization()
        fallback = Task { @MainActor [weak self] in
            try? await Task.sleep(nanoseconds: 2_000_000_000)
            self?.finish()
        }
    }

    private func finish() {
        fallback?.cancel()
        fallback = nil
        resolution?.resume()
    }

    func locationManagerDidChangeAuthorization(_ manager: CLLocationManager) {
        switch manager.authorizationStatus {
        case .notDetermined:
            break
        case .authorizedWhenInUse:
            if fallback == nil {
                requestAlways()
            }
        default:
            finish()
        }
    }
}
