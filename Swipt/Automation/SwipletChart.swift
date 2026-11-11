import Charts
internal import ExpoModulesCore
internal import ExpoUI
import Foundation
import SwiftUI

nonisolated protocol SwipletChartSpan {
    associatedtype Value: Plottable
    var start: Value { get }
    var end: Value { get }
}

nonisolated struct SwipletChartSpanOf<Value: Plottable>: SwipletChartSpan {
    let start: Value
    let end: Value
}

nonisolated enum SwipletChartValueKind: String {
    case number
    case string
    case date

    func value(_ raw: Any?) -> (any Plottable)? {
        switch self {
        case .number:
            return (raw as? NSNumber)?.doubleValue
        case .string:
            return raw as? String
        case .date:
            return Self.date(raw)
        }
    }

    func span(_ start: Any?, _ end: Any?) -> (any SwipletChartSpan)? {
        switch self {
        case .number:
            guard let start = (start as? NSNumber)?.doubleValue, let end = (end as? NSNumber)?.doubleValue else { return nil }
            return SwipletChartSpanOf(start: start, end: end)
        case .string:
            guard let start = start as? String, let end = end as? String else { return nil }
            return SwipletChartSpanOf(start: start, end: end)
        case .date:
            guard let start = Self.date(start), let end = Self.date(end) else { return nil }
            return SwipletChartSpanOf(start: start, end: end)
        }
    }

    static func date(_ raw: Any?) -> Date? {
        (raw as? NSNumber).map { Date(timeIntervalSince1970: $0.doubleValue / 1000) }
    }
}

nonisolated enum SwipletChartMarkKind: String {
    case line
    case area
    case bar
    case point
    case rule
    case rectangle
    case sector
}

nonisolated enum SwipletChartInterpolation: String {
    case linear
    case catmullRom
    case cardinal
    case monotone
    case stepStart
    case stepCenter
    case stepEnd

    var method: InterpolationMethod {
        switch self {
        case .linear: .linear
        case .catmullRom: .catmullRom
        case .cardinal: .cardinal
        case .monotone: .monotone
        case .stepStart: .stepStart
        case .stepCenter: .stepCenter
        case .stepEnd: .stepEnd
        }
    }
}

nonisolated enum SwipletChartSymbol: String {
    case circle
    case square
    case triangle
    case diamond
    case pentagon
    case plus
    case cross
    case asterisk

    var shape: BasicChartSymbolShape {
        switch self {
        case .circle: .circle
        case .square: .square
        case .triangle: .triangle
        case .diamond: .diamond
        case .pentagon: .pentagon
        case .plus: .plus
        case .cross: .cross
        case .asterisk: .asterisk
        }
    }
}

nonisolated enum SwipletChartStacking: String {
    case standard
    case normalized
    case center
    case unstacked

    var method: MarkStackingMethod {
        switch self {
        case .standard: .standard
        case .normalized: .normalized
        case .center: .center
        case .unstacked: .unstacked
        }
    }
}

nonisolated enum SwipletChartLineCap: String {
    case butt
    case round
    case square

    var cap: CGLineCap {
        switch self {
        case .butt: .butt
        case .round: .round
        case .square: .square
        }
    }
}

nonisolated extension [String: Any] {
    func dimension(_ key: String) -> MarkDimension {
        guard let fields = self[key] as? [String: Any] else { return .automatic }
        if let fixed = fields.number("fixed") {
            return .fixed(fixed)
        }
        if let ratio = fields.number("ratio") {
            return .ratio(ratio)
        }
        if let inset = fields.number("inset") {
            return .inset(inset)
        }
        return .automatic
    }
}

nonisolated struct SwipletChartMark {
    let type: SwipletChartMarkKind
    let data: [[String: Any]]
    let xKind: SwipletChartValueKind
    let yKind: SwipletChartValueKind
    let foregroundStyle: Color?
    let gradient: [Color]?
    let opacity: Double?
    let interpolation: SwipletChartInterpolation?
    let lineWidth: Double?
    let lineDash: [Double]?
    let lineCap: SwipletChartLineCap?
    let symbol: SwipletChartSymbol?
    let symbolSize: Double?
    let cornerRadius: Double?
    let stacking: MarkStackingMethod
    let width: MarkDimension
    let height: MarkDimension
    let innerRadius: MarkDimension
    let outerRadius: MarkDimension
    let angularInset: Double?

    init(_ raw: [String: Any]) {
        type = raw.choice("type") ?? .line
        data = raw["data"] as? [[String: Any]] ?? []
        xKind = raw.choice("xKind") ?? .number
        yKind = raw.choice("yKind") ?? .number
        foregroundStyle = raw.color("foregroundStyle")
        gradient = raw.colors("gradient")
        opacity = raw.number("opacity")
        interpolation = raw.choice("interpolation")
        lineWidth = raw.number("lineWidth")
        lineDash = raw["lineDash"] as? [Double]
        lineCap = raw.choice("lineCap")
        symbol = raw.choice("symbol")
        symbolSize = raw.number("symbolSize")
        cornerRadius = raw.number("cornerRadius")
        stacking = (raw.choice("stacking") as SwipletChartStacking?)?.method ?? .standard
        width = raw.dimension("width")
        height = raw.dimension("height")
        innerRadius = raw.dimension("innerRadius")
        outerRadius = raw.dimension("outerRadius")
        angularInset = raw.number("angularInset")
    }
}

nonisolated enum SwipletChartFormatStyle: String {
    case number
    case percent
    case bytes
    case date
}

nonisolated enum SwipletChartByteStyle: String {
    case memory
    case file
    case binary
    case decimal

    var style: ByteCountFormatStyle.Style {
        switch self {
        case .memory: .memory
        case .file: .file
        case .binary: .binary
        case .decimal: .decimal
        }
    }
}

nonisolated enum SwipletChartDateStyle: String {
    case omitted
    case numeric
    case abbreviated
    case long
    case complete

    var style: Date.FormatStyle.DateStyle {
        switch self {
        case .omitted: .omitted
        case .numeric: .numeric
        case .abbreviated: .abbreviated
        case .long: .long
        case .complete: .complete
        }
    }
}

nonisolated enum SwipletChartTimeStyle: String {
    case omitted
    case shortened
    case standard
    case complete

    var style: Date.FormatStyle.TimeStyle {
        switch self {
        case .omitted: .omitted
        case .shortened: .shortened
        case .standard: .standard
        case .complete: .complete
        }
    }
}

nonisolated struct SwipletChartFormat {
    let style: SwipletChartFormatStyle
    let fractionDigits: Int?
    let compact: Bool
    let scale: Double?
    let bytes: SwipletChartByteStyle
    let date: SwipletChartDateStyle
    let time: SwipletChartTimeStyle

    init(_ raw: [String: Any]) {
        style = raw.choice("style") ?? .number
        fractionDigits = raw.number("fractionDigits").map { Int($0) }
        compact = raw.flag("compact")
        scale = raw.number("scale")
        bytes = raw.choice("bytes") ?? .memory
        date = raw.choice("date") ?? .abbreviated
        time = raw.choice("time") ?? .shortened
    }

    func text(_ value: Double, locale: Locale) -> String {
        switch style {
        case .number:
            var format = FloatingPointFormatStyle<Double>.number.locale(locale)
            if let fractionDigits {
                format = format.precision(.fractionLength(fractionDigits))
            }
            if compact {
                format = format.notation(.compactName)
            }
            if let scale {
                format = format.scale(scale)
            }
            return value.formatted(format)
        case .percent:
            var format = FloatingPointFormatStyle<Double>.Percent.percent.locale(locale)
            if let fractionDigits {
                format = format.precision(.fractionLength(fractionDigits))
            }
            if let scale {
                format = format.scale(scale)
            }
            return value.formatted(format)
        case .bytes:
            return Int64(value).formatted(.byteCount(style: bytes.style).locale(locale))
        case .date:
            return text(Date(timeIntervalSince1970: value / 1000), locale: locale)
        }
    }

    func text(_ value: Date, locale: Locale) -> String {
        value.formatted(Date.FormatStyle(date: date.style, time: time.style, locale: locale))
    }
}

nonisolated enum SwipletChartAxisPosition: String {
    case automatic
    case leading
    case trailing
    case top
    case bottom

    var position: AxisMarkPosition {
        switch self {
        case .automatic: .automatic
        case .leading: .leading
        case .trailing: .trailing
        case .top: .top
        case .bottom: .bottom
        }
    }
}

nonisolated enum SwipletChartAxisPreset: String {
    case automatic
    case extended
    case aligned
    case inset

    var preset: AxisMarkPreset {
        switch self {
        case .automatic: .automatic
        case .extended: .extended
        case .aligned: .aligned
        case .inset: .inset
        }
    }
}

nonisolated struct SwipletChartAxis {
    let hidden: Bool
    let preset: AxisMarkPreset
    let position: AxisMarkPosition
    let count: Int?
    let kind: SwipletChartValueKind
    let numbers: [Double]?
    let strings: [String]?
    let labels: [String]?
    let format: SwipletChartFormat?
    let gridLines: Bool
    let ticks: Bool
    let labelsHidden: Bool
    let title: String?

    init(_ raw: [String: Any]) {
        hidden = raw.flag("hidden")
        preset = (raw.choice("preset") as SwipletChartAxisPreset?)?.preset ?? .automatic
        position = (raw.choice("position") as SwipletChartAxisPosition?)?.position ?? .automatic
        count = raw.number("count").map { Int($0) }
        kind = raw.choice("kind") ?? .number
        numbers = raw["numbers"] as? [Double]
        strings = raw["strings"] as? [String]
        labels = raw["labels"] as? [String]
        format = (raw["format"] as? [String: Any]).map { SwipletChartFormat($0) }
        gridLines = raw["gridLines"] as? Bool ?? true
        ticks = raw["ticks"] as? Bool ?? true
        labelsHidden = raw.flag("labelsHidden")
        title = raw.string("title")
    }
}

nonisolated enum SwipletChartScaleType: String {
    case linear
    case log
    case squareRoot
    case symmetricLog
    case date
    case category

    var type: ScaleType {
        switch self {
        case .linear: .linear
        case .log: .log
        case .squareRoot: .squareRoot
        case .symmetricLog: .symmetricLog
        case .date: .date
        case .category: .category
        }
    }
}

nonisolated struct SwipletChartScale {
    let kind: SwipletChartValueKind
    let numbers: [Double]?
    let strings: [String]?
    let type: ScaleType?
    let includesZero: Bool?
    let reversed: Bool?

    init(_ raw: [String: Any]) {
        kind = raw.choice("kind") ?? .number
        numbers = raw["numbers"] as? [Double]
        strings = raw["strings"] as? [String]
        type = (raw.choice("type") as SwipletChartScaleType?)?.type
        includesZero = raw["includesZero"] as? Bool
        reversed = raw["reversed"] as? Bool
    }
}

nonisolated enum SwipletChartLegendPosition: String {
    case automatic
    case overlay
    case top
    case bottom
    case leading
    case trailing
    case topLeading
    case topTrailing
    case bottomLeading
    case bottomTrailing

    var position: AnnotationPosition {
        switch self {
        case .automatic: .automatic
        case .overlay: .overlay
        case .top: .top
        case .bottom: .bottom
        case .leading: .leading
        case .trailing: .trailing
        case .topLeading: .topLeading
        case .topTrailing: .topTrailing
        case .bottomLeading: .bottomLeading
        case .bottomTrailing: .bottomTrailing
        }
    }
}

nonisolated struct SwipletChartLegend {
    let hidden: Bool
    let position: SwipletChartLegendPosition
    let spacing: Double?

    init(_ raw: [String: Any]) {
        hidden = raw.flag("hidden")
        position = raw.choice("position") ?? .automatic
        spacing = raw.number("spacing")
    }
}

nonisolated enum SwipletChartScrollAxes: String {
    case horizontal
    case vertical
    case both

    var axes: Axis.Set {
        switch self {
        case .horizontal: .horizontal
        case .vertical: .vertical
        case .both: [.horizontal, .vertical]
        }
    }
}

nonisolated final class SwipletChartProps: UIBaseViewProps {
    @Field var marks: [[String: Any]] = []
    @Field var xKind: String = "number"
    @Field var yKind: String = "number"
    @Field var xAxis: [String: Any]?
    @Field var yAxis: [String: Any]?
    @Field var xScale: [String: Any]?
    @Field var yScale: [String: Any]?
    @Field var seriesNames: [String]?
    @Field var seriesColors: [Color]?
    @Field var legend: [String: Any]?
    @Field var scrollableAxes: String?
    @Field var xVisibleDomain: Double?
    @Field var yVisibleDomain: Double?
    @Field var initialScrollX: Either<Double, String>?
    @Field var initialScrollY: Either<Double, String>?
    @Field var selectsX: Bool = false
    @Field var selectsY: Bool = false
    @Field var animated: Bool = false
    @Field var revision: Int = 0
    let onXSelectionChange = EventDispatcher()
    let onYSelectionChange = EventDispatcher()
}

struct SwipletChartView: ExpoSwiftUI.View {
    @ObservedObject var props: SwipletChartProps

    init(props: SwipletChartProps) {
        self.props = props
    }

    var body: some View {
        if #available(iOS 17.4, *) {
            SwipletChart(props: props)
        }
    }
}

@available(iOS 17.4, *)
private struct SwipletChart: View {
    @ObservedObject var props: SwipletChartProps

    var body: some View {
        let xAxis = props.xAxis.map { SwipletChartAxis($0) }
        let yAxis = props.yAxis.map { SwipletChartAxis($0) }
        let xLabel = xAxis?.title ?? "X"
        let yLabel = yAxis?.title ?? "Y"
        let xKind = SwipletChartValueKind(rawValue: props.xKind) ?? .number
        let yKind = SwipletChartValueKind(rawValue: props.yKind) ?? .number
        Chart {
            ForEach(Array(props.marks.enumerated()), id: \.offset) { mark in
                SwipletChartLayer(mark: SwipletChartMark(mark.element), xLabel: xLabel, yLabel: yLabel).content
            }
        }
        .modifier(SwipletChartAxisModifier(axis: xAxis, direction: .horizontal))
        .modifier(SwipletChartAxisModifier(axis: yAxis, direction: .vertical))
        .modifier(SwipletChartScaleModifier(scale: props.xScale.map { SwipletChartScale($0) }, direction: .horizontal))
        .modifier(SwipletChartScaleModifier(scale: props.yScale.map { SwipletChartScale($0) }, direction: .vertical))
        .modifier(SwipletChartSeriesModifier(names: props.seriesNames, colors: props.seriesColors))
        .modifier(SwipletChartLegendModifier(legend: props.legend.map { SwipletChartLegend($0) }))
        .chartScrollableAxes(props.scrollableAxes.flatMap { SwipletChartScrollAxes(rawValue: $0) }?.axes ?? [])
        .modifier(SwipletChartVisibleDomainModifier(length: props.xVisibleDomain, direction: .horizontal))
        .modifier(SwipletChartVisibleDomainModifier(length: props.yVisibleDomain, direction: .vertical))
        .modifier(SwipletChartScrollPositionModifier(position: props.initialScrollX, kind: xKind, direction: .horizontal))
        .modifier(SwipletChartScrollPositionModifier(position: props.initialScrollY, kind: yKind, direction: .vertical))
        .modifier(SwipletChartSelectionModifier(enabled: props.selectsX, kind: xKind, direction: .horizontal, change: props.onXSelectionChange))
        .modifier(SwipletChartSelectionModifier(enabled: props.selectsY, kind: yKind, direction: .vertical, change: props.onYSelectionChange))
        .animation(props.animated ? .default : nil, value: props.revision)
    }
}

@available(iOS 17.4, *)
private struct SwipletChartLayer {
    let mark: SwipletChartMark
    let xLabel: String
    let yLabel: String

    var content: AnyChartContent {
        var content = AnyChartContent(
            ForEach(Array(mark.data.enumerated()), id: \.offset) { datum in
                if let content = self.datum(datum.element) {
                    content
                }
            })
        if let gradient = mark.gradient {
            content = AnyChartContent(content.foregroundStyle(LinearGradient(colors: gradient, startPoint: .top, endPoint: .bottom)))
        } else if let color = mark.foregroundStyle {
            content = AnyChartContent(content.foregroundStyle(color))
        }
        if let opacity = mark.opacity {
            content = AnyChartContent(content.opacity(opacity))
        }
        if let interpolation = mark.interpolation {
            content = AnyChartContent(content.interpolationMethod(interpolation.method))
        }
        if mark.lineWidth != nil || mark.lineDash != nil || mark.lineCap != nil {
            let style = StrokeStyle(
                lineWidth: mark.lineWidth ?? 2, lineCap: mark.lineCap?.cap ?? .butt, dash: (mark.lineDash ?? []).map { CGFloat($0) })
            content = AnyChartContent(content.lineStyle(style))
        }
        if let symbol = mark.symbol {
            content = AnyChartContent(content.symbol(symbol.shape))
        }
        if let symbolSize = mark.symbolSize {
            content = AnyChartContent(content.symbolSize(symbolSize))
        }
        if let cornerRadius = mark.cornerRadius {
            content = AnyChartContent(content.cornerRadius(cornerRadius))
        }
        return content
    }

    private func datum(_ datum: [String: Any]) -> AnyChartContent? {
        guard var content = shape(datum) else { return nil }
        if let series = datum.string("series") {
            content = AnyChartContent(content.foregroundStyle(by: .value("Series", series)))
        }
        if let group = datum.string("group") {
            content = AnyChartContent(content.position(by: .value("Group", group)))
        }
        if let color = datum.color("color") {
            content = AnyChartContent(content.foregroundStyle(color))
        }
        return content
    }

    private func shape(_ datum: [String: Any]) -> AnyChartContent? {
        if mark.type == .sector {
            guard let value = datum.number("value") else { return nil }
            return AnyChartContent(
                SectorMark(
                    angle: .value(yLabel, value), innerRadius: mark.innerRadius, outerRadius: mark.outerRadius,
                    angularInset: mark.angularInset.map { CGFloat($0) }))
        }
        let x = mark.xKind.value(datum["x"])
        let y = mark.yKind.value(datum["y"])
        let xSpan = mark.xKind.span(datum["xStart"], datum["xEnd"])
        let ySpan = mark.yKind.span(datum["yStart"], datum["yEnd"])
        if let x, let y {
            return point(x, y)
        }
        if let x, let ySpan {
            return vertical(x, ySpan)
        }
        if let xSpan, let y {
            return horizontal(xSpan, y)
        }
        if let xSpan, let ySpan {
            return rectangle(xSpan, ySpan)
        }
        guard mark.type == .rule else { return nil }
        if let x {
            return rule(x: x)
        }
        if let y {
            return rule(y: y)
        }
        return nil
    }

    private func point<X: Plottable, Y: Plottable>(_ x: X, _ y: Y) -> AnyChartContent? {
        let x = PlottableValue.value(xLabel, x)
        let y = PlottableValue.value(yLabel, y)
        switch mark.type {
        case .line:
            return AnyChartContent(LineMark(x: x, y: y))
        case .area:
            return AnyChartContent(AreaMark(x: x, y: y, stacking: mark.stacking))
        case .bar:
            return AnyChartContent(BarMark(x: x, y: y, width: mark.width, height: mark.height, stacking: mark.stacking))
        case .point:
            return AnyChartContent(PointMark(x: x, y: y))
        case .rectangle:
            return AnyChartContent(RectangleMark(x: x, y: y, width: mark.width, height: mark.height))
        case .rule, .sector:
            return nil
        }
    }

    private func vertical<X: Plottable, Span: SwipletChartSpan>(_ x: X, _ span: Span) -> AnyChartContent? {
        let x = PlottableValue.value(xLabel, x)
        let start = PlottableValue.value(yLabel, span.start)
        let end = PlottableValue.value(yLabel, span.end)
        switch mark.type {
        case .area:
            return AnyChartContent(AreaMark(x: x, yStart: start, yEnd: end))
        case .bar:
            return AnyChartContent(BarMark(x: x, yStart: start, yEnd: end, width: mark.width))
        case .rule:
            return AnyChartContent(RuleMark(x: x, yStart: start, yEnd: end))
        case .rectangle:
            return AnyChartContent(RectangleMark(x: x, yStart: start, yEnd: end, width: mark.width))
        case .line, .point, .sector:
            return nil
        }
    }

    private func horizontal<Span: SwipletChartSpan, Y: Plottable>(_ span: Span, _ y: Y) -> AnyChartContent? {
        let start = PlottableValue.value(xLabel, span.start)
        let end = PlottableValue.value(xLabel, span.end)
        let y = PlottableValue.value(yLabel, y)
        switch mark.type {
        case .area:
            return AnyChartContent(AreaMark(xStart: start, xEnd: end, y: y))
        case .bar:
            return AnyChartContent(BarMark(xStart: start, xEnd: end, y: y, height: mark.height))
        case .rule:
            return AnyChartContent(RuleMark(xStart: start, xEnd: end, y: y))
        case .rectangle:
            return AnyChartContent(RectangleMark(xStart: start, xEnd: end, y: y, height: mark.height))
        case .line, .point, .sector:
            return nil
        }
    }

    private func rectangle<XSpan: SwipletChartSpan, YSpan: SwipletChartSpan>(_ x: XSpan, _ y: YSpan) -> AnyChartContent? {
        let xStart = PlottableValue.value(xLabel, x.start)
        let xEnd = PlottableValue.value(xLabel, x.end)
        let yStart = PlottableValue.value(yLabel, y.start)
        let yEnd = PlottableValue.value(yLabel, y.end)
        guard mark.type == .rectangle else { return nil }
        return AnyChartContent(RectangleMark(xStart: xStart, xEnd: xEnd, yStart: yStart, yEnd: yEnd))
    }

    private func rule<X: Plottable>(x: X) -> AnyChartContent {
        AnyChartContent(RuleMark(x: .value(xLabel, x)))
    }

    private func rule<Y: Plottable>(y: Y) -> AnyChartContent {
        AnyChartContent(RuleMark(y: .value(yLabel, y)))
    }
}

@available(iOS 17.4, *)
private struct SwipletChartAxisModifier: ViewModifier {
    let axis: SwipletChartAxis?
    let direction: Axis
    @Environment(\.locale) private var locale

    func body(content: Content) -> some View {
        if let axis {
            if axis.hidden {
                switch direction {
                case .horizontal: content.chartXAxis(.hidden)
                case .vertical: content.chartYAxis(.hidden)
                }
            } else {
                switch direction {
                case .horizontal:
                    content.chartXAxis { marks(axis) }
                        .chartXAxisLabel(position: .bottom) { title(axis) }
                case .vertical:
                    content.chartYAxis { marks(axis) }
                        .chartYAxisLabel(position: .leading) { title(axis) }
                }
            }
        } else {
            content
        }
    }

    @ViewBuilder
    private func title(_ axis: SwipletChartAxis) -> some View {
        if let title = axis.title {
            Text(title)
        }
    }

    private func marks(_ axis: SwipletChartAxis) -> AnyAxisContent {
        if let strings = axis.strings {
            return AnyAxisContent(AxisMarks(preset: axis.preset, position: axis.position, values: strings) { mark(axis, $0) })
        }
        if let numbers = axis.numbers {
            if axis.kind == .date {
                let dates = numbers.map { Date(timeIntervalSince1970: $0 / 1000) }
                return AnyAxisContent(AxisMarks(preset: axis.preset, position: axis.position, values: dates) { mark(axis, $0) })
            }
            return AnyAxisContent(AxisMarks(preset: axis.preset, position: axis.position, values: numbers) { mark(axis, $0) })
        }
        return AnyAxisContent(
            AxisMarks(preset: axis.preset, position: axis.position, values: .automatic(desiredCount: axis.count)) { mark(axis, $0) })
    }

    @AxisMarkBuilder
    private func mark(_ axis: SwipletChartAxis, _ value: AxisValue) -> some AxisMark {
        if axis.gridLines {
            AxisGridLine()
        }
        if axis.ticks {
            AxisTick()
        }
        if !axis.labelsHidden {
            if let labels = axis.labels, labels.indices.contains(value.index) {
                AxisValueLabel(labels[value.index])
            } else if let format = axis.format, let number = value.as(Double.self) {
                AxisValueLabel(format.text(number, locale: locale))
            } else if let format = axis.format, let date = value.as(Date.self) {
                AxisValueLabel(format.text(date, locale: locale))
            } else {
                AxisValueLabel()
            }
        }
    }
}

@available(iOS 17.4, *)
private struct SwipletChartScaleModifier: ViewModifier {
    let scale: SwipletChartScale?
    let direction: Axis

    func body(content: Content) -> some View {
        if let scale {
            if let strings = scale.strings {
                apply(content, strings, scale)
            } else if let numbers = scale.numbers, let lower = numbers.min(), let upper = numbers.max() {
                if scale.kind == .date {
                    apply(content, Date(timeIntervalSince1970: lower / 1000)...Date(timeIntervalSince1970: upper / 1000), scale)
                } else {
                    apply(content, lower...upper, scale)
                }
            } else {
                apply(content, .automatic(includesZero: scale.includesZero, reversed: scale.reversed), scale)
            }
        } else {
            content
        }
    }

    @ViewBuilder
    private func apply(_ content: Content, _ domain: some ScaleDomain, _ scale: SwipletChartScale) -> some View {
        switch direction {
        case .horizontal: content.chartXScale(domain: domain, type: scale.type)
        case .vertical: content.chartYScale(domain: domain, type: scale.type)
        }
    }
}

@available(iOS 17.4, *)
private struct SwipletChartSeriesModifier: ViewModifier {
    let names: [String]?
    let colors: [Color]?

    func body(content: Content) -> some View {
        if let names, let colors {
            content.chartForegroundStyleScale(domain: names, range: colors)
        } else {
            content
        }
    }
}

@available(iOS 17.4, *)
private struct SwipletChartLegendModifier: ViewModifier {
    let legend: SwipletChartLegend?

    func body(content: Content) -> some View {
        if let legend {
            if legend.hidden {
                content.chartLegend(.hidden)
            } else {
                content.chartLegend(position: legend.position.position, spacing: legend.spacing.map { CGFloat($0) })
            }
        } else {
            content
        }
    }
}

@available(iOS 17.4, *)
private struct SwipletChartVisibleDomainModifier: ViewModifier {
    let length: Double?
    let direction: Axis

    func body(content: Content) -> some View {
        if let length {
            switch direction {
            case .horizontal: content.chartXVisibleDomain(length: length)
            case .vertical: content.chartYVisibleDomain(length: length)
            }
        } else {
            content
        }
    }
}

@available(iOS 17.4, *)
private struct SwipletChartScrollPositionModifier: ViewModifier {
    let position: Either<Double, String>?
    let kind: SwipletChartValueKind
    let direction: Axis

    func body(content: Content) -> some View {
        if let string: String = position?.get() {
            apply(content, string)
        } else if let number: Double = position?.get() {
            if kind == .date {
                apply(content, Date(timeIntervalSince1970: number / 1000))
            } else {
                apply(content, number)
            }
        } else {
            content
        }
    }

    @ViewBuilder
    private func apply(_ content: Content, _ position: some Plottable) -> some View {
        switch direction {
        case .horizontal: content.chartScrollPosition(initialX: position)
        case .vertical: content.chartScrollPosition(initialY: position)
        }
    }
}

@available(iOS 17.4, *)
private struct SwipletChartSelectionModifier: ViewModifier {
    let enabled: Bool
    let kind: SwipletChartValueKind
    let direction: Axis
    let change: EventDispatcher
    @State private var number: Double?
    @State private var string: String?
    @State private var date: Date?

    func body(content: Content) -> some View {
        if enabled {
            switch kind {
            case .number:
                apply(content, $number)
                    .onChange(of: number) { _, value in report(value) }
            case .string:
                apply(content, $string)
                    .onChange(of: string) { _, value in report(value) }
            case .date:
                apply(content, $date)
                    .onChange(of: date) { _, value in report(value.map { $0.timeIntervalSince1970 * 1000 }) }
            }
        } else {
            content
        }
    }

    @ViewBuilder
    private func apply<Value: Plottable>(_ content: Content, _ value: Binding<Value?>) -> some View {
        switch direction {
        case .horizontal: content.chartXSelection(value: value)
        case .vertical: content.chartYSelection(value: value)
        }
    }

    private func report(_ value: Any?) {
        change(value.map { ["value": $0] } ?? [:])
    }
}
