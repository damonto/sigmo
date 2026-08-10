import type { RequestContext } from "./context";
import { secureEqual } from "./crypto";
import { isActive } from "./license";
import { apiError, json, logError, nowISO, parseJSON } from "./http";
import type { TelegramUser } from "./types";
import {
  isRFC3339,
  telegramUpdate as parseTelegramUpdate,
} from "./validation";

export function displayName(user: TelegramUser): string {
  return (
    [user.first_name, user.last_name].filter(Boolean).join(" ").trim() ||
    String(user.id)
  );
}

export function admins(env: Env): Set<number> {
  return new Set(
    env.SIGMO_ADMIN_TELEGRAM_IDS.split(",")
      .map((value) => Number(value.trim()))
      .filter((value) => Number.isSafeInteger(value) && value > 0),
  );
}

function positiveInteger(value: string | undefined): number | undefined {
  if (!value || !/^\d+$/.test(value)) return undefined;
  const parsed = Number(value);
  return Number.isSafeInteger(parsed) && parsed > 0 ? parsed : undefined;
}

const grantUsage =
  "Usage: /grant <telegram_id> [max_devices] [expires_at]\nexpires_at must use YYYY-MM-DD.";

function parseExpiryEndOfDayUTC(value: string): Date | null {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return null;
  const timestamp = `${value}T23:59:59.999Z`;
  return isRFC3339(timestamp) ? new Date(timestamp) : null;
}

async function activatePairing(
  context: RequestContext,
  pairingID: string,
  user: TelegramUser,
): Promise<string> {
  const { db } = context;
  const pairing = await db.findPairing(pairingID);
  if (
    !pairing ||
    pairing.status !== "pending" ||
    new Date(pairing.expiresAt) <= new Date()
  )
    return "Pairing code is invalid or expired. Create a new pairing in Sigmo.";
  const entitlement = await db.findEntitlement(user.id);
  if (!isActive(entitlement))
    return "This Telegram account does not have an active Sigmo Pro entitlement.";
  const timestamp = nowISO();
  const activated = await db.activatePairing({
    pairingId: pairingID,
    deviceId: pairing.deviceId,
    publicKey: pairing.publicKey,
    telegramId: user.id,
    displayName: displayName(user),
    username: user.username ?? "",
    timestamp,
  });
  if (!activated) {
    const currentPairing = await db.findPairing(pairingID);
    if (
      !currentPairing ||
      currentPairing.status !== "pending" ||
      new Date(currentPairing.expiresAt) <= new Date()
    ) {
      return "Pairing code is invalid or expired. Create a new pairing in Sigmo.";
    }
    const currentEntitlement = await db.findEntitlement(user.id);
    if (!isActive(currentEntitlement))
      return "This Telegram account does not have an active Sigmo Pro entitlement.";
    const currentDevice = await db.findDevice(pairing.deviceId);
    if (
      currentDevice &&
      !currentDevice.revokedAt &&
      currentDevice.telegramId !== user.id
    )
      return "This device is linked to another Telegram account. The current owner must revoke it before it can be linked again.";
    return `Device limit reached (${currentEntitlement.maxDevices}). Revoke an existing device first.`;
  }
  return `Sigmo Pro authorized device ${pairing.deviceId}. You can now return to Sigmo.`;
}

async function grantCommand(
  context: RequestContext,
  args: string[],
): Promise<string> {
  if (args.length < 1 || args.length > 3) return grantUsage;
  const telegramID = positiveInteger(args[0]);
  const maxDevices = args[1] === undefined ? 3 : positiveInteger(args[1]);
  const expiresAtValue = args[2];
  const expiresAt = expiresAtValue
    ? parseExpiryEndOfDayUTC(expiresAtValue)
    : null;
  if (
    telegramID === undefined ||
    maxDevices === undefined ||
    (expiresAtValue !== undefined &&
      (!expiresAt || expiresAt.getTime() <= Date.now()))
  ) {
    return grantUsage;
  }
  const timestamp = nowISO();
  await context.db.upsertEntitlement({
    telegramId: telegramID,
    expiresAt: expiresAt?.toISOString() ?? null,
    maxDevices,
    timestamp,
  });
  return `Granted entitlement to ${telegramID} for up to ${maxDevices} devices.`;
}

async function revokeCommand(
  context: RequestContext,
  args: string[],
): Promise<string> {
  if (args.length !== 1) return "Usage: /revoke <telegram_id>";
  const telegramID = positiveInteger(args[0]);
  if (telegramID === undefined) return "Usage: /revoke <telegram_id>";
  const timestamp = nowISO();
  await context.db.revokeEntitlement(telegramID, timestamp);
  return `Revoked entitlement for ${telegramID}.`;
}

async function statusCommand(
  context: RequestContext,
  args: string[],
): Promise<string> {
  if (args.length !== 1) return "Usage: /status <telegram_id>";
  const telegramID = positiveInteger(args[0]);
  if (telegramID === undefined) return "Usage: /status <telegram_id>";
  const row = await context.db.findEntitlement(telegramID);
  return row
    ? `${row.telegramId}: ${row.status}, up to ${row.maxDevices} devices, expires: ${row.expiresAt ?? "never"}`
    : "No entitlement found.";
}

function singleLine(value: string): string {
  return value.replace(/\s+/g, " ").trim();
}

async function entitlementsCommand(
  context: RequestContext,
  args: string[],
): Promise<string> {
  if (args.length !== 0) return "Usage: /entitlements";
  const rows = await context.db.listActiveEntitlements(nowISO());
  if (!rows.length) return "No active entitlements.";
  const lines = rows.map((row) => {
    const displayName = singleLine(row.displayName);
    const username = singleLine(row.username);
    const identity =
      [displayName, username ? `@${username}` : ""].filter(Boolean).join(" ") ||
      "not paired";
    const expiresAt = row.expiresAt?.slice(0, 10) ?? "never";
    return `${row.telegramId} | ${identity} | devices ${row.activeDevices}/${row.maxDevices} | expires ${expiresAt}`;
  });
  return [`Active entitlements (${rows.length}):`, ...lines].join("\n");
}

async function devicesCommand(
  context: RequestContext,
  user: TelegramUser,
  isAdmin: boolean,
  args: string[],
): Promise<string> {
  if (isAdmin ? args.length > 1 : args.length > 0)
    return isAdmin ? "Usage: /devices [telegram_id]" : "Usage: /devices";
  const telegramID = isAdmin && args[0] ? positiveInteger(args[0]) : user.id;
  if (telegramID === undefined) return "Usage: /devices <telegram_id>";
  const rows = await context.db.listDevices(telegramID);
  if (!rows.length) return "No linked devices.";
  return rows
    .map(
      (row) =>
        `${row.deviceId} | last seen ${row.lastSeenAt} | ${row.revokedAt ? "revoked" : "active"}`,
    )
    .join("\n");
}

async function revokeDeviceCommand(
  context: RequestContext,
  user: TelegramUser,
  isAdmin: boolean,
  args: string[],
): Promise<string> {
  if (args.length !== 1) return "Usage: /revoke_device <device_id>";
  const deviceID = args[0] ?? "";
  if (!/^[0-9a-f]{32}$/.test(deviceID))
    return "Usage: /revoke_device <device_id>";
  const device = await context.db.findDevice(deviceID);
  if (!device || (!isAdmin && device.telegramId !== user.id))
    return "Device does not exist or you are not allowed to revoke it.";
  await context.db.revokeDevice(device.deviceId, nowISO());
  return `Revoked device ${device.deviceId}.`;
}

async function botCommand(
  context: RequestContext,
  user: TelegramUser,
  command: string,
  args: string[],
): Promise<string> {
  const isAdmin = admins(context.env).has(user.id);
  switch (command) {
    case "/grant":
      if (isAdmin) return grantCommand(context, args);
      break;
    case "/revoke":
      if (isAdmin) return revokeCommand(context, args);
      break;
    case "/status":
      if (isAdmin) return statusCommand(context, args);
      break;
    case "/entitlements":
      if (isAdmin) return entitlementsCommand(context, args);
      break;
    case "/devices":
      return devicesCommand(context, user, isAdmin, args);
    case "/revoke_device":
      return revokeDeviceCommand(context, user, isAdmin, args);
  }
  return isAdmin
    ? "Available commands: /grant, /revoke, /status, /entitlements, /devices, /revoke_device"
    : "Available commands: /devices, /revoke_device <device_id>";
}

// Telegram accepts up to 4096 characters. Counting UTF-16 code units and
// stopping at 4000 keeps messages conservative for non-BMP characters.
const telegramMessageLimit = 4000;

function splitTelegramText(text: string): string[] {
  const messages: string[] = [];
  let remaining = text;
  while (remaining.length > telegramMessageLimit) {
    let end = remaining.lastIndexOf("\n", telegramMessageLimit);
    if (end <= 0) end = telegramMessageLimit;
    messages.push(remaining.slice(0, end));
    remaining = remaining.slice(end);
    if (remaining.startsWith("\n")) remaining = remaining.slice(1);
  }
  if (remaining || messages.length === 0) messages.push(remaining);
  return messages;
}

async function sendTelegram(
  env: Env,
  chatID: number,
  text: string,
): Promise<void> {
  for (const message of splitTelegramText(text)) {
    const response = await fetch(
      `https://api.telegram.org/bot${env.SIGMO_TELEGRAM_BOT_TOKEN}/sendMessage`,
      {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ chat_id: chatID, text: message }),
      },
    );
    if (!response.ok)
      throw new Error(`Telegram sendMessage returned HTTP ${response.status}`);
  }
}

export async function telegramWebhook(
  request: Request,
  context: RequestContext,
): Promise<Response> {
  const { env, execution } = context;
  const providedSecret =
    request.headers.get("x-telegram-bot-api-secret-token") ?? "";
  if (
    !providedSecret ||
    !env.SIGMO_TELEGRAM_WEBHOOK_SECRET ||
    !(await secureEqual(providedSecret, env.SIGMO_TELEGRAM_WEBHOOK_SECRET))
  )
    return apiError(
      "telegram_webhook_unauthorized",
      "invalid webhook secret",
      403,
    );
  const update = parseTelegramUpdate(await parseJSON(request));
  if (!update)
    return apiError("telegram_update_invalid", "invalid Telegram update", 400);
  const message = update.message;
  if (!message?.from || !message.text) return json({ ok: true });
  if (message.chat.type !== "private" || message.chat.id !== message.from.id)
    return json({ ok: true });

  const [rawCommand, ...args] = message.text.trim().split(/\s+/);
  const commandParts = rawCommand.toLowerCase().split("@");
  if (
    commandParts.length > 2 ||
    (commandParts[1] !== undefined &&
      commandParts[1] !== env.BOT_USERNAME.toLowerCase())
  )
    return json({ ok: true });
  const command = commandParts[0];
  const reply =
    command === "/start"
      ? args.length === 1
        ? await activatePairing(context, args[0], message.from)
        : "Open the pairing link from the Sigmo activation page."
      : await botCommand(context, message.from, command, args);
  execution.waitUntil(
    sendTelegram(env, message.chat.id, reply).catch((caught: unknown) => {
      logError("send Telegram reply", caught, { chatId: message.chat.id });
    }),
  );
  return json({ ok: true });
}
