import Contacts
import XCTest
@testable import Sigmo

@MainActor
final class CallContactsTests: XCTestCase {
    func testLimitedContactsAndPermissionLoss() async {
        let permission = ContactPermissionFixture(.limited)
        let contacts = CallContacts(status: { permission.value }, readNames: {
            [CallContactName(number: "+8613800138000", name: "小明")]
        })
        await contacts.reload()
        XCTAssertEqual(contacts.name(for: "138 0013 8000", region: "CN"), "小明")
        XCTAssertNil(contacts.name(for: "+8613900139000", region: "CN"))
        permission.value = .denied
        await contacts.reload()
        XCTAssertTrue(contacts.names.isEmpty)
        XCTAssertNil(contacts.name(for: "13800138000", region: "CN"))
    }

    func testLateFullAccessReadCannotRestoreUnselectedContacts() async {
        let permission = ContactPermissionFixture(.authorized)
        let gate = SettingsRequestGate()
        let source = ContactNamesFixture(gate: gate)
        let contacts = CallContacts(status: { permission.value }, readNames: { await source.read() })
        let previous = Task { await contacts.reload() }
        await gate.waitForCaller()
        permission.value = .limited
        await contacts.reload()
        await gate.release()
        await previous.value
        XCTAssertEqual(contacts.authorization, .limited)
        XCTAssertEqual(contacts.names.count, 1)
        XCTAssertNil(contacts.name(for: "13900139000", region: "CN"))
    }

    func testCancelledReloadDoesNotReadContacts() async {
        let contacts = CallContacts(status: { .authorized }, readNames: {
            XCTFail("A canceled reload must not start reading the address book")
            return []
        })
        let task = Task { await contacts.reload() }
        task.cancel()
        await task.value
        XCTAssertTrue(contacts.names.isEmpty)
        XCTAssertNil(contacts.failure)
    }
}

@MainActor
private final class ContactPermissionFixture {
    var value: CNAuthorizationStatus
    init(_ value: CNAuthorizationStatus) { self.value = value }
}

private actor ContactNamesFixture {
    let gate: SettingsRequestGate
    private var reads = 0
    init(gate: SettingsRequestGate) { self.gate = gate }

    func read() async -> [CallContactName] {
        reads += 1
        let selected = CallContactName(number: "+8613800138000", name: "小明")
        if reads == 1 {
            await gate.wait()
            return [selected, CallContactName(number: "+8613900139000", name: "Removed contact")]
        }
        return [selected]
    }
}
