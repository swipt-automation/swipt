import SwiftUI

@available(iOS 17.4, *)
enum SwipletIconCatalog {
    static func color(_ value: String) -> Color? {
        if let named = colors[value] {
            return named
        }
        guard value.count == 7, value.hasPrefix("#"), let code = UInt32(value.dropFirst(), radix: 16) else { return nil }
        return Color(.sRGB, red: Double(code >> 16) / 255, green: Double(code >> 8 & 0xFF) / 255, blue: Double(code & 0xFF) / 255)
    }

    private static let colors: [String: Color] = [
        "black": .black,
        "white": .white,
        "gray": .gray,
        "red": .red,
        "orange": .orange,
        "yellow": .yellow,
        "green": .green,
        "mint": .mint,
        "teal": .teal,
        "cyan": .cyan,
        "blue": .blue,
        "indigo": .indigo,
        "purple": .purple,
        "pink": .pink,
        "brown": .brown,
    ]
}
