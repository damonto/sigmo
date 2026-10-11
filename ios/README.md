# Sigmo for iOS

A native SwiftUI client for iPhone and iPad, with English and Simplified Chinese support. Connect to multiple Sigmo backends to manage devices, SIM/eSIM profiles, messages, calls, and settings. Available features depend on each backend's capabilities and Pro activation.

## Requirements

- iOS 26 or later.
- For development: a Mac with Xcode supporting the iOS 26 SDK and Swift 6, plus XcodeGen 2.44.1 or later.
- A reachable Sigmo backend for normal use; automated tests use local fixtures.

## Development

The Xcode project is generated from [project.yml](project.yml). From the repository root on a Mac:

```sh
cd ios
xcodegen generate --spec project.yml
open Sigmo.xcodeproj
```

Select the **Sigmo** scheme and an iPhone or iPad simulator, then run the app. After changing the source file list or project configuration, regenerate the project. Generated projects, `.generated/`, and `DerivedData/` are ignored by Git.

For physical-device builds, configure `DEVELOPMENT_TEAM` and `SIGMO_BUNDLE_ID` in `project.yml` for your Apple developer account. Push notifications also require the corresponding App ID capability and APNs configuration.

For Linux development with builds on a Mac, follow the [Xcode MCP workflow](../scripts/ios/MCP.md). Edit source in the Linux repository; the synchronized Mac workspace is a build copy.

## Connect to Sigmo

Use **Add Sigmo (+)** to enter a name and server URL, including any reverse-proxy prefix. The app appends `/api/v1` automatically. Complete OTP login or Pro activation if the backend requires it; each connection has independent credentials.

Use HTTPS with a system-trusted certificate, or local-network HTTP. Allow local-network access when prompted. Connections are saved locally, and authentication tokens are stored in Keychain.

Switching to the background lets in-flight requests and eSIM operations finish and pauses automatic polling between requests. Returning to the active scene refreshes idle pages without restarting pending work. The app requests additional iOS execution time for HTTP requests, the initial SIM status read, and SIM/eSIM interactions; idle SIM subscriptions suspend in the background. If the extra time expires, the app cancels unfinished work and releases resources. iOS suspension or termination can still interrupt long operations. Refresh the backend state before retrying a write whose result is uncertain.

## Verification

Run **Product → Test** in Xcode with the **Sigmo** scheme to execute `SigmoTests` and `SigmoUITests`. These tests use local fixtures; physical-device, carrier, and APNs checks are separate.

From the repository root, check English/Chinese resources and settings fixtures without Xcode or a live backend:

```sh
python3 ios/scripts/check_settings.py
```
