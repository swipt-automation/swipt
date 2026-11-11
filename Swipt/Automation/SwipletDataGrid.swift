internal import ExpoModulesCore
internal import ExpoUI
import SwiftUI
import UIKit

nonisolated final class SwipletDataGridProps: UIBaseViewProps {
    @Field var columns: [[String: Any]] = []
    @Field var rowCount: Int = 0
    @Field var window: [String: Any] = [:]
    @Field var frozenColumns: Int = 0
    @Field var rowHeight: Double = 44
    @Field var headerHeight: Double = 36
    @Field var header: [String: Any] = [:]
    @Field var sortIndicator: [String: Any]?
    @Field var rowColors: [Any] = []
    let onWindowChange = EventDispatcher()
    let onCellPress = EventDispatcher()
    let onHeaderPress = EventDispatcher()
    let onHeaderLongPress = EventDispatcher()
}

struct SwipletDataGridView: ExpoSwiftUI.View {
    @ObservedObject var props: SwipletDataGridProps

    init(props: SwipletDataGridProps) {
        self.props = props
    }

    var body: some View {
        SwipletDataGridRepresentable(props: props)
    }
}

private struct SwipletDataGridRepresentable: UIViewRepresentable {
    @ObservedObject var props: SwipletDataGridProps

    func makeUIView(context: Context) -> SwipletDataGrid {
        SwipletDataGrid()
    }

    func updateUIView(_ grid: SwipletDataGrid, context: Context) {
        grid.update(props)
    }
}

private enum SwipletDataGridInsets {
    static let leading: CGFloat = 14
    static let trailing: CGFloat = 14
    static let spacing: CGFloat = 8
    static let arrow: CGFloat = 14
}

private func swipletDataGridFont(_ raw: [String: Any], monospacedDigit: Bool) -> UIFont {
    let style: UIFont.TextStyle =
        switch raw.string("textStyle") {
        case "largeTitle": .largeTitle
        case "title": .title1
        case "title2": .title2
        case "title3": .title3
        case "headline": .headline
        case "subheadline": .subheadline
        case "callout": .callout
        case "footnote": .footnote
        case "caption": .caption1
        case "caption2": .caption2
        default: .body
        }
    let size = raw.number("size").map { CGFloat($0) } ?? UIFont.preferredFont(forTextStyle: style).pointSize
    let weight: UIFont.Weight =
        switch raw.string("weight") {
        case "ultraLight": .ultraLight
        case "thin": .thin
        case "light": .light
        case "medium": .medium
        case "semibold": .semibold
        case "bold": .bold
        case "heavy": .heavy
        case "black": .black
        case "regular": .regular
        default: style == .headline ? .semibold : .regular
        }
    if monospacedDigit {
        return .monospacedDigitSystemFont(ofSize: size, weight: weight)
    }
    let font = UIFont.systemFont(ofSize: size, weight: weight)
    let design: UIFontDescriptor.SystemDesign? =
        switch raw.string("design") {
        case "rounded": .rounded
        case "monospaced": .monospaced
        case "serif": .serif
        default: nil
        }
    return design.flatMap { font.fontDescriptor.withDesign($0) }.map { UIFont(descriptor: $0, size: size) } ?? font
}

private final class SwipletDataGridColumn {
    let header: String
    let alignment: NSTextAlignment
    let font: UIFont
    let monospacedDigit: Bool
    let minWidth: CGFloat
    let maxWidth: CGFloat
    let imageSize: CGFloat
    var content: CGFloat = 0
    private let widths = NSCache<NSString, NSNumber>()

    init(_ raw: [String: Any]) {
        header = raw.string("header") ?? ""
        alignment =
            switch raw.string("alignment") {
            case "center": .center
            case "trailing": .right
            default: .natural
            }
        monospacedDigit = raw.flag("monospacedDigit")
        minWidth = CGFloat(raw.number("minWidth") ?? 0)
        maxWidth = raw.number("maxWidth").map { CGFloat($0) } ?? .greatestFiniteMagnitude
        imageSize = CGFloat(raw.number("imageSize") ?? 0)
        font = swipletDataGridFont(raw["font"] as? [String: Any] ?? [:], monospacedDigit: monospacedDigit)
        widths.countLimit = 2048
    }

    func width(_ text: String) -> CGFloat {
        let shape = (monospacedDigit ? String(text.map { $0.isASCII && $0.isNumber ? "0" : $0 }) : text) as NSString
        if let width = widths.object(forKey: shape) {
            return CGFloat(width.doubleValue)
        }
        let width = ceil(shape.size(withAttributes: [.font: font]).width)
        widths.setObject(NSNumber(value: Double(width)), forKey: shape)
        return width
    }

    func fit(_ cell: SwipletDataGridCell) -> Bool {
        let width = width(cell.text) + (imageSize > 0 ? imageSize + SwipletDataGridInsets.spacing : 0)
        guard width > content else { return false }
        content = width
        return true
    }
}

private struct SwipletDataGridCell {
    let fields: [String: Any]
    let text: String

    init(_ raw: Any?) {
        if let fields = raw as? [String: Any] {
            self.fields = fields
            text = fields.string("text") ?? ""
        } else {
            fields = [:]
            text = raw as? String ?? (raw as? NSNumber)?.stringValue ?? ""
        }
    }

    var image: UIImage? {
        if let uri = fields.string("image") {
            return SwipletDataGridImages.image(uri)
        }
        return fields.string("systemImage").flatMap { UIImage(systemName: $0) }
    }

    var color: UIColor? {
        fields["color"].flatMap { try? UIColor.convert(from: $0) }
    }

    var imageColor: UIColor? {
        fields["imageColor"].flatMap { try? UIColor.convert(from: $0) }
    }
}

private enum SwipletDataGridImages {
    private static let cache = NSCache<NSString, UIImage>()

    static func image(_ uri: String) -> UIImage? {
        if let cached = cache.object(forKey: uri as NSString) {
            return cached
        }
        guard let url = URL(string: uri), let data = try? Data(contentsOf: url), let image = UIImage(data: data) else { return nil }
        cache.setObject(image, forKey: uri as NSString)
        return image
    }
}

private struct SwipletDataGridMetrics {
    var widths: [CGFloat] = []
    var offsets: [CGFloat] = []
    var images: [CGFloat] = []
    var total: CGFloat = 0
    var frozen = 0

    var frozenWidth: CGFloat {
        frozen < offsets.count ? offsets[frozen] : total
    }

    func column(at x: CGFloat, scrolled offset: CGFloat) -> Int? {
        let position = x < frozenWidth ? x : x + offset
        return widths.indices.last { offsets[$0] <= position && position < offsets[$0] + widths[$0] }
    }
}

private final class SwipletDataGridSlot {
    let label = UILabel()
    lazy var imageView: UIImageView = {
        let view = UIImageView()
        view.contentMode = .scaleAspectFit
        view.clipsToBounds = true
        view.layer.cornerCurve = .continuous
        label.superview?.addSubview(view)
        return view
    }()
    var hasImageView = false
}

private final class SwipletDataGridRow: UIView {
    private let scrolling = UIView()
    private let frozen = UIView()
    private let edge = UIView()
    private var slots: [SwipletDataGridSlot] = []
    private var metrics = SwipletDataGridMetrics()

    var offset: CGFloat = 0 {
        didSet {
            if offset != oldValue {
                placeFrozen()
            }
        }
    }

    var fill: UIColor? {
        didSet {
            frozen.backgroundColor = fill
            backgroundColor = fill
        }
    }

    override init(frame: CGRect) {
        super.init(frame: frame)
        addSubview(scrolling)
        addSubview(frozen)
        edge.backgroundColor = .separator
        frozen.addSubview(edge)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError()
    }

    func apply(_ metrics: SwipletDataGridMetrics) {
        self.metrics = metrics
        while slots.count < metrics.widths.count {
            slots.append(SwipletDataGridSlot())
        }
        for (index, slot) in slots.enumerated() {
            let container = index < metrics.frozen ? frozen : scrolling
            if slot.label.superview !== container {
                container.addSubview(slot.label)
                if slot.hasImageView {
                    container.addSubview(slot.imageView)
                }
            }
            slot.label.isHidden = index >= metrics.widths.count
        }
        setNeedsLayout()
    }

    func set(_ index: Int, text: String, font: UIFont, color: UIColor, alignment: NSTextAlignment) {
        guard slots.indices.contains(index) else { return }
        let label = slots[index].label
        label.text = text
        label.font = font
        label.textColor = color
        label.textAlignment = alignment
    }

    func set(_ index: Int, image: UIImage?, tint: UIColor?) {
        guard slots.indices.contains(index), metrics.images.indices.contains(index) else { return }
        let slot = slots[index]
        guard let image else {
            if slot.hasImageView {
                slot.imageView.isHidden = true
            }
            return
        }
        if !slot.hasImageView {
            slot.hasImageView = true
            setNeedsLayout()
        }
        slot.imageView.isHidden = false
        slot.imageView.image = image
        slot.imageView.tintColor = tint ?? .secondaryLabel
        slot.imageView.layer.cornerRadius = image.isSymbolImage ? 0 : metrics.images[index] * 0.225
    }

    private func placeFrozen() {
        frozen.frame = CGRect(x: max(offset, 0), y: 0, width: metrics.frozenWidth, height: bounds.height)
        edge.isHidden = offset <= 0 || metrics.frozen == 0
    }

    override func layoutSubviews() {
        super.layoutSubviews()
        scrolling.frame = bounds
        placeFrozen()
        edge.frame = CGRect(x: metrics.frozenWidth - 1 / traitCollection.displayScale, y: 0, width: 1 / traitCollection.displayScale, height: bounds.height)
        for (index, slot) in slots.enumerated() where index < metrics.widths.count {
            var x = metrics.offsets[index] + SwipletDataGridInsets.leading
            var width = metrics.widths[index] - SwipletDataGridInsets.leading - SwipletDataGridInsets.trailing
            let image = metrics.images[index]
            if image > 0 {
                if slot.hasImageView {
                    slot.imageView.frame = CGRect(x: x, y: (bounds.height - image) / 2, width: image, height: image)
                }
                x += image + SwipletDataGridInsets.spacing
                width -= image + SwipletDataGridInsets.spacing
            }
            slot.label.frame = CGRect(x: x, y: 0, width: max(width, 0), height: bounds.height)
        }
    }
}

private final class SwipletDataGridRowCell: UICollectionViewCell {
    static let reuseIdentifier = "SwipletDataGridRowCell"
    let row = SwipletDataGridRow()

    override init(frame: CGRect) {
        super.init(frame: frame)
        row.frame = contentView.bounds
        row.autoresizingMask = [.flexibleWidth, .flexibleHeight]
        contentView.addSubview(row)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError()
    }
}

private final class SwipletDataGridLayout: UICollectionViewLayout {
    var rowHeight: CGFloat = 44
    var rowCount = 0
    var width: CGFloat = 0

    override var collectionViewContentSize: CGSize {
        CGSize(width: width, height: CGFloat(rowCount) * rowHeight)
    }

    override func layoutAttributesForElements(in rect: CGRect) -> [UICollectionViewLayoutAttributes]? {
        guard rowCount > 0, rowHeight > 0 else { return [] }
        let first = max(Int(floor(rect.minY / rowHeight)), 0)
        let last = min(Int(floor(rect.maxY / rowHeight)), rowCount - 1)
        return first <= last ? (first...last).map(attributes) : []
    }

    override func layoutAttributesForItem(at indexPath: IndexPath) -> UICollectionViewLayoutAttributes? {
        attributes(indexPath.item)
    }

    private func attributes(_ row: Int) -> UICollectionViewLayoutAttributes {
        let attributes = UICollectionViewLayoutAttributes(forCellWith: IndexPath(item: row, section: 0))
        attributes.frame = CGRect(x: 0, y: CGFloat(row) * rowHeight, width: width, height: rowHeight)
        return attributes
    }

    override func shouldInvalidateLayout(forBoundsChange newBounds: CGRect) -> Bool {
        false
    }
}

private final class SwipletDataGrid: UIView, UICollectionViewDataSource, UICollectionViewDelegate {
    private let layout = SwipletDataGridLayout()
    private let collectionView: UICollectionView
    private let headerClip = UIView()
    private let headerRow = SwipletDataGridRow()
    private let headerLine = UIView()
    private var arrow = UIImageView()
    private var props: SwipletDataGridProps?
    private var schema: NSArray = []
    private var columns: [SwipletDataGridColumn] = []
    private var metrics = SwipletDataGridMetrics()
    private var headerFont = UIFont.preferredFont(forTextStyle: .subheadline)
    private var headerColor = UIColor.secondaryLabel
    private var headerHeight: CGFloat = 36
    private var rowColors: [UIColor] = []
    private var start = 0
    private var rows: [[String: Any]] = []
    private var reported: [Int] = []
    private var laidOut: CGFloat = 0

    override init(frame: CGRect) {
        collectionView = UICollectionView(frame: .zero, collectionViewLayout: layout)
        super.init(frame: frame)
        collectionView.backgroundColor = .clear
        collectionView.dataSource = self
        collectionView.delegate = self
        collectionView.allowsSelection = false
        collectionView.alwaysBounceVertical = true
        collectionView.register(SwipletDataGridRowCell.self, forCellWithReuseIdentifier: SwipletDataGridRowCell.reuseIdentifier)
        collectionView.addGestureRecognizer(UITapGestureRecognizer(target: self, action: #selector(pressCell(_:))))
        addSubview(collectionView)
        headerClip.clipsToBounds = true
        headerRow.fill = .systemBackground
        headerClip.addSubview(headerRow)
        headerLine.backgroundColor = .separator
        headerClip.addSubview(headerLine)
        arrow.contentMode = .center
        headerClip.addGestureRecognizer(UITapGestureRecognizer(target: self, action: #selector(pressHeader(_:))))
        headerClip.addGestureRecognizer(UILongPressGestureRecognizer(target: self, action: #selector(longPressHeader(_:))))
        addSubview(headerClip)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError()
    }

    func update(_ props: SwipletDataGridProps) {
        self.props = props
        let schema =
            props.columns.map { column in
                var column = column
                column["prototype"] = nil
                return column
            } as NSArray
        var resized = false
        if !schema.isEqual(self.schema) {
            self.schema = schema
            columns = props.columns.map { SwipletDataGridColumn($0) }
            resized = true
        }
        let header = props.header
        headerFont = (header["font"] as? [String: Any]).map { swipletDataGridFont($0, monospacedDigit: false) } ?? .preferredFont(forTextStyle: .subheadline)
        headerColor = header["color"].flatMap { try? UIColor.convert(from: $0) } ?? .secondaryLabel
        rowColors = props.rowColors.compactMap { try? UIColor.convert(from: $0) }
        for (index, column) in columns.enumerated() where index < props.columns.count {
            if let prototype = props.columns[index]["prototype"] {
                resized = column.fit(SwipletDataGridCell(prototype)) || resized
            }
        }
        start = Int(props.window.number("start") ?? 0)
        rows = props.window["rows"] as? [[String: Any]] ?? []
        for row in rows {
            for (index, cell) in (row["cells"] as? [Any] ?? []).enumerated() where index < columns.count {
                resized = columns[index].fit(SwipletDataGridCell(cell)) || resized
            }
        }
        let rowHeight = CGFloat(props.rowHeight)
        let counted = layout.rowCount != props.rowCount || layout.rowHeight != rowHeight
        let frozen = min(max(props.frozenColumns, 0), columns.count)
        if headerHeight != CGFloat(props.headerHeight) {
            headerHeight = CGFloat(props.headerHeight)
            setNeedsLayout()
        }
        if resized || frozen != metrics.frozen {
            metrics.frozen = frozen
            measure()
        }
        if counted {
            layout.rowCount = props.rowCount
            layout.rowHeight = rowHeight
            layout.invalidateLayout()
            collectionView.reloadData()
        } else {
            for cell in collectionView.visibleCells {
                if let cell = cell as? SwipletDataGridRowCell, let indexPath = collectionView.indexPath(for: cell) {
                    configure(cell, at: indexPath.item)
                }
            }
        }
        configureHeader()
        report()
    }

    private func measure() {
        var widths = columns.map { column in
            let fitting = SwipletDataGridInsets.leading + SwipletDataGridInsets.trailing
                + max(column.content, ceil((column.header as NSString).size(withAttributes: [.font: headerFont]).width) + SwipletDataGridInsets.arrow)
            return max(min(fitting, column.maxWidth), column.minWidth)
        }
        let natural = widths.reduce(0, +)
        if natural > 0, natural < bounds.width {
            let scale = bounds.width / natural
            widths = widths.map { floor($0 * scale) }
            if let last = widths.indices.last {
                widths[last] += bounds.width - widths.reduce(0, +)
            }
        }
        metrics.widths = widths
        metrics.images = columns.map(\.imageSize)
        metrics.offsets = widths.indices.map { widths[..<$0].reduce(0, +) }
        metrics.total = widths.reduce(0, +)
        laidOut = bounds.width
        layout.width = metrics.total
        layout.invalidateLayout()
        for cell in collectionView.visibleCells {
            (cell as? SwipletDataGridRowCell)?.row.apply(metrics)
        }
        headerRow.apply(metrics)
        setNeedsLayout()
    }

    override func layoutSubviews() {
        super.layoutSubviews()
        if laidOut != bounds.width, !columns.isEmpty {
            measure()
        }
        headerClip.frame = CGRect(x: 0, y: 0, width: bounds.width, height: headerHeight)
        headerLine.frame = CGRect(x: 0, y: headerHeight - 1 / traitCollection.displayScale, width: bounds.width, height: 1 / traitCollection.displayScale)
        collectionView.frame = CGRect(x: 0, y: headerHeight, width: bounds.width, height: max(bounds.height - headerHeight, 0))
        placeHeader()
        report()
    }

    private func placeHeader() {
        let offset = collectionView.contentOffset.x
        headerRow.frame = CGRect(x: -offset, y: 0, width: max(metrics.total, bounds.width), height: headerHeight)
        headerRow.offset = offset
    }

    private func configureHeader() {
        headerRow.apply(metrics)
        let sorted = props?.sortIndicator.map { Int($0.number("column") ?? -1) }
        for (index, column) in columns.enumerated() {
            headerRow.set(index, text: column.header, font: headerFont, color: headerColor, alignment: column.alignment)
        }
        if let sorted, columns.indices.contains(sorted), metrics.widths.indices.contains(sorted) {
            arrow.image = UIImage(
                systemName: props?.sortIndicator?.flag("descending") == true ? "chevron.down" : "chevron.up",
                withConfiguration: UIImage.SymbolConfiguration(pointSize: 10, weight: .bold))
            arrow.tintColor = tintColor
            let x = metrics.offsets[sorted] + metrics.widths[sorted] - SwipletDataGridInsets.trailing
            arrow.frame = CGRect(x: x, y: 0, width: SwipletDataGridInsets.arrow, height: headerHeight)
            let container = headerRow.subviews[sorted < metrics.frozen ? 1 : 0]
            if arrow.superview !== container {
                container.addSubview(arrow)
            }
            arrow.isHidden = false
        } else {
            arrow.isHidden = true
        }
        placeHeader()
    }

    private func configure(_ cell: SwipletDataGridRowCell, at index: Int) {
        let row = cell.row
        row.apply(metrics)
        row.offset = collectionView.contentOffset.x
        row.fill = rowColors.isEmpty ? .systemBackground : rowColors[index % rowColors.count]
        let cells = rows.indices.contains(index - start) ? rows[index - start]["cells"] as? [Any] ?? [] : []
        for (column, definition) in columns.enumerated() {
            let content = SwipletDataGridCell(cells.indices.contains(column) ? cells[column] : nil)
            row.set(column, text: content.text, font: definition.font, color: content.color ?? .label, alignment: definition.alignment)
            row.set(column, image: definition.imageSize > 0 ? content.image : nil, tint: content.imageColor)
        }
    }

    private func report() {
        guard layout.rowCount > 0, layout.rowHeight > 0, collectionView.bounds.height > 0 else { return }
        let first = max(Int(floor(collectionView.contentOffset.y / layout.rowHeight)), 0)
        let last = min(Int(floor((collectionView.contentOffset.y + collectionView.bounds.height) / layout.rowHeight)), layout.rowCount - 1)
        guard reported != [first, last], first <= last else { return }
        reported = [first, last]
        props?.onWindowChange(["start": first, "end": last])
    }

    func collectionView(_ collectionView: UICollectionView, numberOfItemsInSection section: Int) -> Int {
        layout.rowCount
    }

    func collectionView(_ collectionView: UICollectionView, cellForItemAt indexPath: IndexPath) -> UICollectionViewCell {
        let cell = collectionView.dequeueReusableCell(withReuseIdentifier: SwipletDataGridRowCell.reuseIdentifier, for: indexPath)
        if let cell = cell as? SwipletDataGridRowCell {
            configure(cell, at: indexPath.item)
        }
        return cell
    }

    func scrollViewDidScroll(_ scrollView: UIScrollView) {
        let offset = scrollView.contentOffset.x
        for cell in collectionView.visibleCells {
            (cell as? SwipletDataGridRowCell)?.row.offset = offset
        }
        placeHeader()
        report()
    }

    @objc private func pressCell(_ recognizer: UITapGestureRecognizer) {
        let point = recognizer.location(in: collectionView)
        guard let indexPath = collectionView.indexPathForItem(at: point), rows.indices.contains(indexPath.item - start),
            let key = rows[indexPath.item - start].string("key"),
            let column = metrics.column(at: point.x - collectionView.contentOffset.x, scrolled: collectionView.contentOffset.x)
        else { return }
        props?.onCellPress(["key": key, "column": column])
    }

    @objc private func pressHeader(_ recognizer: UITapGestureRecognizer) {
        if let column = metrics.column(at: recognizer.location(in: headerClip).x, scrolled: collectionView.contentOffset.x) {
            props?.onHeaderPress(["column": column])
        }
    }

    @objc private func longPressHeader(_ recognizer: UILongPressGestureRecognizer) {
        if recognizer.state == .began,
            let column = metrics.column(at: recognizer.location(in: headerClip).x, scrolled: collectionView.contentOffset.x)
        {
            props?.onHeaderLongPress(["column": column])
        }
    }
}
