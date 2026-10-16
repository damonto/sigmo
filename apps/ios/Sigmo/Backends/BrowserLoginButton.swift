import SwiftUI

struct BrowserLoginButton: View {
    let session: BackendSession

    var body: some View {
        Button {
            Task { await session.loginWithBrowser() }
        } label: {
            HStack {
                Label("auth.browserLogin", systemImage: "safari")
                if session.isBrowserLogin {
                    Spacer()
                    ProgressView()
                }
            }
        }
        .disabled(session.isBrowserLogin || session.isLoggingOut)
        .accessibilityIdentifier("loginBackend")
    }
}
