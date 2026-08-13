export type TelegramPageCallback =
  | { resource: "entitlements"; page: number }
  | { resource: "devices"; telegramID: number; page: number };

export function entitlementPageCallback(page: number): string {
  return `page:entitlements:${page}`;
}

export function devicePageCallback(
  telegramID: number,
  page: number,
): string {
  return `page:devices:${telegramID}:${page}`;
}

export function parseTelegramPageCallback(
  value: string,
): TelegramPageCallback | null {
  const parts = value.split(":");
  if (parts[0] !== "page") return null;

  if (parts.length === 3 && parts[1] === "entitlements") {
    const page = positiveInteger(parts[2]);
    return page === null ? null : { resource: "entitlements", page };
  }
  if (parts.length === 4 && parts[1] === "devices") {
    const telegramID = positiveInteger(parts[2]);
    const page = positiveInteger(parts[3]);
    if (telegramID === null || page === null) return null;
    return { resource: "devices", telegramID, page };
  }
  return null;
}

function positiveInteger(value: string | undefined): number | null {
  if (!value || !/^\d+$/.test(value)) return null;
  const parsed = Number(value);
  return Number.isSafeInteger(parsed) && parsed > 0 ? parsed : null;
}
