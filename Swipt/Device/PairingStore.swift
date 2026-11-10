import Foundation
import LibSwipt
import Perception
import os

struct PairedDevice {
    var name: String
    var canMatchService: Bool
}

@Perceptible
final class PairingStore {
    static let shared = PairingStore()

    private(set) var record: Data?
    private(set) var device: PairedDevice?
    private(set) var revision = 0

    var isPaired: Bool { record != nil }

    init() {
        let record: Data
        do {
            record = try Data(contentsOf: AppDirectory.pairingRecord)
        } catch CocoaError.fileReadNoSuchFile {
            return
        } catch {
            ErrorAlert.shared.report(String(localized: "Couldn’t Read the Pairing Record"), error: error)
            return
        }
        do {
            device = try Self.check(record)
            self.record = record
        } catch {
            AppDirectory.logger.error("remove unreadable pairing record: \(error, privacy: .public)")
            try? FileManager.default.removeItem(at: AppDirectory.pairingRecord)
        }
    }

    private static func check(_ record: Data) throws -> PairedDevice {
        LibSwiptLog.installed
        var error: NSError?
        guard let info = LibSwiptParsePairingRecord(record, &error) else {
            throw error ?? MessageError("libswipt returned no pairing record information")
        }
        return PairedDevice(name: info.deviceName, canMatchService: info.canMatchService)
    }

    func clear() {
        try? FileManager.default.removeItem(at: AppDirectory.pairingRecord)
        record = nil
        device = nil
        revision += 1
    }

    func save(_ record: Data) throws {
        let device = try Self.check(record)
        var url = AppDirectory.pairingRecord
        try record.write(to: url, options: .atomic)
        var values = URLResourceValues()
        values.isExcludedFromBackup = true
        try url.setResourceValues(values)
        self.record = record
        self.device = device
        revision += 1
    }
}
