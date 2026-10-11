import type { RequestContext } from '../http/context';
import { secureEqual } from '../security/crypto';
import { isActive } from '../license/service';
import { apiError, json } from '../http/response';
import { logError } from '../observability/log';
import { parseJson } from '../http/body';
import { createBootstrapDownloads } from '../releases/service';
import { telegramAdmins } from './access';
import { sendTelegram } from './client';
import { availableCommands } from './commands';
import {
  boldMarkdownV2,
  codeMarkdownV2,
  escapeMarkdownV2,
  preformattedMarkdownV2,
} from './markdown';
import {
  fieldMarkdownV2,
  inlineFieldMarkdownV2,
  markdownReply,
  titledMessage,
  usageMessage,
} from './messages';
import {
  devicePage,
  entitlementPage,
  handleTelegramPageCallback,
} from './pages';
import type { TelegramInlineKeyboard, TelegramReply } from './messages';
import type { ReleaseChannel } from '../releases/types';
import type { TelegramUser } from './types';
import { isRfc3339 } from '../http/validation';
import { telegramUpdate as parseTelegramUpdate } from './validation';

const BOOTSTRAP_TARGETS = [
  { target: 'linux-amd64', label: 'amd64' },
  { target: 'linux-amd64-musl', label: 'amd64 (musl)' },
  { target: 'linux-arm64', label: 'arm64' },
  { target: 'linux-arm64-musl', label: 'arm64 (musl)' },
  { target: 'linux-arm', label: 'arm' },
  { target: 'linux-arm-musl', label: 'arm (musl)' },
] as const;

const DOWNLOAD_USAGE = titledMessage('Download usage', [
  [
    boldMarkdownV2('Commands'),
    codeMarkdownV2('/download [stable|dev]'),
    codeMarkdownV2('/download <stable|dev> <target>'),
  ].join('\n'),
  [
    boldMarkdownV2('Targets'),
    codeMarkdownV2(BOOTSTRAP_TARGETS.map(({ target }) => target).join(', ')),
  ].join('\n'),
]);

export function displayName(user: TelegramUser): string {
  return (
    [user.first_name, user.last_name].filter(Boolean).join(' ').trim() ||
    String(user.id)
  );
}

function positiveInteger(value: string | undefined): number | undefined {
  if (!value || !/^\d+$/.test(value)) {
    return undefined;
  }
  const parsed = Number(value);
  return Number.isSafeInteger(parsed) && parsed > 0 ? parsed : undefined;
}

const GRANT_USAGE = usageMessage(
  'Grant usage',
  ['/grant <telegram_id> [max_devices] [expires_at]'],
  'expires_at must use YYYY-MM-DD.',
);

function parseExpiryEndOfDayUtc(value: string): Date | null {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) {
    return null;
  }
  const timestamp = `${value}T23:59:59.999Z`;
  return isRfc3339(timestamp) ? new Date(timestamp) : null;
}

async function activatePairing(
  context: RequestContext,
  pairingId: string,
  user: TelegramUser,
): Promise<string> {
  const { db } = context;
  const pairing = await db.findPairing(pairingId);
  if (
    !pairing ||
    pairing.status !== 'pending' ||
    new Date(pairing.expiresAt) <= new Date()
  ) {
    return titledMessage('Pairing unavailable', [
      escapeMarkdownV2(
        'The pairing code is invalid or expired. Create a new pairing in Sigmo.',
      ),
    ]);
  }
  const entitlement = await db.findEntitlement(user.id);
  if (!isActive(entitlement)) {
    return titledMessage('Entitlement required', [
      escapeMarkdownV2(
        'This Telegram account does not have an active Sigmo Pro entitlement.',
      ),
    ]);
  }
  const timestamp = new Date().toISOString();
  const activated = await db.activatePairing({
    pairingId: pairingId,
    deviceId: pairing.deviceId,
    publicKey: pairing.publicKey,
    sessionId: pairing.sessionId,
    refreshTokenHash: pairing.refreshTokenHash,
    fingerprintHash: pairing.fingerprintHash,
    telegramId: user.id,
    displayName: displayName(user),
    username: user.username ?? '',
    timestamp,
  });
  if (!activated) {
    const currentPairing = await db.findPairing(pairingId);
    if (
      !currentPairing ||
      currentPairing.status !== 'pending' ||
      new Date(currentPairing.expiresAt) <= new Date()
    ) {
      return titledMessage('Pairing unavailable', [
        escapeMarkdownV2(
          'The pairing code is invalid or expired. Create a new pairing in Sigmo.',
        ),
      ]);
    }
    const currentEntitlement = await db.findEntitlement(user.id);
    if (!isActive(currentEntitlement)) {
      return titledMessage('Entitlement required', [
        escapeMarkdownV2(
          'This Telegram account does not have an active Sigmo Pro entitlement.',
        ),
      ]);
    }
    const currentDevice = await db.findDevice(pairing.deviceId);
    if (
      currentDevice &&
      !currentDevice.revokedAt &&
      currentDevice.telegramId !== user.id
    ) {
      return titledMessage('Device already linked', [
        escapeMarkdownV2(
          'This device belongs to another Telegram account. The current owner must revoke it before it can be linked again.',
        ),
      ]);
    }
    const currentSession = await db.findDeviceSession(pairing.deviceId);
    if (
      currentDevice &&
      !currentDevice.revokedAt &&
      currentSession &&
      currentSession.fingerprintHash !== pairing.fingerprintHash
    ) {
      return titledMessage('Device identity in use', [
        escapeMarkdownV2(
          'This device identity is bound to another host. Revoke the existing device before pairing it again.',
        ),
      ]);
    }
    return titledMessage('Device limit reached', [
      fieldMarkdownV2(
        'Device limit',
        String(currentEntitlement.maxDevices),
        'code',
      ),
      escapeMarkdownV2('Revoke an existing device before pairing again.'),
    ]);
  }
  return titledMessage('Device authorized', [
    fieldMarkdownV2('Device ID', pairing.deviceId, 'code'),
    escapeMarkdownV2('You can now return to Sigmo.'),
  ]);
}

async function startCommand(
  context: RequestContext,
  user: TelegramUser,
  args: string[],
): Promise<TelegramReply> {
  if (args.length === 1) {
    return markdownReply(await activatePairing(context, args[0], user));
  }
  if (args.length > 1) {
    return markdownReply(usageMessage('Start usage', ['/start [pairing_id]']));
  }

  const entitlement = await context.db.findEntitlement(user.id);
  const status = isActive(entitlement) ? 'Active' : 'Inactive';
  return markdownReply(
    titledMessage('Sigmo Pro', [
      fieldMarkdownV2('Entitlement', status),
      fieldMarkdownV2('Telegram ID', String(user.id), 'code'),
      [
        boldMarkdownV2('Downloads'),
        escapeMarkdownV2('/download'),
        `${boldMarkdownV2('Dev')}: ${codeMarkdownV2('/download dev')}`,
      ].join('\n'),
    ]),
  );
}

function parseDownloadArgs(
  args: string[],
): { channel: ReleaseChannel; target?: string } | null {
  if (args.length === 0) {
    return { channel: 'stable' };
  }
  const channel = args[0]?.toLowerCase();
  if (channel !== 'stable' && channel !== 'dev') {
    return null;
  }
  if (args.length === 1) {
    return { channel };
  }
  if (args.length !== 2) {
    return null;
  }
  const target = args[1]?.toLowerCase();
  if (!BOOTSTRAP_TARGETS.some((candidate) => candidate.target === target)) {
    return null;
  }
  return { channel, target };
}

function channelName(channel: ReleaseChannel): string {
  return channel === 'stable' ? 'Stable' : 'Dev';
}

async function downloadCommand(
  request: Request,
  context: RequestContext,
  user: TelegramUser,
  args: string[],
): Promise<TelegramReply> {
  const parsed = parseDownloadArgs(args);
  if (!parsed) {
    return markdownReply(DOWNLOAD_USAGE);
  }
  const selectedTargets = parsed.target
    ? BOOTSTRAP_TARGETS.filter(({ target }) => target === parsed.target)
    : BOOTSTRAP_TARGETS;
  const result = await createBootstrapDownloads(request, context, {
    telegramId: user.id,
    channel: parsed.channel,
    targets: selectedTargets.map(({ target }) => target),
  });
  if (!result.ok) {
    if (result.reason === 'entitlement_inactive') {
      return markdownReply(
        titledMessage('Entitlement required', [
          escapeMarkdownV2(
            'This Telegram account does not have an active Sigmo Pro entitlement.',
          ),
        ]),
      );
    }
    if (result.reason === 'release_unavailable') {
      return markdownReply(
        titledMessage('Release unavailable', [
          escapeMarkdownV2(
            `Sigmo Pro ${channelName(parsed.channel)} is not available yet. Please try again later.`,
          ),
        ]),
      );
    }
    return markdownReply(
      titledMessage('Target unavailable', [
        escapeMarkdownV2(
          `The Sigmo Pro ${channelName(parsed.channel)} release does not include the requested target.`,
        ),
        fieldMarkdownV2('Target', result.target ?? 'Unknown', 'code'),
      ]),
    );
  }

  const downloads = new Map(
    result.downloads.map((download) => [download.target, download.url]),
  );
  const buttons = selectedTargets.map(({ target, label }) => ({
    text: label,
    url: downloads.get(target) ?? '',
  }));
  if (buttons.some(({ url }) => !url)) {
    throw new Error('bootstrap download result is missing a requested target');
  }
  const inlineKeyboard: TelegramInlineKeyboard = [];
  for (let index = 0; index < buttons.length; index += 2) {
    inlineKeyboard.push(buttons.slice(index, index + 2));
  }

  return markdownReply(
    titledMessage('Sigmo Pro download', [
      fieldMarkdownV2('Channel', channelName(result.channel)),
      fieldMarkdownV2('Version', result.version, 'code'),
      escapeMarkdownV2(
        'Links expire in 5 minutes and can be used once. Choose your platform below.',
      ),
      [
        boldMarkdownV2('After downloading'),
        preformattedMarkdownV2('gzip -d <file>.gz\nchmod +x <file>'),
      ].join('\n'),
    ]),
    inlineKeyboard,
  );
}

async function grantCommand(
  context: RequestContext,
  args: string[],
): Promise<string> {
  if (args.length < 1 || args.length > 3) {
    return GRANT_USAGE;
  }
  const telegramId = positiveInteger(args[0]);
  const maxDevices = args[1] === undefined ? 3 : positiveInteger(args[1]);
  const expiresAtValue = args[2];
  const expiresAt = expiresAtValue
    ? parseExpiryEndOfDayUtc(expiresAtValue)
    : null;
  if (
    telegramId === undefined ||
    maxDevices === undefined ||
    (expiresAtValue !== undefined &&
      (!expiresAt || expiresAt.getTime() <= Date.now()))
  ) {
    return GRANT_USAGE;
  }
  const timestamp = new Date().toISOString();
  await context.db.upsertEntitlement({
    telegramId: telegramId,
    expiresAt: expiresAt?.toISOString() ?? null,
    maxDevices,
    timestamp,
  });
  return titledMessage('Entitlement granted', [
    inlineFieldMarkdownV2('Telegram ID', String(telegramId)),
    inlineFieldMarkdownV2('Device limit', String(maxDevices)),
    inlineFieldMarkdownV2('Expires', expiresAtValue ?? 'Never'),
  ]);
}

async function revokeCommand(
  context: RequestContext,
  args: string[],
): Promise<string> {
  if (args.length !== 1) {
    return usageMessage('Revoke usage', ['/revoke <telegram_id>']);
  }
  const telegramId = positiveInteger(args[0]);
  if (telegramId === undefined) {
    return usageMessage('Revoke usage', ['/revoke <telegram_id>']);
  }
  const timestamp = new Date().toISOString();
  await context.db.revokeEntitlement(telegramId, timestamp);
  return titledMessage('Entitlement revoked', [
    inlineFieldMarkdownV2('Telegram ID', String(telegramId)),
  ]);
}

async function statusCommand(
  context: RequestContext,
  args: string[],
): Promise<string> {
  if (args.length !== 1) {
    return usageMessage('Status usage', ['/status <telegram_id>']);
  }
  const telegramId = positiveInteger(args[0]);
  if (telegramId === undefined) {
    return usageMessage('Status usage', ['/status <telegram_id>']);
  }
  const row = await context.db.findEntitlement(telegramId);
  if (!row) {
    return titledMessage('Entitlement not found', [
      inlineFieldMarkdownV2('Telegram ID', String(telegramId)),
    ]);
  }
  return titledMessage('Entitlement status', [
    inlineFieldMarkdownV2('Telegram ID', String(row.telegramId)),
    inlineFieldMarkdownV2(
      'Status',
      row.status === 'active' ? 'Active' : 'Revoked',
    ),
    inlineFieldMarkdownV2('Device limit', String(row.maxDevices)),
    inlineFieldMarkdownV2('Expires', row.expiresAt?.slice(0, 10) ?? 'Never'),
  ]);
}

async function entitlementsCommand(
  context: RequestContext,
  args: string[],
): Promise<TelegramReply> {
  if (args.length !== 0) {
    return markdownReply(usageMessage('Entitlements usage', ['/entitlements']));
  }
  return entitlementPage(context, 1);
}

async function devicesCommand(
  context: RequestContext,
  user: TelegramUser,
  isAdmin: boolean,
  args: string[],
): Promise<TelegramReply> {
  if (isAdmin ? args.length > 1 : args.length !== 0) {
    return markdownReply(
      usageMessage('Devices usage', [
        isAdmin ? '/devices [telegram_id]' : '/devices',
      ]),
    );
  }
  const telegramId = isAdmin && args[0] ? positiveInteger(args[0]) : user.id;
  if (telegramId === undefined) {
    return markdownReply(
      usageMessage('Devices usage', ['/devices [telegram_id]']),
    );
  }
  return devicePage(context, telegramId, 1);
}

async function revokeDeviceCommand(
  context: RequestContext,
  user: TelegramUser,
  isAdmin: boolean,
  args: string[],
): Promise<string> {
  if (args.length !== 1) {
    return usageMessage('Revoke device usage', ['/revoke_device <device_id>']);
  }
  const deviceId = args[0] ?? '';
  if (!/^[0-9a-f]{32}$/.test(deviceId)) {
    return usageMessage('Revoke device usage', ['/revoke_device <device_id>']);
  }
  const device = await context.db.findDevice(deviceId);
  if (!device || (!isAdmin && device.telegramId !== user.id)) {
    return titledMessage('Device unavailable', [
      escapeMarkdownV2(
        'The device does not exist or you are not allowed to revoke it.',
      ),
    ]);
  }
  await context.db.revokeDevice(device.deviceId, new Date().toISOString());
  return titledMessage('Device revoked', [
    inlineFieldMarkdownV2('Device ID', device.deviceId),
  ]);
}

async function botCommand(
  request: Request,
  context: RequestContext,
  user: TelegramUser,
  command: string,
  args: string[],
): Promise<TelegramReply> {
  const isAdmin = telegramAdmins(context.env).has(user.id);
  switch (command) {
    case '/download':
      return downloadCommand(request, context, user, args);
    case '/grant':
      if (isAdmin) {
        return markdownReply(await grantCommand(context, args));
      }
      break;
    case '/revoke':
      if (isAdmin) {
        return markdownReply(await revokeCommand(context, args));
      }
      break;
    case '/status':
      if (isAdmin) {
        return markdownReply(await statusCommand(context, args));
      }
      break;
    case '/entitlements':
      if (isAdmin) {
        return entitlementsCommand(context, args);
      }
      break;
    case '/devices':
      return devicesCommand(context, user, isAdmin, args);
    case '/revoke_device':
      return markdownReply(
        await revokeDeviceCommand(context, user, isAdmin, args),
      );
  }
  return markdownReply(availableCommands(isAdmin));
}

export async function telegramWebhook(
  request: Request,
  context: RequestContext,
): Promise<Response> {
  const { env, execution } = context;
  const providedSecret =
    request.headers.get('x-telegram-bot-api-secret-token') ?? '';
  if (
    !providedSecret ||
    !env.SIGMO_TELEGRAM_WEBHOOK_SECRET ||
    !(await secureEqual(providedSecret, env.SIGMO_TELEGRAM_WEBHOOK_SECRET))
  ) {
    return apiError(
      'telegram_webhook_unauthorized',
      'invalid webhook secret',
      403,
    );
  }
  const update = parseTelegramUpdate(await parseJson(request));
  if (!update) {
    return apiError('telegram_update_invalid', 'invalid Telegram update', 422);
  }
  if (update.callbackQuery) {
    const callbackQuery = update.callbackQuery;
    execution.waitUntil(
      handleTelegramPageCallback(context, callbackQuery).catch(
        (caught: unknown) => {
          logError('handle Telegram page callback', caught, {
            callbackQueryId: callbackQuery.id,
          });
        },
      ),
    );
    return json({ ok: true });
  }
  const message = update.message;
  if (!message?.from || !message.text) {
    return json({ ok: true });
  }
  if (message.chat.type !== 'private' || message.chat.id !== message.from.id) {
    return json({ ok: true });
  }

  const [rawCommand, ...args] = message.text.trim().split(/\s+/);
  const commandParts = rawCommand.toLowerCase().split('@');
  if (
    commandParts.length > 2 ||
    (commandParts[1] !== undefined &&
      commandParts[1] !== env.BOT_USERNAME.toLowerCase())
  ) {
    return json({ ok: true });
  }
  const command = commandParts[0];
  const reply =
    command === '/start'
      ? await startCommand(context, message.from, args)
      : await botCommand(request, context, message.from, command, args);
  execution.waitUntil(
    sendTelegram(env.SIGMO_TELEGRAM_BOT_TOKEN, message.chat.id, reply).catch(
      (caught: unknown) => {
        logError('send Telegram reply', caught, { chatId: message.chat.id });
      },
    ),
  );
  return json({ ok: true });
}
