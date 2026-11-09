import Foundation
import LibSwipt
import Perception
import UIKit
import dnssd

@Perceptible
final class PairingSession {
    enum State: Equatable {
        case idle
        case waiting
        case code(String)
        case failed(DeviceProblem)
    }

    private(set) var state: State = .idle

    let hostName: String = {
        let info = Bundle.main.localizedInfoDictionary ?? Bundle.main.infoDictionary
        return info?["CFBundleDisplayName"] as? String ?? "Swipt"
    }()

    private var settingsPath: String {
        String(localized: "Privacy & Security › Developer Mode")
    }

    @PerceptionIgnored private let device: LibSwiptDevice
    @PerceptionIgnored private var responder: LibSwiptPairingResponder?
    private let advertiser = BonjourAdvertiser()
    @PerceptionIgnored private var continuedTask: ContinuedTask?
    @PerceptionIgnored private var backgroundTask: UIBackgroundTaskIdentifier = .invalid
    @PerceptionIgnored private var observers: [NSObjectProtocol] = []

    init(device: LibSwiptDevice) {
        self.device = device
    }

    var isInProgress: Bool {
        switch state {
        case .waiting, .code: true
        default: false
        }
    }

    func start() {
        guard responder == nil else { return }
        let events = PairingEvents { [weak self] event in
            DispatchQueue.main.async { self?.handle(event) }
        }
        let responder: LibSwiptPairingResponder
        do {
            responder = try device.newPairingResponder(hostName, record: PairingStore.shared.record, handler: events)
        } catch {
            state = .failed(DeviceProblem(kind: .pairing, message: error.localizedDescription))
            return
        }
        self.responder = responder
        advertiser.onFailure = { [weak self] errorCode in
            self?.fail(DeviceProblem(kind: .pairing, message: Self.publishingMessage(errorCode)))
        }
        var txt: [String: String] = [:]
        for entry in responder.txtRecord()?.toArray() ?? [] {
            txt[entry.key] = entry.value
        }
        let published = advertiser.start(
            name: responder.serviceName(), type: responder.serviceType(), port: Int(responder.port()), txt: txt)
        guard published == kDNSServiceErr_NoError else {
            fail(DeviceProblem(kind: .pairing, message: Self.publishingMessage(published)))
            return
        }
        responder.start()
        state = .waiting
        let continuedTask = ContinuedTask(kind: "pairing")
        continuedTask.onExpire = { [weak self] in self?.cancel() }
        continuedTask.submit(title: String(localized: "Pair in Settings"), subtitle: settingsPath)
        self.continuedTask = continuedTask
        continuedTask.setProgress(completed: 1, total: 3)
        watchAppState()
    }

    func acknowledgeFailure() {
        if case .failed = state {
            state = .idle
        }
    }

    func cancel() {
        guard responder != nil else { return }
        finish()
        state = .idle
    }

    private func handle(_ event: PairingEvent) {
        guard responder != nil else { return }
        switch event {
        case .pin(let pin):
            state = .code(pin)
            continuedTask?.update(title: String(localized: "Pairing Code: \(Self.grouped(pin))"), subtitle: settingsPath)
            continuedTask?.setProgress(completed: 2, total: 3)
        case .paired(let record):
            do {
                try PairingStore.shared.save(record)
            } catch {
                fail(DeviceProblem(kind: .pairing, message: error.localizedDescription))
                return
            }
            continuedTask?.update(title: String(localized: "Paired"), subtitle: String(localized: "Tap to return to Swipt"))
            continuedTask?.setProgress(completed: 3, total: 3)
            finish(succeeded: true)
            state = .idle
        case .failed(let message):
            fail(DeviceProblem(kind: .pairing, message: message))
        }
    }

    private func fail(_ problem: DeviceProblem) {
        continuedTask?.update(title: problem.title, subtitle: problem.message)
        finish()
        state = .failed(problem)
    }

    private func finish(succeeded: Bool = false) {
        advertiser.stop()
        if let responder {
            let box = UncheckedSendable(value: responder)
            DispatchQueue.global(qos: .userInitiated).async { box.value.close() }
        }
        responder = nil
        for observer in observers {
            NotificationCenter.default.removeObserver(observer)
        }
        observers.removeAll()
        continuedTask?.finish(success: succeeded)
        continuedTask = nil
        endBackgroundTask()
    }

    private func backgroundTimeRanOut() {
        guard isInProgress, UIApplication.shared.applicationState == .background else { return }
        fail(DeviceProblem(kind: .pairing, message: String(localized: "Swipt couldn’t keep waiting for Settings in the background.")))
    }

    static func grouped(_ pin: String) -> String {
        var groups: [Substring] = []
        var rest = Substring(pin)
        while !rest.isEmpty {
            groups.append(rest.prefix(3))
            rest = rest.dropFirst(3)
        }
        return groups.joined(separator: " ")
    }

    private static func publishingMessage(_ errorCode: DNSServiceErrorType) -> String {
        if errorCode == kDNSServiceErr_PolicyDenied {
            return String(localized: "Swipt isn’t allowed to use the local network. Allow it in Settings › Privacy & Security › Local Network.")
        }
        return String(localized: "Couldn’t publish the pairing service (error \(errorCode)).")
    }

    private func watchAppState() {
        let center = NotificationCenter.default
        observers.append(
            center.addObserver(forName: UIApplication.didEnterBackgroundNotification, object: nil, queue: .main) { [weak self] _ in
                MainActor.assumeIsolated { self?.beginBackgroundTask() }
            })
        observers.append(
            center.addObserver(forName: UIApplication.willEnterForegroundNotification, object: nil, queue: .main) { [weak self] _ in
                MainActor.assumeIsolated { self?.endBackgroundTask() }
            })
    }

    private func beginBackgroundTask() {
        guard isInProgress, backgroundTask == .invalid else { return }
        backgroundTask = UIApplication.shared.beginBackgroundTask(withName: "Pairing") { [weak self] in
            guard let self else { return }
            endBackgroundTask()
            if continuedTask?.isRunning != true {
                backgroundTimeRanOut()
            }
        }
    }

    private func endBackgroundTask() {
        guard backgroundTask != .invalid else { return }
        UIApplication.shared.endBackgroundTask(backgroundTask)
        backgroundTask = .invalid
    }
}
