import Foundation
import os

nonisolated enum AppDirectory {
    static let logger = Logger(subsystem: Bundle.main.bundleIdentifier ?? "Swipt", category: "storage")
    static let support = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
    static let pairingRecord = support.appendingPathComponent("PairingRecord.plist")

    #if os(macOS)
    static let group = FileManager.default.containerURL(
        forSecurityApplicationGroupIdentifier: Bundle.main.object(forInfoDictionaryKey: "AppGroupIdentifier") as! String)!
    static let caches: URL = {
        var caches = group.appendingPathComponent("Library/Caches", isDirectory: true)
        do {
            try FileManager.default.createDirectory(at: caches, withIntermediateDirectories: true)
            var values = URLResourceValues()
            values.isExcludedFromBackup = true
            try caches.setResourceValues(values)
        } catch {
            AppDirectory.logger.error("exclude caches from backup: \(error, privacy: .public)")
        }
        return caches
    }()
    #else
    static let caches = FileManager.default.urls(for: .cachesDirectory, in: .userDomainMask)[0]
    #endif

    static func size(of directory: URL) -> Int64 {
        guard let enumerator = FileManager.default.enumerator(at: directory, includingPropertiesForKeys: [.totalFileAllocatedSizeKey]) else {
            return 0
        }
        var size: Int64 = 0
        for case let url as URL in enumerator {
            size += Int64((try? url.resourceValues(forKeys: [.totalFileAllocatedSizeKey]).totalFileAllocatedSize) ?? 0)
        }
        return size
    }

    static func reset() throws {
        try FileManager.default.createDirectory(at: support, withIntermediateDirectories: true)
        for item in try FileManager.default.contentsOfDirectory(at: support, includingPropertiesForKeys: nil)
        where item.lastPathComponent != pairingRecord.lastPathComponent {
            try FileManager.default.removeItem(at: item)
        }
        try clearCaches()
    }

    static func clearCaches() throws {
        for item in try FileManager.default.contentsOfDirectory(at: caches, includingPropertiesForKeys: nil) {
            try FileManager.default.removeItem(at: item)
        }
    }
}
