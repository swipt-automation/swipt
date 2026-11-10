import Foundation
import Perception
import dnssd

@Perceptible
final class LocalHostName {
    private(set) var name: String?
    private(set) var addresses: [String] = []

    @PerceptionIgnored private var reference: DNSServiceRef?

    var hosts: [String] {
        (name.map { [$0] } ?? []) + addresses
    }

    func start() {
        addresses = Self.localAddresses()
        guard reference == nil, let (interface, address) = Self.linkLocalAddress() else { return }
        let reverseName = address.reversed().map { "\(String($0 & 0xf, radix: 16)).\(String($0 >> 4, radix: 16))." }.joined() + "ip6.arpa."
        let context = Unmanaged.passUnretained(self).toOpaque()
        var serviceReference: DNSServiceRef?
        let error = DNSServiceQueryRecord(
            &serviceReference, 0, interface, reverseName, UInt16(kDNSServiceType_PTR), UInt16(kDNSServiceClass_IN),
            { _, flags, _, errorCode, _, _, _, length, data, _, context in
                guard let context, errorCode == kDNSServiceErr_NoError, let data else { return }
                let target = LocalHostName.domainName(UnsafeRawBufferPointer(start: data, count: Int(length)))
                let added = flags & DNSServiceFlags(kDNSServiceFlagsAdd) != 0
                MainActor.assumeIsolated {
                    let query = Unmanaged<LocalHostName>.fromOpaque(context).takeUnretainedValue()
                    if added {
                        query.name = target
                    } else if query.name == target {
                        query.name = nil
                    }
                }
            }, context)
        guard error == kDNSServiceErr_NoError, let serviceReference else { return }
        reference = serviceReference
        DNSServiceSetDispatchQueue(serviceReference, .main)
    }

    func stop() {
        if let reference {
            DNSServiceRefDeallocate(reference)
        }
        reference = nil
        name = nil
    }

    private static func localAddresses() -> [String] {
        var list: UnsafeMutablePointer<ifaddrs>?
        guard getifaddrs(&list) == 0, let first = list else { return [] }
        defer { freeifaddrs(list) }
        var hosts: [(host: String, lan: Bool)] = []
        for entry in sequence(first: first, next: { $0.pointee.ifa_next }) {
            let flags = Int32(entry.pointee.ifa_flags)
            let interface = String(cString: entry.pointee.ifa_name)
            guard flags & IFF_UP != 0, flags & IFF_LOOPBACK == 0, !interface.hasPrefix("pdp_ip"), !interface.hasPrefix("ipsec"),
                let address = entry.pointee.ifa_addr, Int32(address.pointee.sa_family) == AF_INET
            else { continue }
            var buffer = [CChar](repeating: 0, count: Int(NI_MAXHOST))
            guard getnameinfo(address, socklen_t(address.pointee.sa_len), &buffer, socklen_t(buffer.count), nil, 0, NI_NUMERICHOST) == 0 else { continue }
            hosts.append((String(cString: buffer), interface.hasPrefix("en")))
        }
        return hosts.sorted { $0.lan && !$1.lan }.map(\.host)
    }

    private static func linkLocalAddress() -> (interface: UInt32, address: [UInt8])? {
        var list: UnsafeMutablePointer<ifaddrs>?
        guard getifaddrs(&list) == 0, let first = list else { return nil }
        defer { freeifaddrs(list) }
        for entry in sequence(first: first, next: { $0.pointee.ifa_next }) {
            let flags = Int32(entry.pointee.ifa_flags)
            guard flags & IFF_UP != 0, flags & IFF_LOOPBACK == 0, String(cString: entry.pointee.ifa_name).hasPrefix("en"),
                let address = entry.pointee.ifa_addr, Int32(address.pointee.sa_family) == AF_INET6
            else { continue }
            var bytes = address.withMemoryRebound(to: sockaddr_in6.self, capacity: 1) { pointer in
                withUnsafeBytes(of: pointer.pointee.sin6_addr) { Array($0) }
            }
            guard bytes[0] == 0xfe, bytes[1] & 0xc0 == 0x80 else { continue }
            bytes[2] = 0
            bytes[3] = 0
            return (if_nametoindex(entry.pointee.ifa_name), bytes)
        }
        return nil
    }

    nonisolated private static func domainName(_ data: UnsafeRawBufferPointer) -> String? {
        var labels: [String] = []
        var index = 0
        while index < data.count, data[index] != 0 {
            let end = index + 1 + Int(data[index])
            guard end <= data.count else { return nil }
            labels.append(String(decoding: data[(index + 1)..<end], as: UTF8.self))
            index = end
        }
        return labels.isEmpty ? nil : labels.joined(separator: ".")
    }
}
