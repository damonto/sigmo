import type { RequestContext } from "./context";
import { secureEqual } from "./crypto";
import { isActive } from "./license";
import { apiError, json, logError, nowISO, parseJSON } from "./http";
import { createBootstrapDownloads } from "./releases";
import { admins, availableCommands } from "./telegram_commands";
import type { ReleaseChannel, TelegramUser } from "./types";
import {
  isRFC3339,
  telegramUpdate as parseTelegramUpdate,
} from "./validation";

type TelegramInlineKeyboard = Array<
  Array<{
    text: string;
    url: string;
  }>
>;

type TelegramReply = {
  text: string;
  replyMarkup?: {
    inline_keyboard: TelegramInlineKeyboard;
  };
};

const bootstrapTargets = [
  { target: "linux-amd64", label: "amd64" },
  { target: "linux-amd64-musl", label: "amd64 (musl)" },
  { target: "linux-arm64", label: "arm64" },
  { target: "linux-arm64-musl", label: "arm64 (musl)" },
  { target: "linux-arm", label: "arm" },
  { target: "linux-arm-musl", label: "arm (musl)" },
] as const;

const downloadUsage = [
  "Usage:",
  "/download [stable|dev]",
  "/download <stable|dev> <target>",
  `Targets: ${bootstrapTargets.map(({ target }) => target).join(", ")}`,
].join("\n");

export function displayName(user: TelegramUser): string {
  return (
    [user.first_name, user.last_name].filter(Boolean).join(" ").trim() ||
    String(user.id)
  );
}

function positiveInteger(value: string | undefined): number | undefined {
  if (!value || !/^\d+$/.test(value)) return undefined;
  const parsed = Number(value);
  return Number.isSafeInteger(parsed) && parsed > 0 ? parsed : undefined;
}

function textReply(text: string): TelegramReply {
  return { text };
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

async function startCommand(
  context: RequestContext,
  user: TelegramUser,
  args: string[],
): Promise<TelegramReply> {
  if (args.length === 1)
    return textReply(await activatePairing(context, args[0], user));
  if (args.length > 1) return textReply("Usage: /start [pairing_id]");

  const entitlement = await context.db.findEntitlement(user.id);
  const status = isActive(entitlement) ? "active" : "inactive";
  return textReply(
    [
      `Telegram ID: ${user.id}`,
      `Sigmo Pro entitlement: ${status}`,
      "Stable download: /download stable",
      "Dev download: /download dev",
    ].join("\n"),
  );
}

function parseDownloadArgs(
  args: string[],
): { channel: ReleaseChannel; target?: string } | null {
  if (args.length === 0) return { channel: "stable" };
  const channel = args[0]?.toLowerCase();
  if (channel !== "stable" && channel !== "dev") return null;
  if (args.length === 1) return { channel };
  if (args.length !== 2) return null;
  const target = args[1]?.toLowerCase();
  if (!bootstrapTargets.some((candidate) => candidate.target === target))
    return null;
  return { channel, target };
}

function channelName(channel: ReleaseChannel): string {
  return channel === "stable" ? "Stable" : "Dev";
}

async function downloadCommand(
  request: Request,
  context: RequestContext,
  user: TelegramUser,
  args: string[],
): Promise<TelegramReply> {
  const parsed = parseDownloadArgs(args);
  if (!parsed) return textReply(downloadUsage);
  const selectedTargets = parsed.target
    ? bootstrapTargets.filter(({ target }) => target === parsed.target)
    : bootstrapTargets;
  const result = await createBootstrapDownloads(request, context, {
    telegramId: user.id,
    channel: parsed.channel,
    targets: selectedTargets.map(({ target }) => target),
  });
  if (!result.ok) {
    if (result.reason === "entitlement_inactive")
      return textReply(
        "This Telegram account does not have an active Sigmo Pro entitlement.",
      );
    if (result.reason === "release_unavailable")
      return textReply(
        `Sigmo Pro ${channelName(parsed.channel)} is not available yet. Please try again later.`,
      );
    return textReply(
      `The Sigmo Pro ${channelName(parsed.channel)} release does not include ${result.target ?? "the requested target"}.`,
    );
  }

  const downloads = new Map(
    result.downloads.map((download) => [download.target, download.url]),
  );
  const buttons = selectedTargets.map(({ target, label }) => ({
    text: label,
    url: downloads.get(target) ?? "",
  }));
  if (buttons.some(({ url }) => !url))
    throw new Error("bootstrap download result is missing a requested target");
  const inlineKeyboard: TelegramInlineKeyboard = [];
  for (let index = 0; index < buttons.length; index += 2)
    inlineKeyboard.push(buttons.slice(index, index + 2));

  return {
    text: [
      `Channel: ${channelName(result.channel)}`,
      `Version: ${result.version}`,
      "Download links expire in 15 minutes.",
      "Downloads are gzip archives. Run gzip -d on the .gz file, then chmod +x the extracted file.",
    ].join("\n"),
    replyMarkup: { inline_keyboard: inlineKeyboard },
  };
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
  if (telegramID === undefined) return "Usage: /devices [telegram_id]";
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
  request: Request,
  context: RequestContext,
  user: TelegramUser,
  command: string,
  args: string[],
): Promise<TelegramReply> {
  const isAdmin = admins(context.env).has(user.id);
  switch (command) {
    case "/download":
      return downloadCommand(request, context, user, args);
    case "/grant":
      if (isAdmin) return textReply(await grantCommand(context, args));
      break;
    case "/revoke":
      if (isAdmin) return textReply(await revokeCommand(context, args));
      break;
    case "/status":
      if (isAdmin) return textReply(await statusCommand(context, args));
      break;
    case "/entitlements":
      if (isAdmin) return textReply(await entitlementsCommand(context, args));
      break;
    case "/devices":
      return textReply(await devicesCommand(context, user, isAdmin, args));
    case "/revoke_device":
      return textReply(
        await revokeDeviceCommand(context, user, isAdmin, args),
      );
  }
  return textReply(availableCommands(isAdmin));
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
  reply: TelegramReply,
): Promise<void> {
  const messages = splitTelegramText(reply.text);
  for (const [index, message] of messages.entries()) {
    const body: {
      chat_id: number;
      text: string;
      reply_markup?: TelegramReply["replyMarkup"];
    } = { chat_id: chatID, text: message };
    if (index === messages.length - 1 && reply.replyMarkup)
      body.reply_markup = reply.replyMarkup;
    const response = await fetch(
      `https://api.telegram.org/bot${env.SIGMO_TELEGRAM_BOT_TOKEN}/sendMessage`,
      {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify(body),
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
      ? await startCommand(context, message.from, args)
      : await botCommand(request, context, message.from, command, args);
  execution.waitUntil(
    sendTelegram(env, message.chat.id, reply).catch((caught: unknown) => {
      logError("send Telegram reply", caught, { chatId: message.chat.id });
    }),
  );
  return json({ ok: true });
}
