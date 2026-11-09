import Foundation
import LibSwipt
import Network
import Perception
import os

@Perceptible
final class DeviceConnection {
    nonisolated private static let logger = Logger(subsystem: Bundle.main.bundleIdentifier ?? "Swipt", category: "device")

    enum Phase: Equatable {
        case idle
        case connecting
        case preparing
        case running
        case failed

        var isConnected: Bool {
            switch self {
            case .idle, .failed: false
            case .connecting, .preparing, .running: true
            }
        }

        var isAttached: Bool {
            switch self {
            case .idle, .connecting, .failed: false
            case .preparing, .running: true
            }
        }
    }

    private(set) var phase: Phase = .idle
    private(set) var tunnelFound: Bool?
    private(set) var tunnelThroughLocalNetwork = false
    private(set) var usesCellular = false
    private(set) var isAvailable = false

    let remotePairing = RemotePairingBrowser()
    let pairingSession: PairingSession
    @PerceptionIgnored private let device: LibSwiptDevice
    @PerceptionIgnored private var probeTask: Task<Void, Never>?
    @PerceptionIgnored private var pathMonitor: NWPathMonitor?
    @PerceptionIgnored private var probeClients = 0
    @PerceptionIgnored private var isBrowsing = false
    @PerceptionIgnored private var ready: (() -> Void)?

    private var autoUnlock: AutoUnlockSettings { .shared }
    private var pairing: PairingStore { .shared }

    init(_ device: LibSwiptDevice, events: DeviceEvents) {
        self.device = device
        pairingSession = PairingSession(device: device)
        remotePairing.onPortChange = { [weak self] port in
            self?.device.setRemotePairingPort(Int32(port))
            self?.probe()
        }
        events.connect { [weak self] change in
            DispatchQueue.main.async { self?.handle(change) }
        }
    }

    var unavailableReason: String? {
        if !pairing.isPaired {
            return String(localized: "Pair this device in Swipt first.")
        }
        if usesCellular {
            return String(localized: "Swipt only stops working on cellular data, due to Apple’s restrictions on developer services.")
        }
        if autoUnlock.problem != nil {
            return String(localized: "Check Settings › Automatic Unlock.")
        }
        return nil
    }

    func start(ready: @escaping () -> Void) {
        self.ready = ready
        observePairing()
        observeSettings()
        monitorPath()
    }

    func clearFailure() {
        if phase == .failed {
            phase = .idle
        }
    }

    func setBrowsing(_ browsing: Bool) {
        guard browsing != isBrowsing else { return }
        isBrowsing = browsing
        if browsing {
            remotePairing.start()
        } else {
            remotePairing.stop()
        }
    }

    func startProbing() {
        remotePairing.start()
        probeClients += 1
        if probeClients == 1 {
            probe()
        }
    }

    private func monitorPath() {
        let monitor = NWPathMonitor()
        monitor.pathUpdateHandler = { [weak self] path in
            let cellular = path.usesInterfaceType(.cellular)
            Task { @MainActor in
                guard let self else { return }
                if self.usesCellular != cellular {
                    self.usesCellular = cellular
                }
                if let ready = self.ready {
                    self.ready = nil
                    self.observeAvailability()
                    ready()
                }
                self.device.checkRoute()
                self.probe()
            }
        }
        monitor.start(queue: .main)
        pathMonitor = monitor
    }

    private func probe() {
        probeTask?.cancel()
        probeTask = nil
        if usesCellular, tunnelFound != false {
            tunnelFound = false
        }
        guard probeClients > 0, !usesCellular else { return }
        probeTask = Task {
            var reportedFailure: String?
            while !Task.isCancelled {
                let found: Bool
                let throughLocalNetwork: Bool
                if phase.isAttached {
                    found = true
                    throughLocalNetwork = device.loopbackAddress()?.localNetwork ?? false
                } else {
                    let port = remotePairing.port ?? 0
                    let probe = UncheckedSendable(value: LibSwiptNewLoopbackProbe(Int32(port))!)
                    let result = await withTaskCancellationHandler {
                        do {
                            let localNetwork = try await blocking { try probe.value.run().localNetwork }
                            return (found: true, localNetwork: localNetwork, failure: String?.none)
                        } catch {
                            return (found: false, localNetwork: false, failure: error.localizedDescription)
                        }
                    } onCancel: {
                        probe.value.cancel()
                    }
                    guard !Task.isCancelled else { return }
                    found = result.found
                    throughLocalNetwork = result.localNetwork
                    if let failure = result.failure, failure != reportedFailure {
                        Self.logger.notice("Look for the tunnel: \(failure, privacy: .public)")
                    }
                    reportedFailure = result.failure
                }
                if tunnelFound != found {
                    tunnelFound = found
                }
                if tunnelThroughLocalNetwork != throughLocalNetwork {
                    tunnelThroughLocalNetwork = throughLocalNetwork
                }
                try? await Task.sleep(seconds: 2)
            }
        }
    }

    func stopProbing() {
        remotePairing.stop()
        probeClients = max(0, probeClients - 1)
        guard probeClients == 0 else { return }
        probeTask?.cancel()
        probeTask = nil
    }

    private func observePairing() {
        let record = withPerceptionTracking {
            pairing.record
        } onChange: { [weak self] in
            DispatchQueue.main.async {
                self?.remotePairing.refresh()
                self?.clearFailure()
                self?.observePairing()
            }
        }
        configure { try $0.setPairingRecord(record ?? Data()) }
    }

    private func observeAvailability() {
        let message = withPerceptionTracking {
            unavailableReason ?? ""
        } onChange: { [weak self] in
            DispatchQueue.main.async { self?.observeAvailability() }
        }
        if isAvailable != message.isEmpty {
            isAvailable = message.isEmpty
        }
        configure { $0.setUnavailable(message) }
    }

    private var settings: LibSwiptDeviceSettings {
        let settings = LibSwiptDeviceSettings()
        settings.autoUnlockPasscode = autoUnlock.isEnabled && autoUnlock.problem == nil ? autoUnlock.passcode : ""
        settings.autoUnlockPasscodeType = autoUnlock.passcodeType.rawValue
        return settings
    }

    private func observeSettings() {
        let settings = withPerceptionTracking {
            self.settings
        } onChange: { [weak self] in
            DispatchQueue.main.async { self?.observeSettings() }
        }
        let box = UncheckedSendable(value: settings)
        configure { try $0.update(box.value) }
    }

    private func configure(_ change: @escaping @Sendable (LibSwiptDevice) throws -> Void) {
        let box = UncheckedSendable(value: device)
        Platform.queue.async {
            do {
                try change(box.value)
            } catch {
                Self.logger.error("Configure the device: \(error, privacy: .public)")
            }
        }
    }

    private func handle(_ change: DeviceStateChange) {
        let wasAttached = phase.isAttached
        switch change.state {
        case LibSwiptDeviceConnecting:
            phase = .connecting
        case LibSwiptDevicePreparing:
            phase = .preparing
        case LibSwiptDeviceConnected:
            phase = .running
        case LibSwiptDeviceFailed:
            phase = .failed
            if change.errorCode == LibSwiptDeviceErrorPairingRejected {
                pairing.clear()
            }
        default:
            phase = change.errorCode == LibSwiptDeviceErrorNone ? .idle : .failed
        }
        if wasAttached, !phase.isAttached {
            probe()
        }
    }
}
