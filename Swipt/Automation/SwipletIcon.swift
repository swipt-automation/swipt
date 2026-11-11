import Foundation
import LibSwipt
import SwiftUI
import UIKit

nonisolated struct SwipletIcon: Hashable, Sendable {
    static let `default` = SwipletIcon(color: "brown", symbol: "square.2.layers.3d.fill")

    var color: String?
    var symbol: String
    var symbolColor: String?
    var image: UIImage?

    static func load(from swiplet: URL) -> SwipletIcon {
        var icon = LibSwiptLoadSwipletIcon(swiplet.path).map(SwipletIcon.init) ?? .default
        icon.image = UIImage(contentsOfFile: swiplet.appendingPathComponent(LibSwiptSwipletIconFile).path)
        return icon
    }
}

nonisolated extension SwipletIcon {
    init(_ icon: LibSwiptSwipletIcon) {
        self.init(color: icon.color.isEmpty ? nil : icon.color, symbol: icon.symbol, symbolColor: icon.symbolColor.isEmpty ? nil : icon.symbolColor)
    }
}

@available(iOS 17.4, *)
struct SwipletIconImage: View {
    let icon: SwipletIcon
    var size: CGFloat = 24

    var body: some View {
        let shape = RoundedRectangle(cornerRadius: size * 0.225, style: .continuous)
        if let image = icon.image {
            Image(uiImage: image)
                .resizable()
                .scaledToFill()
                .frame(width: size, height: size)
                .clipShape(shape)
        } else if let color = icon.color {
            SwipletIconGlyph(symbol: icon.symbol, pointSize: size * 0.6)
                .foregroundStyle(icon.symbolColor.flatMap(SwipletIconCatalog.color) ?? .white)
                .frame(width: size, height: size)
                .background(LinearGradient(colors: [.white.opacity(0.14), .clear], startPoint: .top, endPoint: .bottom), in: shape)
                .background(SwipletIconCatalog.color(color) ?? .brown, in: shape)
        } else if let color = icon.symbolColor.flatMap(SwipletIconCatalog.color) {
            SwipletIconGlyph(symbol: icon.symbol, pointSize: size * 0.6)
                .foregroundStyle(color)
                .frame(width: size, height: size)
        } else {
            SwipletIconGlyph(symbol: icon.symbol, pointSize: size * 0.6)
                .frame(width: size, height: size)
        }
    }
}

@available(iOS 17.4, *)
struct SwipletIconGlyph: View {
    let symbol: String
    let pointSize: CGFloat

    var body: some View {
        let configuration = UIImage.SymbolConfiguration(pointSize: pointSize)
        let image = UIImage(systemName: symbol, withConfiguration: configuration)
        let symbol = image == nil ? SwipletIcon.default.symbol : symbol
        let size = (image ?? UIImage(systemName: symbol, withConfiguration: configuration))?.size ?? .zero
        Image(systemName: symbol)
            .font(.system(size: pointSize))
            .scaleEffect(min(1, pointSize * 1.4 / max(size.width, size.height, 1)))
    }
}
