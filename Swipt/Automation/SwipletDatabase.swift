import Foundation
import GRDB

enum SwipletDatabase {
    private nonisolated struct SwipletRow: Codable, FetchableRecord, PersistableRecord {
        static let databaseTableName = "swiplets"

        var id: String
        var position: Int
        var repository: String
    }

    private nonisolated struct RepositoryRow: Codable, FetchableRecord, PersistableRecord {
        static let databaseTableName = "repositories"

        var url: String
        var position: Int
        var name: String
        var localizedNames: [String: String]?
        var iconColor: String?
        var iconSymbol: String?
        var iconSymbolColor: String?
        var image: Data?
        var etag: String
    }

    private nonisolated struct RepositorySwipletRow: Codable, FetchableRecord, PersistableRecord {
        static let databaseTableName = "repositorySwiplets"

        var repository: String
        var position: Int
        var id: String
        var name: String
        var localizedNames: [String: String]
        var description: String
        var version: String
        var versionCode: Int64
        var minimumAPILevel: Int64
        var minimumIOSVersion: String
        var iconColor: String?
        var iconSymbol: String
        var iconSymbolColor: String?
        var url: String
        var sha256: String
    }

    private static let database = AppDatabase.shared

    static func swiplets() throws -> [(id: String, repository: String)] {
        try database.read { db in
            try SwipletRow.order(Column("position")).fetchAll(db).map { ($0.id, $0.repository) }
        }
    }

    static func setSwipletIDs(_ ids: [String]) throws {
        try database.write { db in
            try SwipletRow.filter(!ids.contains(Column("id"))).deleteAll(db)
            for (position, id) in ids.enumerated() {
                let repository = try SwipletRow.fetchOne(db, key: id)?.repository ?? ""
                try SwipletRow(id: id, position: position, repository: repository).save(db)
            }
        }
    }

    static func setRepository(_ repository: String, of id: String) throws {
        try database.write { db in
            let position = try SwipletRow.fetchOne(db, key: id)?.position ?? nextPosition(of: SwipletRow.self, in: db)
            try SwipletRow(id: id, position: position, repository: repository).save(db)
        }
    }

    static func repositories() throws -> [Repository] {
        try database.read { db in
            try RepositoryRow.order(Column("position")).fetchAll(db).map { row in
                let swiplets = try RepositorySwipletRow.filter(Column("repository") == row.url).order(Column("position")).fetchAll(db)
                return Repository(
                    url: row.url, name: row.name, localizedNames: row.localizedNames ?? [:],
                    icon: row.iconSymbol.map { SwipletIcon(color: row.iconColor, symbol: $0, symbolColor: row.iconSymbolColor) }, image: row.image,
                    etag: row.etag,
                    swiplets: swiplets.map { swiplet in
                        RepositorySwiplet(
                            id: swiplet.id, name: swiplet.name, localizedNames: swiplet.localizedNames, description: swiplet.description,
                            version: swiplet.version, versionCode: swiplet.versionCode, minimumAPILevel: swiplet.minimumAPILevel,
                            minimumIOSVersion: swiplet.minimumIOSVersion,
                            icon: SwipletIcon(color: swiplet.iconColor, symbol: swiplet.iconSymbol, symbolColor: swiplet.iconSymbolColor),
                            url: swiplet.url, checksum: swiplet.sha256)
                    })
            }
        }
    }

    static func save(_ repository: Repository) throws {
        try database.write { db in
            let position = try RepositoryRow.fetchOne(db, key: repository.url)?.position ?? nextPosition(of: RepositoryRow.self, in: db)
            try RepositoryRow(
                url: repository.url, position: position, name: repository.name, localizedNames: repository.localizedNames,
                iconColor: repository.icon?.color, iconSymbol: repository.icon?.symbol, iconSymbolColor: repository.icon?.symbolColor,
                image: repository.image, etag: repository.etag
            ).save(db)
            try RepositorySwipletRow.filter(Column("repository") == repository.url).deleteAll(db)
            for (position, swiplet) in repository.swiplets.enumerated() {
                try RepositorySwipletRow(
                    repository: repository.url, position: position, id: swiplet.id, name: swiplet.name, localizedNames: swiplet.localizedNames,
                    description: swiplet.description, version: swiplet.version, versionCode: swiplet.versionCode,
                    minimumAPILevel: swiplet.minimumAPILevel, minimumIOSVersion: swiplet.minimumIOSVersion,
                    iconColor: swiplet.icon.color, iconSymbol: swiplet.icon.symbol, iconSymbolColor: swiplet.icon.symbolColor, url: swiplet.url,
                    sha256: swiplet.checksum
                ).insert(db)
            }
        }
    }

    static func removeRepository(_ url: String) throws {
        try database.write { db in
            _ = try RepositoryRow.deleteOne(db, key: url)
        }
    }

    private static func nextPosition(of table: (some TableRecord).Type, in db: Database) throws -> Int {
        try table.select(max(Column("position")), as: Int.self).fetchOne(db).map { $0 + 1 } ?? 0
    }
}
