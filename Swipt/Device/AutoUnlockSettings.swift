import Foundation
import LibSwipt
import Perception
import SwiftUI

@Perceptible
final class AutoUnlockSettings {
    static let shared = AutoUnlockSettings()

    private enum Key {
        static let isEnabled = "autoUnlocks"
        static let passcodeType = "autoUnlockPasscodeType"
        static let passcode = "autoUnlockPasscode"
        static let isSetUp = "autoUnlockSetUp"
    }

    var isEnabled: Bool {
        didSet { store.set(isEnabled, for: Key.isEnabled) }
    }

    var passcodeType: PasscodeType {
        didSet { store.set(passcodeType.rawValue, for: Key.passcodeType) }
    }

    var passcode: String {
        didSet { store.set(passcode, for: Key.passcode) }
    }

    var isSetUp: Bool {
        didSet { store.set(isSetUp, for: Key.isSetUp) }
    }

    private let store = PreferenceStore.shared

    init() {
        isEnabled = store.value(Bool.self, for: Key.isEnabled) ?? false
        passcodeType = store.value(Int32.self, for: Key.passcodeType).flatMap(PasscodeType.init(rawValue:)) ?? .pin
        passcode = store.value(String.self, for: Key.passcode) ?? ""
        isSetUp = store.value(Bool.self, for: Key.isSetUp) ?? false
    }

    var isDecided: Bool { isSetUp || isEnabled }

    var problem: String? {
        isEnabled ? passcodeType.problem(for: passcode) : nil
    }
}

enum PasscodeType: RawRepresentable, CaseIterable {
    case pin
    case numeric
    case alphanumeric

    init?(rawValue: Int32) {
        switch rawValue {
        case LibSwiptPasscodeTypePIN: self = .pin
        case LibSwiptPasscodeTypeNumeric: self = .numeric
        case LibSwiptPasscodeTypeAlphanumeric: self = .alphanumeric
        default: return nil
        }
    }

    var rawValue: Int32 {
        switch self {
        case .pin: LibSwiptPasscodeTypePIN
        case .numeric: LibSwiptPasscodeTypeNumeric
        case .alphanumeric: LibSwiptPasscodeTypeAlphanumeric
        }
    }

    var title: Text {
        switch self {
        case .pin: Text("Numeric Passcode")
        case .numeric: Text("Custom Numeric Passcode")
        case .alphanumeric: Text("Custom Alphanumeric Passcode")
        }
    }

    var subtitle: Text {
        switch self {
        case .pin: Text("4 or 6 digits on a keypad")
        case .numeric: Text("Any length on a keypad")
        case .alphanumeric: Text("Letters and numbers on a keyboard")
        }
    }

    func problem(for passcode: String) -> String? {
        guard !passcode.isEmpty else { return String(localized: "Enter this device’s lock screen passcode.") }
        guard passcode.utf8.count <= 64 else { return String(localized: "Automatic unlock supports passcodes up to 64 characters.") }
        let isNumeric = passcode.utf8.allSatisfy { (0x30...0x39).contains($0) }
        switch self {
        case .pin:
            guard isNumeric && (passcode.utf8.count == 4 || passcode.utf8.count == 6) else {
                return String(localized: "Enter a 4-digit or 6-digit passcode, or select the passcode type used by this device.")
            }
        case .numeric:
            guard isNumeric else { return String(localized: "A custom numeric passcode can contain only digits from 0 to 9.") }
        case .alphanumeric:
            guard passcode.utf8.allSatisfy({ (0x20...0x7e).contains($0) }) else {
                return String(localized: "Automatic unlock supports English letters, numbers, spaces, and standard punctuation. Other characters aren’t supported.")
            }
        }
        return nil
    }
}
