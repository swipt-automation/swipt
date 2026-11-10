import Perception
import SwiftUI
import UIKit

enum SetupStep {
    case loopback
    case autoUnlock
}

struct SetupView: View {
    static var isNeeded: Bool {
        !PairingStore.shared.isPaired || Platform.shared.device.tunnelFound == false || !AutoUnlockSettings.shared.isDecided
    }

    @State private var path: [SetupStep] = []

    private let pairing = PairingStore.shared
    private let device = Platform.shared.device

    var body: some View {
        WithPerceptionTracking {
            NavigationStack(path: $path) {
                PairingSetupView { path.append(device.tunnelFound == true ? .autoUnlock : .loopback) }
                    .navigationDestination(for: SetupStep.self) { step in
                        Group {
                            switch step {
                            case .loopback:
                                LoopbackSetupView {
                                    if path.last == .loopback {
                                        path.append(.autoUnlock)
                                    }
                                }
                            case .autoUnlock:
                                AutoUnlockSetupView {
                                    if path.last == .autoUnlock, device.tunnelFound == false {
                                        path.append(.loopback)
                                    }
                                }
                            }
                        }
                        .navigationBarBackButtonHidden(true)
                    }
            }
            .onAppear {
                if pairing.isPaired {
                    path = device.tunnelFound == false ? [.loopback] : [.autoUnlock]
                }
            }
            .onChangeCompat(of: pairing.isPaired) { paired in
                if !paired {
                    path = []
                }
            }
        }
    }
}

struct AutomatedSetupView: View {
    static let isRequested = ProcessInfo.processInfo.arguments.contains("--setup")

    let finished: () -> Void

    var body: some View {
        SetupPage("gearshape.2", Text("Setting Up Swipt")) {
            ProgressView()
                .controlSize(.large)
        } actions: {
        }
        .task {
            Task {
                await SetupPermissions.request()
                finished()
            }
            do {
                try await Task.sleep(nanoseconds: 60_000_000_000)
                finished()
            } catch {}
        }
    }
}

struct LoopbackSetupView: View {
    let next: () -> Void

    private static let localDevVPN = URL(string: "localdevvpn://")!
    private static let localDevVPNStore = URL(string: "https://apps.apple.com/app/id6755608044")!

    @Environment(\.openURL) private var openURL
    @Environment(\.scenePhase) private var scenePhase
    @State private var hasLocalDevVPN = false

    private let device = Platform.shared.device

    var body: some View {
        SetupPage(
            device.usesCellular ? "antenna.radiowaves.left.and.right.slash" : "network",
            device.usesCellular ? Text("Unsupported Network") : Text("Loopback VPN"),
            description: device.usesCellular
                ? Text("Swipt only stops working on cellular data, due to Apple’s restrictions on developer services.")
                : Text("Swipt needs a loopback VPN to connect to developer services.")
        ) {
        } actions: {
            if !device.usesCellular {
                if hasLocalDevVPN {
                    SetupPrimaryButton(title: Text("Open LocalDevVPN")) { openURL(Self.localDevVPN) }
                } else {
                    SetupPrimaryButton(title: Text("Get LocalDevVPN")) { openURL(Self.localDevVPNStore) }
                }
                SetupSecondaryButton(title: Text("More Options…")) {
                    openURL(URL(string: "https://swipt.io/\(Bundle.main.preferredLocalizations.first ?? "en")/docs/setup")!)
                }
            }
        }
        .onChangeCompat(of: scenePhase == .active, initial: true) { active in
            guard active else { return }
            hasLocalDevVPN = UIApplication.shared.canOpenURL(Self.localDevVPN)
        }
        .onChangeCompat(of: device.tunnelFound == true, initial: true) { found in
            if found {
                next()
            }
        }
    }
}
