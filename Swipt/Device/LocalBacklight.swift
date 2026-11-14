import Foundation
import notify

nonisolated enum LocalBacklight {
    private static let displayStatusKey = "com.apple.iokit.hid.displayStatus"
    private static let blankedScreenKey = "com.apple.springboard.hasBlankedScreen"
    private static let backlight = Backlight()

    static var state: Int32 {
        guard backlight != nil, let state = currentDisplayState else { return -1 }
        return Int32(state.rawValue)
    }

    // BLSBacklightState values; BLSBacklightStateIsActive is true for ActiveOn and Dimmed
    // (BacklightServices 24A446, NSStringFromBLSBacklightState 0x22582161c,
    // BLSBacklightStateIsActive 0x2258259c4).
    private enum DisplayState: Int {
        case off, inactiveOn, activeOn, dimmed

        var isOn: Bool { self == .activeOn || self == .dimmed }
    }

    private static var currentDisplayState: DisplayState? {
        guard let hidState = state(of: displayStatusKey), let blanked = state(of: blankedScreenKey) else { return nil }
        if let backlight {
            guard let state = DisplayState(rawValue: backlight.state) else { return nil }
            // backboardd posts com.apple.springboard.hasBlankedScreen when the state of the display itself has changed
            // (BacklightServicesHost 24A446, -[BLSHOSDisplayInterface notifyDisplayBlankedIfChangedForCADisplayState:]
            // 0x22575d748; backboardd 24B5110a, 0x10000e73c). When Home woke a display that had just locked, that came
            // after the backlight became activeOn, and SpringBoard took the keys pressed before it as requests to wake
            // (iOS 27.3 24C5037i).
            return (hidState == 0 || blanked != 0) && state.isOn ? .off : state
        }
        return hidState == 0 ? .off : .activeOn
    }

    private static func state(of name: String) -> UInt64? {
        var token: Int32 = 0
        guard notify_register_check(name, &token) == NOTIFY_STATUS_OK else { return nil }
        defer { notify_cancel(token) }
        var state: UInt64 = 0
        guard notify_get_state(token, &state) == NOTIFY_STATUS_OK else { return nil }
        return state
    }

    private final class Backlight: @unchecked Sendable {
        private typealias GetState = @convention(c) (AnyObject, Selector) -> Int
        private let controller: NSObject
        private let getState: GetState
        private let selector = NSSelectorFromString("backlightState")

        init?() {
            _ = dlopen("/System/Library/PrivateFrameworks/BacklightServices.framework/BacklightServices", RTLD_NOW)
            let sharedSelector = NSSelectorFromString("sharedBacklight")
            guard let type = NSClassFromString("BLSBacklight"),
                  let sharedMethod = class_getClassMethod(type, sharedSelector),
                  let stateMethod = class_getInstanceMethod(type, selector) else { return nil }
            typealias SharedBacklight = @convention(c) (AnyClass, Selector) -> Unmanaged<AnyObject>
            let shared = unsafeBitCast(method_getImplementation(sharedMethod), to: SharedBacklight.self)
            guard let controller = shared(type, sharedSelector).takeUnretainedValue() as? NSObject else { return nil }
            self.controller = controller
            getState = unsafeBitCast(method_getImplementation(stateMethod), to: GetState.self)
        }

        var state: Int { getState(controller, selector) }
    }
}
