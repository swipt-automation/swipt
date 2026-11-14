import SwiftUI
#if os(iOS)
import UIKit
import UserNotifications
#endif

@main
enum SwiptMain {
    static func main() {
        #if os(macOS)
        // AppKit opens no default window whenever saved state exists, even if SwiftUI restores no window from it,
        // unless the app is already active (AppKit 2775.10.103.1, macOS 27.0.1 26A434,
        // UUID 2688AFBC-5C39-3132-BCF5-8D00D32D7615,
        // -[NSApplication _reopenWindowsAsNecessaryIncludingRestorableState:withFullFidelity:completionHandler:]
        // 0x1852a5f00..0x1852a5ff4,
        // -[NSPersistentUIRestorer _finishedRestoringWindowsWithZOrder:options:completionHandler:] 0x185ca0144..0x185ca01d0,
        // ___58-[NSApplication(NSAppleEventHandling) _handleAEOpenEvent:]_block_invoke 0x185024488..0x1850244c0).
        UserDefaults.standard.register(defaults: ["ApplePersistenceIgnoreState": true])
        #endif
        SwiptApp.main()
    }
}

final class AppLaunch {
    init() {
        _ = AppDatabase.shared
        #if os(iOS)
        BadgeDots.install()
        if Platform.isSupported {
            Platform.shared.start()
        }
        #endif
    }
}

struct SwiptApp: App {
    #if os(iOS)
    @UIApplicationDelegateAdaptor(AppDelegate.self) private var appDelegate
    #endif
    private let launch = AppLaunch()

    var body: some Scene {
        #if os(iOS)
        WindowGroup {
            HomeView()
        }
        #else
        Window(Text(verbatim: "Swipt"), id: "home") {
            HomeView()
        }
        .defaultSize(width: 900, height: 560)
        .commands {
            AppCommands()
            SidebarCommands()
        }
        #endif
    }
}

#if os(iOS)
final class AppDelegate: NSObject, UIApplicationDelegate {
    // RCTKeyWindow falls back to the application delegate's window while no scene is in the foreground, unless
    // the scene manifest lists scene configurations (react-native 0.88.1, React/Base/RCTUtils.mm:672-674, 680-695),
    // and the delegate SwiftUI installs has no window method and forwards the call to this delegate
    // (SwiftUI 8.0.84, iOS 27.0.1 24A446, UUID 1D2A8243-1E78-3E43-AF2A-428BECD7EDD3,
    // __INSTANCE_METHODS__TtC7SwiftUI11AppDelegate 0x18b906e08,
    // -[SwiftUI.AppDelegate forwardingTargetForSelector:] 0x18a904c68).
    var window: UIWindow? {
        get {
            let windows = UIApplication.shared.connectedScenes.lazy.compactMap { $0 as? UIWindowScene }.flatMap(\.windows)
            return windows.first(where: \.isKeyWindow) ?? windows.first
        }
        set {}
    }

    func application(_ application: UIApplication, didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]? = nil) -> Bool {
        UNUserNotificationCenter.current().delegate = self
        return true
    }
}

extension AppDelegate: UNUserNotificationCenterDelegate {
    func userNotificationCenter(_ center: UNUserNotificationCenter, willPresent notification: UNNotification) async -> UNNotificationPresentationOptions {
        [.banner, .list, .sound]
    }

    func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse) async {
        let content = response.notification.request.content
        guard content.categoryIdentifier == ErrorAlert.notificationCategory else { return }
        await MainActor.run {
            if #available(iOS 17.4, *), let name = content.userInfo[ErrorAlert.swipletKey] as? String, !SwipletHost.shared.isPresented(name),
               let entry = SwipletStore.shared.interfaces[name] {
                SwipletPresentation.open(name: name, entry: entry)
            }
            ErrorAlert.shared.reopen(content)
        }
    }
}
#endif
