import { describe, expect, it } from "vitest";

import {
  devicePageCallback,
  entitlementPageCallback,
  parseTelegramPageCallback,
} from "../src/telegram_pagination";

describe("Telegram pagination callbacks", () => {
  it.each([
    {
      name: "entitlement page",
      value: entitlementPageCallback(2),
      want: { resource: "entitlements", page: 2 },
    },
    {
      name: "device page",
      value: devicePageCallback(4_000_000_001, 3),
      want: { resource: "devices", telegramID: 4_000_000_001, page: 3 },
    },
  ])("round-trips $name", ({ value, want }) => {
    expect(parseTelegramPageCallback(value)).toEqual(want);
  });

  it.each([
    "",
    "page:entitlements:0",
    "page:entitlements:2:extra",
    "page:devices:1001",
    "page:devices:1001:-1",
    "page:devices:not-a-number:2",
  ])("rejects malformed data %s", (value) => {
    expect(parseTelegramPageCallback(value)).toBeNull();
  });
});
