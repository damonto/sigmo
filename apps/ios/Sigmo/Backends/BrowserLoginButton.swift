import SwiftUI

struct BrowserLoginButton: View {
    let session: BackendSession

    var body: some View {
        LoadingButton(isWorking: session.isBrowserLogin) {
            Task { await session.loginWithBrowser() }
        } label: {
            Label("auth.browserLogin", systemImage: "safari")
        }
        .disabled(session.isLoggingOut)
        .accessibilityIdentifier("loginBackend")
    }
}
