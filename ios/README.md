# Sigmo for iOS

Sigmo is a native SwiftUI client for multiple Sigmo backends. Source lives in this repository; the development Mac pulls saved files and builds the generated Xcode project through the Xcode MCP gateway.

The app manages named Sigmo connections, independent OTP logins and Pro activation, backend capabilities, device lists, native SIM/eSIM management, and native backend settings. The home screen combines devices from every connection in one continuous native list. Below the navigation toolbar, a native SwiftUI search field and horizontal All / Sigmo-name tabs stay pinned. Search uses the system's default sizing and editing controls. The connection filter uses a native `UISegmentedControl` with its system gray track and Liquid Glass selection. UIKit owns the press, drag, and selection transitions; no custom selection capsule or background image overrides its rendering. It fills the search field’s width and is 32 points high at the default text size, shorter than the 44-point search field; both scale with Dynamic Type. Long connection lists scroll horizontally without automatic safe-area insets. Restoring or changing the selection reveals it after layout, including after a resize or connection rename; unchanged state preserves manual scrolling. Font sizing follows the view’s Dynamic Type traits. The bridge is a `UIScrollView` containing the native control, with normal viewport clipping. Public touch and gesture overrides allow overflowing segments to scroll; a control that fits keeps native drag-to-select behavior. A separate Manage Sigmo list owns connection actions. A full-width native navigation stack and a single-column device list serve iPhone and iPad. The Messages tab provides native SMS conversations with shared read state and SIM history. Unified APNs uses an encrypted preview and a Notification Service Extension; see [Messages and push](../docs/ios-messages-push.md). The Calls tab now includes native calling, CallKit, direct WebRTC audio, contacts, USSD, and local two-way recording. See [Calls implementation and acceptance](../docs/ios-calls.md); physical incoming-call and TestFlight acceptance remain separate.

Home only shows devices and connection tabs for online backends that have loaded successfully and still permit device access. All stays available; if the selected backend goes offline, needs login/activation, or is removed, the selection returns to All. Login, Pro activation, and offline states appear as compact status icons in Manage Sigmo; Log In and Activate Pro remain in the connection details. Pull to refresh Home, management, or connection details to reconnect. Cached devices and tabs from a failed connection stay hidden until a successful refresh, and an online backend with no devices shows the existing empty state.

`BackendSession.isOnline` supplies the online predicate for status labels, icons, devices, and tabs. `BackendStore` owns ordered and online session queries; Home derives its device rows and retains the selected connection. Online state stays derived from the session, including during a refresh and when a backend has no devices.

Verified on **2026-10-11**, iPhone Sigmo Development with **iOS 27**: app and test targets build successfully, and all **29 focused checks** (24 unit tests and 5 UI journeys) pass in `Test-Sigmo-2026.10.11_15-44-57-+0800.xcresult`. Console logs confirm all 29 executions, including initial loading versus refreshing, empty online backends, cached-device recovery, search and connection filters, status icons, login/activation, and returning to All after logout or deletion. The **1,020-key** bilingual resource check also passes. These checks use in-memory backends.

Follow-up validation on the same iPhone simulator covers the native segmented
filter, full-screen in-call keypad, and WebRTC recording delegate binding:

- All four Home UI checks passed in `Test-Sigmo-2026.10.11_17-44-05-+0800.xcresult`:
  press-and-drag selection followed by repeated management/device navigation,
  search plus selection, long-name scrolling, and Chinese maximum Dynamic Type.
  Light and dark screenshots show clear text. Default layout assertions verify
  the 32-point height, search-field width, and 10-point spacing below search.
- Two filter unit checks and eight more UI journeys passed in
  `Test-Sigmo-2026.10.11_17-46-56-+0800.xcresult`: selection across updates/removal,
  width and scrolling, adding and editing connections, independent login, logout,
  activation, deletion, and returning from Messages with the search/filter intact.
- The 15 recording/WebRTC unit checks and both keypad UI checks passed in
  `Test-Sigmo-2026.10.11_16-44-16-+0800.xcresult`. The keypad checks require every key,
  Hide Keypad, and hangup to be visible without scrolling, including large text and
  landscape. That run's two Home failures are covered by the passing Home rerun above.

Review follow-up: four filter unit checks and five state/navigation UI journeys
passed in `Test-Sigmo-2026.10.11_18-37-41-+0800.xcresult`. They cover restoring a
selection before sizing, resizing and renaming, returning to All, preserving a
manual scroll position during unchanged updates, local Dynamic Type overrides,
search, logout/deletion, and returning from Messages. Native dragging and repeated
navigation passed in `Test-Sigmo-2026.10.11_18-35-30-+0800.xcresult`. The strengthened
long-name journey passed in `Test-Sigmo-2026.10.11_18-40-55-+0800.xcresult`: the
selection is visible immediately after returning, swiping preserves it, and tapping
All restores the other backend’s devices.

Builds and the 1,020-key bilingual check pass. These are local fixture and simulator
checks; the recording callback fix still needs a new physical-device carrier call.

## Messages search

Messages uses SwiftUI's `.searchable` with an always-visible navigation-bar drawer.
Each device tab owns its `NavigationStack`, toolbar, and destinations. Home switches
to the device workspace outside its own navigation stack and retains its search and
connection filter. A single call presentation host surrounds both screens. There is
no custom search control or UIKit search adapter. The system owns focus, keyboard
composition, clear/cancel controls, sizing, and accessibility; English and Simplified
Chinese prompts use the existing localized resources.

Search uses the backend's number and full-message query, including older messages
outside the latest preview, and combines with Unread and unassigned-history filters.
Input is debounced for 250 ms; cancelled or obsolete requests cannot replace newer
results. Clearing or cancelling restores the selected list, and search leaves the
modem's unread badge unchanged. The native no-results view sits below the filters
and scrolls at large text sizes. Returning from a conversation preserves the query;
Messages and Calls keep independent search state when switching tabs.

Fixture validation on **2026-10-11**, iPhone Sigmo Development with **iOS 27**:
all **29 Messages/Home/push unit checks** and **15 UI journeys** have passing results
across `Test-Sigmo-2026.10.11_14-34-50-+0800.xcresult` and the focused reruns
`Test-Sigmo-2026.10.11_14-46-22-+0800.xcresult` and
`Test-Sigmo-2026.10.11_14-50-23-+0800.xcresult`. This includes all eight Messages
journeys, Home search/filter restoration, unread reconciliation, device refresh and
settings, eSIM navigation/cancellation, and a call surviving tab changes and return
to Home. Search screenshots were inspected in light appearance and Chinese dark
appearance at maximum Dynamic Type. The settings regression also exposed a missing
MSS validation footer, now laid out explicitly with its hint. Test queries wait for
tabs to be hittable and inspect native navigation titles after saving.

Builds and the **1,020-key** bilingual resource check pass. These checks use in-memory
fixtures; no modem, carrier call, or APNs service is used. The current native-search
implementation could not be rerun on iOS 26.5 or iPad because those simulator data
directories are missing.

## Call recordings

Recordings in the local library and call details show their number, line, and date
beside a circular Liquid Glass play/pause button. Saved recordings always show a
seek slider and elapsed/total time; seeking becomes available after selecting a
recording. Only one recording plays
at a time. Leaving its page, locking/backgrounding the app, receiving/starting a
call, or an audio interruption stops playback. Playback waits for CallKit to release
the call audio session. Swipe left to reveal Delete, then confirm in the system
alert. Cancelling preserves playback; confirming stops the selected recording
before deleting its files. There are no separate Stop or Share controls.

Hanging up detaches recording from the call immediately, then drains the PCM writer
and saves the AAC file independently of the call screen. A saving row shows
**Saving recording…**, then the playback button and timeline. Incomplete files show **Recording
needs recovery**, with per-record errors and recovery actions. A media disconnect
before the terminal call event no longer skips finalization. Capture queue overflow
keeps the recording recoverable instead of committing an incomplete capture as saved.
Recovery also supports a single surviving CAF track and reports the current retry's error.

Playback layout validation on **2026-10-11**, iPhone Sigmo Development with **iOS 27**:

- App and test targets build successfully; the resource check passes with **1,020 bilingual keys**.
- All **11 recording/player unit checks** passed in
  `Test-Sigmo-2026.10.11_14-52-43-+0800.xcresult`, including deletion failure and retry.
- All **four recording UI journeys** passed in
  `Test-Sigmo-2026.10.11_15-03-09-+0800.xcresult`, with execution confirmed in the
  console log. They cover the always-visible timeline, pause/resume/seek, background
  and navigation cleanup, confirmed deletion during playback, multiple recordings,
  and natural completion. The multiple-recording fixture captures segments of one
  local call. Light appearance and Chinese maximum Dynamic Type screenshots were inspected.
- A follow-up refactor keeps one optional scrub position, passes the player directly
  to the timeline, and centralizes per-recording error selection. App/test builds
  and the resource check pass. All **15/15 related checks passed** in
  `Test-Sigmo-2026.10.11_15-14-13-+0800.xcresult`, including error precedence and
  per-recording isolation. After the gateway restart, the console log confirmed
  all 15 unique cases started and passed: 11 recording/player unit checks and
  four recording UI journeys.
- The iPad (A16), iPadOS 27 attempt could not start because its simulator data
  directory is missing. Its result summary reused earlier passing statuses; the
  console log in `Test-Sigmo-2026.10.11_15-07-28-+0800.xcresult` records the launch
  failure. **This iPad run is not counted as passing.**

Earlier recording validation on **2026-10-11**:

- All **219 iOS unit checks** and **six Calls UI journeys** passed on iPhone in
  `Test-Sigmo-2026.10.11_12-21-42-+0800.xcresult`, including deterministic queue
  overflow, recovery retries, single-track recovery, and player lifetime cleanup.
- Both recording UI journeys also passed on **iPad (A16), iPadOS 27** in
  `Test-Sigmo-2026.10.11_12-30-41-+0800.xcresult`: library playback/seek/background
  cleanup, and call-details playback in Chinese at maximum Dynamic Type.
- `python3 ios/scripts/check_settings.py` passes with **1,016 bilingual keys**.

These checks use synthesized local audio and fixture calls; no carrier call is placed.

## Project configuration

| Setting                   | Value                                              |
| ------------------------- | -------------------------------------------------- |
| Language and UI           | Swift 6 and SwiftUI                                |
| Minimum deployment target | iOS 26                                             |
| Bundle identifier         | `com.damon.sigmo`                                  |
| Test bundle identifiers   | `com.damon.sigmo.Tests`, `com.damon.sigmo.UITests` |
| Development team          | `465NH4KUMT`                                       |
| Signing                   | Automatic                                          |
| App category              | Utilities (`public.app-category.utilities`)        |
| Project generator         | XcodeGen; configuration in `project.yml`           |

The app icon is in `Sigmo/Resources/Assets.xcassets/AppIcon.appiconset`. It reuses `web/public/icons/icon-512.png`, scaled to an opaque 1024×1024 PNG. Xcode generates the iPhone and iPad variants. Update this asset when the Web logo changes.

## Connections and storage

Use **Add Sigmo (+)** in the home navigation bar or **Connect Sigmo** in the empty state to enter a name and server address. The title stays **Sigmo** with any number of connections, and the add button has a minimum 44×44-point hit area. Enter the server address, including any reverse-proxy prefix; the client appends `/api/v1` for requests. Previously saved full API addresses are still accepted, but lists, details, and editors display the server address without that suffix. Saving does not require the connection to be online.

The leading navigation button opens **Manage Sigmo**. Rows show the saved name, address, and connection status. Tap a row for connection details; swipe left to reveal **Settings** and **Edit**, without automatic full-swipe execution. Settings opens native Authentication, Proxy, Notification, MCP & Agent Skills, and Updates pages. Edit changes the saved connection. Details retains information, authentication, activation, and deletion confirmed by a native alert with Cancel/Delete actions, without a device list. The management toolbar provides adding and reordering; the order is saved and controls both the home tabs and merged device order. Debug diagnostics also lives here.

Home defaults to **All** and keeps the horizontal connection filters visible while scrolling. A filter follows its connection UUID through renaming; deleting the selected connection returns to All. Rows retain saved connection order and each server's device order, without section headings. The first device has no top separator; separators between devices remain visible. Every row carries its connection name, including in single-connection views. Local search matches carrier, phone number (formatted or digits only), module name, SIM type, and Sigmo name; multiple search terms combine with the selected connection tab without making search requests. Module names remain available through search, VoiceOver, and the device detail title. Pull down to refresh all connections. Offline, login, and activation notices identify the affected connection above the device rows when search is empty; cached offline devices remain visible. eSIM rows open profile management; physical SIM rows open SIM details with phone number, carrier, ICCID, connection status, reminders, and SIM Applications. Both keep an Info toolbar button for full modem information. Unknown SIM types display device information directly.

Configuration and backend order are atomically saved in Application Support at `Sigmo/backends.json`. Tokens are stored separately in Keychain under the backend UUID with `AfterFirstUnlockThisDeviceOnly` accessibility. Changing an endpoint, logging out, or deleting a backend clears that backend's credential and device snapshot. Device data is held only in memory. A corrupt configuration is reported and preserved instead of silently reset.

Each backend checks licensing before OTP requirements, capabilities, and devices. OTP delivery and Pro pairing start only from an explicit action. Activation opens a validated Telegram link, observes pairing while the screen is visible, and reconciles licensing after the server restarts. A failure or expired login never clears another backend's state. Pairing and authorization states use explicit localized labels, including a localized fallback for unknown statuses. Inline Retry actions share the same icon, native bordered style, and control size across home, settings, and device pages. Capability flags are displayed independently, and unknown flags remain visible.

The app uses system-trusted HTTPS and the system local-network HTTP exception for IP addresses, `.local` names, and unqualified host names. It does not bypass certificate validation or follow redirects to a different origin. iOS requests local-network access when needed; permission-denied and connection errors can be retried from the backend details.

Home and device detail pages refresh once on entry or return from the background, and on pull-to-refresh. Idle pages do not schedule periodic requests, including unknown SIM classification, profile connection summaries, connection settings, messages, and call history. Writes and incoming message/call events still reconcile the affected snapshots. Refreshes allow at most three concurrent backends with a 15-second timeout per ordinary request; manual refresh retries immediately. Backgrounding cancels ordinary work. Offline snapshots retain their last-success timestamp.

## Native backend settings

Each Settings page operates on its owning backend through the existing authenticated `BackendSession`. Authentication, Proxy, and each notification channel keep in-memory drafts and save explicitly. Drafts survive navigation within Settings and are discarded when the settings flow closes. MCP enablement and update preferences save immediately; failures retain the last confirmed values. Authentication changes reconcile login requirements, and disabling the final OTP delivery channel is rejected before submission.

Notification forms use the backend schema for native text, secure text, number, toggle, picker, recipient-list, and HTTP-header controls. Telegram, Bark, Gotify, ServerChan³, Lark, WeCom, HTTP, and Email are supported. Native APNs has a separate **This Device Push** entry with Calls, SMS, and SIM reminder switches.

MCP provides endpoint/configuration copying, dynamic permissions, all-device or selected-device API keys, revocation, and the latest 50 audit events. A newly issued secret exists only in the creation sheet and is cleared when it closes. The Skill ZIP is downloaded with backend authentication and exported through the system Files dialog. Endpoint generation preserves reverse-proxy prefixes.

Updates manages the **Sigmo backend executable**, not the iOS app. Community uses Stable; Pro can select Stable or Dev. The page respects container/developer/signature-key restrictions, observes installation, and reconnects after restart without repeating an installation POST. Restart recovery uses a two-minute timeout and offers a read-only retry. Leaving the page or backgrounding stops observation; reopening reads the authoritative status.

Settings pages dispatch through a typed section enum; backend-defined controls decode into a finite control enum with an unsupported-field fallback. Network methods live in `Backends/BackendAPI+Settings.swift`, separately from the settings DTOs and the three page-state models. Confirmed snapshots and operation flags have private setters; editable drafts and presentation bindings remain writable.

View-owned actions use SwiftUI `.task(id:)` and clear pending actions when leaving the view or foreground. Recipient rows have stable IDs through insertions and deletions. Authentication changes restart only the existing refresh rather than cancelling unrelated requests. Requests check cancellation before entering the transport, and update recovery retries temporary connection failures rather than certificate errors.

Settings requests share credential invalidation and cancellation with the existing session. Logout/401 removes only that backend's protected state; old responses cannot repopulate it. Configuration values and issued MCP secrets are not persisted or logged by the app. All new flows include English and Simplified Chinese resources.

## Native modem settings

The device's Settings tab contains Airplane Mode, Network Mode, Frequency Bands, and General. Each menu uses a white SF Symbol on a colored rounded-square background, matching the native Settings presentation. These are modem-scoped controls, separate from the backend's Authentication/Proxy/Notifications settings. Native grouped Forms and pushed lists follow the existing iPhone/iPad navigation, support Dynamic Type and VoiceOver, and include English and Simplified Chinese resources.

Airplane Mode applies immediately through a system switch, without a confirmation dialog. While enabled, network mode and band changes are disabled; Alias and MSS remain editable. Network Mode applies a single selection immediately and keeps the selection page open. Frequency Bands keeps a local multi-selection until Done; at least one supported band is required. General saves Alias and MSS together with Done. Alias is trimmed and can be cleared to restore the default name. MSS is the APDU payload size in bytes and must be an integer from 64 to 254; its initial value comes from the backend. Back discards an editor's unsaved changes. Foreground refresh and failed saves retain edited drafts.

`ModemSettingsState` owns confirmed snapshots and independent read errors; `BackendAPI+ModemSettings.swift` uses the existing authenticated session and `/modems/:id` endpoints. Radio technologies and mode masks retain their full `UInt64` values, and a band's identity includes both its technology and its `UInt16` number. A failure reading one module does not remove the other modules. Writes stay exclusive through their authoritative readback, then refresh the existing device snapshot so names and status icons update throughout the app.

A successful PUT with an unavailable readback shows a saved-but-refresh-needed message. Interrupted or disconnected writes are reconciled using GET requests; no write is automatically repeated. Leaving a page or backgrounding cancels its view-owned requests. Session invalidation, device removal, and read revisions prevent older responses from restoring cleared or newer data. The Debug fixtures hold independent modem settings and can simulate unsupported controls, slow reads, partial failures, rejected writes, and disconnects after a write has applied.

The relevant checks are `ModemSettingsTests`, `ModemSettingsFlowTests`, and `python3 ios/scripts/check_settings.py`. They use in-memory transports rather than real modems. Verification on 2026-10-08/09:

- The application and test targets built successfully. All 144 iOS unit tests, including all 24 modem Settings tests, passed on the iOS 27.0 development iPhone simulator in `Test-Sigmo-2026.10.08_23-44-00-+0800.xcresult`. The same run passed the complete Airplane Mode UI flow.
- Network Mode and Frequency Bands UI flows passed in `Test-Sigmo-2026.10.08_23-46-32-+0800.xcresult`. The Settings overview screenshot was inspected: General aligns with the network rows, and all four icons retain their colored backgrounds and increased padding. Screenshots are attached to the result bundles.
- The General UI check stopped at the MSS footer assertion. Its query was updated to support a combined native Form footer, but the rerun and the remaining Settings UI checks were blocked by concurrent Messages code referencing an undefined `nativePush` environment value. Those checks, Chinese/dark/large-text screenshot inspection, and iPad verification are not yet counted as passing.
- The bilingual resource check passes with 760 keys. The public Go module's `go test ./...` passed using a temporary build cache. All settings verification used fixtures and fakes.

## Device presentation

Device icons use the Web client’s `flag-icons` 7.5.0 flags, converted to vector PDF and bundled in `Resources/Flags.xcassets`, with a globe for an unknown region. Rebuild them with `python3 ios/scripts/update_flags.py` after installing the Web dependencies and `rsvg-convert`; native PDF preserves details that Xcode’s SVG importer drops. Devices appear as continuous list rows with native separators, without individual card backgrounds. The flag sits to the left of three lines: carrier plus trailing status icons, phone number, and Sigmo/SIM tags. Native disclosure indicators at the trailing edge identify each row as a link to device details. A missing number displays **No Number / 无号码**. Carrier and tags truncate visually when needed while preserving their full VoiceOver labels; the number can scale to remain readable. Search and connection tabs stay above the scrolling list. The pinned search field retains its native Liquid Glass surface. The connection filter keeps the system segmented-control track, labels, and interactive selection in light and dark appearances. The two home toolbar buttons share native styling and equal hit areas of at least 44×44 points. The no-connections view sits outside the List, with a compact Connect Sigmo action that supports large text.

A red unread count sits at the bottom right of each row, beside the tags: zero is hidden, values over 99 display `99+`, and VoiceOver receives the full count. Each connection fetches one unfiltered `/conversations` snapshot and sums counts by module ID, including historical SIM/eSIM lines still assigned to that module. Unassigned history contributes only to the existing global badge. Initial/foreground/manual refresh and foreground notification events update the module counts; read, unread, and delete actions fetch a new authoritative summary immediately. Search results never replace these counts. Session cancellation and snapshot revisions reject stale responses; logout, address changes, and module removal clear the affected counts. Transient failures preserve the last snapshot, and old servers without the conversations API omit the badge.

`Components/PhoneNumberText.swift` provides a reusable SwiftUI number view and formatter backed by [PhoneNumberKit](https://github.com/PhoneNumberKit/PhoneNumberKit), with an Up to Next Major Version requirement from 5.0.11 in `project.yml`. The parser is allocated once on the main actor. Matching SIM-country numbers use national formatting; other international numbers retain their country code. Empty, short, unparseable, and regionless national numbers have explicit fallbacks; formatting never uses the iPhone’s region to guess a backend number.

The trailing status strip orders indicators as Data, Wi-Fi Calling, VoLTE, radio generation, signal. Connection icons appear only for confirmed active states; IMS icons also require the owning backend’s capability. Radio generations use compact 2G/3G/4G/5G glyphs; unknown technology is never assumed to be 4G. Percentages and standalone registration/data/Wi-Fi Calling rows are removed from rows; VoiceOver still announces signal strength and icon meanings. Airplane mode replaces the signal bars with an airplane and hides cellular radio/VoLTE indicators.

Wi-Fi Calling and VoLTE use hand-drawn handset glyphs paired with Wi-Fi arcs and outlined LTE lettering. Their template PDFs in `Resources/Radio.xcassets` inherit the status strip’s foreground color and scale with Dynamic Type. Edit `scripts/ims-vowifi.svg` or `scripts/ims-volte.svg`, then run `python3 ios/scripts/update_ims_icons.py` from the repository root with `rsvg-convert` installed to rebuild the assets.

Both `GET /api/v1/modems` and `GET /api/v1/modems/:id` now include `volteEnabled` and `volteConnected`. Pro fills these through the existing modem-overview extension, alongside Wi-Fi Calling. iOS reads IMS state from the same device snapshot without an additional request per modem. Missing/null fields remain unknown. Device information shows one row per supported service: Connected, Disconnected, Disabled, or Unknown; disabled takes precedence over a stale connection flag. Snapshots retain the existing backend-scoped authentication and request-generation checks. Third-party license notices ship in `Resources/ThirdPartyNotices.txt`.

## Native modem Detail

The modem screen retains four native bottom tabs: SIM, Messages, Calls, and Settings. Messages supports SMS conversations and SIM history; Calls supports history, dialing, USSD, contacts, and local recordings, with app-wide CallKit and audio state. Settings contains native modem controls. The eSIM Detail home uses system inset grouped sections for SIM slots and status, hardware identifiers, and profiles grouped by secure element. Profile rows use the system body and caption styles. Tapping a profile opens a native Form with Label and Phone number values, Turn On This Line, four connection-setting entries, reminders, SIM Applications, information, and Delete eSIM. Editable rows, switches, and navigation links use the same system row sizing; reminder details appear in the section footer. Label, number, PIN, and reminder forms use toolbar Cancel/Done actions; text editors focus the input on presentation. Profile icons scale from 32 points with Dynamic Type. Connection and SIM Application observation runs only while that page is visible. Each secure-element profile list ends with a native Add eSIM row, including empty lists. Profile rows display On or Off; Profile information labels this value Profile State. Hardware and Info values use native labeled rows with a long-press Copy menu. The navigation title and backend subtitle are width-limited and truncate on one line; accessibility retains the full identity. Info pushes hardware, SIM, eUICC, and network information; its eSIM Notifications entry replaces the Web client's hidden title gesture. Physical SIMs open SIM and network details on the initial screen, with reminder and SIM Application actions when available; device identifiers, hardware, firmware, and feature information are available through the same Info toolbar button. The title menu switches only between modems on the same backend.

Initial hardware, profile, connection, and notification reads use disabled SwiftUI redacted placeholders with localized accessibility labels. Refreshes retain existing data. Native alerts present operation errors and confirmations for switching slots, enabling or deleting profiles, removing reminders or notifications, and deleting a transfer source profile. Dismissing an error preserves form input; connection read failures remain available for manual retry. Radio-generation glyphs compensate for the template PDFs' transparent margins so their visible size matches the other status indicators.

Native forms support PIN unlocking, confirmed slot/profile activation, nickname changes, profile deletion, phone-number edits, and backend-owned reminders. Enabled profiles cannot be deleted or disabled from the profile controls. Names are limited to 64 UTF-8 bytes; reminder times must be in the future and repeat intervals must be 1–3650 days. Connection controls read and preserve the existing APN/routing/proxy and Wi-Fi Calling underlay configuration; disconnecting Wi-Fi Calling does not disable the service.

Add eSIM binds download and transfer to the list’s secure element; there is no separate destination-selection step. Profile sections are titled eSIMs; multiple elements also show SE 1 or SE 2. The SIM status row shows the carrier on the left and a trailing SIM 1 / SIM 2 menu for multiple slots. Single-slot devices omit the SIM 1 selector and active-slot information row. Operation confirmations explain which eSIM will be turned off or deleted without using secure-element terminology. Tapping Add eSIM performs one discovery request with a spinner at the end of the tapped row, before opening setup. An available-download alert on Detail offers Download or Not Now; an empty result opens Set Up eSIM directly without an alert. A failed discovery reports its error in setup and leaves the other methods available. Leaving Detail or entering the background cancels pending setup so late responses cannot open it. Returning between setup steps does not repeat discovery. The Add eSIM page offers Transfer eSIM and Use QR Code. The scanner places the camera above the instructions and a gray capsule Enter Details Manually button fixed at the bottom; manual entry focuses the address field automatically and keeps Next in the top controls. Scanned codes proceed to preview, or to manual entry when a confirmation code is required. Available eSIMs have a selection list and a system-sized Download button. Setup, preview, confirmation, progress, and completion share one large rounded sheet with glass navigation controls, left-aligned headings, and system-sized primary buttons. The cellular symbol and grouped input fields follow the native setup layout, and the header moves up when the keyboard appears. Installation accepts manual fields, pasted LPA codes, and VisionKit camera scanning. Camera denial or unavailable hardware leaves manual input available. Pro transfer retains modem/CCID sources, required source IMEI, profile selection, carrier input, source deletion confirmation, and carrier Websheets. Progress reports server stages rather than inventing a download percentage.

SIM Application implements the menu, display, input, inkey, confirmation, and back protocol. Each native notification row has a leading operation icon, a first line with its exact #sequence number and ICCID, and a second line with a colored operation tag and the SM-DP address. Long identifiers and large text can wrap, and VoiceOver receives a localized description of all fields. A single secure element has no redundant eSIM section heading. Trailing swipe actions reveal icon-only Resend and Delete buttons. Full swipe does not execute either action; a system alert confirms deletion before the row is removed. Empty notification groups are hidden. Profile and notification operations always carry their secure-element identity, so matching sequence numbers on different elements remain independent.

HTTP and Foundation WebSocket operations share the owning BackendSession's authentication and cancellation boundary. Writes invalidate older modem reads, and successful operations reconcile the authoritative snapshots. Unknown SIM classification and profile connection summaries refresh on entry, foreground return, or pull-to-refresh, without timed retries. SIM application and installation WebSockets remain event-driven.

Installation/transfer remains in the foreground. Backgrounding, cancellation, or a lost connection closes the session and never automatically replays a write. Refresh reconciles changes already applied, including the transfer source modem. Transient inactive states, such as system permission prompts, do not cancel backend sessions. Carrier pages run in an ephemeral WKWebView through the existing authenticated backend proxy; callbacks and reverse-proxy prefixes stay associated with that backend. PINs, activation/confirmation codes, and Websheet credentials are not persisted or logged.

Debug scenarios `detail`, `detail-physical`, `detail-locked`, `detail-loading`, `detail-unavailable`, `detail-single`, and `detail-notifications` run entirely against an in-memory HTTP/WebSocket fixture. The latter two cover single-slot/single-eUICC hardware and all four notification operations, including duplicate sequence numbers across elements. Additional `detail-discovery-loading`, `detail-no-discoveries`, `detail-discovery-error`, and `detail-empty-profiles` scenarios cover inline loading, cancellation, empty/failed discovery, and Add eSIM in empty lists. `ModemDetailTests`, `CarrierWebsheetTests`, and `ModemDetailFlowTests` cover contracts, state transitions, native navigation, and cancellation. They never operate real SIMs. Real camera recognition and carrier interoperability still require physical-device checks.

## Profile connection settings

The active Profile has four native SwiftUI destinations: **Network Selection**, **Internet**, **Wi-Fi Calling**, and **VoLTE**. IMS entries follow each backend's capabilities. The Network Selection summary shows the current `registeredOperator` name, falling back to its PLMN code. It never substitutes the SIM brand or a registration-state label. When the operator is missing, `Searching` displays Searching, `Idle`/`Denied` display No Service, and unknown states remain Unknown. Backend state names are matched without case sensitivity.

Network Selection reads the actual automatic/manual mode. Turning Automatic off starts an asynchronous scan and displays one row per PLMN; tapping a network applies manual registration. Returning without a selection leaves the backend mode unchanged. Enabling Automatic applies automatic registration. Scan failures and empty results have retry controls; leaving stops client polling without cancelling the server's scan. Polling is once per second within the five-minute scan deadline.

Internet shows the connection switch and status, an **APN** editor, and **Default Route**, **Proxy Service**, and **Always On** switches directly on its main page. Connected sessions show interface, duration, traffic, IP addresses, DNS, route metric, public IP/country/organization, and proxy details inline below the controls. HTTP and SOCKS5 proxy URLs can be copied with encoded credentials. APN, IP type, authentication, username, and password are editable only while disconnected. Drafts survive navigation and refreshes and are submitted with the next connection; connected preferences apply immediately and then read back. Failed writes preserve inputs. Public information has a separate retry and is invalidated when disconnected or the bearer changes.

Wi-Fi Calling's switch controls service enablement. Connection status, duration, reconnect, and disconnect remain separate; disconnecting a session preserves enablement. A native alert confirms enablement, and required carrier setup opens once for that enable attempt. Cancellation keeps a Continue Setup entry. Uplinks include the backend system network, this modem, and connected modems on the same backend; a saved unavailable uplink remains visible. Update Emergency Address appears only when the backend allows it. Existing ephemeral carrier Websheets preserve backend authentication, queue/deduplicate callbacks, forward terminal callbacks before closing, and refresh settings afterwards.

VoLTE displays enablement separately from connection status and duration. The Data Path subpage offers QMAP, Legacy BAM-DMUX, and Qualcomm 410 only while disabled; MBIM has a fixed channel. A modem registration without Sigmo enablement displays Managed by Device with an unavailable duration. Changes refresh both the Profile summary and device snapshot.

Each destination owns a concrete observable state scoped to backend access, SIM identity, and the active Profile. Connection pages read once on entry and foreground return, and support pull-to-refresh; conflicting reads are paused during writes. SIM changes, modem removal, lost access, and cancellation reject stale responses. Airplane mode blocks cellular registration and connection changes; Wi-Fi Calling follows its uplink. Ordinary HTTP requests use 15 seconds; registration, Internet connect/disconnect, VoLTE changes, and carrier-flow startup use 120 seconds with a matching session resource timeout. An uncertain write is reconciled by reading, never automatically replayed. APN passwords and proxy credentials stay in memory.

`ConnectionTests` and `ConnectionFlowTests` use in-memory fixtures and local carrier HTML. Debug scenarios `detail-connection-scan`, `detail-connection-scan-error`, `detail-connection-empty`, `detail-connection-read-error`, `detail-connection-write-error`, `detail-connection-setup`, `detail-connection-mbim`, and `detail-connection-community` cover recovery, carrier callbacks, capabilities, and device variants. Their controls support English/Chinese, Dynamic Type, and dark appearance. The interaction references are [Apple's SwiftUI containers](https://developer.apple.com/documentation/swiftui/picking-container-views-for-your-content), [iOS automatic/manual network selection](https://deviceguides.ee.co.uk/apple/iphone-17-ios-26/connectivity/selecting-network/), and [Apple's Wi-Fi Calling enablement flow](https://support.apple.com/en-us/108066).

## Development

Follow the [MCP setup and operating guide](../scripts/ios/MCP.md) to start the Linux source service and run `curl -fsS http://DEVELOPMENT_HOST:8766/start-mac.sh | bash` in one Mac terminal. The server embeds the SSH target and source address. The Bash launcher starts the outgoing SSH tunnel, waits for source readiness, and starts the Xcode gateway. The repository's `.codex/config.toml` connects Codex to the development host's local `127.0.0.1:8765` forward as the `xcode` MCP server. Ctrl-C stops both Mac services; rerun the same command after a network disconnect.

Edit files on Linux. The Mac workspace at `~/Library/Developer/SigmoMCP/workspace` is a build copy; generated `.xcodeproj` files, `.generated/Info.plist`, DerivedData, credentials, and signing material stay outside version control.

Use `XcodeOpenWorkspace`, select the intended run destination, then call `BuildProject`, `RunAllTests`, or `RunProject`. `GetConsoleOutput` reads the app's OSLog and stdout. The Debug diagnostics screen provides a manual log action and counter.

The diagnostics screen shows the installed app's bundle identifier and version. Use the gateway's source revision to identify the exact saved snapshot used by an MCP operation.

## Verification

Native home search sizing (2026-10-10, iOS 27.0 fixtures):

- Search now uses SwiftUI's `.searchable` navigation-bar drawer with system sizing, typography, and editing controls. The custom 36-point glass field has been removed.
- App/test builds, the **1,008-key** resource check, and all **4 affected iPhone UI tests** pass in `Test-Sigmo-2026.10.10_19-37-50-+0800.xcresult`. Coverage includes search/clear/filter interactions, placement above the tabs, empty states, dark appearance, and Chinese maximum accessibility text.
- The final spacing/background follow-up passes **3/3 UI tests** in `Test-Sigmo-2026.10.10_20-48-46-+0800.xcresult`. Measured toolbar-to-search and search-to-tabs gaps are both **10 points**. The title and toolbar buttons stay aligned, and their minimum hit areas are preserved. The additional bar background and top scroll-edge effect are removed; list content clips below the header. Standard-size light and maximum-text dark scrolling screenshots confirm clear header surroundings without content bleeding behind the controls.

Compact home glass controls (2026-10-10, iOS 27.0 fixtures):

- App and test targets build successfully; all **6 affected iPhone UI tests** pass in `Test-Sigmo-2026.10.10_19-31-10-+0800.xcresult`, with each execution confirmed in the console log. The resource checker passes with **1,008 bilingual keys**.
- Checks cover search and connection-filter combinations, selected state, edge taps and 44-point hit areas, the compact header height, search clearing, independent backend devices, row navigation, empty states, dark appearance, and Chinese maximum accessibility text.
- Light, dark, and large-text screenshots confirm the shared outer glass capsule and separate inner selection. The first device's top separator is hidden while separators between devices remain visible.

Home controls and disclosure indicators (2026-10-10, iOS 27.0 fixtures):

- App and test targets build successfully; all **8 affected iPhone UI tests** pass in `Test-Sigmo-2026.10.10_15-52-11-+0800.xcresult`. The resource checker passes with **1,009 bilingual keys**.
- Checks verify equal, circular toolbar button bounds of at least 44×44 points, a compact and tappable Connect Sigmo button in English and Chinese maximum accessibility text, Sigmo-before-SIM tag order, native row navigation, backend isolation, unread readback, and explicit refresh behavior. Empty-state and list screenshots were inspected in light/dark appearances.

Home background and explicit refresh (2026-10-10, iOS 27.0 fixtures):

- App/test builds and the **1,009-key** bilingual resource check pass. All **206 unit tests and 12 affected iPhone UI tests** pass together (**218/218**) in `Test-Sigmo-2026.10.10_15-37-10-+0800.xcresult`.
- The new lifecycle fixture changes its displayed name/carrier on every modem request. It verifies no idle refresh beyond the previous 30-second home interval or the unknown-SIM retry interval, then verifies pull-to-refresh and background/foreground updates on both home and detail.
- Connection settings, message read/reply/history, unread badge reconciliation, call navigation, and detail/profile flows also pass. Light and dark home screenshots were inspected: navigation, search/filter surroundings, list, and bottom safe area use the same background.
- The final device-information foreground refresh also passes the repeated detail/info/notifications UI journey (**1/1**) in `Test-Sigmo-2026.10.10_15-46-04-+0800.xcresult`.

Home list and search refinement (2026-10-10, iOS 27.0 fixtures):

- Application and test targets build successfully; the resource checker passes with **1,009 bilingual keys**. Changed-file whitespace and conflict-marker checks also pass.
- All **206 unit tests** pass in `Test-Sigmo-2026.10.10_15-07-08-+0800.xcresult`, including local search without network requests, combined search/tab filters, address display round trips, and English/Chinese authorization states with unknown-value fallbacks.
- All **28 affected iPhone UI checks** have passing results across that full batch and the focused correction batches `Test-Sigmo-2026.10.10_15-21-29-+0800.xcresult` and `Test-Sigmo-2026.10.10_15-25-04-+0800.xcresult`. These are aggregate results: the full batch passed 229 of 234 checks, and subsequent batches resolve its five UI failures. The final four-check batch also verifies Retry labels after their accessibility correction.
- Checks cover continuous three-line rows, No Number, local search plus tabs, duplicate module IDs, bottom-right unread badges/readback, native deletion alerts including cancellation, address display/editing, management navigation, activation, notifications, and shared recovery controls. English, Chinese maximum accessibility text, dark-mode, and unread-badge screenshots were inspected. iPad runtime verification remains limited by the missing simulator data directory described below.

Home and Sigmo management verification (2026-10-10, iOS 27.0 fixtures):

- The app and both test targets build successfully. The local resource checker passes with 1,004 bilingual keys; home/management key coverage and changed-file whitespace checks also pass.
- All **203 unit tests** and **25 affected iPhone UI tests** have passing results across `Test-Sigmo-2026.10.10_14-17-24-+0800.xcresult` and `Test-Sigmo-2026.10.10_14-36-03-+0800.xcresult`. The first batch passed 227 of 228 checks; the second verifies the corrected Settings navigation test, which now returns through the management list. These are aggregate results across batches.
- Coverage includes merged filtering, duplicate module IDs, rename/deletion, ordering persistence, management swipes and details, notification navigation, historical-line unread aggregation, read/unread/delete reconciliation, stale responses, logout/address changes, transient errors, and legacy servers. The final toolbar refinement passed four repeated UI checks in `Test-Sigmo-2026.10.10_14-40-54-+0800.xcresult`, including the actual 44×44-point minimum add-button bounds. Final English, Chinese maximum accessibility text, and dark-mode home screenshots were inspected; missing phone numbers, long names, and `99+` are covered by the fixture UI checks.
- Current iPad execution remains unverified: the simulator's data directory is missing, so the runner could not launch. The apparent per-test statuses returned for `Test-Sigmo-2026.10.10_14-31-50-+0800.xcresult` were stale and are **not counted**. Re-run the adaptive-grid and management checks on a working iPad simulator.

Calls verification (2026-10-09): all 183 unit tests and four Calls UI journeys
pass on the iPhone simulator (**187/187**), including the final Keychain-unlock
recovery. The subsequent keypad layout correction passes all four Calls UI
journeys on both iPhone iOS 27.0 and iPad iOS 26.5 (**4/4** each), confirmed
from their saved execution logs. These reruns repeat the existing UI journeys.
Public/Pro Go suites, Web's 506 tests/type checking/build, Worker's
101 tests/type checking/dry-run, and the 966-key resource check also pass.
See [Calls implementation and acceptance](../docs/ios-calls.md) for result
bundles, device coverage, deployment order, and pending real-call acceptance.

Profile connection settings verification (2026-10-08/09, iOS 27 as requested):

- All **144 SigmoTests** in the 2026-10-08 source snapshot passed on `iPhone Sigmo Development`, including the 26 new connection regressions (`Test-Sigmo-2026.10.08_23-19-58-+0800.xcresult`). Fixtures cover staged manual registration, operator summaries, failed/late responses, complete Internet payloads, draft preservation, proxy URL encoding, public-info invalidation, Wi-Fi service/session separation, carrier callbacks, unavailable uplinks, QMI/MBIM, and SIM/authentication isolation.
- All **12 affected UI journeys** have passing results across `Test-Sigmo-2026.10.08_23-23-18-+0800.xcresult` and `Test-Sigmo-2026.10.08_23-34-03-+0800.xcresult`. This is an aggregate after correcting three automation assertions for iOS 27, not a claim that the first batch passed completely. It covers the ten connection journeys plus Profile activation and connection/number navigation. Tests use only in-memory services and local carrier HTML.
- A stricter follow-up at maximum Chinese accessibility text size found raw APN/IP labels and unidentified text-clipping reports in Internet (`Test-Sigmo-2026.10.08_23-52-39-+0800.xcresult`). Labels now expose their name and value separately, and option labels/footers explicitly allow vertical expansion. Verification of these final refinements is pending; the other in-progress Messages/Push sources currently prevent rebuilding because `EnvironmentValues.nativePush` is not yet defined. The unidentified clipping reports are not counted as passing.
- Earlier iOS 26.5 attempts did not launch because the listed simulators lacked their data directories; they produced no test results and are excluded. This feature has not yet been verified on an iPad simulator or physical hardware. The latest successful app/test build before the unrelated Messages/Push changes reported no warnings.

The `Sigmo` scheme includes `SigmoTests` and `SigmoUITests`. Unit coverage includes URL and redirect policy, DTO decoding, independent auth/capabilities, stale responses, refresh concurrency/cancellation/backoff, Keychain, configuration persistence/failures, Pro activation recovery, phone formatting, and VoLTE snapshot isolation and missing/null/disabled IMS states. UI coverage includes adding/editing connections, persistence, management-list swipe actions, details navigation, merged connection filters, and unread row badges, native settings forms/key/export/update journeys, continuous device rows, local search, missing phone numbers, explicit refresh and foreground lifecycle, OTP, activation, Chinese large text on the home/detail screens, dark appearance, text-clipping/element-description audits, and diagnostics. The scheme sets `SIGMO_UNIT_TESTS=1` so the hosted unit-test app starts with isolated empty configuration. UI tests explicitly select Debug-only fixtures using `SIGMO_UI_SCENARIO`; ordinary launches never select fixtures automatically.

Notification and single-hardware verification (2026-10-08):

- The app and test targets compile successfully. The resource checker passes with 579 bilingual keys.
- The completed iPhone batches provide passing results for all 37 distinct affected checks: 32 Detail unit tests and five UI journeys. Results span `Test-Sigmo-2026.10.08_15-47-12-+0800.xcresult`, `Test-Sigmo-2026.10.08_16-30-04-+0800.xcresult`, and `Test-Sigmo-2026.10.08_17-07-52-+0800.xcresult`; this is an aggregate after focused corrections, not a claim that the initial batch passed completely.
- All three iPad journeys passed in `Test-Sigmo-2026.10.08_17-17-22-+0800.xcresult`: notification resend and confirmed deletion, single-slot/single-eUICC labels and installation, and Chinese accessibility text with dark appearance.
- Notification checks cover all four operation labels, exact sequence numbers, cancellation, and deletion scoped to one element when another has the same sequence number. Normal and accessibility text layouts pass text-clipping and element-description audits. Native swipe actions and the final iPhone/iPad layouts were inspected in recorded screenshots.

Native Detail appearance verification (2026-10-08):

- The app and test targets compile successfully. The resource checker passes with 574 bilingual keys.
- Focused iPhone batches passed 50 distinct checks: 34 unit tests, all 14 Detail UI journeys, and the two home-card layout/appearance checks. The batches are `Test-Sigmo-2026.10.08_14-28-33-+0800.xcresult`, `Test-Sigmo-2026.10.08_14-47-05-+0800.xcresult`, and `Test-Sigmo-2026.10.08_14-54-20-+0800.xcresult`; the final batch repeats the profile-row check after its spacing refinement.
- All four iPad checks passed in `Test-Sigmo-2026.10.08_14-57-02-+0800.xcresult`: profile row sizing and quick actions, Chinese accessibility text with dark appearance, confirmed slot/profile operations, and PIN error recovery. These targeted checks do not replace the earlier interrupted 12-check iPad batch.
- New fixtures cover delayed reads and unavailable profiles. UI checks verify skeleton replacement, retryable system alerts, preserved PIN input, confirmation cancellation, equal row heights, and large-text Profile accessibility. Recorded iPhone screenshots were inspected for the native form layout and loading placeholders.
- The initial 121-check run returned an incomplete Xcode result bundle and is not counted as passing; the results above come from the completed focused batches.

Detail state and view refactoring (2026-10-08):

- The existing `@MainActor @Observable` state remains owned by the screen. Tab selection and refresh identities use concrete enums/structs; short forms still use one sheet. Profile actions and installation setup are split into named sections without introducing a second navigation or networking layer.
- A mutation stays busy until its authoritative refresh finishes. SIM changes and modem removal invalidate associated caches; partial failures on the same SIM preserve successful data. Connection reads discard older responses, and activation/deletion revalidate the current profile.
- View-owned operations and Websheet callbacks have individual request identities. Old download cancellations and SIM dismissals cannot clear a replacement session. Transfer profile results are tied to the queried source and IMEI; editing the IMEI requires another read before transfer can start. Scanner availability uses an enum and its coordinator ignores duplicate recognition after delivering a valid code.
- Ten additional fixture regressions cover these races and stale-state boundaries. Swift 6 application/test targets built successfully with no reported errors. The complete iPhone suite passed **116/116** (88 unit and 28 UI tests) in `Test-Sigmo-2026.10.08_01-04-35-+0800.xcresult`.
- A final lifecycle correction keeps the PIN form open while its unlock task refreshes associated data. After that correction, all 30 Detail unit tests plus the iPhone PIN UI journey passed **31/31** in `Test-Sigmo-2026.10.08_01-17-51-+0800.xcresult`. These are follow-up checks of the changed code, not additional distinct tests.
- The current 12-check iPad run (all Detail journeys plus adaptive card/title bounds) lost its MCP connection before returning results. The subsequent gateway health request timed out; **no pass/fail result is available for that run**. Resume these iPad checks when the gateway is reachable. Do not count the interrupted run as passing.
- The local resource checker passes (560 bilingual keys), as do whitespace/conflict-marker checks of the changed files. Fixture UI screenshots remain attached to their Xcode result bundles; real camera scanning still requires a physical device.

Earlier native Detail verification (2026-10-07/08), using iOS 26.5 fixtures only:

- Swift 6 application and test targets build successfully. The local resource check passes with 560 bilingual keys.
- All 78 unit tests passed in `Test-Sigmo-2026.10.07_23-22-12-+0800.xcresult`, including 20 Detail state/contract tests and the local WKWebView callback test. The discovery/back-navigation contract was checked again in `Test-Sigmo-2026.10.07_23-42-40-+0800.xcresult`.
- All 28 distinct iPhone UI tests have passing results across the full UI batch (`Test-Sigmo-2026.10.07_23-26-56-+0800.xcresult`) and focused corrections (`Test-Sigmo-2026.10.07_23-42-40-+0800.xcresult`). These are aggregate results across batches, not a claim that each earlier bundle passed completely.
- The compact profile list, secondary action pages, SE selection, native installation flow, movable button, copy menus, and title bounds passed all 12 focused iPhone checks in `Test-Sigmo-2026.10.07_23-55-04-+0800.xcresult`. Subsequent native form/icon refinements passed their four iPhone journeys across `Test-Sigmo-2026.10.08_00-08-26-+0800.xcresult` and `Test-Sigmo-2026.10.08_00-16-38-+0800.xcresult`; the latter also verifies the final Reminder toolbar and full-row SIM Application interaction.

- The final iPad batch passed 11 of 12 checks in `Test-Sigmo-2026.10.08_00-23-31-+0800.xcresult`. The remaining phone-number automation tried to tap an already-focused field during keyboard presentation; it now types directly into the automatically focused field and captures the editor for visual review.
- Inspection of the iPad screenshots led to layout corrections: summary icon columns scale with Dynamic Type, the storage fallback keeps its leading alignment, and the floating button sits closer to the tab bar. Their first rerun lost the MCP connection. The iPhone checks now pass in the full 2026-10-08 suite above; current iPad verification is still unresolved because the later 12-check run also lost transport. Neither interrupted iPad batch is counted as passing.

Run the local check without Xcode or a real backend:

```sh
python3 ios/scripts/check_settings.py
```

It verifies bilingual key parity, strings-file syntax, settings UI/schema localization coverage, and fixture field/control parity with the Go schema. Debug settings fixtures execute writes, OTP tests, key operations, and simulated updates in memory.

Current backend/IMS refinement (2026-10-07):

- Both modem API responses include VoLTE enablement and connection flags. Public Go and Pro (`esim_transfer,ims`) suites pass; Web unit tests pass (489 tests), and TypeScript checking passes.
- The combined iOS app and test targets compile. All 33 foundation unit tests pass on iOS 26.5 iPhone, including IMS null/disabled states, backend isolation, stale responses, storage, phone formatting, and refresh limits (`Test-Sigmo-2026.10.07_20-52-35-+0800.xcresult`).
- All 11 foundation UI journeys passed on iOS 26.5 iPhone after two detail-accessibility assertions were updated, with the large-text carrier/status row fixed (`Test-Sigmo-2026.10.07_20-53-23-+0800.xcresult` plus the two passing reruns in `Test-Sigmo-2026.10.07_21-01-59-+0800.xcresult`). All 11 also passed on iPad (`Test-Sigmo-2026.10.07_21-05-02-+0800.xcresult`). The subsequent toolbar-symbol refinement compiles and its native symbol is available. These cover single/multiple-backend navigation, edit/settings/details, airplane/VoLTE icons, unified IMS rows, long names, English/Chinese, dark appearance, maximum Dynamic Type, and diagnostics.
- The development service was restarted with the documented command. Read-only checks confirm the new boolean fields on all four device-list entries and a matching device-detail response; OTP delivery and activation remain fixture-only.

Earlier Xcode verification:

| Check                                     | Result                                                                                                                                   |
| ----------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------- |
| `Sigmo iPhone 26 Verification` — iOS 26.5 | All 39 unit/UI tests passed                                                                                                              |
| `Sigmo iPad 26 Verification` — iOS 26.5   | All 39 unit/UI tests passed, including all 8 UI journeys, adaptive columns, dropdown actions, and large-text home/details                |
| Real development backend                  | Decoded authorized Pro status, all three capabilities, four devices, and a device detail using the production `BackendAPI` over LAN HTTP |
| Localization and build                    | English/Chinese key parity; application and test targets compile with Swift 6                                                            |

The earlier foundation iPhone result is `Test-Sigmo-2026.10.07_19-33-48-+0800.xcresult`; the iPad result is `Test-Sigmo-2026.10.07_19-28-25-+0800.xcresult`. After integrating the parallel native settings work, the menu navigation and settings/VoLTE fixture-isolation checks also passed in `Test-Sigmo-2026.10.07_19-55-30-+0800.xcresult`. They remain in the Mac's MCP action artifacts/Xcode results. UI screenshots are attached to the results; screenshot copies are also written to the UI runner's temporary directory for inspection.

Text-clipping and element-description audits cover the home and detail screens. One-line device-name truncation is intentional; clipping issues for identified name elements are excluded, with their full accessibility text and one-line height asserted separately. On the long-name scenario and at the largest accessibility size, XCTest also reports clipping without an identifiable element on the home screen. That audit is inconclusive: these scenarios check card/number bounds, full accessibility labels, single-line name height, and element descriptions, with screenshots for review. Detail-screen clipping audits remain enabled. The optional XCTest contrast audit produced unidentified `SwiftUI.AccessibilityNode` reports on both the previous populated home screen and the stock empty state, without an element or measurable contrast details. This check is **inconclusive**, not counted as passing; screenshots were inspected and a further contrast review remains appropriate before distribution. It is excluded from the automated regression suite rather than silently filtering individual failures.

Real-service checks use only GET requests; OTP delivery and Pro activation are exercised with local fixtures. Physical-device signing and installation remain separate checks. Existing stale simulator entries had missing data directories, so dedicated iPhone/iPad 26.5 verification simulators were created without erasing existing devices.

Start the development backend, when needed, with the project command:

```sh
./scripts/dev.sh --listen-address=0.0.0.0:9527 --db-path=build/sigmo-dev.db --debug
```

The script changes into `pro/`, so this explicit relative database path resolves to `pro/build/sigmo-dev.db`. From the development Mac, use `http://10.10.10.101:9527`; `0.0.0.0` is the server's listen address, not a client destination.

Run the workflow tests from the repository root:

```sh
uv run --with fastmcp==4.1.0 python -m unittest discover -s scripts/ios -p 'test_*.py' -v
```

These tests use temporary repositories and a local HTTP server. They do not operate a modem, contact a carrier, send notifications, or require Xcode.
