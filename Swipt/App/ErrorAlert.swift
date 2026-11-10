import Perception
import SwiftUI
import os
#if os(iOS)
import UIKit
import UserNotifications
#endif

@Perceptible
final class ErrorAlert {
    static let shared = ErrorAlert()

    nonisolated static let notificationCategory = "error"
    nonisolated static let swipletKey = "swiplet"
    nonisolated private static let logger = Logger(subsystem: Bundle.main.bundleIdentifier ?? "Swipt", category: "error")

    private(set) var alert: AlertState?
    private(set) var level = 0
    @PerceptionIgnored private var queued: [AlertState] = []

    func report(_ failure: String, error: any Error) {
        report(AlertState(failure, error: error))
    }

    func report(_ alert: AlertState, swiplet: String? = nil) {
        Self.logger.error("\(alert.title, privacy: .public): \(alert.message, privacy: .public)")
        guard enqueue(alert) else { return }
        #if os(iOS)
        guard UIApplication.shared.applicationState == .background else { return }
        let content = UNMutableNotificationContent()
        content.title = alert.title
        content.body = alert.message
        content.sound = .default
        content.categoryIdentifier = Self.notificationCategory
        if let swiplet {
            content.userInfo[Self.swipletKey] = swiplet
        }
        UNUserNotificationCenter.current().add(UNNotificationRequest(identifier: Self.identifier(of: alert), content: content, trigger: nil)) { error in
            if let error {
                Self.logger.error("Post the error notification: \(error, privacy: .public)")
            }
        }
        #endif
    }

    func cover() -> Int {
        level += 1
        return level
    }

    func uncover() {
        level -= 1
    }

    #if os(iOS)
    func reopen(_ notification: UNNotificationContent) {
        enqueue(AlertState(notification.title, errorMessage: notification.body))
    }
    #endif

    @discardableResult
    private func enqueue(_ alert: AlertState) -> Bool {
        guard !([self.alert].compactMap(\.self) + queued).contains(where: { $0.title == alert.title && $0.message == alert.message }) else { return false }
        if self.alert == nil, queued.isEmpty {
            self.alert = alert
        } else {
            queued.append(alert)
        }
        return true
    }

    fileprivate func dismiss() {
        #if os(iOS)
        if let alert {
            UNUserNotificationCenter.current().removeDeliveredNotifications(withIdentifiers: [Self.identifier(of: alert)])
        }
        #endif
        alert = nil
        guard !queued.isEmpty else { return }
        DispatchQueue.main.async { [self] in
            guard alert == nil, !queued.isEmpty else { return }
            alert = queued.removeFirst()
        }
    }

    private static func identifier(of alert: AlertState) -> String {
        "\(notificationCategory)\n\(alert.title)\n\(alert.message)"
    }
}

struct ErrorAlertPresenter: ViewModifier {
    private let level: Int
    private let errors = ErrorAlert.shared

    init(level: Int = 0) {
        self.level = level
    }

    func body(content: Content) -> some View {
        WithPerceptionTracking {
            content.alert(Binding(get: { errors.level == level ? errors.alert : nil }, set: { if $0 == nil { errors.dismiss() } }))
        }
    }
}
