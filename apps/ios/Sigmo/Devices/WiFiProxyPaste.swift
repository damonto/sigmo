import Foundation
import Network

struct WiFiProxyAddress: Equatable {
    let host: String
    let port: Int
    let username: String
    let password: String
}

enum WiFiProxyPaste: Equatable {
    case text
    case invalid
    case address(WiFiProxyAddress)

    static let defaultPort = 1080

    init(_ clipboardText: String) {
        let text = clipboardText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !Self.isPlainHost(text) else {
            self = .text
            return
        }

        self = Self.parseAddress(text).map(Self.address) ?? .invalid
    }

    private static func parseAddress(_ text: String) -> WiFiProxyAddress? {
        var authority = text[...]
        let schemePrefix = text.prefixMatch(of: #/socks5h?:///#.ignoresCase())
        if let schemePrefix {
            authority = text[schemePrefix.range.upperBound...]
            if authority.last == "/" { authority = authority.dropLast() }
        }
        let authorityPattern = #/(?:[a-zA-Z0-9._~!$&'()*+,;=:%-]+@)?[a-zA-Z0-9.:\[\]-]+/#
        // Foundation can escape colons in a password, but malformed percent escapes must stay invalid.
        guard authority.wholeMatch(of: authorityPattern) != nil, authority.last != ":",
              authority.firstMatch(of: #/%(?![0-9a-fA-F]{2})/#) == nil,
              let url = URLComponents(string: "socks5://\(authority)", encodingInvalidCharacters: true),
              let rawHost = url.encodedHost, let host = proxyHost(rawHost) else { return nil }

        let hasPort = url.rangeOfPort != nil
        let hasCredentials = authority.contains("@")
        guard schemePrefix != nil || (hasPort && !hasCredentials),
              let port = hasPort ? url.port : defaultPort,
              (1...65535).contains(port) else { return nil }

        let username = url.user ?? ""
        let password = url.password ?? ""
        guard !hasCredentials || (validCredential(username) && validCredential(password)) else { return nil }
        return WiFiProxyAddress(host: host, port: port, username: username, password: password)
    }

    private static func unbracket(_ host: String) -> Substring {
        host.first == "[" && host.last == "]" ? host.dropFirst().dropLast() : host[...]
    }

    private static func isPlainHost(_ text: String) -> Bool {
        guard let colon = text.firstIndex(of: ":") else { return true }
        let ipv6 = #/[a-fA-F0-9:.]+(?:%[a-zA-Z0-9_.-]+)?/#
        return colon != text.lastIndex(of: ":") && unbracket(text).wholeMatch(of: ipv6) != nil
    }

    private static func validCredential(_ value: String) -> Bool {
        !value.isEmpty && value.utf8.count <= 255 && value.rangeOfCharacter(from: CharacterSet(charactersIn: "\r\n")) == nil
    }

    private static func proxyHost(_ rawHost: String) -> String? {
        let host = unbracket(rawHost)
        if rawHost.first == "[" {
            let address = String(host)
            return IPv6Address(address) == nil ? nil : address
        }

        let hostname = host.last == "." ? host.dropLast() : host
        let hostLabel = #/[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?/#
        guard !hostname.isEmpty, hostname.utf8.count <= 253,
              hostname.split(separator: ".", omittingEmptySubsequences: false).allSatisfy({
                  $0.wholeMatch(of: hostLabel) != nil
              }) else { return nil }
        return String(host)
    }
}
