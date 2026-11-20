import CoreLocation
import CoreTelephony
import Network
import Perception
import SwiftUI
import UIKit
import UserNotifications

enum SetupPermissions {
    static func request() async {
        let location = LocationPermission()
        async let whenInUse: Void = location.requestWhenInUse()
        async let notifications: Void = requestNotifications()
        async let localNetwork: Void = requestLocalNetwork()
        async let cellularData: Void = requestCellularData()
        _ = await (whenInUse, notifications, localNetwork, cellularData)
        await location.requestAlways()
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
    private var resolution: Resolution?
    private var waitingForActive = false
    private var waitingForAlways = false
    private var promptShown = false
    private var timer: Task<Void, Never>?
    private var settle: Task<Void, Never>?

    func requestWhenInUse() async {
        guard manager.authorizationStatus == .notDetermined else { return }
        await withCheckedContinuation { (continuation: CheckedContinuation<Void, Never>) in
            resolution = Resolution(continuation)
            manager.delegate = self
            manager.requestWhenInUseAuthorization()
        }
    }

    func requestAlways() async {
        guard manager.authorizationStatus == .authorizedWhenInUse else { return }
        if UIApplication.shared.applicationState != .active {
            await waitUntilActive()
        }
        guard manager.authorizationStatus == .authorizedWhenInUse else { return }
        await withCheckedContinuation { (continuation: CheckedContinuation<Void, Never>) in
            resolution = Resolution(continuation)
            waitingForAlways = true
            promptShown = false
            NotificationCenter.default.addObserver(
                self, selector: #selector(willResignActive), name: UIApplication.willResignActiveNotification, object: nil
            )
            NotificationCenter.default.addObserver(
                self, selector: #selector(didBecomeActive), name: UIApplication.didBecomeActiveNotification, object: nil
            )
            timer = Task { [weak self] in
                try? await Task.sleep(nanoseconds: 1_000_000_000)
                guard !Task.isCancelled, let self, !self.promptShown else { return }
                self.finish()
            }
            manager.delegate = self
            manager.requestAlwaysAuthorization()
        }
    }

    private func waitUntilActive() async {
        await withCheckedContinuation { (continuation: CheckedContinuation<Void, Never>) in
            resolution = Resolution(continuation)
            waitingForActive = true
            NotificationCenter.default.addObserver(
                self, selector: #selector(didBecomeActive), name: UIApplication.didBecomeActiveNotification, object: nil
            )
            if UIApplication.shared.applicationState == .active {
                finish()
                return
            }
            timer = Task { [weak self] in
                try? await Task.sleep(nanoseconds: 2_000_000_000)
                guard !Task.isCancelled else { return }
                self?.finish()
            }
        }
    }

    private func finish() {
        timer?.cancel()
        timer = nil
        settle?.cancel()
        settle = nil
        waitingForActive = false
        waitingForAlways = false
        promptShown = false
        NotificationCenter.default.removeObserver(self)
        let current = resolution
        resolution = nil
        current?.resume()
    }

    @objc private func willResignActive() {
        guard waitingForAlways else { return }
        promptShown = true
        timer?.cancel()
        timer = nil
    }

    @objc private func didBecomeActive() {
        if waitingForActive {
            finish()
            return
        }
        guard waitingForAlways, promptShown else { return }
        settle?.cancel()
        settle = Task { [weak self] in
            try? await Task.sleep(nanoseconds: 300_000_000)
            guard !Task.isCancelled else { return }
            self?.finish()
        }
    }

    func locationManagerDidChangeAuthorization(_ manager: CLLocationManager) {
        let status = manager.authorizationStatus
        if waitingForAlways {
            if status != .authorizedWhenInUse {
                finish()
            }
        } else if !waitingForActive, status != .notDetermined {
            finish()
        }
    }
}
