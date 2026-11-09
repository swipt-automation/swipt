import Foundation
import LibSwipt
internal import SwiptHost

nonisolated final class SwipletWeb: SwiptHostWeb, @unchecked Sendable {
    private final class Completion: NSObject, LibSwiptSwipletWebCompletionProtocol, @unchecked Sendable {
        let completion: @Sendable (SwiptHostWebResult) -> Void

        init(_ completion: @escaping @Sendable (SwiptHostWebResult) -> Void) {
            self.completion = completion
        }

        func done(_ value: LibSwiptSwipletWebValue?) {
            guard let value else {
                completion(.json("null"))
                return
            }
            if !value.failure.isEmpty {
                completion(.failure(value.failure))
            } else if value.isData {
                completion(.data(value.data ?? Data()))
            } else {
                completion(.json(value.json))
            }
        }
    }

    private let web: LibSwiptSwipletWeb

    init(_ core: LibSwiptPlatform, swiplet: String) throws {
        web = try core.openSwipletWeb(swiplet)
    }

    func start(_ id: String, name: String, arguments: String, completion: @escaping @Sendable (SwiptHostWebResult) -> Void) {
        web.start(id, name: name, arguments: arguments, completion: Completion(completion))
    }

    func cancel(_ id: String) {
        web.cancel(id)
    }

    func put(_ data: Data) -> String {
        web.put(data)
    }

    func release(_ id: String, reason: String) {
        web.release(id, reason: reason)
    }

    func openFile(root: String, path: String) -> String {
        web.openFile(root, path: path)
    }

    func compression(format: String, mode: String) throws -> [String] {
        try web.compression(format, mode: mode).toArray()
    }

    func random(_ length: Int) throws -> Data {
        try web.random(Int32(clamping: length)).data ?? Data()
    }

    func digest(algorithm: String, data: Data) throws -> Data {
        try web.digest(algorithm, data: data).data ?? Data()
    }

    func decodeText(_ data: Data, fatal: Bool) -> (text: String, valid: Bool, restored: Int) {
        guard let decoded = web.decodeText(data, fatal: fatal) else { return ("", true, 0) }
        return (decoded.text, decoded.valid, Int(decoded.restored))
    }

    func close() {
        web.close()
    }
}
