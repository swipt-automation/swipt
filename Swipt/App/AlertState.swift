import SwiftUI
#if os(iOS)
import UIKit
#else
import AppKit
#endif

struct AlertState {
    struct Action {
        var label: String
        var role: ButtonRole?
        var isDefault = false
        var perform: () -> Void
    }

    var title: String
    var message: String
    var actions: [Action]

    init(title: String, message: String = "", actions: [Action] = [Action(label: String(localized: "OK"), role: .cancel) {}]) {
        self.title = title
        self.message = message
        self.actions = actions
    }

    init(_ failure: String, error: (any Error)?) {
        if let error = error as? any RecoverableError {
            let options = error.recoveryOptions
            let recoveries = options.indices.map { index in
                Action(label: options[index], role: nil) { _ = error.attemptRecovery(optionIndex: index) }
            }
            self.init(title: failure, message: Self.message(of: error), actions: recoveries + [Action(label: String(localized: "OK"), role: .cancel, isDefault: true) {}])
        } else {
            self.init(failure, errorMessage: error.map(Self.message) ?? "")
        }
    }

    private static func message(of error: any Error) -> String {
        let bridged = error as NSError
        if error is LocalizedError || bridged.domain == "go" {
            return error.localizedDescription
        }
        let description = String(describing: error)
        return description.hasPrefix("Error Domain=") ? "\(error.localizedDescription) (\(bridged.domain) \(bridged.code))" : description
    }

    init(_ failure: String, errorMessage: String, closeLabel: String = String(localized: "OK"), onClose: @escaping () -> Void = {}) {
        let message = errorMessage.trimmingCharacters(in: .whitespacesAndNewlines)
        var actions = [Action(label: closeLabel, role: .cancel, isDefault: true, perform: onClose)]
        if !message.isEmpty {
            actions.insert(
                Action(label: String(localized: "Copy"), role: nil) {
                    #if os(iOS)
                    UIPasteboard.general.string = "\(failure)\n\(message)"
                    #else
                    NSPasteboard.general.clearContents()
                    NSPasteboard.general.setString("\(failure)\n\(message)", forType: .string)
                    #endif
                    onClose()
                }, at: 0)
        }
        self.init(title: failure, message: message, actions: actions)
    }
}

extension View {
    func alert(_ state: Binding<AlertState?>) -> some View {
        alert(
            state.wrappedValue?.title ?? "",
            isPresented: Binding(get: { state.wrappedValue != nil }, set: { if !$0 { state.wrappedValue = nil } }),
            presenting: state.wrappedValue
        ) { alert in
            ForEach(alert.actions.indices, id: \.self) { index in
                let action = alert.actions[index]
                #if os(macOS)
                Button(action.label, role: action.role, action: action.perform)
                    .keyboardShortcut(action.isDefault ? .defaultAction : nil)
                #else
                Button(action.label, role: action.role, action: action.perform)
                #endif
            }
        } message: { alert in
            if !alert.message.isEmpty {
                Text(alert.message)
            }
        }
    }
}

nonisolated struct MessageError: LocalizedError {
    let message: String

    init(_ message: String) {
        self.message = message
    }

    init(_ operation: String, status: OSStatus) {
        message = "\(operation): OSStatus \(status)"
    }

    var errorDescription: String? { message }
}
