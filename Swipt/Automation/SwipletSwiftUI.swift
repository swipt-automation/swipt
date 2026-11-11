internal import ExpoModulesCore
internal import ExpoUI
import Foundation
import SwiftUI

nonisolated final class SwipletSwiftUIModule: Module {
    func definition() -> ModuleDefinition {
        Name("SwiptSwiftUI")

        OnCreate {
            ViewModifierRegistry.register("swiptHorizontalSizeClass") { parameters, _, _ in
                SwipletHorizontalSizeClassModifier(sizeClass: parameters.string("sizeClass") == "compact" ? .compact : .regular)
            }
        }

        ExpoUIView(SwipletChartView.self)
        ExpoUIView(SwipletDataGridView.self)
    }
}

nonisolated private struct SwipletHorizontalSizeClassModifier: ViewModifier {
    let sizeClass: UserInterfaceSizeClass

    func body(content: Content) -> some View {
        content.environment(\.horizontalSizeClass, sizeClass)
    }
}

nonisolated extension [String: Any] {
    func number(_ key: String) -> Double? {
        (self[key] as? NSNumber)?.doubleValue
    }

    func string(_ key: String) -> String? {
        self[key] as? String
    }

    func flag(_ key: String) -> Bool {
        self[key] as? Bool ?? false
    }

    func color(_ key: String) -> Color? {
        self[key].flatMap { try? Color.convert(from: $0) }
    }

    func colors(_ key: String) -> [Color]? {
        (self[key] as? [Any])?.compactMap { try? Color.convert(from: $0) }
    }

    func choice<Choice: RawRepresentable<String>>(_ key: String) -> Choice? {
        string(key).flatMap { Choice(rawValue: $0) }
    }
}
