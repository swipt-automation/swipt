import Foundation
import LibSwipt
import Perception
import UIKit
internal import SwiptHost
import UniformTypeIdentifiers
import os

extension UTType {
    nonisolated static let swiplet = UTType(exportedAs: "io.nekohasekai.swipt.swiplet", conformingTo: .zip)
}

@Perceptible
final class SwipletStore {
    static let shared = SwipletStore()
    nonisolated static let directory = AppDirectory.support
        .appendingPathComponent("Swiplets", isDirectory: true)
    nonisolated static let caches = AppDirectory.caches.appendingPathComponent("Swiplets", isDirectory: true)

    private(set) var ids: [String] = []
    private(set) var directories: [String: URL] = [:]
    private(set) var icons: [String: SwipletIcon] = [:]
    private(set) var titles: [String: String] = [:]
    private(set) var versions: [String: String] = [:]
    private(set) var interfaces: [String: SwipletEntry] = [:]
    private(set) var actions: [String: [SwipletAction]] = [:]
    private(set) var withServices: Set<String> = []
    private(set) var services: [String: SwipletService] = [:]
    private(set) var outdated: Set<String> = []
    private(set) var updating: Set<String> = []
    private(set) var downloadProgress: [String: SwipletDownloadProgress] = [:]
    private(set) var repositories: [Repository] = []
    private(set) var repositoryURLs: [String: String] = [:]
    private var manifests: [String: LibSwiptSwipletManifest] = [:]
    @PerceptionIgnored private var repositoriesChecked: [String: Date] = [:]

    private init() {
        var stored: [Repository] = []
        do {
            stored = try SwipletDatabase.repositories()
        } catch {
            AppDirectory.logger.error("load repositories: \(error, privacy: .public)")
        }
        for repository in stored where repository.isOfficial && repository.url != Repository.official.url {
            removeRepository(repository.url)
        }
        repositories = [stored.first { $0.url == Repository.official.url } ?? .official] + stored.filter { !$0.isOfficial }
    }

    func reload() {
        let swiplets: [LibSwiptSwipletInfo]
        do {
            swiplets = try Platform.shared.swiplets()
        } catch {
            ErrorAlert.shared.report(String(localized: "Couldn’t Load the Swiplets"), error: error)
            return
        }
        manifests = Dictionary(uniqueKeysWithValues: swiplets.compactMap { swiplet in swiplet.manifest.map { (swiplet.id_, $0) } })
        directories = Dictionary(uniqueKeysWithValues: swiplets.map { ($0.id_, URL(fileURLWithPath: $0.directory, isDirectory: true)) })
        titles = manifests.mapValues { manifest in
            let title = Self.preferredName(manifest)
            return title.isEmpty ? manifest.id_ : title
        }
        versions = manifests.mapValues(\.version)
        outdated = Set(swiplets.filter(\.needsUpdate).map(\.id_))
        do {
            let stored = try SwipletDatabase.swiplets()
            let storedIDs = stored.map(\.id)
            let added = swiplets.map(\.id_).filter { !storedIDs.contains($0) }
            ids = storedIDs.filter { directories[$0] != nil } + added.sorted { (titles[$0] ?? $0).localizedStandardCompare(titles[$1] ?? $1) == .orderedAscending }
            if ids != storedIDs {
                try SwipletDatabase.setSwipletIDs(ids)
            }
            repositoryURLs = Dictionary(uniqueKeysWithValues: stored.filter { directories[$0.id] != nil && !$0.repository.isEmpty }.map { ($0.id, $0.repository) })
        } catch {
            AppDirectory.logger.error("load the swiplet order: \(error, privacy: .public)")
        }
        icons = directories.mapValues { SwipletIcon.load(from: $0) }
        interfaces = directories.filter { !outdated.contains($0.key) }.compactMapValues { directory in
            let entry = directory
                .appendingPathComponent(LibSwiptSwipletSourceFolder, isDirectory: true)
                .appendingPathComponent(LibSwiptSwipletInterfaceFile)
            guard let modified = try? entry.resourceValues(forKeys: [.contentModificationDateKey]).contentModificationDate else { return nil }
            return SwipletEntry(url: entry, modified: modified)
        }
        actions = manifests.mapValues { manifest in
            (manifest.actions()?.toArray() ?? []).map { action in
                SwipletAction(
                    name: action.name, title: action.title, description: action.summary, shortcuts: action.exposed(LibSwiptCallerShortcuts))
            }
        }
        withServices = Set(manifests.filter { $0.value.service && !outdated.contains($0.key) }.keys)
        services = Dictionary(uniqueKeysWithValues: withServices.map { ($0, Platform.shared.serviceState(of: $0)) })
    }

    func reorder(_ ids: [String]) {
        self.ids = ids
        do {
            try SwipletDatabase.setSwipletIDs(ids)
        } catch {
            AppDirectory.logger.error("save the swiplet order: \(error, privacy: .public)")
        }
    }

    var hasEnabledServices: Bool {
        services.values.contains(where: \.enabled)
    }

    func service(of id: String) -> SwipletService {
        services[id] ?? SwipletService()
    }

    func dataDirectory(of id: String) -> URL? {
        directories[id]?.appendingPathComponent(LibSwiptSwipletDataFolder, isDirectory: true)
    }

    func assetsDirectory(of id: String) -> URL? {
        directories[id]?.appendingPathComponent(LibSwiptSwipletAssetsFolder, isDirectory: true)
    }

    nonisolated static func cacheDirectory(of id: String) -> URL {
        caches.appendingPathComponent(id, isDirectory: true)
    }

    func clearData(of id: String) async throws {
        try await Platform.shared.clearSwipletData(id)
    }

    func serviceChanged(_ id: String, to service: SwipletService) {
        services[id] = service
        SwiptHostModule.send("service", to: id, [
            "enabled": service.enabled, "status": service.status.rawValue, "error": service.error,
            "since": service.since.map { $0.timeIntervalSince1970 * 1000 } ?? 0,
        ])
    }

    func setServiceEnabled(_ id: String, _ enabled: Bool) async throws {
        try await Platform.shared.setServiceEnabled(id, enabled)
    }

    @available(iOS 17.4, *)
    func open(_ picked: URL) async throws -> SwipletPreview {
        let file = try Self.temporaryFile()
        do {
            try await Task.detached(priority: .userInitiated) {
                try picked.withPickedFile { try FileManager.default.copyItem(at: $0, to: file) }
            }.value
            return try Self.preview(of: file)
        } catch {
            Self.discard(file)
            throw error
        }
    }

    @available(iOS 17.4, *)
    func open(downloadingFrom link: String) async throws -> SwipletPreview {
        let download = try await Self.download(link.trimmingCharacters(in: .whitespacesAndNewlines))
        do {
            return try Self.preview(of: download.file)
        } catch {
            Self.discard(download.file)
            throw error
        }
    }

    @available(iOS 17.4, *)
    func importSwiplet(_ preview: SwipletPreview) async throws {
        defer { reload() }
        let id = try await Platform.shared.installSwiplet(preview.file)
        try SwipletDatabase.setRepository("", of: id)
        Self.discard(preview.file)
    }

    func discard(_ preview: SwipletPreview) {
        Self.discard(preview.file)
    }

    func repository(_ url: String) -> Repository? {
        repositories.first { $0.url == url }
    }

    var updates: [SwipletUpdate] {
        ids.compactMap(update(of:))
    }

    func update(of id: String) -> SwipletUpdate? {
        guard let url = repositoryURLs[id], let repository = repository(url), let swiplet = repository.swiplets.first(where: { $0.id == id }),
            let local = manifests[id]
        else { return nil }
        let isNewer = swiplet.versionCode > 0 ? swiplet.versionCode > local.versionCode : !swiplet.version.isEmpty && swiplet.version != local.version
        guard isNewer, Platform.shared.isSwipletCompatible(swiplet) else { return nil }
        return SwipletUpdate(repository: repository, swiplet: swiplet)
    }

    @available(iOS 17.4, *)
    func addRepository(_ link: String) async throws {
        let link = link.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !repositories.contains(where: { $0.url == link }), !Repository.isOfficial(link) else {
            throw MessageError(String(localized: "The repository is already added."))
        }
        guard let repository = try await Self.fetchRepository(link, etag: "") else { return }
        try SwipletDatabase.save(repository)
        repositories.append(repository)
        repositoriesChecked[link] = .now
    }

    func removeRepository(_ url: String) {
        repositories.removeAll { $0.url == url }
        repositoriesChecked[url] = nil
        do {
            try SwipletDatabase.removeRepository(url)
        } catch {
            AppDirectory.logger.error("remove repository \(url, privacy: .public): \(error, privacy: .public)")
        }
    }

    @available(iOS 17.4, *)
    func refreshRepository(_ url: String, force: Bool) async throws {
        guard let stored = repository(url) else { return }
        if !force, let last = repositoriesChecked[url], Date.now.timeIntervalSince(last) < Self.updateInterval {
            return
        }
        let fetched = try await Self.fetchRepository(url, etag: stored.etag)
        repositoriesChecked[url] = .now
        guard let fetched, let index = repositories.firstIndex(where: { $0.url == url }) else { return }
        try SwipletDatabase.save(fetched)
        repositories[index] = fetched
    }

    @available(iOS 17.4, *)
    func install(_ swiplet: RepositorySwiplet, from repository: Repository) async throws {
        updating.insert(swiplet.id)
        defer {
            updating.remove(swiplet.id)
            reload()
        }
        let download = try await self.download(swiplet.id, from: swiplet.url, checksum: swiplet.checksum)
        defer { Self.discard(download.file) }
        guard download.id == swiplet.id else { throw MessageError(String(localized: "The repository sent a different swiplet.")) }
        let id = try await Platform.shared.installSwiplet(download.file)
        try SwipletDatabase.setRepository(repository.url, of: id)
    }

    @available(iOS 17.4, *)
    func refreshRepositories() async {
        for repository in repositories {
            try? await refreshRepository(repository.url, force: false)
        }
    }

    @available(iOS 17.4, *)
    func applyUpdates(_ ids: [String]) async throws {
        let pending = ids.filter { !updating.contains($0) }.compactMap(update(of:))
        updating.formUnion(pending.map(\.swiplet.id))
        var failure: (any Error)?
        for update in pending {
            do {
                try await install(update.swiplet, from: update.repository)
            } catch {
                failure = failure ?? error
            }
        }
        if let failure {
            throw failure
        }
    }

    @available(iOS 17.4, *)
    private func download(_ id: String, from link: String, checksum: String) async throws -> SwipletDownload {
        let (progress, continuation) = AsyncStream.makeStream(of: SwipletDownloadProgress.self, bufferingPolicy: .bufferingNewest(1))
        async let download = Self.download(link, checksum: checksum, progress: continuation)
        for await received in progress {
            downloadProgress[id] = received
        }
        downloadProgress[id] = nil
        return try await download
    }

    nonisolated static let updateInterval: TimeInterval = 60 * 60

    nonisolated private static func fetchRepository(_ url: String, etag: String) async throws -> Repository? {
        let downloader = UncheckedSendable(value: LibSwiptNewSwipletDownloader()!)
        return try await withTaskCancellationHandler {
            try await blocking {
                let fetched = try downloader.value.fetchRepository(url, etag: etag)
                guard !fetched.notModified else { return nil }
                return Repository(
                    url: url, name: fetched.name, localizedNames: fetched.localizedNames()?.toDictionary() ?? [:],
                    icon: fetched.icon.map { SwipletIcon(color: $0.color.isEmpty ? nil : $0.color, symbol: $0.symbol) }, image: fetched.image,
                    etag: fetched.entityTag,
                    swiplets: (fetched.swiplets()?.toArray() ?? []).map { swiplet in
                        RepositorySwiplet(
                            id: swiplet.id_, name: swiplet.name, localizedNames: swiplet.localizedNames()?.toDictionary() ?? [:],
                            description: swiplet.summary, version: swiplet.version, versionCode: swiplet.versionCode,
                            minimumAPILevel: swiplet.minimumAPILevel, minimumIOSVersion: swiplet.minimumIOSVersion,
                            icon: SwipletIcon(color: swiplet.color, symbol: swiplet.symbol), url: swiplet.url, checksum: swiplet.checksum)
                    })
            }
        } onCancel: {
            downloader.value.cancel()
        }
    }

    nonisolated private static func download(
        _ link: String, checksum: String = "", progress: AsyncStream<SwipletDownloadProgress>.Continuation? = nil
    ) async throws -> SwipletDownload {
        defer { progress?.finish() }
        let file = try temporaryFile()
        let downloader = UncheckedSendable(value: LibSwiptNewSwipletDownloader()!)
        let handler = SwipletDownloadEvents(progress)
        do {
            return try await withTaskCancellationHandler {
                try await blocking {
                    var error: NSError?
                    let id = downloader.value.download(link, destination: file.path, checksum: checksum, handler: handler, error: &error)
                    if let error { throw error }
                    return SwipletDownload(file: file, id: id)
                }
            } onCancel: {
                downloader.value.cancel()
            }
        } catch {
            discard(file)
            throw error
        }
    }

    nonisolated private static func preview(of file: URL) throws -> SwipletPreview {
        var error: NSError?
        let manifest = LibSwiptReadSwipletManifest(file.path, &error)
        if let error { throw error }
        guard let manifest else { throw MessageError(String(localized: "The file is not a swiplet.")) }
        let title = preferredName(manifest)
        return SwipletPreview(
            file: file, id: manifest.id_, title: title.isEmpty ? manifest.id_ : title, version: manifest.version,
            icon: SwipletIcon(color: manifest.color, symbol: manifest.symbol))
    }

    nonisolated private static func discard(_ file: URL) {
        try? FileManager.default.removeItem(at: file.deletingLastPathComponent())
    }

    nonisolated private static func temporaryFile() throws -> URL {
        let folder = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString, isDirectory: true)
        try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        return folder.appendingPathComponent("swiplet")
    }

    @available(iOS 17.4, *)
    func export(_ id: String) async throws -> URL {
        let folder = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString, isDirectory: true)
        try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        let file = folder.appendingPathComponent(id).appendingPathExtension(for: .swiplet)
        try await Platform.shared.exportSwiplet(id, to: file)
        return file
    }

    nonisolated static func preferredName(_ name: String, localized: [String: String]) -> String {
        let languages = ["en"] + localized.keys.filter { $0 != "en" }.sorted()
        let preferred = Bundle.preferredLocalizations(from: languages, forPreferences: Locale.preferredLanguages).first ?? "en"
        return localized[preferred] ?? name
    }

    nonisolated private static func preferredName(_ manifest: LibSwiptSwipletManifest) -> String {
        preferredName(manifest.name, localized: manifest.localizedNames()?.toDictionary() ?? [:])
    }

    func remove(_ id: String) async throws {
        defer { reload() }
        try await Platform.shared.removeSwiplet(id)
    }
}

nonisolated struct SwipletEntry: Hashable, Sendable {
    let url: URL
    let modified: Date
}

nonisolated struct SwipletAction: Hashable, Sendable {
    let name: String
    let title: String
    let description: String
    let shortcuts: Bool
}

nonisolated struct SwipletUpdate: Sendable {
    let repository: Repository
    let swiplet: RepositorySwiplet
}

nonisolated struct SwipletDownloadProgress: Equatable, Sendable {
    let received: Int64
    let total: Int64
}

nonisolated struct SwipletPreview: Identifiable, Sendable {
    let file: URL
    let id: String
    let title: String
    let version: String
    let icon: SwipletIcon
}

nonisolated struct SwiptOutdatedError: LocalizedError, RecoverableError {
    var errorDescription: String? {
        String(localized: "This swiplet requires a newer version of Swipt.")
    }

    var recoveryOptions: [String] {
        [String(localized: "Learn More")]
    }

    func attemptRecovery(optionIndex: Int) -> Bool {
        Task { @MainActor in
            _ = await UIApplication.shared.open(URL(string: LibSwiptUpdateGuide)!)
        }
        return true
    }
}

nonisolated struct IOSOutdatedError: LocalizedError {
    let version: String
    let isPad: Bool

    var errorDescription: String? {
        isPad
            ? String(localized: "This swiplet requires iPadOS \(version) or later.")
            : String(localized: "This swiplet requires iOS \(version) or later.")
    }
}

nonisolated struct Repository: Identifiable, Sendable {
    private static let stableURL = "https://github.com/swipt-automation/official-swiplets/releases/latest/download/swiplets.json"
    private static let nightlyURL = "https://github.com/swipt-automation/official-swiplets/releases/download/nightly/swiplets.json"

    static let official = Repository(
        url: Bundle.main.object(forInfoDictionaryKey: "SwiptChannel") as? String == "stable" ? stableURL : nightlyURL,
        name: String(localized: "Official Swiplets"), localizedNames: [:], icon: SwipletIcon(color: nil, symbol: "checkmark.seal.fill"),
        image: nil, etag: "", swiplets: [])

    let url: String
    let name: String
    let localizedNames: [String: String]
    let icon: SwipletIcon?
    let image: Data?
    let etag: String
    let swiplets: [RepositorySwiplet]

    var id: String { url }

    var isOfficial: Bool { Self.isOfficial(url) }

    static func isOfficial(_ url: String) -> Bool {
        url == stableURL || url == nightlyURL
    }

    var title: String {
        SwipletStore.preferredName(name, localized: localizedNames)
    }

    var swipletIcon: SwipletIcon {
        var shown = icon ?? .default
        shown.image = image.flatMap(UIImage.init(data:))
        return shown
    }
}

nonisolated struct RepositorySwiplet: Identifiable, Sendable {
    let id: String
    let name: String
    let localizedNames: [String: String]
    let description: String
    let version: String
    let versionCode: Int64
    let minimumAPILevel: Int64
    let minimumIOSVersion: String
    let icon: SwipletIcon
    let url: String
    let checksum: String

    var title: String {
        let title = SwipletStore.preferredName(name, localized: localizedNames)
        return title.isEmpty ? id : title
    }
}

nonisolated private struct SwipletDownload: Sendable {
    let file: URL
    let id: String
}

nonisolated private final class SwipletDownloadEvents: NSObject, LibSwiptSwipletDownloadHandlerProtocol, @unchecked Sendable {
    private let progress: AsyncStream<SwipletDownloadProgress>.Continuation?

    init(_ progress: AsyncStream<SwipletDownloadProgress>.Continuation?) {
        self.progress = progress
    }

    func onProgress(_ received: Int64, total: Int64) {
        progress?.yield(SwipletDownloadProgress(received: received, total: total))
    }
}
