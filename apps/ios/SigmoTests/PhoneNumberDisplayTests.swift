import XCTest
@testable import Sigmo

@MainActor
final class PhoneNumberDisplayTests: XCTestCase {
    func testFormatsUsingTheSIMRegionAndKeepsUnparseableValues() {
        let cases: [(String, String?, String)] = [
            ("+8613800000000", "CN", "138 0000 0000"),
            ("13800000000", " cn ", "138 0000 0000"),
            ("+12025550123", "US", "(202) 555-0123"),
            ("02070313000", "GB", "020 7031 3000"),
            ("+442070313000", "CN", "+44 20 7031 3000"),
            ("+442070313000", nil, "+44 20 7031 3000"),
            ("02070313000", nil, "02070313000"),
            ("02070313000", "XX", "02070313000"),
            ("  unknown  ", "CN", "unknown"),
            ("10086", "CN", "10086"),
            ("*123#", "US", "*123#"),
            ("", nil, ""),
        ]
        for (input, region, expected) in cases {
            XCTAssertEqual(PhoneNumberDisplay.format(input, regionCode: region), expected, "\(input), \(region ?? "none")")
        }
    }

    func testRadioGlyphDoesNotAssumeAnUnknownTechnologyIs4G() {
        for (input, expected) in [("LTE", "4G"), ("5gnr", "5G"), ("UMTS", "3G"), ("EDGE", "2G"), ("future", "?"), ("", "?")] {
            XCTAssertEqual(ModemStatusIcons.radioGlyph(input), expected)
        }
    }
}
