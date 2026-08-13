import type { ActiveEntitlementPage, DevicePage } from "./database";
import {
  boldMarkdownV2,
  codeMarkdownV2,
  escapeMarkdownV2,
} from "./telegram_markdown";
import {
  devicePageCallback,
  entitlementPageCallback,
} from "./telegram_pagination";

const identityDisplayLimit = 64;

export type TelegramInlineKeyboard = Array<
  Array<
    | { text: string; url: string }
    | { text: string; callback_data: string }
  >
>;

export type TelegramReply = {
  text: string;
  replyMarkup?: {
    inline_keyboard: TelegramInlineKeyboard;
  };
};

export function titledMessage(title: string, blocks: string[]): string {
  return [boldMarkdownV2(title), ...blocks].join("\n\n");
}

export function fieldMarkdownV2(
  label: string,
  value: string,
  format: "text" | "code" = "text",
): string {
  const renderedValue =
    format === "code" ? codeMarkdownV2(value) : escapeMarkdownV2(value);
  return `${boldMarkdownV2(label)}\n${renderedValue}`;
}

export function inlineFieldMarkdownV2(label: string, value: string): string {
  return `${boldMarkdownV2(label)}: ${codeMarkdownV2(value)}`;
}

export function markdownReply(
  text: string,
  inlineKeyboard?: TelegramInlineKeyboard,
): TelegramReply {
  if (!inlineKeyboard) return { text };
  return { text, replyMarkup: { inline_keyboard: inlineKeyboard } };
}

export function usageMessage(
  title: string,
  commands: string[],
  note?: string,
): string {
  const blocks = [commands.map(codeMarkdownV2).join("\n")];
  if (note) blocks.push(escapeMarkdownV2(note));
  return titledMessage(title, blocks);
}

export function truncatedSingleLine(value: string, limit: number): string {
  const compact = value.replace(/\s+/g, " ").trim();
  const codePoints = Array.from(compact);
  if (codePoints.length <= limit) return compact;
  if (limit <= 3) return ".".repeat(Math.max(limit, 0));
  return `${codePoints.slice(0, limit - 3).join("")}...`;
}

function pageNavigation(
  page: number,
  pageCount: number,
  callbackData: (page: number) => string,
): TelegramInlineKeyboard | undefined {
  const navigation: TelegramInlineKeyboard[number] = [];
  if (page > 1)
    navigation.push({
      text: "Previous",
      callback_data: callbackData(page - 1),
    });
  if (page < pageCount)
    navigation.push({ text: "Next", callback_data: callbackData(page + 1) });
  return navigation.length > 0 ? [navigation] : undefined;
}

export function renderEntitlementPage(
  result: ActiveEntitlementPage,
  page: number,
  pageCount: number,
): TelegramReply {
  if (result.total === 0)
    return markdownReply(
      titledMessage("Active entitlements", [
        escapeMarkdownV2("No active entitlements."),
      ]),
    );
  const sections = result.rows.map((row) => {
    const displayName =
      truncatedSingleLine(row.displayName, identityDisplayLimit) ||
      "Not paired";
    const username = truncatedSingleLine(row.username, identityDisplayLimit);
    const fields = [
      inlineFieldMarkdownV2("Telegram ID", String(row.telegramId)),
      inlineFieldMarkdownV2(
        "Devices",
        `${row.activeDevices} / ${row.maxDevices}`,
      ),
      inlineFieldMarkdownV2(
        "Expires",
        row.expiresAt?.slice(0, 10) ?? "Never",
      ),
    ];
    if (username)
      fields.splice(1, 0, inlineFieldMarkdownV2("Username", `@${username}`));
    return [boldMarkdownV2(displayName), ...fields].join("\n");
  });
  const header = titledMessage("Active entitlements", [
    inlineFieldMarkdownV2("Page", `${page} / ${pageCount}`),
    inlineFieldMarkdownV2("Total", String(result.total)),
  ]);
  return markdownReply(
    [header, ...sections].join("\n\n"),
    pageNavigation(page, pageCount, entitlementPageCallback),
  );
}

export function renderDevicePage(
  result: DevicePage,
  telegramID: number,
  page: number,
  pageCount: number,
): TelegramReply {
  if (result.total === 0)
    return markdownReply(
      titledMessage("Linked devices", [
        inlineFieldMarkdownV2("Telegram ID", String(telegramID)),
        escapeMarkdownV2("No linked devices."),
      ]),
    );
  const sections = result.rows.map((row) =>
    [
      boldMarkdownV2(row.revokedAt ? "Revoked device" : "Active device"),
      inlineFieldMarkdownV2("Device ID", row.deviceId),
      inlineFieldMarkdownV2("Last seen", row.lastSeenAt),
    ].join("\n"),
  );
  const header = titledMessage("Linked devices", [
    inlineFieldMarkdownV2("Telegram ID", String(telegramID)),
    inlineFieldMarkdownV2("Page", `${page} / ${pageCount}`),
    inlineFieldMarkdownV2("Total", String(result.total)),
  ]);
  return markdownReply(
    [header, ...sections].join("\n\n"),
    pageNavigation(page, pageCount, (targetPage) =>
      devicePageCallback(telegramID, targetPage),
    ),
  );
}
