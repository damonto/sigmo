import { describe, expect, it } from "vitest";

import {
  renderDevicePage,
  renderEntitlementPage,
  truncatedSingleLine,
  usageMessage,
} from "../src/telegram_messages";

describe("Telegram messages", () => {
  it.each([
    {
      name: "normalizes whitespace",
      input: "Alice\n  Admin",
      limit: 20,
      want: "Alice Admin",
    },
    {
      name: "truncates by Unicode code point",
      input: "A😀B😀C",
      limit: 5,
      want: "A😀B😀C",
    },
    {
      name: "keeps emoji intact at the boundary",
      input: "A😀B😀C",
      limit: 4,
      want: "A...",
    },
    {
      name: "honors limits shorter than the ellipsis",
      input: "Alice",
      limit: 2,
      want: "..",
    },
  ])("$name", ({ input, limit, want }) => {
    expect(truncatedSingleLine(input, limit)).toBe(want);
  });

  it("formats command usage as copyable code", () => {
    expect(usageMessage("Devices usage", ["/devices [telegram_id]"])).toBe(
      "*Devices usage*\n\n`/devices [telegram_id]`",
    );
  });

  it("renders entitlement rows and navigation", () => {
    const reply = renderEntitlementPage(
      {
        rows: [
          {
            telegramId: 1001,
            displayName: "Alice *Admin*",
            username: "alice_admin",
            activeDevices: 1,
            maxDevices: 3,
            expiresAt: null,
          },
        ],
        total: 11,
      },
      1,
      2,
    );

    expect(reply.text).toContain("*Alice \\*Admin\\**");
    expect(reply.text).toContain("*Username*: `@alice_admin`");
    expect(reply.replyMarkup?.inline_keyboard).toEqual([
      [{ text: "Next", callback_data: "page:entitlements:2" }],
    ]);
  });

  it("renders device rows and navigation", () => {
    const reply = renderDevicePage(
      {
        rows: [
          {
            deviceId: "00000000000000000000000000001001",
            lastSeenAt: "2026-08-13T00:00:00.000Z",
            revokedAt: null,
          },
        ],
        total: 12,
      },
      1001,
      2,
      2,
    );

    expect(reply.text).toContain("*Page*: `2 / 2`");
    expect(reply.replyMarkup?.inline_keyboard).toEqual([
      [
        {
          text: "Previous",
          callback_data: "page:devices:1001:1",
        },
      ],
    ]);
  });
});
