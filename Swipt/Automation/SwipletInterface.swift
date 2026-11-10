internal import Expo
internal import ExpoObjC
import LibSwipt
import MapKit
import Perception
import RCTBlob
import React
import ReactAppDependencyProvider
import SwiftUI
internal import SwiptHost
import os

@available(iOS 17.4, *)
struct SwipletInterface: View {
    let name: String
    let entry: SwipletEntry

    private let swiplets = SwipletStore.shared

    var body: some View {
        WithPerceptionTracking {
            SwipletSurfaceView(name: name, entry: swiplets.interfaces[name] ?? entry)
                .ignoresSafeArea()
        }
    }
}

@available(iOS 17.4, *)
private struct SwipletSurfaceView: UIViewControllerRepresentable {
    let name: String
    let entry: SwipletEntry

    func makeUIViewController(context: Context) -> SwipletViewController {
        SwipletViewController(name: name, entry: entry)
    }

    func updateUIViewController(_ controller: SwipletViewController, context: Context) {
        controller.show(entry)
    }

    static func dismantleUIViewController(_ controller: SwipletViewController, coordinator: ()) {
        controller.stop()
    }
}

@available(iOS 17.4, *)
private final class SwipletViewController: UIViewController {
    private let name: String
    private var displayed: SwipletSurface
    private var loading: SwipletSurface?

    init(name: String, entry: SwipletEntry) {
        self.name = name
        displayed = SwipletHost.shared.take(name, entry: entry)
        super.init(nibName: nil, bundle: nil)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError()
    }

    override func viewDidLoad() {
        super.viewDidLoad()
        attach(displayed)
    }

    func stop() {
        displayed.stop()
        loading?.stop()
    }

    private func attach(_ surface: SwipletSurface) {
        surface.view.frame = view.bounds
        surface.view.autoresizingMask = [.flexibleWidth, .flexibleHeight]
        view.addSubview(surface.view)
        surface.whenFailed { [name] message in
            let title = SwipletStore.shared.titles[name] ?? name
            ErrorAlert.shared.report(AlertState(String(localized: "Couldn’t Open “\(title)”"), errorMessage: message) {
                SwipletHost.shared.exit(swiplet: name)
            })
        }
    }

    func show(_ entry: SwipletEntry) {
        guard entry != (loading ?? displayed).entry else { return }
        loading?.stop()
        let next = SwipletHost.shared.makeSurface(name, entry: entry)
        loading = next
        next.whenMounted { [weak self, weak next] in
            guard let self, let next, loading === next else { return }
            loading = nil
            attach(next)
            displayed.view.removeFromSuperview()
            displayed.stop()
            displayed = next
        }
    }
}

@available(iOS 17.4, *)
private final class SwipletRuntime: SwiptHostDelegate {
    nonisolated let swiplet: String
    let entry: SwipletEntry
    // The factory holds its delegate weakly.
    private let delegate: ExpoReactNativeFactoryDelegate
    let factory: ExpoReactNativeFactory
    let surfaces = NSMapTable<NSString, SwipletSurface>.strongToWeakObjects()
    nonisolated(unsafe) private var modules: RCTModuleRegistry?
    var attached = 0
    var broken = false
    private var terminated = false
    private var dialogs: [String: Task<DialogAnswer, any Error>] = [:]
    private var calls: [String: Task<String, any Error>] = [:]

    init(swiplet: String, entry: SwipletEntry, delegate: ExpoReactNativeFactoryDelegate) {
        self.swiplet = swiplet
        self.entry = entry
        self.delegate = delegate
        factory = ExpoReactNativeFactory(delegate: delegate)
        factory.appContextHandler = { [weak self] appContext in
            SwipletSandbox.replaceModules(in: appContext)
            guard let self else { return }
            SwiptHostModule.bind(appContext, to: self)
        }
        // defaultConfiguration returns nil when React Native is built without the dev menu.
        factory.rootViewFactory.initializeReactHost(
            launchOptions: nil, bundleConfiguration: factory.bundleConfiguration, devMenuConfiguration: RCTDevMenuConfiguration.default() ?? RCTDevMenuConfiguration())
        modules = factory.rootViewFactory.reactHost?.value(forKey: "moduleRegistry") as? RCTModuleRegistry
    }

    func terminate() {
        guard !terminated else { return }
        terminated = true
        for dialog in dialogs.values {
            dialog.cancel()
        }
        SwiptHostModule.unbind(self)
        factory.terminateJavaScript()
        factory.rootViewFactory.reactHost = nil
    }

    func call(_ id: String, action: String, input: String) async throws -> String {
        let call = Task { try await Platform.shared.callAction(swiplet, action: action, input: input, caller: LibSwiptCallerApp, dialogs: .app) }
        calls[id] = call
        defer { calls[id] = nil }
        return try await call.value
    }

    func cancelCall(_ id: String) {
        calls[id]?.cancel()
    }

    func setServiceEnabled(_ enabled: Bool) async throws {
        try await SwipletStore.shared.setServiceEnabled(swiplet, enabled)
    }

    func searchPlaces(_ query: String) async throws -> String {
        guard !query.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { return "[]" }
        let request = MKLocalSearch.Request()
        request.naturalLanguageQuery = query
        let items: [MKMapItem]
        do {
            items = try await MKLocalSearch(request: request).start().mapItems
        } catch MKError.placemarkNotFound {
            items = []
        }
        let places = items.map { item -> [String: Any] in
            let coordinate = item.coordinateCompat
            var place: [String: Any] = ["coordinate": [coordinate.latitude, coordinate.longitude]]
            place["name"] = item.name
            place["address"] = item.fullAddressCompat
            return place
        }
        return String(decoding: try JSONSerialization.data(withJSONObject: places), as: UTF8.self)
    }

    func presentDialog(_ id: String, request: String) async throws -> String? {
        let decoded = try JSONDecoder().decode(DialogRequest.self, from: Data(request.utf8))
        let dialog = Task { try await ScriptDialogs.shared.present(decoded) }
        dialogs[id] = dialog
        defer { dialogs[id] = nil }
        return try? await dialog.value.json
    }

    func withdrawDialog(_ id: String) {
        dialogs[id]?.cancel()
    }

    func exit() {
        SwipletHost.shared.exit(swiplet: swiplet)
    }

    func log(level: String, message: String) {
        let level = switch level {
        case "error": LibSwiptLogLevelError
        case "warn": LibSwiptLogLevelWarn
        case "debug": LibSwiptLogLevelDebug
        default: LibSwiptLogLevelInfo
        }
        Platform.shared.logSwiplet(swiplet, level: level, message: message)
    }

    func report(message: String) {
        let title = String(localized: "“\(SwipletStore.shared.titles[swiplet] ?? swiplet)” Error")
        ErrorAlert.shared.report(AlertState(title, errorMessage: message), swiplet: swiplet)
    }

    nonisolated func storeBlob(_ data: Data, id: String) {
        (modules?.module(for: RCTBlobManager.self) as? RCTBlobManager)?.store(data, withId: id)
    }

    nonisolated func removeBlob(_ id: String) {
        (modules?.module(for: RCTBlobManager.self) as? RCTBlobManager)?.remove(id)
    }

    func surfaceDidMount(_ surface: String) {
        surfaces.object(forKey: surface as NSString)?.didMount()
    }

    func fail(surface: String, message: String) {
        surfaces.object(forKey: surface as NSString)?.fail(message)
    }
}

@available(iOS 17.4, *)
final class SwipletSurface {
    let view: RCTSurfaceHostingProxyRootView
    fileprivate let runtime: SwipletRuntime
    private var stopped = false
    private var mounted = false
    private var pending: [() -> Void] = []
    private var failure: String?
    private var onFailure: ((String) -> Void)?

    fileprivate init(view: RCTSurfaceHostingProxyRootView, runtime: SwipletRuntime) {
        self.view = view
        self.runtime = runtime
    }

    var entry: SwipletEntry {
        runtime.entry
    }

    func whenMounted(_ action: @escaping () -> Void) {
        if mounted {
            action()
        } else {
            pending.append(action)
        }
    }

    fileprivate func didMount() {
        mounted = true
        let actions = pending
        pending = []
        actions.forEach { $0() }
    }

    func whenFailed(_ action: @escaping (String) -> Void) {
        if let failure {
            action(failure)
        } else {
            onFailure = action
        }
    }

    fileprivate func fail(_ message: String) {
        runtime.broken = true
        guard failure == nil else { return }
        failure = message
        onFailure?(message)
    }

    func stop() {
        guard !stopped else { return }
        stopped = true
        view.surface.stop()
        SwipletHost.shared.detach(runtime)
    }
}

@available(iOS 17.4, *)
final class SwipletHost {
    static let shared = SwipletHost()

    private final class FactoryDelegate: ExpoReactNativeFactoryDelegate {
        private let files: SwipletFiles

        init(files: SwipletFiles) {
            self.files = files
            super.init()
            dependencyProvider = RCTAppDependencyProvider()
        }

        override func bundleURL() -> URL? {
            Bundle.main.url(forResource: "main", withExtension: "jsbundle")
        }

        // RCTTurboModuleManagerDelegate, which Swift doesn't see; nil makes the default instance.
        @objc(getModuleInstanceFromClass:)
        nonisolated func moduleInstance(from moduleClass: AnyClass) -> AnyObject? {
            switch ObjectIdentifier(moduleClass) {
            case ObjectIdentifier(RCTNetworking.self): SwipletNetworking.make(files: files)
            case ObjectIdentifier(RCTActionSheetManager.self): SwipletActionSheetManager(files: files)
            default: nil
            }
        }
    }

    private static let preparedLimit = 2

    private var runtimes: [String: SwipletRuntime] = [:]
    private var interfaces: [String: SwipletEntry] = [:]
    private var recent: [String] = []
    private var prepared: [String: SwipletSurface] = [:]
    private var presented: [(name: String, exit: () -> Void)] = []

    private init() {
        SwipletSandbox.ignoreReloads()
        NotificationCenter.default.addObserver(forName: UIApplication.didReceiveMemoryWarningNotification, object: nil, queue: .main) { _ in
            MainActor.assumeIsolated {
                SwipletHost.shared.releasePrepared()
            }
        }
    }

    func prepare(_ interfaces: [String: SwipletEntry]) {
        self.interfaces = interfaces
        let wanted = recent.filter { interfaces[$0] != nil && !isPresented($0) }.suffix(Self.preparedLimit)
        for (name, surface) in prepared where !wanted.contains(name) && !isPresented(name) || surface.entry != interfaces[name] {
            surface.stop()
            prepared[name] = nil
        }
        for name in wanted where prepared[name] == nil {
            prepared[name] = makeSurface(name, entry: interfaces[name]!)
        }
    }

    private func releasePrepared() {
        for surface in prepared.values {
            surface.stop()
        }
        prepared = [:]
    }

    fileprivate func detach(_ runtime: SwipletRuntime) {
        runtime.attached -= 1
        guard runtime.attached == 0 else { return }
        if runtimes[runtime.swiplet] === runtime {
            runtimes[runtime.swiplet] = nil
        }
        runtime.terminate()
    }

    func take(_ name: String, entry: SwipletEntry) -> SwipletSurface {
        if let surface = prepared.removeValue(forKey: name) {
            if surface.entry == entry {
                return surface
            }
            surface.stop()
        }
        return makeSurface(name, entry: entry)
    }

    private func runtime(_ name: String, entry: SwipletEntry) -> SwipletRuntime {
        if let runtime = runtimes[name], runtime.entry == entry, !runtime.broken {
            return runtime
        }
        let files = SwipletFiles(
            roots: [SwipletStore.shared.dataDirectory(of: name), SwipletStore.shared.assetsDirectory(of: name), SwipletStore.cacheDirectory(of: name)].compactMap(\.self))
        let runtime = SwipletRuntime(swiplet: name, entry: entry, delegate: FactoryDelegate(files: files))
        runtimes[name] = runtime
        return runtime
    }

    func makeSurface(_ name: String, entry: SwipletEntry) -> SwipletSurface {
        let service = SwipletStore.shared.service(of: name)
        let id = UUID().uuidString
        let runtime = runtime(name, entry: entry)
        runtime.attached += 1
        var source = ""
        do {
            source = try String(contentsOf: entry.url, encoding: .utf8)
        } catch {
            AppDirectory.logger.error("read the interface of \(name, privacy: .public): \(error, privacy: .public)")
        }
        let view = runtime.factory.rootViewFactory.view(
            withModuleName: "main",
            initialProperties: [
                "surface": id,
                "name": name,
                "file": entry.url.lastPathComponent,
                "source": source,
                "dataDirectory": SwipletStore.shared.dataDirectory(of: name)?.absoluteString ?? "",
                "assetsDirectory": SwipletStore.shared.assetsDirectory(of: name)?.absoluteString ?? "",
                "cacheDirectory": SwipletStore.cacheDirectory(of: name).absoluteString,
                "service": [
                    "enabled": service.enabled, "status": service.status.rawValue, "error": service.error,
                    "since": service.since.map { $0.timeIntervalSince1970 * 1000 } ?? 0,
                ] as [String: Any],
            ]
        ) as! RCTSurfaceHostingProxyRootView
        let windowBounds = UIApplication.shared.connectedScenes.lazy.compactMap { ($0 as? UIWindowScene)?.keyWindow?.bounds }.first
        view.frame = windowBounds ?? .zero
        view.layoutIfNeeded()
        let surface = SwipletSurface(view: view, runtime: runtime)
        runtime.surfaces.setObject(surface, forKey: id as NSString)
        return surface
    }

    func didPresent(_ name: String, exit: @escaping () -> Void) {
        presented.removeAll { $0.name == name }
        presented.append((name, exit))
        recent.removeAll { $0 == name }
        recent.append(name)
        prepare(interfaces)
    }

    func didDismiss(_ name: String) {
        presented.removeAll { $0.name == name }
        prepare(interfaces)
    }

    func exit(swiplet: String) {
        presented.last { $0.name == swiplet }?.exit()
    }

    func isPresented(_ name: String) -> Bool {
        presented.contains { $0.name == name }
    }
}
