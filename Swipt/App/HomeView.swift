import Perception
import SwiftUI

@Perceptible
final class HomeRequest {
    enum Pending: Equatable {
        case settings
    }

    static let shared = HomeRequest()
    var pending: Pending?
}

enum HomeTab: Hashable {
    case swiplets
    case developer
    case settings
}

struct HomeView: View {
    @State private var selection: HomeTab = .swiplets
    private let homeRequest = HomeRequest.shared
    #if os(iOS)
    @Environment(\.scenePhase) private var scenePhase
    @State private var isProbing = false
    @State private var isSettingUp = AutomatedSetupView.isRequested
    private let developer = DeveloperSettings.shared
    #endif

    var body: some View {
        layout
            .modifier(ErrorAlertPresenter())
            #if os(iOS)
            .modifier(SwipletOpener())
            .background {
                if AutomatedSetupView.isRequested, !isSettingUp {
                    AutomatedSetupFinished()
                }
            }
            #endif
            .onChangeCompat(of: homeRequest.pending, initial: true) { pending in
                guard let pending else { return }
                homeRequest.pending = nil
                switch pending {
                case .settings: selection = .settings
                }
            }
    }

    @ViewBuilder
    private var layout: some View {
        #if os(iOS)
        WithPerceptionTracking {
            if Platform.isSupported, isSettingUp {
                AutomatedSetupView { isSettingUp = false }
            } else if Platform.isSupported, SetupView.isNeeded {
                SetupView()
            } else if #available(iOS 18, *) {
                TabView(selection: $selection) {
                    Tab("Swiplets", systemImage: "wand.and.stars", value: HomeTab.swiplets) {
                        NavigationStackCompat {
                            swipletsView
                        }
                    }
                    Tab("Developer", systemImage: "terminal", value: HomeTab.developer) {
                        NavigationStackCompat {
                            developerView
                        }
                    }
                    .badge(isPublishing ? Text(verbatim: "") : nil)
                    .accessibilityValue(Text("Published"), isEnabled: isPublishing)
                    Tab("Settings", systemImage: "gearshape", value: HomeTab.settings) {
                        NavigationStackCompat {
                            SettingsView()
                        }
                    }
                }
                .tabViewStyle(.sidebarAdaptable)
            } else {
                TabView(selection: $selection) {
                    NavigationStackCompat {
                        swipletsView
                    }
                    .tabItem { Label("Swiplets", systemImage: "wand.and.stars") }
                    .tag(HomeTab.swiplets)
                    NavigationStackCompat {
                        developerView
                    }
                    .tabItem { Label("Developer", systemImage: "terminal") }
                    .tag(HomeTab.developer)
                    .badge(isPublishing ? Text(verbatim: "") : nil)
                    .accessibilityValue(isPublishing ? Text("Published") : Text(verbatim: ""))
                    NavigationStackCompat {
                        SettingsView()
                    }
                    .tabItem { Label("Settings", systemImage: "gearshape") }
                    .tag(HomeTab.settings)
                }
            }
        }
        .onChangeCompat(of: scenePhase == .active, initial: true) { active in
            guard Platform.isSupported, active != isProbing else { return }
            isProbing = active
            if active {
                Platform.shared.device.startProbing()
            } else {
                Platform.shared.device.stopProbing()
            }
        }
        #else
        NavigationSplitView {
            List(selection: Binding(get: { selection }, set: { if let page = $0 { selection = page } })) {
                Label("Swiplets", systemImage: "wand.and.stars")
                    .tag(HomeTab.swiplets)
                Label("Developer", systemImage: "terminal")
                    .tag(HomeTab.developer)
                Label("Settings", systemImage: "gearshape")
                    .tag(HomeTab.settings)
            }
            .navigationSplitViewColumnWidth(min: 160, ideal: 180, max: 240)
        } detail: {
            NavigationStack {
                switch selection {
                case .swiplets:
                    ContentUnavailableView("Not Available on Mac", systemImage: "desktopcomputer")
                        .navigationTitle("Swiplets")
                case .developer:
                    ContentUnavailableView("Not Available on Mac", systemImage: "desktopcomputer")
                        .navigationTitle("Developer")
                case .settings:
                    SettingsView()
                }
            }
            .id(selection)
        }
        #endif
    }

    #if os(iOS)
    @ViewBuilder
    private var swipletsView: some View {
        if #available(iOS 17.4, *), Platform.isSupported {
            SwipletsView()
        } else {
            PlatformUnsupportedView(title: "Swiplets")
        }
    }

    @ViewBuilder
    private var developerView: some View {
        if Platform.isSupported {
            DeveloperView()
        } else {
            PlatformUnsupportedView(title: "Developer")
        }
    }

    private var isPublishing: Bool {
        Platform.isSupported && developer.isPublishing
    }
    #endif
}
