import Foundation
import Observation

@MainActor @Observable
final class WiFiProxyState {
    enum Catalog: Equatable {
        case idle, loading(UUID), loaded, failed(BackendFailure), unsupported
    }

    private let scope: ProfileConnectionScope
    private(set) var catalog = Catalog.idle
    private(set) var proxies: [WiFiProxy] = []
    private(set) var selectedID = ""
    private(set) var check: ConnectionAction<String>?
    private(set) var result: WiFiProxyCheck?

    init(scope: ProfileConnectionScope) {
        self.scope = scope
    }

    var selectedProxy: WiFiProxy? { proxies.first { $0.id == selectedID } }

    func loadCatalog() async {
        guard scope.available, !Task.isCancelled else { return }
        switch catalog {
        case .idle, .failed: break
        default: return
        }
        let request = Catalog.loading(UUID())
        catalog = request
        defer {
            if catalog == request { catalog = .idle }
        }
        do {
            let proxies = try await scope.read { api, token in try await api.wifiCallingProxies(token: token) }
            guard catalog == request else { return }
            self.proxies = proxies
            catalog = .loaded
        } catch {
            guard catalog == request, scope.available, !Task.isCancelled else { return }
            let failure = BackendFailure.wrapping(error)
            if failure.status == 404 || failure.status == 405 {
                catalog = .unsupported
                return
            }
            catalog = .failed(failure)
        }
    }

    func select(_ id: String) {
        guard selectedID != id else { return }
        selectedID = id
        clearCheck()
    }

    func save(_ id: String?, _ settings: WiFiProxySettings) async throws {
        let saved = try await scope.read { api, token in
            try await api.saveWiFiProxy(id, settings: settings, token: token)
        }
        if let index = proxies.firstIndex(where: { $0.id == saved.id }) {
            proxies[index] = saved
        } else {
            proxies.append(saved)
        }
        selectedID = saved.id
        clearCheck()
    }

    func requestCheck() {
        guard scope.available, check == nil, let proxy = selectedProxy else { return }
        result = nil
        check = .init(value: proxy.id)
    }

    func runCheck(_ check: ConnectionAction<String>) async throws {
        guard self.check?.id == check.id, !Task.isCancelled else { return }
        defer {
            if self.check?.id == check.id { self.check = nil }
        }
        let modem = scope.modemID
        let proxyID = check.value
        do {
            let result = try await scope.read { api, token in
                try await api.checkWiFiProxy(modem, proxyId: proxyID, token: token)
            }
            guard self.check?.id == check.id else { return }
            self.result = result
        } catch {
            guard self.check?.id == check.id, scope.available, !Task.isCancelled else { return }
            throw error
        }
    }

    func cancelPending() {
        if case .loading = catalog { catalog = .idle }
        clearCheck()
    }

    private func clearCheck() {
        check = nil
        result = nil
    }
}
