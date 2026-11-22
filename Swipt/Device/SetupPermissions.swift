import CoreLocation
import CoreTelephony
import Network
import Perception
import SwiftUI
import UIKit
import UserNotifications
import dnssd

enum SetupPermissions {
    static func request() async {
        let location = LocationPermission()
        let asksLocation = location.status == .notDetermined
        async let whenInUse: Void = asksLocation ? location.requestWhenInUse() : ()
        async let notifications: Void = requestNotifications()
        async let localNetwork: Void = requestLocalNetwork()
        async let cellularData: Void = requestCellularData()
        _ = await (whenInUse, notifications, localNetwork, cellularData)
        if asksLocation, #available(iOS 18, *) {
            await location.requestAlways()
        }
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
                switch state {
                case .failed, .waiting(.dns(DNSServiceErrorType(kDNSServiceErr_PolicyDenied))):
                    Task { @MainActor in resolution.resume() }
                default:
                    break
                }
            }
            listener.stateUpdateHandler = { state in
                switch state {
                case .failed, .waiting(.dns(DNSServiceErrorType(kDNSServiceErr_PolicyDenied))):
                    Task { @MainActor in resolution.resume() }
                default:
                    break
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

struct AutomatedSetupFinished: View {
    @State private var observer = LocationStatusObserver()

    var body: some View {
        WithPerceptionTracking {
            Color.clear
                .accessibilityElement()
                .accessibilityIdentifier("setup-finished")
                .accessibilityValue(
                    observer.status == .authorizedAlways || observer.status == .restricted
                        ? Text(verbatim: "always") : Text(verbatim: "")
                )
        }
    }
}

@Perceptible
private final class LocationStatusObserver: NSObject, CLLocationManagerDelegate {
    var status: CLAuthorizationStatus
    @PerceptionIgnored private let manager = CLLocationManager()

    override init() {
        status = manager.authorizationStatus
        super.init()
        manager.delegate = self
    }

    func locationManagerDidChangeAuthorization(_ manager: CLLocationManager) {
        status = manager.authorizationStatus
    }
}

private final class LocationPermission: NSObject, CLLocationManagerDelegate {
    private let manager = CLLocationManager()
    private var isAlwaysDenied = false
    private var waiting: (condition: () -> Bool, continuation: CheckedContinuation<Void, Never>)?

    override init() {
        super.init()
        manager.delegate = self
        NotificationCenter.default.addObserver(
            self, selector: #selector(update), name: UIApplication.didBecomeActiveNotification, object: nil
        )
    }

    var status: CLAuthorizationStatus { manager.authorizationStatus }

    func requestWhenInUse() async {
        manager.requestWhenInUseAuthorization()
        await wait { self.status != .notDetermined }
    }

    @available(iOS 18, *)
    func requestAlways() async {
        await wait { UIApplication.shared.applicationState == .active }
        guard status == .authorizedWhenInUse else { return }
        // The location alerts leave the app active, and Keep Only While Using on the Always alert reports no
        // authorization change, only alwaysAuthorizationDenied of the session (iOS 27.3 24C5037i).
        let session = CLServiceSession(authorization: .always)
        let diagnostics = Task {
            for try await diagnostic in session.diagnostics
            where diagnostic.alwaysAuthorizationDenied || diagnostic.authorizationDenied
                || diagnostic.authorizationDeniedGlobally || diagnostic.authorizationRestricted
            {
                isAlwaysDenied = true
                update()
            }
        }
        await wait { self.status != .authorizedWhenInUse || self.isAlwaysDenied }
        diagnostics.cancel()
        session.invalidate()
    }

    private func wait(until condition: @escaping () -> Bool) async {
        guard !condition() else { return }
        await withCheckedContinuation { continuation in
            waiting = (condition, continuation)
        }
    }

    @objc private func update() {
        guard let waiting, waiting.condition() else { return }
        self.waiting = nil
        waiting.continuation.resume()
    }

    func locationManagerDidChangeAuthorization(_ manager: CLLocationManager) {
        update()
    }
}
