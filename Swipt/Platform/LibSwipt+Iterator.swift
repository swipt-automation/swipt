import Foundation
import LibSwipt

extension LibSwiptStringIteratorProtocol {
    nonisolated func toArray() -> [String] {
        var array: [String] = []
        while hasNext() {
            array.append(next())
        }
        return array
    }
}

extension LibSwiptTXTRecordEntryIteratorProtocol {
    nonisolated func toArray() -> [LibSwiptTXTRecordEntry] {
        var array: [LibSwiptTXTRecordEntry] = []
        while hasNext() {
            if let value = next() {
                array.append(value)
            }
        }
        return array
    }
}

extension LibSwiptLocalizedNameIteratorProtocol {
    nonisolated func toDictionary() -> [String: String] {
        var dictionary: [String: String] = [:]
        while hasNext() {
            if let value = next() {
                dictionary[value.language] = value.name
            }
        }
        return dictionary
    }
}

extension LibSwiptSwipletActionIteratorProtocol {
    nonisolated func toArray() -> [LibSwiptSwipletAction] {
        var array: [LibSwiptSwipletAction] = []
        while hasNext() {
            if let value = next() {
                array.append(value)
            }
        }
        return array
    }
}

extension LibSwiptSwipletInfoIteratorProtocol {
    nonisolated func toArray() -> [LibSwiptSwipletInfo] {
        var array: [LibSwiptSwipletInfo] = []
        while hasNext() {
            if let value = next() {
                array.append(value)
            }
        }
        return array
    }
}

extension LibSwiptRepositorySwipletIteratorProtocol {
    nonisolated func toArray() -> [LibSwiptRepositorySwiplet] {
        var array: [LibSwiptRepositorySwiplet] = []
        while hasNext() {
            if let value = next() {
                array.append(value)
            }
        }
        return array
    }
}

extension Sequence<String> {
    nonisolated func toStringIterator() -> LibSwiptStringIteratorProtocol {
        StringArrayIterator(Array(self))
    }
}

nonisolated private final class StringArrayIterator: NSObject, LibSwiptStringIteratorProtocol {
    private let array: [String]
    private var index: Int = 0
    private var nextValue: String = ""

    init(_ array: [String]) {
        self.array = array
    }

    func len() -> Int32 {
        Int32(array.count - index)
    }

    func hasNext() -> Bool {
        guard index < array.count else { return false }
        nextValue = array[index]
        index += 1
        return true
    }

    func next() -> String {
        nextValue
    }
}
