import Foundation
import GRDB
import os

enum AppDatabase {
    private static let url: URL = {
        #if os(iOS)
        let directory = AppDirectory.support
        #else
        let directory = AppDirectory.group
        #endif
        return directory.appendingPathComponent("Swipt.sqlite")
    }()

    static let shared = open()

    private static func open() -> DatabaseQueue {
        do {
            do {
                return try load()
            } catch let error as DatabaseError where error.resultCode == .SQLITE_CORRUPT || error.resultCode == .SQLITE_NOTADB {
                AppDirectory.logger.error("reset \(url.lastPathComponent, privacy: .public): \(error, privacy: .public)")
                try remove()
                return try load()
            }
        } catch {
            fatalError("cannot open \(url.lastPathComponent): \(error)")
        }
    }

    private static func load() throws -> DatabaseQueue {
        try FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
        var database = try DatabaseQueue(path: url.path)
        if try database.read(migrator.hasBeenSuperseded) {
            AppDirectory.logger.error("reset the data of a newer Swipt")
            try database.close()
            try remove()
            try AppDirectory.reset()
            database = try DatabaseQueue(path: url.path)
        }
        try migrator.migrate(database)
        return database
    }

    private static func remove() throws {
        for path in [url.path, url.path + "-journal", url.path + "-wal", url.path + "-shm"] where FileManager.default.fileExists(atPath: path) {
            try FileManager.default.removeItem(atPath: path)
        }
    }

    private static let migrator: DatabaseMigrator = {
        var migrator = DatabaseMigrator()
        migrator.registerMigration("0.1.0", migrate: createTables)
        migrator.registerMigration("0.1.0-1", migrate: migrate0_1_0_1)
        return migrator
    }()

    private nonisolated static func createTables(_ db: Database) throws {
        try db.create(table: "preferences", options: .ifNotExists) { t in
            t.primaryKey("name", .text)
            t.column("data", .blob).notNull()
        }
        try db.create(table: "swiplets", options: .ifNotExists) { t in
            t.primaryKey("id", .text)
            t.column("position", .integer).notNull()
            t.column("etag", .text).notNull()
        }
        try db.create(table: "repositories", options: .ifNotExists) { t in
            t.primaryKey("url", .text)
            t.column("position", .integer).notNull()
            t.column("name", .text).notNull()
            t.column("localizedNames", .text)
            t.column("iconColor", .text)
            t.column("iconSymbol", .text)
            t.column("image", .blob)
            t.column("etag", .text).notNull()
        }
        try db.create(table: "repositorySwiplets", options: .ifNotExists) { t in
            t.column("repository", .text).notNull().references("repositories", onDelete: .cascade)
            t.column("position", .integer).notNull()
            t.column("id", .text).notNull()
            t.column("name", .text).notNull()
            t.column("localizedNames", .text).notNull()
            t.column("description", .text).notNull()
            t.column("version", .text).notNull()
            t.column("versionCode", .integer).notNull()
            t.column("iconColor", .text)
            t.column("iconSymbol", .text)
            t.column("url", .text).notNull()
            t.column("sha256", .text).notNull()
            t.primaryKey(["repository", "id"])
        }
    }

    private nonisolated static func migrate0_1_0_1(_ db: Database) throws {
        try db.alter(table: "swiplets") { t in
            t.drop(column: "etag")
            t.add(column: "repository", .text).notNull().defaults(to: "")
        }
        try db.execute(sql: "DELETE FROM repositorySwiplets")
        try db.execute(sql: "UPDATE repositories SET etag = ''")
        try db.alter(table: "repositorySwiplets") { t in
            t.add(column: "minimumAPILevel", .integer).notNull().defaults(to: 1)
            t.add(column: "minimumIOSVersion", .text).notNull().defaults(to: "")
        }
        for (oldName, name) in [
            ("serverRunsInBackground", "runsInBackground"),
            ("serverBackgroundMode", "backgroundMode"),
            ("serverAutoUnlocks", "autoUnlocks"),
            ("serverAutoUnlockPasscodeType", "autoUnlockPasscodeType"),
            ("serverAutoUnlockPasscode", "autoUnlockPasscode"),
        ] {
            try db.execute(sql: "UPDATE OR IGNORE preferences SET name = ? WHERE name = ?", arguments: [name, oldName])
            try db.execute(sql: "DELETE FROM preferences WHERE name = ?", arguments: [oldName])
        }
    }
}
