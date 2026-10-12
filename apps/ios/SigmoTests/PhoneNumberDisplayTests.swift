import XCTest
@testable import Sigmo

@MainActor
final class PhoneNumberDisplayTests: XCTestCase {
    func testFormatsUsingTheSIMRegionAndKeepsUnparseableValues() {
        let cases: [(String, String?, String)] = [
            ("64210647889", "NZ", "+64 210 647 889"),
            ("+64210647889", "NZ", "+64 210 647 889"),
            ("0210647889", "NZ", "+64 210 647 889"),
            ("447386042885", "GB", "+44 7386 042885"),
            ("07386042885", "GB", "+44 7386 042885"),
            ("12245556666", "US", "+1 (224) 555-6666"),
            ("2245556666", "US", "+1 (224) 555-6666"),
            ("+12245556666", "US", "+1 (224) 555-6666"),
            ("8613800000000", "CN", "+86 138 0000 0000"),
            ("+8613800000000", "CN", "+86 138 0000 0000"),
            ("13800000000", " cn ", "+86 138 0000 0000"),
            ("+12025550123", "US", "+1 (202) 555-0123"),
            ("02070313000", "GB", "+44 20 7031 3000"),
            ("+442070313000", "CN", "+44 20 7031 3000"),
            ("+442070313000", nil, "+44 20 7031 3000"),
            ("02070313000", nil, "02070313000"),
            ("02070313000", "XX", "02070313000"),
            ("  unknown  ", "CN", "unknown"),
            ("10086", "CN", "10086"),
            ("0118613800138000", "US", "+86 138 0013 8000"),
            ("*123#", "US", "*123#"),
            ("*13800000000#", "CN", "*13800000000#"),
            ("BANK123", "US", "BANK123"),
            ("BANK2025550123", "US", "BANK2025550123"),
            ("1-800-FLOWERS", "US", "1-800-FLOWERS"),
            ("10690760295102", "CN", "10690760295102"),
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
