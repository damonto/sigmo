# Sigmo for iOS

A native SwiftUI client for iPhone and iPad, with English and Simplified Chinese support. Connect to multiple Sigmo backends to manage devices, SIM/eSIM profiles, messages, calls, and settings. Available features depend on each backend's capabilities and Pro activation.

## Requirements

- iOS 26 or later.
- For development: a Mac with Xcode supporting the iOS 26 SDK and Swift 6, plus XcodeGen 2.44.1 or later.
- A reachable Sigmo backend for normal use; automated tests use local fixtures.

## Development

The Xcode project is generated from [project.yml](project.yml). From the repository root on a Mac:

```sh
cd apps/ios
xcodegen generate --spec project.yml
open Sigmo.xcodeproj
```

Select the **Sigmo** scheme and an iPhone or iPad simulator, then run the app. After changing the source file list or project configuration, regenerate the project. Generated projects, `.generated/`, and `DerivedData/` are ignored by Git.

For physical-device builds, configure `DEVELOPMENT_TEAM` and `SIGMO_BUNDLE_ID` in `project.yml` for your Apple developer account. Push notifications also require the corresponding App ID capability and APNs configuration.

For Linux development with builds on a Mac, follow the [Xcode MCP workflow](../../scripts/ios/MCP.md). Edit source in the Linux repository; the synchronized Mac workspace is a build copy.

## Encryption export compliance

The app declares `ITSAppUsesNonExemptEncryption: false` in `targets.Sigmo.info.properties` in [project.yml](project.yml) for distribution excluding France. It uses Apple's system encryption and standard encryption supplied by native WebRTC. [Apple's documentation requirements](https://developer.apple.com/help/app-store-connect/reference/app-information/export-compliance-documentation-for-encryption/) require a French encryption declaration for standard algorithms outside Apple's operating system only when distributing in France.

Before release, deselect **France** under **App Store Connect → Pricing and Availability → App Availability → Manage → Manage Availability**, then confirm the change. This setting is managed in App Store Connect; the plist does not restrict distribution. See [Apple's availability instructions](https://developer.apple.com/help/app-store-connect/manage-your-apps-availability/manage-availability-for-your-app-on-the-app-store/).

Before adding France, complete the encryption questionnaire and upload the required declaration under **App Information → App Encryption Documentation**. After Apple approves it, set `ITSAppUsesNonExemptEncryption` to `true` and add the Apple-issued `ITSEncryptionExportComplianceCode` as a string alongside it in `project.yml`, regenerate the project, and upload a new build. Reassess the declaration when encryption dependencies or usage change.

## Connect to Sigmo

Use **Add Sigmo (+)** to enter a name and server URL, including any reverse-proxy prefix. The app appends `/api/v1` automatically. Complete OTP or browser login (OIDC / Cloudflare Access), then Pro activation if required. Each connection has independent Keychain credentials. Cloudflare Access requires Managed OAuth; a redirect-only login page is insufficient. See [OIDC](../../README.md#oidc) and [Cloudflare Access](../../README.md#cloudflare-access) for provider configuration.

Use HTTPS with a system-trusted certificate, or local-network HTTP. Allow local-network access when prompted. Connections are saved locally, and authentication tokens are stored in Keychain.

Switching to the background lets in-flight requests and eSIM operations finish and pauses automatic polling between requests. Returning to the active scene refreshes idle pages without restarting pending work. The app requests additional iOS execution time for HTTP requests, the initial SIM status read, and SIM/eSIM interactions; idle SIM subscriptions suspend in the background. If the extra time expires, the app cancels unfinished work and releases resources. iOS suspension or termination can still interrupt long operations. Refresh the backend state before retrying a write whose result is uncertain.

## Calls from system Recents

Selecting Sigmo in a system contact card or tapping a Sigmo entry in the Phone app's Recents opens Sigmo through a system call activity. The app accepts `INStartCallIntent` for audio calls or calls with an unspecified capability, as well as the legacy `INStartAudioCallIntent` still used by system calling interfaces. Both use the same callback routing and LiveCommunicationKit call flow. This entry does not require a Siri extension or Siri voice support.

The legacy adapter intentionally uses deprecated SiriKit types. Swift 6.4 and newer suppress deprecation warnings only on the adapter declarations with `@diagnose`; older compilers retain those warnings and the same compatibility behavior.

Sigmo restores the backend state before dialing, then uses the most recent saved line for that number when the backend, modem, and original SIM are still available for calls. Using different lines for the same number does not require selecting one again. A missing saved route, unavailable backend, removed modem, or changed, inactive, or locked SIM opens the appropriate dialer for line selection.

Simulator fixtures cover parsing, navigation, and dialing decisions. Before release, verify Phone Recents on an iPhone with the app running, backgrounded, and terminated, including a number called from multiple SIMs. These system entry checks are separate from fixture tests.

Calls use PushKit for incoming invitations, LiveCommunicationKit for system conversation actions, and the native WebRTC SDK for media. The app prefers Opus and retains PCMU for older backends. New backends bridge Opus to the negotiated IMS codec using pure Go `pion/opus`, preserving the existing 16 kHz wideband PCM path; PCMU fallback remains narrowband. Local hold pauses both capture and playback through WebRTC's manually managed audio session. Recordings continue to use the existing PCM taps and AAC export.

## Diagnostic logs

Open **Manage Sigmo → Development diagnostics** to enable local diagnostic logging, including in Release builds. Logging is off by default. The switch and saved events survive app restarts. Turning logging off stops collection and keeps existing logs available for export or clearing.

Logs contain HTTP request summaries, network and decoding failures, WebSocket lifecycle events, and key app errors. Request and response bodies, credentials, query values, message content, and audio are excluded; dynamic resource identifiers in paths are hidden. The main app's notification registration and processing are included; the notification extension and full system crash reports are outside this log.

Call diagnostics correlate VoIP invitations, LiveCommunicationKit actions, backend state changes, and media startup using the call UUID. System integration events use the `conversation.*` prefix. They include invitation validation/decryption failures, sanitized call states and timestamps, ownership (`none`, `self`, or `other`), explicit end causes, sanitized error domains/codes, action completion, timeouts, resets, and audio activation. Phone numbers, contact names, client identities, DTMF digits, SDP, and ICE candidates are excluded. Unknown backend state/reason strings are recorded as `other`.

The app retains up to five 1 MiB files in private storage, excluded from backups, and rotates out the oldest events. **Export logs** saves a UTF-8 text snapshot through the system file exporter, with app/build and iOS version information. Logs are not uploaded automatically. **Clear logs** deletes the saved events without changing the logging switch.

## Transfer from a local SIM reader

With an HTTPS backend providing `esimTransfer`, connect an iOS-compatible CCID reader to the iPhone or iPad and choose it from the transfer source list. Readers and SIM insertion/removal update live. Empty readers show an insert-SIM hint; inserting a ready card makes them selectable. The backend performs the transfer to its target modem over [Remote APDU](../../internal/app/esimtransfer/README.md), including profile reads and any source-deletion confirmation.

Unplugging the selected reader or SIM stops the session and clears its profiles. Reinserting it does not resume or replay commands. Check the source and target state, then select the reader again to retry. Real reader compatibility and carrier transfers need physical-device validation; simulator tests use fake readers.

## Verification

Run **Product → Test** in Xcode with the **Sigmo** scheme to execute `SigmoTests` and `SigmoUITests`. These tests use local fixtures; physical-device, carrier, and APNs checks are separate.

From the repository root, check English/Chinese resources and settings fixtures without Xcode or a live backend:

```sh
python3 apps/ios/scripts/check_settings.py
python3 apps/ios/scripts/sync_carrier_bridge.py --check
```

The carrier WebKit regression tests run a copy of the server's bridge script. After changing `internal/app/websheet/bridge.js`, refresh that fixture with `python3 apps/ios/scripts/sync_carrier_bridge.py` before synchronizing the iOS workspace to a Mac.

## Offline demo and App Review

With no saved connections, the **Devices** welcome page shows **Connect to Sigmo** as the primary button, with a plain **Try Demo** text button below it. Tap **Try Demo** to explore without a server or account, including in Release builds. The demo uses the regular app screens with two isolated sample connections. Tap **Demo Mode** to generate an incoming message or call, read sample input instructions, or reset the data. **Exit Demo** returns to the welcome page. **Manage Sigmo** only manages connections and no longer contains the demo entry.

Messages, contacts, call history, eSIM changes, and settings are kept locally for the current demo session. Calls use local simulated media; demo recordings contain a sample tone. The eSIM setup page offers **Use Sample Activation Code**, and transfers include a virtual local USB reader. Carrier setup uses bundled HTML. No real modem, SIM, carrier service, remote push delivery, or external notification channel is exercised. The demo does not modify real connection credentials or recordings. It closes if a real call needs attention.

SIM PIN management is available in active SIM details. The local demo starts with PIN `1234` and PUK `12345678`. Incorrect codes decrement local attempt counters; exhausting PIN attempts opens the PUK flow. These operations never reach a real SIM.

The demo bundles the public MCP skill for offline downloads. After changing `skills/sigmo-control/`, regenerate the archive with `python3 apps/ios/scripts/build_demo_assets.py` from the repository root.

The **Sigmo Release** scheme builds the same application with the Release configuration for checking the public entry without Debug fixtures. Run the normal **Sigmo** scheme's `DemoTests` and `DemoFlowTests` for state consistency, recording, isolation, socket cleanup, concurrent reset/exit, edited connection addresses, and public-entry coverage. Also check the Release build on an iPhone/iPad before submitting it; simulator checks do not validate APNs, carrier service, or physical hardware.

After verifying the submitted build, include its build number and these instructions in Beta App Review Information:

> Tap Try Demo directly on the welcome page. No credentials, server, SIM, or external hardware are required. Demo Mode is labeled throughout the experience. Tap its label for sample incoming messages/calls and Reset Demo Data. Use Sample Activation Code in eSIM setup; the confirmation code is 123456 and the SIM PIN is 1234. Modem, carrier, hardware, and remote push interactions are simulated locally. Normal operation connects to a user-hosted Sigmo server and its hardware.
