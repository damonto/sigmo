import XCTest
@testable import Sigmo

@MainActor
final class CallWebRTCTests: XCTestCase {
    func testRecordingDelegatesReachTheNativeAudioProcessingAdapters() {
        let capture = CallPCMTap()
        let render = CallPCMTap()
        let processing = CallWebRTC.processing(capture: capture, render: render)
        // Read through the SDK's native getters, not a Swift-side stored copy.
        // Passing these delegates only to the M150 initializer returns nil here.
        XCTAssertTrue(processing.capturePostProcessingDelegate === capture)
        XCTAssertTrue(processing.renderPreProcessingDelegate === render)
    }

    func testFrameworkSupportsBackendOpusAndPCMUCodecs() {
        let factory = CallWebRTC.factory(capture: CallPCMTap(), render: CallPCMTap())
        let codecs = factory.rtpSenderCapabilities(forKind: "audio").codecs
        XCTAssertTrue(codecs.contains { $0.name == "PCMU" && $0.clockRate?.intValue == 8000 })
        XCTAssertTrue(codecs.contains { $0.name.lowercased() == "opus" && $0.clockRate?.intValue == 48000 })
    }
}
