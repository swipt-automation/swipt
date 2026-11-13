import Contacts
import ContactsUI
import MapKit
import Perception
import SwiftUI
import UIKit
import UniformTypeIdentifiers
import os

@available(iOS 17.4, *)
@Perceptible
final class ScriptDialogs {
    static let shared = ScriptDialogs()

    struct Presentation: Identifiable {
        let id: UUID
        let request: DialogRequest
        fileprivate let continuation: CheckedContinuation<DialogAnswer, any Error>
    }

    private(set) var presentations: [Presentation] = []
    private(set) var isKeyboardVisible = false
    @PerceptionIgnored var platterFrame = CGRect.null
    @PerceptionIgnored private var backdrop: UIImage?
    @PerceptionIgnored private var window: UIWindow?
    @PerceptionIgnored private var teardown: Task<Void, Never>?

    // -[WFSystemOverlayViewController shouldShowDimmingLayer] (iOS 27.2 24B5084k): a list, or a keyboard on a phone.
    var isDimmed: Bool {
        guard let first = presentations.first else { return false }
        if case .disambiguation = first.request { return true }
        return isKeyboardVisible && UIDevice.current.userInterfaceIdiom == .phone
    }

    private init() {
        NotificationCenter.default.addObserver(forName: UIScene.didActivateNotification, object: nil, queue: .main) { _ in
            MainActor.assumeIsolated { self.update() }
        }
        NotificationCenter.default.addObserver(forName: UIScene.didEnterBackgroundNotification, object: nil, queue: .main) { _ in
            MainActor.assumeIsolated { self.showBackdrop(nil) }
        }
        NotificationCenter.default.addObserver(forName: UIResponder.keyboardWillShowNotification, object: nil, queue: .main) { _ in
            MainActor.assumeIsolated { self.isKeyboardVisible = true }
        }
        NotificationCenter.default.addObserver(forName: UIResponder.keyboardWillHideNotification, object: nil, queue: .main) { _ in
            MainActor.assumeIsolated { self.isKeyboardVisible = false }
        }
    }

    func showBackdrop(_ image: Data?) {
        backdrop = image.flatMap(UIImage.init(data:))
        update()
        if backdrop != nil, let scene = window?.windowScene, scene.activationState == .background {
            window?.layoutIfNeeded()
            UIApplication.shared.requestSceneSessionRefresh(scene.session)
        }
    }

    func present(_ request: DialogRequest) async throws -> DialogAnswer {
        let id = UUID()
        return try await withTaskCancellationHandler {
            try await withCheckedThrowingContinuation { continuation in
                guard !Task.isCancelled else {
                    continuation.resume(throwing: CancellationError())
                    return
                }
                presentations.append(Presentation(id: id, request: request, continuation: continuation))
                update()
            }
        } onCancel: {
            Task { @MainActor in self.finish(id, with: .failure(CancellationError())) }
        }
    }

    func answer(_ presentation: Presentation, with answer: DialogAnswer) {
        finish(presentation.id, with: .success(answer))
    }

    func dismiss(_ presentation: Presentation) {
        finish(presentation.id, with: .failure(CancellationError()))
    }

    // The system platter passes other touches to the app below unless it dims the screen
    // (-[WFSystemOverlayViewController updateDimmingLayerVisibility], iOS 27.2 24B5084k).
    func capturesTouch(at point: CGPoint) -> Bool {
        backdrop != nil || isDimmed || (!presentations.isEmpty && platterFrame.contains(point))
    }

    func tapOutside() {
        if isKeyboardVisible {
            UIApplication.shared.sendAction(#selector(UIResponder.resignFirstResponder), to: nil, from: nil, for: nil)
        } else if let first = presentations.first, case .disambiguation = first.request {
            dismiss(first)
        }
    }

    private func finish(_ id: UUID, with result: Result<DialogAnswer, any Error>) {
        guard let index = presentations.firstIndex(where: { $0.id == id }) else { return }
        presentations.remove(at: index).continuation.resume(with: result)
        update()
    }

    private func update() {
        teardown?.cancel()
        let scenes = UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }
        let active = scenes.first { $0.activationState == .foregroundActive }
        guard !presentations.isEmpty || backdrop != nil, let scene = active ?? scenes.first else {
            if active == nil {
                removeWindow()
            } else {
                teardown = Task {
                    try? await Task.sleep(for: .seconds(Metrics.exitDuration))
                    if !Task.isCancelled { removeWindow() }
                }
            }
            return
        }
        if window?.windowScene !== scene {
            let window = OverlayWindow(windowScene: scene)
            window.windowLevel = .alert
            window.rootViewController = OverlayController()
            window.makeKeyAndVisible()
            self.window = window
        }
        (window?.rootViewController as? OverlayController)?.backdrop = backdrop
    }

    private func removeWindow() {
        window?.isHidden = true
        window = nil
    }
}

// Shortcuts labels its dialogs from these tables (iOS 27.2 24B115): the confirmation verbs and Cancel from AppIntents,
// Done, Text, Choose and File from WorkflowUI, Yes and No from WorkflowUICore, Contact, Location and Number from WorkflowKit,
// the address search from WorkflowEditor.
private enum SystemText {
    static func appIntents(_ key: String) -> String {
        lookup("/System/Library/Frameworks/AppIntents.framework", key)
    }

    static func workflowUI(_ key: String) -> String {
        lookup("/System/Library/PrivateFrameworks/WorkflowUI.framework", key)
    }

    static func workflowUICore(_ key: String) -> String {
        lookup("/System/Library/PrivateFrameworks/WorkflowUICore.framework", key)
    }

    static func workflowKit(_ key: String) -> String {
        lookup("/System/Library/PrivateFrameworks/WorkflowKit.framework", key)
    }

    static func workflowEditor(_ key: String) -> String {
        lookup("/System/Library/PrivateFrameworks/WorkflowEditor.framework", key)
    }

    private static func lookup(_ path: String, _ key: String) -> String {
        Bundle(path: path)?.localizedString(forKey: key, value: key, table: nil) ?? key
    }
}

private nonisolated enum Metrics {
    static let padWidth: CGFloat = 500
    static let padTop: CGFloat = 13
    static let cornerRadius: CGFloat = 41
    static let inset: CGFloat = 15
    static let spacing: CGFloat = 12
    static let buttonHeight: CGFloat = 54
    static let buttonFontSize: CGFloat = 19
    static let rowHeight: CGFloat = 55
    static let dimming = 0.36
    static let exitDuration = 0.4
}

@available(iOS 17.4, *)
private final class OverlayWindow: UIWindow {
    override func hitTest(_ point: CGPoint, with event: UIEvent?) -> UIView? {
        guard ScriptDialogs.shared.capturesTouch(at: point) || rootViewController?.presentedViewController != nil else { return nil }
        return super.hitTest(point, with: event)
    }
}

@available(iOS 17.4, *)
private final class OverlayController: UIViewController {
    private let backdropView = UIImageView()
    private let host = UIHostingController(rootView: DialogOverlay())

    var backdrop: UIImage? {
        didSet {
            backdropView.image = backdrop
            setNeedsStatusBarAppearanceUpdate()
        }
    }

    override var prefersStatusBarHidden: Bool {
        backdrop != nil
    }

    override func viewDidLoad() {
        super.viewDidLoad()
        backdropView.frame = view.bounds
        backdropView.autoresizingMask = [.flexibleWidth, .flexibleHeight]
        view.addSubview(backdropView)
        addChild(host)
        host.view.backgroundColor = .clear
        host.view.frame = view.bounds
        host.view.autoresizingMask = [.flexibleWidth, .flexibleHeight]
        view.addSubview(host.view)
        host.didMove(toParent: self)
    }
}

@available(iOS 17.4, *)
private struct DialogOverlay: View {
    private let dialogs = ScriptDialogs.shared

    var body: some View {
        WithPerceptionTracking {
            ZStack(alignment: .top) {
                Color.black
                    .opacity(dialogs.isDimmed ? Metrics.dimming : 0)
                    .contentShape(.rect)
                    .ignoresSafeArea()
                    .onTapGesture { dialogs.tapOutside() }
                if let presentation = dialogs.presentations.first {
                    DialogPlatter(presentation: presentation)
                        .id(presentation.id)
                        .transition(
                            AsymmetricTransition(
                                insertion: DialogTransition(scale: 0.7).animation(.spring(response: 0.55, dampingFraction: 0.68)),
                                removal: DialogTransition(scale: 0.81).animation(.spring(response: Metrics.exitDuration, dampingFraction: 1))))
                }
            }
            .animation(.spring(response: Metrics.exitDuration, dampingFraction: 1), value: dialogs.isDimmed)
        }
    }
}

// -[WFBannerTransitionSettings setDefaultValues] (iOS 27.2 24B5084k)
@available(iOS 17.4, *)
private struct DialogTransition: Transition {
    let scale: CGFloat

    func body(content: Content, phase: TransitionPhase) -> some View {
        content
            .scaleEffect(phase.isIdentity ? 1 : scale)
            .blur(radius: phase.isIdentity ? 0 : 10)
            .opacity(phase.isIdentity ? 1 : 0)
    }
}

@available(iOS 17.4, *)
private struct DialogPlatter: View {
    let presentation: ScriptDialogs.Presentation
    @State private var drag: CGFloat = 0
    @State private var contentHeight: CGFloat?

    var body: some View {
        ScrollView {
            content.onGeometryChange(for: CGFloat.self, of: \.size.height) { contentHeight = $0 }
        }
        .scrollBounceBehavior(.basedOnSize)
        .frame(maxHeight: contentHeight)
        .frame(maxWidth: UIDevice.isPad ? Metrics.padWidth : .infinity)
        .clipShape(.rect(cornerRadius: Metrics.cornerRadius))
        .glassEffectCompat(in: .rect(cornerRadius: Metrics.cornerRadius))
        .onGeometryChange(for: CGRect.self) { $0.frame(in: .global) } action: { ScriptDialogs.shared.platterFrame = $0 }
        .padding(.horizontal, 8)
        .padding(.top, UIDevice.isPad ? Metrics.padTop : 5.0 / 3)
        .ignoresSafeArea(.container, edges: UIDevice.isPad ? .top : [])
        .offset(y: drag < 0 ? drag : sqrt(drag) * 3)
        .gesture(
            DragGesture(minimumDistance: 12)
                .onChanged { drag = $0.translation.height }
                .onEnded { value in
                    if value.translation.height < -60 || value.predictedEndTranslation.height < -160 {
                        swipedAway()
                    } else {
                        withAnimation(.spring(response: 0.16, dampingFraction: 0.75)) { drag = 0 }
                    }
                })
    }

    @ViewBuilder
    private var content: some View {
        switch presentation.request {
        case .choice(let prompt, let options):
            let order = options.indices.filter { options[$0].style == .cancel } + options.indices.filter { options[$0].style != .cancel }
            DialogLayout(prompt: prompt, separated: true) {
                DialogActions(
                    order.map { index in
                        let option = options[index]
                        return switch option.style {
                        case .default: DialogAction(title: option.title, role: .prominent) { answer(.index(index)) }
                        case .destructive: DialogAction(title: option.title, role: .destructive) { answer(.index(index)) }
                        case .cancel: DialogAction(title: option.title, role: .neutral) { dismiss() }
                        }
                    })
            }
        case .confirmation(let prompt, let action):
            let (accept, deny) =
                switch action {
                case .named(let name): (SystemText.appIntents(name.title), SystemText.appIntents("Cancel"))
                case .custom(let accept, let deny, _): (accept, deny)
                }
            DialogLayout(prompt: prompt, separated: true) {
                DialogActions([
                    DialogAction(title: deny, role: .neutral) { dismiss() },
                    DialogAction(title: accept, role: .prominent) { answer(.none) },
                ])
            }
        case .valueConfirmation(let prompt, _):
            DialogLayout(prompt: prompt, separated: true) {
                DialogActions([
                    DialogAction(title: SystemText.workflowUICore("No"), role: .neutral) { answer(.confirmed(false)) },
                    DialogAction(title: SystemText.workflowUICore("Yes"), role: .prominent) { answer(.confirmed(true)) },
                ])
            }
        case .disambiguation(let prompt, let values):
            DialogLayout(prompt: prompt, separated: false) {
                DialogList(rows: values) { answer(.index($0)) }
            }
        case .value(let prompt, let field):
            ValueDialog(prompt: prompt, field: field, answer: answer, dismiss: dismiss)
        }
    }

    // AppIntent.requestChoice answers a swiped-away banner with its first option (iOS 27.2 24B5084k).
    private func swipedAway() {
        if case .choice(_, let options) = presentation.request, options.first?.style != .cancel {
            answer(.index(0))
        } else {
            dismiss()
        }
    }

    private func answer(_ answer: DialogAnswer) {
        ScriptDialogs.shared.answer(presentation, with: answer)
    }

    private func dismiss() {
        ScriptDialogs.shared.dismiss(presentation)
    }
}

private extension DialogRequest.ActionName {
    var title: String {
        rawValue.reduce(into: "") { title, character in
            if title.isEmpty {
                title.append(character.uppercased())
            } else {
                if character.isUppercase { title.append(" ") }
                title.append(character)
            }
        }
    }
}

private struct DialogLayout<Content: View>: View {
    let prompt: String?
    let separated: Bool
    var contentTop = Metrics.inset
    @ViewBuilder let content: Content
    @Environment(\.displayScale) private var displayScale
    @Environment(\.colorScheme) private var colorScheme

    var body: some View {
        VStack(spacing: 0) {
            if let prompt {
                Text(verbatim: prompt)
                    .font(.title3)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.horizontal, 27)
                    .padding(.top, 21)
                    .padding(.bottom, 14)
            }
            content
                .padding(.horizontal, Metrics.inset)
                .padding(.top, prompt != nil && !separated ? 0 : contentTop)
                .padding(.bottom, Metrics.inset)
                .overlay(alignment: .top) {
                    if prompt != nil, separated {
                        Rectangle()
                            .fill(colorScheme == .dark ? .white.opacity(0.3) : .black.opacity(0.34))
                            .frame(height: 1 / displayScale)
                    }
                }
        }
    }
}

private struct DialogAction {
    let title: String
    let role: DialogButton.Role
    let action: () -> Void

    var labelWidth: CGFloat {
        let font = UIFont.systemFont(ofSize: Metrics.buttonFontSize, weight: role == .neutral ? .medium : .semibold)
        return (title as NSString).size(withAttributes: [.font: font]).width
    }
}

@available(iOS 17.4, *)
private struct DialogActions: View {
    private let actions: [DialogAction]

    init(_ actions: [DialogAction]) {
        self.actions = actions
    }

    var body: some View {
        ActionsLayout(labelWidths: actions.map(\.labelWidth)) {
            ForEach(actions.indices, id: \.self) { index in
                let action = actions[index]
                DialogButton(title: action.title, role: action.role, action: action.action)
            }
        }
    }
}

// -[WFCompactActionGroupView layoutSubviews] (iOS 27.2 24B5084k)
private struct ActionsLayout: Layout {
    let labelWidths: [CGFloat]

    private func stacks(in width: CGFloat) -> Bool {
        labelWidths.count != 2 || labelWidths.contains { $0 + 4 > width / 2 - 22.5 }
    }

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let width = proposal.width ?? Metrics.padWidth
        let rows = CGFloat(stacks(in: width) ? subviews.count : 1)
        return CGSize(width: width, height: rows * Metrics.buttonHeight + (rows - 1) * Metrics.spacing)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        let stacked = stacks(in: bounds.width)
        let width = stacked ? bounds.width : (bounds.width - Metrics.spacing) / 2
        for (index, subview) in subviews.enumerated() {
            let position = CGFloat(index)
            let origin =
                stacked
                ? CGPoint(x: bounds.minX, y: bounds.minY + position * (Metrics.buttonHeight + Metrics.spacing))
                : CGPoint(x: bounds.minX + position * (width + Metrics.spacing), y: bounds.minY)
            subview.place(at: origin, proposal: ProposedViewSize(width: width, height: Metrics.buttonHeight))
        }
    }
}

private struct DialogButton: View {
    enum Role {
        case neutral
        case prominent
        case destructive
    }

    let title: String
    let role: Role
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            Text(verbatim: title)
                .font(.system(size: Metrics.buttonFontSize, weight: role == .neutral ? .medium : .semibold))
                .lineLimit(1)
        }
        .buttonStyle(DialogButtonStyle(role: role))
    }
}

private struct DialogButtonStyle: ButtonStyle {
    let role: DialogButton.Role
    @Environment(\.colorScheme) private var colorScheme

    private var fill: Color {
        switch (role, colorScheme) {
        case (.neutral, .dark): .white.opacity(0.167)
        case (.neutral, _): .black.opacity(0.083)
        case (.prominent, .dark): Color(red: 0.114, green: 0.682, blue: 1)
        case (.prominent, _): .blue
        case (.destructive, .dark): Color(red: 1, green: 0.392, blue: 0.404)
        case (.destructive, _): .red
        }
    }

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .foregroundStyle(role == .neutral ? AnyShapeStyle(.primary) : AnyShapeStyle(.white))
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(fill, in: .capsule)
            .contentShape(.capsule)
            .opacity(configuration.isPressed ? 0.6 : 1)
    }
}

@available(iOS 17.4, *)
private struct DialogList: View {
    let rows: [String]
    let choose: (Int) -> Void
    @Environment(\.colorScheme) private var colorScheme

    var body: some View {
        VStack(spacing: 0) {
            ForEach(rows.indices, id: \.self) { index in
                Button {
                    choose(index)
                } label: {
                    Text(verbatim: rows[index])
                        .font(.title3)
                        .lineLimit(1)
                        .padding(.bottom, 2.5)
                        .frame(maxWidth: .infinity, minHeight: Metrics.rowHeight, alignment: .leading)
                        .padding(.horizontal, 24)
                        .contentShape(.rect)
                }
                .buttonStyle(.plain)
                .overlay(alignment: .top) {
                    if index > 0 {
                        Rectangle()
                            .fill(.separator)
                            .frame(height: 0.5)
                            .padding(.leading, 24)
                            .padding(.trailing, 10)
                    }
                }
            }
        }
        .background(.white.opacity(colorScheme == .dark ? 0.14 : 0.9), in: .rect(cornerRadius: 26))
    }
}

@available(iOS 17.4, *)
private struct ValueDialog: View {
    let prompt: String?
    let field: DialogRequest.Field
    let answer: (DialogAnswer) -> Void
    let dismiss: () -> Void

    @State private var text = ""
    @State private var count = 0
    @State private var date = Date.now
    @State private var chosen: (title: String, answer: DialogAnswer)?
    @State private var isChoosing = false
    @Environment(\.colorScheme) private var colorScheme

    private var fieldFill: Color {
        .white.opacity(colorScheme == .dark ? 0.07 : 0.5)
    }

    var body: some View {
        switch field {
        case .text(let multiline, let literal):
            input(separated: false, spacing: Metrics.inset) {
                InputField(text: $text, kind: .text(multiline: multiline, literal: literal), placeholder: SystemText.workflowUI("Text")) { submitText() }
                    .frame(height: multiline ? 114 : Metrics.buttonHeight)
                    .overlay(alignment: .trailing) {
                        if !multiline { clearButton }
                    }
                    .background(fieldFill, in: multiline ? AnyShape(.rect(cornerRadius: 26)) : AnyShape(.capsule))
            } done: {
                submitText()
            }
        case .integer(let stepper, let min, let max):
            if stepper {
                input(separated: true, contentTop: 0, spacing: Metrics.inset) {
                    DialogRow {
                        Text(count, format: .number)
                            .monospacedDigit()
                    } accessory: {
                        Stepper(value: $count, in: (min ?? .min)...(max ?? .max)) { EmptyView() }
                            .labelsHidden()
                    }
                    .onAppear { count = Swift.min(Swift.max(0, min ?? .min), max ?? .max) }
                } done: {
                    answer(.number(Double(count)))
                }
            } else {
                numberInput(integer: true, min: min.map(Double.init), max: max.map(Double.init))
            }
        case .number(let min, let max):
            numberInput(integer: false, min: min, max: max)
        case .date:
            input(separated: true, contentTop: 0, spacing: Metrics.inset) {
                DatePicker(selection: $date, displayedComponents: .date) { EmptyView() }
                    .datePickerStyle(.graphical)
                    .labelsHidden()
                    .frame(height: 351)
                    .padding(.horizontal, -1)
            } done: {
                answer(.date(date))
            }
        case .time:
            input(separated: true, contentTop: 0, spacing: Metrics.inset) {
                DatePicker(selection: $date, displayedComponents: .hourAndMinute) { EmptyView() }
                    .datePickerStyle(.wheel)
                    .labelsHidden()
                    .fixedSize(horizontal: true, vertical: false)
                    .frame(maxWidth: .infinity)
            } done: {
                answer(.date(date))
            }
        case .dateTime:
            input(separated: true, contentTop: 0, spacing: Metrics.inset) {
                DatePicker(selection: $date, displayedComponents: [.date, .hourAndMinute]) { EmptyView() }
                    .datePickerStyle(.graphical)
                    .labelsHidden()
                    .padding(.horizontal, -1)
                    .padding(.bottom, -23.7)
            } done: {
                answer(.date(date))
            }
        case .file:
            chooser(SystemText.workflowUI("File"))
                .fileImporter(isPresented: $isChoosing, allowedContentTypes: [.item], allowsMultipleSelection: false) { result in
                    do {
                        guard let url = try result.get().first else { return }
                        let accessing = url.startAccessingSecurityScopedResource()
                        defer {
                            if accessing { url.stopAccessingSecurityScopedResource() }
                        }
                        let data = try Data(contentsOf: url)
                        let type = (try? url.resourceValues(forKeys: [.contentTypeKey]).contentType) ?? UTType(filenameExtension: url.pathExtension)
                        chosen = (url.lastPathComponent, .file(name: url.lastPathComponent, type: type?.identifier ?? "", data: data))
                    } catch CocoaError.userCancelled {
                    } catch {
                        ErrorAlert.shared.report(String(localized: "Can’t Open the File"), error: error)
                    }
                }
        case .person:
            chooser(SystemText.workflowKit("Contact"))
                .background {
                    if isChoosing {
                        ContactPicker { name, handle in
                            chosen = (name, .person(name: name, handle: handle))
                            isChoosing = false
                        } dismiss: {
                            isChoosing = false
                        }
                    }
                }
        case .placemark:
            chooser(SystemText.workflowKit("Location"))
                .sheet(isPresented: $isChoosing) {
                    PlacemarkSearch { title, answer in
                        chosen = (title, answer)
                        isChoosing = false
                    } dismiss: {
                        isChoosing = false
                    }
                }
        }
    }

    private func input(
        separated: Bool, contentTop: CGFloat = Metrics.inset, spacing: CGFloat = Metrics.spacing,
        @ViewBuilder content: () -> some View, done: @escaping () -> Void
    ) -> some View {
        DialogLayout(prompt: prompt, separated: separated, contentTop: contentTop) {
            VStack(spacing: spacing) {
                content()
                DialogActions([
                    DialogAction(title: SystemText.workflowUI("Cancel"), role: .neutral, action: dismiss),
                    DialogAction(title: SystemText.workflowUI("Done"), role: .prominent, action: done),
                ])
            }
        }
    }

    private func chooser(_ label: String) -> some View {
        input(separated: true, contentTop: 0, spacing: Metrics.inset) {
            DialogRow {
                Text(verbatim: label)
            } accessory: {
                Button(chosen?.title ?? SystemText.workflowUI("Choose")) { isChoosing = true }
                    .buttonStyle(.plain)
                    .foregroundStyle(chosen == nil ? AnyShapeStyle(.tint) : AnyShapeStyle(.secondary))
                    .lineLimit(1)
            }
        } done: {
            if let chosen {
                answer(chosen.answer)
            } else {
                isChoosing = true
            }
        }
    }

    private func numberInput(integer: Bool, min: Double?, max: Double?) -> some View {
        let value = Double(text.replacingOccurrences(of: Locale.current.decimalSeparator ?? ".", with: ".")).flatMap { $0.isFinite ? $0 : nil }
        let fits = value.map { $0 >= min ?? -.infinity && $0 <= max ?? .infinity } ?? false
        return input(separated: false, spacing: Metrics.inset) {
            VStack(spacing: 8) {
                InputField(text: $text, kind: .number(integer: integer, negative: min.map { $0 < 0 } ?? true), placeholder: SystemText.workflowKit("Number")) {
                    if fits, let value { answer(.number(value)) }
                }
                .frame(height: 87)
                .overlay(alignment: .trailing) { clearButton }
                .background(fieldFill, in: .rect(cornerRadius: 26))
                if !text.isEmpty, !fits, min != nil || max != nil {
                    Text(verbatim: DialogRequest.rangeHint(min: min, max: max))
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
            }
        } done: {
            if fits, let value { answer(.number(value)) }
        }
    }

    @ViewBuilder
    private var clearButton: some View {
        if !text.isEmpty {
            Button {
                text = ""
            } label: {
                Image(systemName: "xmark.circle.fill")
                    .font(.title3)
                    .foregroundStyle(.tertiary)
                    .frame(width: 23)
                    .frame(maxHeight: .infinity)
                    .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .padding(.trailing, 13)
        }
    }

    private func submitText() {
        answer(.text(text))
    }
}

private struct DialogRow<Label: View, Accessory: View>: View {
    @ViewBuilder let label: Label
    @ViewBuilder let accessory: Accessory

    var body: some View {
        HStack {
            label
            Spacer(minLength: 16)
            accessory
        }
        .padding(.horizontal, 1)
        .frame(height: 52)
    }
}

@available(iOS 17.4, *)
private struct InputField: UIViewRepresentable {
    enum Kind: Equatable {
        case text(multiline: Bool, literal: Bool)
        case number(integer: Bool, negative: Bool)
    }

    @Binding var text: String
    let kind: Kind
    let placeholder: String
    let submit: () -> Void

    func makeCoordinator() -> Coordinator {
        Coordinator(self)
    }

    func makeUIView(context: Context) -> UIView {
        let input: UIView & UITextInput
        switch kind {
        case .text(true, let literal):
            let view = PlaceholderTextView()
            view.placeholder.text = placeholder
            view.font = .preferredFont(forTextStyle: .title3)
            view.backgroundColor = .clear
            view.textContainerInset = UIEdgeInsets(top: 15, left: 18, bottom: 15, right: 18)
            view.delegate = context.coordinator
            if literal {
                view.autocapitalizationType = .none
                view.autocorrectionType = .no
                view.spellCheckingType = .no
                view.smartQuotesType = .no
                view.smartDashesType = .no
                view.smartInsertDeleteType = .no
            }
            input = view
        case .text(false, let literal):
            let field = UITextField()
            field.placeholder = placeholder
            field.font = .preferredFont(forTextStyle: .title3)
            field.returnKeyType = .done
            field.delegate = context.coordinator
            field.addTarget(context.coordinator, action: #selector(Coordinator.changed(_:)), for: .editingChanged)
            if literal {
                field.autocapitalizationType = .none
                field.autocorrectionType = .no
                field.spellCheckingType = .no
                field.smartQuotesType = .no
                field.smartDashesType = .no
                field.smartInsertDeleteType = .no
            }
            input = field
        case .number(let integer, let negative):
            let field = UITextField()
            field.placeholder = placeholder
            field.font = UIFont(
                descriptor: UIFontDescriptor.preferredFontDescriptor(withTextStyle: .largeTitle).withSymbolicTraits(.traitBold) ?? .preferredFontDescriptor(withTextStyle: .largeTitle),
                size: 0)
            field.textAlignment = .center
            field.keyboardType = integer ? .numberPad : .decimalPad
            field.delegate = context.coordinator
            field.addTarget(context.coordinator, action: #selector(Coordinator.changed(_:)), for: .editingChanged)
            if negative {
                field.inputAccessoryView = context.coordinator.signAccessory(for: field)
            }
            input = field
        }
        context.coordinator.input = input
        let container = UIView()
        input.translatesAutoresizingMaskIntoConstraints = false
        container.addSubview(input)
        let (leading, trailing): (CGFloat, CGFloat) =
            switch kind {
            case .text(true, _): (0, 0)
            case .text(false, _): (19, 43)
            case .number: (45, 45)
            }
        NSLayoutConstraint.activate([
            input.leadingAnchor.constraint(equalTo: container.leadingAnchor, constant: leading),
            input.trailingAnchor.constraint(equalTo: container.trailingAnchor, constant: -trailing),
            input.topAnchor.constraint(equalTo: container.topAnchor),
            input.bottomAnchor.constraint(equalTo: container.bottomAnchor),
        ])
        DispatchQueue.main.async { input.becomeFirstResponder() }
        return container
    }

    func updateUIView(_ view: UIView, context: Context) {
        context.coordinator.parent = self
        if let field = context.coordinator.input as? UITextField, field.text != text {
            field.text = text
        }
    }

    final class Coordinator: NSObject, UITextFieldDelegate, UITextViewDelegate {
        var parent: InputField
        weak var input: (UIView & UITextInput)?

        init(_ parent: InputField) {
            self.parent = parent
        }

        @objc func changed(_ field: UITextField) {
            parent.text = field.text ?? ""
        }

        func textViewDidChange(_ view: UITextView) {
            parent.text = view.text
            (view as? PlaceholderTextView)?.placeholder.isHidden = !view.text.isEmpty
        }

        func textFieldShouldReturn(_ field: UITextField) -> Bool {
            parent.submit()
            return false
        }

        func signAccessory(for field: UITextField) -> UIView {
            var configuration: UIButton.Configuration = if #available(iOS 26, *) { .glass() } else { .gray() }
            configuration.image = UIImage(systemName: "plus.forwardslash.minus")
            configuration.cornerStyle = .capsule
            let button = UIButton(
                configuration: configuration,
                primaryAction: UIAction { [weak self, weak field] _ in
                    guard let self, let field else { return }
                    let current = field.text ?? ""
                    field.text = current.hasPrefix("-") ? String(current.dropFirst()) : "-" + current
                    self.changed(field)
                })
            button.translatesAutoresizingMaskIntoConstraints = false
            let bar = UIView(frame: CGRect(x: 0, y: 0, width: 0, height: 60))
            bar.addSubview(button)
            NSLayoutConstraint.activate([
                button.centerXAnchor.constraint(equalTo: bar.centerXAnchor),
                button.centerYAnchor.constraint(equalTo: bar.centerYAnchor),
                button.widthAnchor.constraint(equalToConstant: 135),
                button.heightAnchor.constraint(equalToConstant: 48),
            ])
            return bar
        }
    }
}

private final class PlaceholderTextView: UITextView {
    let placeholder = UILabel()

    override init(frame: CGRect, textContainer: NSTextContainer?) {
        super.init(frame: frame, textContainer: textContainer)
        placeholder.textColor = .placeholderText
        placeholder.font = .preferredFont(forTextStyle: .title3)
        placeholder.translatesAutoresizingMaskIntoConstraints = false
        addSubview(placeholder)
        NSLayoutConstraint.activate([
            placeholder.leadingAnchor.constraint(equalTo: leadingAnchor, constant: 22),
            placeholder.topAnchor.constraint(equalTo: topAnchor, constant: 15),
        ])
    }

    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not supported")
    }
}

private struct ContactPicker: UIViewControllerRepresentable {
    let choose: (String, (type: String, value: String)?) -> Void
    let dismiss: () -> Void

    func makeCoordinator() -> Coordinator {
        Coordinator(self)
    }

    func makeUIViewController(context: Context) -> UIViewController {
        let picker = CNContactPickerViewController()
        picker.delegate = context.coordinator
        return Host(picker: picker)
    }

    func updateUIViewController(_ controller: UIViewController, context: Context) {}

    private final class Host: UIViewController {
        private var picker: CNContactPickerViewController?

        init(picker: CNContactPickerViewController) {
            self.picker = picker
            super.init(nibName: nil, bundle: nil)
        }

        required init?(coder: NSCoder) {
            fatalError("init(coder:) is not supported")
        }

        override func viewDidAppear(_ animated: Bool) {
            super.viewDidAppear(animated)
            if let picker {
                self.picker = nil
                present(picker, animated: true)
            }
        }
    }

    final class Coordinator: NSObject, CNContactPickerDelegate {
        let parent: ContactPicker

        init(_ parent: ContactPicker) {
            self.parent = parent
        }

        func contactPicker(_ picker: CNContactPickerViewController, didSelect contact: CNContact) {
            let name = CNContactFormatter.string(from: contact, style: .fullName) ?? ""
            let handle: (type: String, value: String)? =
                if let phone = contact.phoneNumbers.first {
                    ("phoneNumber", phone.value.stringValue)
                } else if let email = contact.emailAddresses.first {
                    ("emailAddress", email.value as String)
                } else {
                    nil
                }
            parent.choose(name, handle)
        }

        func contactPickerDidCancel(_ picker: CNContactPickerViewController) {
            parent.dismiss()
        }
    }
}

@available(iOS 17.4, *)
private struct PlacemarkSearch: View {
    let choose: (String, DialogAnswer) -> Void
    let dismiss: () -> Void
    @State private var query = ""
    @State private var completer = PlacemarkCompleter()
    @State private var selection: MKLocalSearchCompletion?

    var body: some View {
        WithPerceptionTracking {
            NavigationStack {
                List(completer.results, id: \.self) { result in
                    Button {
                        selection = result
                    } label: {
                        HStack {
                            VStack(alignment: .leading, spacing: 2) {
                                Text(verbatim: result.title)
                                Text(verbatim: result.subtitle)
                                    .font(.subheadline)
                                    .foregroundStyle(.secondary)
                            }
                            .lineLimit(1)
                            Spacer()
                            if selection == result {
                                Image(systemName: "checkmark")
                                    .foregroundStyle(.tint)
                            }
                        }
                        .contentShape(.rect)
                    }
                    .buttonStyle(.plain)
                }
                .listStyle(.plain)
                .navigationTitle(Text(verbatim: SystemText.workflowKit("Location")))
                .navigationBarTitleDisplayMode(.inline)
                .searchable(text: $query, prompt: Text(verbatim: SystemText.workflowEditor("Search or Enter Address")))
                .onChange(of: query) { completer.search(query) }
                .toolbar {
                    ToolbarItem(placement: .cancellationAction) {
                        if #available(iOS 26, *) {
                            Button(role: .cancel, action: dismiss)
                        } else {
                            Button(SystemText.workflowUI("Cancel"), role: .cancel, action: dismiss)
                        }
                    }
                    ToolbarItem(placement: .confirmationAction) {
                        if #available(iOS 26, *) {
                            Button(role: .confirm, action: done)
                        } else {
                            Button(SystemText.workflowUI("Done"), action: done)
                        }
                    }
                }
            }
        }
    }

    private func done() {
        if let selection { confirm(selection) }
    }

    private func confirm(_ completion: MKLocalSearchCompletion) {
        Task {
            var item: MKMapItem?
            do {
                item = try await MKLocalSearch(request: MKLocalSearch.Request(completion: completion)).start().mapItems.first
            } catch {
                AppDirectory.logger.error("look up the place: \(error, privacy: .public)")
            }
            let name = item?.name ?? completion.title
            choose(name, .placemark(name: name, coordinate: item?.coordinateCompat, address: item?.fullAddressCompat))
        }
    }
}

extension MKMapItem {
    var coordinateCompat: CLLocationCoordinate2D {
        if #available(iOS 26, *) { location.coordinate } else { placemark.coordinate }
    }

    var fullAddressCompat: String? {
        if #available(iOS 26, *) { address?.fullAddress } else { placemark.title }
    }
}

@Perceptible
private final class PlacemarkCompleter: NSObject, MKLocalSearchCompleterDelegate {
    private(set) var results: [MKLocalSearchCompletion] = []
    @PerceptionIgnored private let completer = MKLocalSearchCompleter()

    override init() {
        super.init()
        completer.delegate = self
    }

    func search(_ query: String) {
        if query.isEmpty {
            results = []
        } else {
            completer.queryFragment = query
        }
    }

    func completerDidUpdateResults(_ completer: MKLocalSearchCompleter) {
        results = completer.results
    }
}
