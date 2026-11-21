import { env } from 'cloudflare:workers';
import {
  createExecutionContext,
  waitOnExecutionContext,
} from 'cloudflare:test';
import { afterEach, assert, describe, expect, it, vi } from 'vitest';

import {
  encodeBase64,
  randomToken,
  sha256,
  signEd25519,
  TEXT_ENCODER,
} from '../src/security/crypto';
import worker from '../src/index';
import { deviceId } from '../src/license/service';
import { Database } from '../src/storage/authorization';
import { rotationMessage } from '../src/license/service';
import { telegramAdmins } from '../src/telegram/access';
import type { SignedLease } from '../src/license/types';

const IncomingRequest = Request<unknown, IncomingRequestCfProperties>;

interface DeviceIdentity {
  deviceId: string;
  publicKey: string;
  privateKey: CryptoKey;
  fingerprintHash: string;
  refreshToken: string;
}

interface PairingCreated {
  id: string;
  pollToken: string;
  activationUrl: string;
  expiresAt: string;
}

interface PairingStatus {
  status: string;
  lease?: SignedLease;
}

type TelegramCommandScope =
  { type: 'all_private_chats' } | { type: 'chat'; chat_id: number };

interface TelegramCommandCall {
  method: 'setMyCommands' | 'deleteMyCommands';
  commands?: Array<{ command: string; description: string }>;
  scope: TelegramCommandScope;
}

interface TelegramApiResponse {
  ok: boolean;
  result?: boolean;
  description?: string;
}

type TelegramApiResponder =
  TelegramApiResponse | ((call: TelegramCommandCall) => TelegramApiResponse);

interface TelegramMessage {
  chat_id: number;
  text: string;
  parse_mode: 'MarkdownV2';
  reply_markup?: {
    inline_keyboard: Array<Array<{ text: string; callback_data: string }>>;
  };
}

interface TelegramCallbackAnswer {
  callback_query_id: string;
  text?: string;
}

type TelegramMessageEdit = TelegramMessage & {
  message_id: number;
};

type TelegramMethodCall =
  | { method: 'sendMessage'; body: TelegramMessage }
  | { method: 'editMessageText'; body: TelegramMessageEdit }
  | { method: 'answerCallbackQuery'; body: TelegramCallbackAnswer };

afterEach(async () => {
  vi.restoreAllMocks();
  await env.DB.prepare('DELETE FROM telegram_command_admins').run();
});

async function request(
  path: string,
  init: RequestInit<IncomingRequestCfProperties> = {},
): Promise<Response> {
  const ctx = createExecutionContext();
  const response = await worker.fetch(
    new IncomingRequest(new URL(path, 'https://updates.example'), init),
    env,
    ctx,
  );
  await waitOnExecutionContext(ctx);
  return response;
}

function jsonRequest(
  value: unknown,
  headers: HeadersInit = {},
): RequestInit<IncomingRequestCfProperties> {
  const requestHeaders = new Headers(headers);
  requestHeaders.set('content-type', 'application/json');
  return {
    method: 'POST',
    headers: requestHeaders,
    body: JSON.stringify(value),
  };
}

async function createIdentity(): Promise<DeviceIdentity> {
  const pair = await crypto.subtle.generateKey('Ed25519', true, [
    'sign',
    'verify',
  ]);
  assert('privateKey' in pair && 'publicKey' in pair);
  const exported = await crypto.subtle.exportKey('raw', pair.publicKey);
  assert(exported instanceof ArrayBuffer);
  const rawPublicKey = exported;
  const publicKey = encodeBase64(rawPublicKey);
  const refreshToken = randomToken(32);
  return {
    deviceId: await deviceId(publicKey),
    publicKey,
    privateKey: pair.privateKey,
    fingerprintHash: await sha256(randomToken(32)),
    refreshToken,
  };
}

async function grant(
  telegramId: number,
  maxDevices = 3,
  expiresAt: string | null = null,
): Promise<void> {
  const timestamp = new Date().toISOString();
  await env.DB.prepare(
    `INSERT INTO entitlements
       (telegram_id, status, expires_at, max_devices, display_name, username, created_at, updated_at)
     VALUES (?, 'active', ?, ?, ?, ?, ?, ?)`,
  )
    .bind(
      telegramId,
      expiresAt,
      maxDevices,
      `User ${telegramId}`,
      `user${telegramId}`,
      timestamp,
      timestamp,
    )
    .run();
}

function mockTelegram(): TelegramMessage[] {
  const messages: TelegramMessage[] = [];
  vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
    const outgoing = new Request(input, init);
    const url = new URL(outgoing.url);
    if (
      outgoing.method !== 'POST' ||
      url.origin !== 'https://api.telegram.org'
    ) {
      throw new Error(`unexpected outbound request: ${outgoing.url}`);
    }
    if (!url.pathname.endsWith('/sendMessage')) {
      throw new Error(`unexpected Telegram method: ${url.pathname}`);
    }
    messages.push(await outgoing.json<TelegramMessage>());
    return Response.json({ ok: true });
  });
  return messages;
}

function mockTelegramMethods(): TelegramMethodCall[] {
  const calls: TelegramMethodCall[] = [];
  vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
    const outgoing = new Request(input, init);
    const url = new URL(outgoing.url);
    if (
      outgoing.method !== 'POST' ||
      url.origin !== 'https://api.telegram.org'
    ) {
      throw new Error(`unexpected outbound request: ${outgoing.url}`);
    }
    const method = url.pathname.split('/').at(-1);
    if (
      method !== 'sendMessage' &&
      method !== 'editMessageText' &&
      method !== 'answerCallbackQuery'
    ) {
      throw new Error(`unexpected Telegram method: ${url.pathname}`);
    }
    const body = await outgoing.json();
    calls.push({ method, body } as TelegramMethodCall);
    return Response.json({ ok: true });
  });
  return calls;
}

function mockTelegramCommands(
  responder: TelegramApiResponder = { ok: true, result: true },
): TelegramCommandCall[] {
  const calls: TelegramCommandCall[] = [];
  vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
    const outgoing = new Request(input, init);
    const url = new URL(outgoing.url);
    if (
      outgoing.method !== 'POST' ||
      url.origin !== 'https://api.telegram.org'
    ) {
      throw new Error(`unexpected outbound request: ${outgoing.url}`);
    }
    const method: TelegramCommandCall['method'] | null = url.pathname.endsWith(
      '/setMyCommands',
    )
      ? 'setMyCommands'
      : url.pathname.endsWith('/deleteMyCommands')
        ? 'deleteMyCommands'
        : null;
    if (!method) {
      throw new Error(`unexpected Telegram method: ${url.pathname}`);
    }
    const body = await outgoing.json<{
      commands?: Array<{ command: string; description: string }>;
      scope: TelegramCommandScope;
    }>();
    const call: TelegramCommandCall = { method, ...body };
    calls.push(call);
    const response =
      typeof responder === 'function' ? responder(call) : responder;
    return Response.json(response);
  });
  return calls;
}

async function runScheduled(workerEnv: Env = env): Promise<void> {
  await worker.scheduled(
    {
      scheduledTime: Date.now(),
      cron: '0 * * * *',
      noRetry() {},
    },
    workerEnv,
  );
}

async function telegramCommandAdminIds(): Promise<number[]> {
  const result = await env.DB.prepare(
    `SELECT telegram_id
     FROM telegram_command_admins
     ORDER BY telegram_id ASC`,
  ).all<{ telegram_id: number }>();
  return result.results.map(({ telegram_id }) => telegram_id);
}

async function createPairing(
  identity: DeviceIdentity,
): Promise<PairingCreated> {
  const response = await request(
    '/v1/license-pairings',
    jsonRequest({
      deviceId: identity.deviceId,
      publicKey: identity.publicKey,
      refreshTokenHash: await sha256(identity.refreshToken),
      fingerprintHash: identity.fingerprintHash,
    }),
  );
  expect(response.status).toBe(201);
  return response.json<PairingCreated>();
}

async function activatePairing(
  pairing: Pick<PairingCreated, 'id'>,
  telegramId: number,
  text = `/start ${pairing.id}`,
): Promise<void> {
  const response = await request(
    '/v1/telegram-updates',
    jsonRequest(
      {
        message: {
          chat: { id: telegramId, type: 'private' },
          from: {
            id: telegramId,
            first_name: 'Test',
            last_name: 'User',
            username: `user${telegramId}`,
          },
          text,
        },
      },
      {
        'x-telegram-bot-api-secret-token': env.SIGMO_TELEGRAM_WEBHOOK_SECRET,
      },
    ),
  );
  expect(response.status).toBe(200);
}

async function pressTelegramButton(
  telegramId: number,
  data: string,
  callbackId = 'callback-1',
  messageId = 42,
): Promise<void> {
  const response = await request(
    '/v1/telegram-updates',
    jsonRequest(
      {
        callback_query: {
          id: callbackId,
          from: {
            id: telegramId,
            first_name: 'Test',
            last_name: 'User',
            username: `user${telegramId}`,
          },
          message: {
            message_id: messageId,
            chat: { id: telegramId, type: 'private' },
          },
          data,
        },
      },
      {
        'x-telegram-bot-api-secret-token': env.SIGMO_TELEGRAM_WEBHOOK_SECRET,
      },
    ),
  );
  expect(response.status).toBe(200);
}

async function pollPairing(pairing: PairingCreated): Promise<PairingStatus> {
  const response = await request(`/v1/license-pairings/${pairing.id}`, {
    headers: { authorization: `Bearer ${pairing.pollToken}` },
  });
  expect(response.status).toBe(200);
  return response.json<PairingStatus>();
}

async function authorizedDevice(
  telegramId: number,
): Promise<{ identity: DeviceIdentity; proof: SignedLease }> {
  await grant(telegramId);
  const identity = await createIdentity();
  const pairing = await createPairing(identity);
  mockTelegram();
  await activatePairing(pairing, telegramId);
  vi.restoreAllMocks();
  const status = await pollPairing(pairing);
  expect(status.status).toBe('active');
  assert(status.lease);
  return { identity, proof: status.lease };
}

async function createRotationRequest(
  identity: DeviceIdentity,
  proof: SignedLease,
): Promise<{
  body: Record<string, string | number>;
  nextRefreshToken: string;
}> {
  const challengeResponse = await request(
    '/v1/license-challenges',
    jsonRequest({
      deviceId: identity.deviceId,
      sessionId: proof.lease.sessionId,
      generation: proof.lease.generation,
    }),
  );
  expect(challengeResponse.status).toBe(201);
  const challenge = await challengeResponse.json<{ challenge: string }>();
  return signRotationRequest(identity, proof, challenge.challenge);
}

async function signRotationRequest(
  identity: DeviceIdentity,
  proof: SignedLease,
  challenge: string,
): Promise<{
  body: Record<string, string | number>;
  nextRefreshToken: string;
}> {
  const nextRefreshToken = randomToken(32);
  const nextRefreshTokenHash = await sha256(nextRefreshToken);
  const rotationId = randomToken(24);
  const message = rotationMessage({
    deviceId: identity.deviceId,
    sessionId: proof.lease.sessionId,
    generation: proof.lease.generation,
    challenge,
    rotationId,
    nextRefreshTokenHash,
    fingerprintHash: identity.fingerprintHash,
  });
  const signature = await crypto.subtle.sign(
    'Ed25519',
    identity.privateKey,
    TEXT_ENCODER.encode(message),
  );
  return {
    body: {
      deviceId: identity.deviceId,
      sessionId: proof.lease.sessionId,
      generation: proof.lease.generation,
      challenge,
      refreshToken: identity.refreshToken,
      nextRefreshTokenHash,
      rotationId,
      fingerprintHash: identity.fingerprintHash,
      signature: encodeBase64(signature),
    },
    nextRefreshToken,
  };
}

async function signedHeaders(
  identity: DeviceIdentity,
  proof: SignedLease,
  method: string,
  path: string,
): Promise<Headers> {
  const timestamp = String(Math.floor(Date.now() / 1000));
  const message = `${method}\n${path}\n${timestamp}`;
  const signature = await crypto.subtle.sign(
    'Ed25519',
    identity.privateKey,
    TEXT_ENCODER.encode(message),
  );
  const headers = new Headers();
  headers.set('x-sigmo-device-id', identity.deviceId);
  headers.set(
    'x-sigmo-lease',
    encodeBase64(TEXT_ENCODER.encode(JSON.stringify(proof)), true),
  );
  headers.set('x-sigmo-timestamp', timestamp);
  headers.set('x-sigmo-signature', encodeBase64(signature));
  return headers;
}

describe('Telegram pairing', () => {
  it('cleans expired pairings and challenges during scheduled maintenance', async () => {
    const { identity, proof } = await authorizedDevice(9_000_000_123);
    await createRotationRequest(identity, proof);
    const expired = await createPairing(await createIdentity());
    const active = await createPairing(await createIdentity());
    const timestamp = new Date(Date.now() - 60_000).toISOString();
    await env.DB.batch([
      env.DB.prepare(
        'UPDATE pairings SET expires_at = ? WHERE pairing_id = ?',
      ).bind(timestamp, expired.id),
      env.DB.prepare(
        'UPDATE challenges SET expires_at = ? WHERE device_id = ?',
      ).bind(timestamp, identity.deviceId),
    ]);
    const db = new Database(env.DB);
    mockTelegramCommands();

    await runScheduled();

    await expect(db.findPairing(expired.id)).resolves.toBeNull();
    await expect(db.findPairing(active.id)).resolves.toMatchObject({
      status: 'pending',
    });
    const challenge = await env.DB.prepare(
      'SELECT challenge_hash FROM challenges WHERE device_id = ?',
    )
      .bind(identity.deviceId)
      .first();
    expect(challenge).toBeNull();
  });

  it('registers described command menus for users and administrators', async () => {
    const calls = mockTelegramCommands();

    await runScheduled();

    expect(calls).toHaveLength(2);
    const userRegistration = calls.find(
      ({ method, scope }) =>
        method === 'setMyCommands' && scope.type === 'all_private_chats',
    );
    expect(userRegistration?.commands?.map(({ command }) => command)).toEqual([
      'start',
      'devices',
      'revoke_device',
    ]);
    const adminRegistration = calls.find(
      ({ method, scope }) =>
        method === 'setMyCommands' && scope.type === 'chat',
    );
    expect(adminRegistration?.scope).toEqual({ type: 'chat', chat_id: 1001 });
    expect(adminRegistration?.commands?.map(({ command }) => command)).toEqual([
      'start',
      'grant',
      'revoke',
      'status',
      'entitlements',
      'devices',
      'revoke_device',
    ]);
    expect(
      calls
        .flatMap(({ commands }) => commands ?? [])
        .every(
          ({ command, description }) =>
            /^[a-z0-9_]{1,32}$/.test(command) &&
            description.length >= 1 &&
            description.length <= 256,
        ),
    ).toBe(true);
    await expect(telegramCommandAdminIds()).resolves.toEqual([1001]);
  });

  it('deletes command scopes for removed administrators', async () => {
    await env.DB.batch([
      env.DB.prepare(
        'INSERT INTO telegram_command_admins (telegram_id) VALUES (?)',
      ).bind(1001),
      env.DB.prepare(
        'INSERT INTO telegram_command_admins (telegram_id) VALUES (?)',
      ).bind(1002),
    ]);
    const calls = mockTelegramCommands();

    await runScheduled();

    expect(calls).toContainEqual({
      method: 'deleteMyCommands',
      scope: { type: 'chat', chat_id: 1002 },
    });
    await expect(telegramCommandAdminIds()).resolves.toEqual([1001]);
  });

  it('keeps the previous scope state when Telegram does not return true', async () => {
    await env.DB.prepare(
      'INSERT INTO telegram_command_admins (telegram_id) VALUES (?)',
    )
      .bind(1002)
      .run();
    mockTelegramCommands(({ method }) =>
      method === 'deleteMyCommands'
        ? { ok: true, result: false }
        : { ok: true, result: true },
    );

    await expect(runScheduled()).rejects.toThrow(
      'Telegram deleteMyCommands did not return true',
    );
    await expect(telegramCommandAdminIds()).resolves.toEqual([1002]);
  });

  it('does not reconcile command menus from /start', async () => {
    const methods: string[] = [];
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const outgoing = new Request(input, init);
      const method = new URL(outgoing.url).pathname.split('/').at(-1) ?? '';
      methods.push(method);
      return Response.json({ ok: true });
    });

    await activatePairing({ id: 'unused' }, 3201, '/start');

    expect(methods).toEqual(['sendMessage']);
  });

  it('shows the Telegram ID and entitlement status from /start', async () => {
    const activeTelegramId = 3202;
    const inactiveTelegramId = 3203;
    await grant(activeTelegramId);
    const messages = mockTelegram();

    await activatePairing({ id: 'unused' }, activeTelegramId, '/start');
    await activatePairing({ id: 'unused' }, inactiveTelegramId, '/start');

    expect(messages).toHaveLength(2);
    expect(messages[0].text).toContain(
      `*Telegram ID*\n\`${activeTelegramId}\``,
    );
    expect(messages[0].text).toContain('*Entitlement*\nActive');
    expect(messages[1].text).toContain(
      `*Telegram ID*\n\`${inactiveTelegramId}\``,
    );
    expect(messages[1].text).toContain('*Entitlement*\nInactive');
    expect(
      messages.every(({ parse_mode }) => parse_mode === 'MarkdownV2'),
    ).toBe(true);
    expect(messages.every((message) => !message.reply_markup)).toBe(true);
    expect(messages.every(({ text }) => !text.includes('/download'))).toBe(
      true,
    );
  });

  it('returns the authorization command help for the retired download command', async () => {
    const messages = mockTelegram();

    await activatePairing({ id: 'unused' }, 3204, '/download');

    expect(messages).toHaveLength(1);
    expect(messages[0].text).toContain('*Available commands*');
    expect(messages[0].text).toContain('/devices');
    expect(messages[0].text).not.toContain('/download');
    expect(messages[0].reply_markup).toBeUndefined();
  });

  it('compares the webhook secret before processing an update', async () => {
    const outbound = vi.spyOn(globalThis, 'fetch');
    const response = await request(
      '/v1/telegram-updates',
      jsonRequest(
        {
          message: {
            chat: { id: 1, type: 'private' },
            text: '/devices',
          },
        },
        { 'x-telegram-bot-api-secret-token': 'wrong-secret' },
      ),
    );
    expect(response.status).toBe(403);
    await expect(
      response.json<{ error_code: string; detail: string }>(),
    ).resolves.toMatchObject({
      error_code: 'telegram_webhook_unauthorized',
      detail: 'invalid webhook secret',
    });
    expect(outbound).not.toHaveBeenCalled();
  });

  it('expires stale pairings without authorizing a device', async () => {
    const pairing = await createPairing(await createIdentity());
    await env.DB.prepare(
      'UPDATE pairings SET expires_at = ? WHERE pairing_id = ?',
    )
      .bind(new Date(Date.now() - 60_000).toISOString(), pairing.id)
      .run();

    const expire = vi.spyOn(Database.prototype, 'markPairingExpired');
    const head = await request(`/v1/license-pairings/${pairing.id}`, {
      method: 'HEAD',
      headers: { authorization: `Bearer ${pairing.pollToken}` },
    });
    expect(head.status).toBe(200);
    expect(await head.text()).toBe('');
    expect(expire).not.toHaveBeenCalled();

    const status = await pollPairing(pairing);
    expect(status.status).toBe('expired');
    expect(expire).toHaveBeenCalledOnce();
  });

  it('rejects device keys that are not raw Ed25519 public keys', async () => {
    const response = await request(
      '/v1/license-pairings',
      jsonRequest({
        deviceId: 'not-a-device',
        publicKey: encodeBase64(new Uint8Array(31)),
        refreshTokenHash: await sha256(randomToken(32)),
        fingerprintHash: await sha256(randomToken(32)),
      }),
    );
    expect(response.status).toBe(422);
    await expect(
      response.json<{ error_code: string; detail: string }>(),
    ).resolves.toMatchObject({
      error_code: 'device_public_key_invalid',
      detail: 'publicKey is not a valid Ed25519 public key',
    });
  });

  it('keeps an unentitled Telegram user pending', async () => {
    const pairing = await createPairing(await createIdentity());
    const messages = mockTelegram();
    await activatePairing(pairing, 2001);

    expect(messages).toHaveLength(1);
    expect(messages[0].text).toContain(
      'does not have an active Sigmo Pro entitlement',
    );
    const status = await pollPairing(pairing);
    expect(status.status).toBe('pending');
  });

  it('enforces the device limit inside the activation transaction', async () => {
    await grant(3001, 1);
    const first = await createPairing(await createIdentity());
    const second = await createPairing(await createIdentity());
    const messages = mockTelegram();

    await activatePairing(first, 3001);
    await activatePairing(second, 3001);

    expect(messages.at(-1)?.text).toContain('Device limit reached');
    expect((await pollPairing(first)).status).toBe('active');
    expect((await pollPairing(second)).status).toBe('pending');
  });

  it('does not rebind an active device to another Telegram user', async () => {
    await grant(3051);
    await grant(3052);
    const identity = await createIdentity();
    const first = await createPairing(identity);
    const second = await createPairing(identity);
    const messages = mockTelegram();

    await activatePairing(first, 3051);
    await activatePairing(second, 3052);

    expect(messages.at(-1)?.text).toContain(
      'belongs to another Telegram account',
    );
    expect((await pollPairing(first)).status).toBe('active');
    expect((await pollPairing(second)).status).toBe('pending');
    const owner = await env.DB.prepare(
      'SELECT telegram_id FROM devices WHERE device_id = ?',
    )
      .bind(identity.deviceId)
      .first<{ telegram_id: number }>();
    expect(owner?.telegram_id).toBe(3051);
  });

  it('pins an active device fingerprint until the device is revoked', async () => {
    const telegramId = 3071;
    await grant(telegramId);
    const identity = await createIdentity();
    const first = await createPairing(identity);
    const copiedIdentity: DeviceIdentity = {
      ...identity,
      fingerprintHash: await sha256(randomToken(32)),
      refreshToken: randomToken(32),
    };
    const copiedPairing = await createPairing(copiedIdentity);
    const messages = mockTelegram();

    await activatePairing(first, telegramId);
    await activatePairing(copiedPairing, telegramId);

    expect(messages.at(-1)?.text).toContain('bound to another host');
    expect((await pollPairing(first)).status).toBe('active');
    expect((await pollPairing(copiedPairing)).status).toBe('pending');
    const pinned = await env.DB.prepare(
      'SELECT fingerprint_hash FROM device_sessions WHERE device_id = ?',
    )
      .bind(identity.deviceId)
      .first<{ fingerprint_hash: string }>();
    expect(pinned?.fingerprint_hash).toBe(identity.fingerprintHash);

    await activatePairing(
      { id: 'unused' },
      telegramId,
      `/revoke_device ${identity.deviceId}`,
    );
    const revokedSession = await env.DB.prepare(
      'SELECT device_id FROM device_sessions WHERE device_id = ?',
    )
      .bind(identity.deviceId)
      .first();
    expect(revokedSession).toBeNull();

    await activatePairing(copiedPairing, telegramId);
    expect((await pollPairing(copiedPairing)).status).toBe('active');
    const rebound = await env.DB.prepare(
      'SELECT fingerprint_hash FROM device_sessions WHERE device_id = ?',
    )
      .bind(identity.deviceId)
      .first<{ fingerprint_hash: string }>();
    expect(rebound?.fingerprint_hash).toBe(copiedIdentity.fingerprintHash);
  });

  it('limits outstanding pairings per device', async () => {
    const identity = await createIdentity();
    const refreshTokenHash = await sha256(identity.refreshToken);
    for (let index = 0; index < 3; index += 1) {
      await createPairing(identity);
    }

    const response = await request(
      '/v1/license-pairings',
      jsonRequest({
        deviceId: identity.deviceId,
        publicKey: identity.publicKey,
        refreshTokenHash,
        fingerprintHash: identity.fingerprintHash,
      }),
    );
    expect(response.status).toBe(429);
    await expect(
      response.json<{ error_code: string; detail: string }>(),
    ).resolves.toMatchObject({ error_code: 'license_pairing_limit_exceeded' });
  });

  it('enforces the outstanding pairing limit under concurrency', async () => {
    const identity = await createIdentity();
    const refreshTokenHash = await sha256(identity.refreshToken);
    const responses = await Promise.all(
      Array.from({ length: 6 }, () =>
        request(
          '/v1/license-pairings',
          jsonRequest({
            deviceId: identity.deviceId,
            publicKey: identity.publicKey,
            refreshTokenHash,
            fingerprintHash: identity.fingerprintHash,
          }),
        ),
      ),
    );

    expect(
      responses.filter((response) => response.status === 201),
    ).toHaveLength(3);
    expect(
      responses.filter((response) => response.status === 429),
    ).toHaveLength(3);
  });

  it('ignores commands received from Telegram group chats', async () => {
    const messages = mockTelegram();
    const telegramId = 1001;
    const grantedId = 3099;
    const response = await request(
      '/v1/telegram-updates',
      jsonRequest(
        {
          message: {
            chat: { id: -100123, type: 'supergroup' },
            from: {
              id: telegramId,
              first_name: 'Admin',
            },
            text: `/grant ${grantedId}`,
          },
        },
        {
          'x-telegram-bot-api-secret-token': env.SIGMO_TELEGRAM_WEBHOOK_SECRET,
        },
      ),
    );

    expect(response.status).toBe(200);
    expect(messages).toHaveLength(0);
    const entitlement = await env.DB.prepare(
      'SELECT telegram_id FROM entitlements WHERE telegram_id = ?',
    )
      .bind(grantedId)
      .first();
    expect(entitlement).toBeNull();
  });

  it('ignores commands addressed to another Telegram bot', async () => {
    const messages = mockTelegram();
    const telegramId = 3088;

    await activatePairing(
      { id: 'unused' },
      1001,
      `/grant@AnotherBot ${telegramId}`,
    );

    expect(messages).toHaveLength(0);
    const entitlement = await env.DB.prepare(
      'SELECT telegram_id FROM entitlements WHERE telegram_id = ?',
    )
      .bind(telegramId)
      .first();
    expect(entitlement).toBeNull();
  });

  it('executes administrator grant, status, and revoke commands', async () => {
    const messages = mockTelegram();
    const admin = 1001;
    const telegramId = 3101;
    const expiresAt = '2099-08-09';

    await activatePairing(
      { id: 'unused' },
      admin,
      `/grant ${telegramId} 2 ${expiresAt}`,
    );
    const granted = await env.DB.prepare(
      'SELECT * FROM entitlements WHERE telegram_id = ?',
    )
      .bind(telegramId)
      .first<{ status: string; max_devices: number; expires_at: string }>();
    expect(granted).toMatchObject({
      status: 'active',
      max_devices: 2,
      expires_at: `${expiresAt}T23:59:59.999Z`,
    });

    await activatePairing({ id: 'unused' }, admin, `/status ${telegramId}`);
    expect(messages.at(-1)?.text).toContain(`*Telegram ID*: \`${telegramId}\``);
    expect(messages.at(-1)?.text).toContain('*Status*: `Active`');

    await activatePairing({ id: 'unused' }, admin, `/revoke ${telegramId}`);
    const revoked = await env.DB.prepare(
      'SELECT status FROM entitlements WHERE telegram_id = ?',
    )
      .bind(telegramId)
      .first<{ status: string }>();
    expect(revoked?.status).toBe('revoked');
  });

  it('lists only active unexpired entitlements for administrators', async () => {
    await env.DB.prepare(`UPDATE entitlements SET status = 'revoked'`).run();
    const activeId = 3151;
    const expiringId = 3152;
    const revokedId = 3153;
    const expiredId = 3154;
    await grant(activeId, 2);
    await grant(expiringId, 4, '2099-12-31T23:59:59.999Z');
    await grant(revokedId);
    await grant(expiredId, 3, '2000-01-01T23:59:59.999Z');
    const timestamp = new Date().toISOString();
    await env.DB.batch([
      env.DB.prepare(
        'UPDATE entitlements SET display_name = ?, username = ? WHERE telegram_id = ?',
      ).bind('Alice Admin', 'alice', activeId),
      env.DB.prepare(
        `UPDATE entitlements SET status = 'revoked' WHERE telegram_id = ?`,
      ).bind(revokedId),
      env.DB.prepare(
        `INSERT INTO devices
           (device_id, telegram_id, public_key, created_at, last_seen_at, revoked_at)
         VALUES (?, ?, ?, ?, ?, NULL), (?, ?, ?, ?, ?, ?)`,
      ).bind(
        '00000000000000000000000000003151',
        activeId,
        'public-key-1',
        timestamp,
        timestamp,
        '00000000000000000000000000003152',
        activeId,
        'public-key-2',
        timestamp,
        timestamp,
        timestamp,
      ),
    ]);
    const messages = mockTelegram();

    await activatePairing({ id: 'unused' }, 1001, '/entitlements');

    expect(messages).toHaveLength(1);
    expect(messages[0].text).toContain('*Active entitlements*');
    expect(messages[0].text).toContain('*Total*: `2`');
    expect(messages[0].text).toContain(
      `*Alice Admin*\n*Telegram ID*: \`${activeId}\`\n*Username*: \`@alice\`\n*Devices*: \`1 / 2\`\n*Expires*: \`Never\``,
    );
    expect(messages[0].text).toContain(
      `*User ${expiringId}*\n*Telegram ID*: \`${expiringId}\`\n*Username*: \`@user${expiringId}\`\n*Devices*: \`0 / 4\`\n*Expires*: \`2099-12-31\``,
    );
    expect(messages[0].text).not.toContain(String(revokedId));
    expect(messages[0].text).not.toContain(String(expiredId));
  });

  it('does not expose entitlement lists to regular users', async () => {
    const entitlementId = 3161;
    await grant(entitlementId);
    const messages = mockTelegram();

    await activatePairing({ id: 'unused' }, 3162, '/entitlements');

    expect(messages).toHaveLength(1);
    expect(messages[0].text).toBe(
      [
        '*Available commands*',
        '/start\nShow Pro status or authorize a device\n*Usage*: `/start [pairing_id]`',
        '/devices\nList linked devices\n*Usage*: `/devices`',
        '/revoke\\_device\nRevoke a linked device by ID\n*Usage*: `/revoke_device <device_id>`',
      ].join('\n\n'),
    );
    expect(messages[0].text).not.toContain(String(entitlementId));
  });

  it('shows administrator-specific command usage in help', async () => {
    const messages = mockTelegram();

    await activatePairing({ id: 'unused' }, 1001, '/unknown');

    expect(messages).toHaveLength(1);
    expect(messages[0].text).toContain('/devices\n');
    expect(messages[0].text).toContain('*Usage*: `/devices [telegram_id]`');
    expect(messages[0].text).toContain('/revoke\\_device\n');
    expect(messages[0].text).toContain(
      '*Usage*: `/grant <telegram_id> [max_devices] [expires_at]`',
    );
    expect(messages[0].text).toContain('*Usage*: `/entitlements`');
  });

  it('rejects page numbers in public commands', async () => {
    const messages = mockTelegram();

    await activatePairing({ id: 'unused' }, 1001, '/entitlements 2');
    await activatePairing({ id: 'unused' }, 12_001, '/devices 2');
    await activatePairing({ id: 'unused' }, 1001, '/devices 12001 2');

    expect(messages.map(({ text }) => text)).toEqual([
      '*Entitlements usage*\n\n`/entitlements`',
      '*Devices usage*\n\n`/devices`',
      '*Devices usage*\n\n`/devices [telegram_id]`',
    ]);
  });

  it('truncates oversized entitlement names without breaking MarkdownV2', async () => {
    await env.DB.prepare(`UPDATE entitlements SET status = 'revoked'`).run();
    const telegramId = 3171;
    await grant(telegramId);
    await env.DB.prepare(
      'UPDATE entitlements SET display_name = ? WHERE telegram_id = ?',
    )
      .bind('A'.repeat(5_000), telegramId)
      .run();
    const messages = mockTelegram();

    await activatePairing({ id: 'unused' }, 1001, '/entitlements');

    expect(messages).toHaveLength(1);
    expect(messages.every((message) => message.text.length <= 4_000)).toBe(
      true,
    );
    expect(messages[0].text).toContain(`*${'A'.repeat(61)}\\.\\.\\.*`);
    expect(messages[0].text).not.toContain('A'.repeat(65));
    expect(messages[0].parse_mode).toBe('MarkdownV2');
  });

  it('queries entitlement pages in groups of ten', async () => {
    await env.DB.prepare(`UPDATE entitlements SET status = 'revoked'`).run();
    const telegramIds = Array.from(
      { length: 20 },
      (scenario, index) => 10_000 + index,
    );
    for (const telegramId of telegramIds) {
      await grant(telegramId);
      await env.DB.prepare(
        'UPDATE entitlements SET display_name = ? WHERE telegram_id = ?',
      )
        .bind(`${'A'.repeat(150)} ${telegramId}`, telegramId)
        .run();
    }
    const calls = mockTelegramMethods();

    await activatePairing({ id: 'unused' }, 1001, '/entitlements');
    await pressTelegramButton(1001, 'page:entitlements:2');

    expect(calls).toHaveLength(3);
    if (calls[0]?.method !== 'sendMessage') {
      assert.fail('expected sendMessage');
    }
    if (calls[2]?.method !== 'editMessageText') {
      assert.fail('expected editMessageText');
    }
    const firstPage = calls[0].body;
    const secondPage = calls[2].body;
    expect(firstPage.text.length).toBeLessThanOrEqual(4_000);
    expect(secondPage.text.length).toBeLessThanOrEqual(4_000);
    expect(firstPage.text).toContain('*Page*: `1 / 2`');
    expect(firstPage.reply_markup?.inline_keyboard).toEqual([
      [{ text: 'Next', callback_data: 'page:entitlements:2' }],
    ]);
    expect(secondPage.text).toContain('*Page*: `2 / 2`');
    expect(secondPage.reply_markup?.inline_keyboard).toEqual([
      [{ text: 'Previous', callback_data: 'page:entitlements:1' }],
    ]);
    for (const [index, telegramId] of telegramIds.entries()) {
      const expectedPage = index < 10 ? firstPage : secondPage;
      const otherPage = index < 10 ? secondPage : firstPage;
      const field = `*Telegram ID*: \`${telegramId}\``;
      expect(expectedPage.text).toContain(field);
      expect(otherPage.text).not.toContain(field);
    }
  });

  it('edits entitlement pages from Telegram callback buttons', async () => {
    await env.DB.prepare(`UPDATE entitlements SET status = 'revoked'`).run();
    for (let index = 0; index < 11; index++) {
      await grant(11_000 + index);
    }
    const calls = mockTelegramMethods();

    await pressTelegramButton(1001, 'page:entitlements:2');

    expect(calls).toHaveLength(2);
    expect(calls[0]).toEqual({
      method: 'answerCallbackQuery',
      body: { callback_query_id: 'callback-1' },
    });
    expect(calls[1]).toMatchObject({
      method: 'editMessageText',
      body: {
        chat_id: 1001,
        message_id: 42,
        parse_mode: 'MarkdownV2',
        reply_markup: {
          inline_keyboard: [
            [{ text: 'Previous', callback_data: 'page:entitlements:1' }],
          ],
        },
      },
    });
    if (calls[1]?.method !== 'editMessageText') {
      assert.fail('expected editMessageText');
    }
    expect(calls[1].body.text).toContain('*Page*: `2 / 2`');
  });

  it('falls back to the last entitlement page for a stale button', async () => {
    await env.DB.prepare(`UPDATE entitlements SET status = 'revoked'`).run();
    for (let index = 0; index < 11; index++) {
      await grant(11_100 + index);
    }
    const calls = mockTelegramMethods();

    await activatePairing({ id: 'unused' }, 1001, '/entitlements');
    if (calls[0]?.method !== 'sendMessage') {
      assert.fail('expected sendMessage');
    }
    expect(calls[0].body.reply_markup?.inline_keyboard).toEqual([
      [{ text: 'Next', callback_data: 'page:entitlements:2' }],
    ]);

    await env.DB.prepare(
      `UPDATE entitlements SET status = 'revoked' WHERE telegram_id >= ?`,
    )
      .bind(11_110)
      .run();

    await pressTelegramButton(1001, 'page:entitlements:2');

    expect(calls).toHaveLength(3);
    expect(calls[1]).toEqual({
      method: 'answerCallbackQuery',
      body: { callback_query_id: 'callback-1' },
    });
    expect(calls[2]).toMatchObject({
      method: 'editMessageText',
      body: {
        chat_id: 1001,
        message_id: 42,
        reply_markup: { inline_keyboard: [] },
      },
    });
    if (calls[2]?.method !== 'editMessageText') {
      assert.fail('expected editMessageText');
    }
    expect(calls[2].body.text).toContain('*Page*: `1 / 1`');
  });

  it('queries device pages in groups of ten', async () => {
    const telegramId = 10_100;
    await grant(telegramId);
    const timestamp = new Date().toISOString();
    for (let index = 0; index < 12; index++) {
      await env.DB.prepare(
        `INSERT INTO devices
           (device_id, telegram_id, public_key, created_at, last_seen_at, revoked_at)
         VALUES (?, ?, ?, ?, ?, NULL)`,
      )
        .bind(
          index.toString(16).padStart(32, '0'),
          telegramId,
          `public-key-${index}`,
          new Date(Date.parse(timestamp) + index).toISOString(),
          timestamp,
        )
        .run();
    }
    const calls = mockTelegramMethods();

    await activatePairing({ id: 'unused' }, telegramId, '/devices');
    await pressTelegramButton(telegramId, `page:devices:${telegramId}:2`);

    expect(calls).toHaveLength(3);
    if (calls[0]?.method !== 'sendMessage') {
      assert.fail('expected sendMessage');
    }
    if (calls[2]?.method !== 'editMessageText') {
      assert.fail('expected editMessageText');
    }
    const firstPage = calls[0].body;
    const secondPage = calls[2].body;
    expect(firstPage.text).toContain('*Page*: `1 / 2`');
    expect(firstPage.reply_markup?.inline_keyboard).toEqual([
      [
        {
          text: 'Next',
          callback_data: `page:devices:${telegramId}:2`,
        },
      ],
    ]);
    expect(secondPage.text).toContain('*Page*: `2 / 2`');
    expect(secondPage.reply_markup?.inline_keyboard).toEqual([
      [
        {
          text: 'Previous',
          callback_data: `page:devices:${telegramId}:1`,
        },
      ],
    ]);
    expect(firstPage.text.match(/\*Device ID\*/g)).toHaveLength(10);
    expect(secondPage.text.match(/\*Device ID\*/g)).toHaveLength(2);
  });

  it('falls back to the last device page for a stale button', async () => {
    const telegramId = 10_200;
    await grant(telegramId);
    const timestamp = new Date().toISOString();
    for (let index = 0; index < 11; index++) {
      await env.DB.prepare(
        `INSERT INTO devices
           (device_id, telegram_id, public_key, created_at, last_seen_at, revoked_at)
         VALUES (?, ?, ?, ?, ?, NULL)`,
      )
        .bind(
          (20_000 + index).toString(16).padStart(32, '0'),
          telegramId,
          `public-key-${index}`,
          new Date(Date.parse(timestamp) + index).toISOString(),
          timestamp,
        )
        .run();
    }
    const calls = mockTelegramMethods();

    await activatePairing({ id: 'unused' }, telegramId, '/devices');
    if (calls[0]?.method !== 'sendMessage') {
      assert.fail('expected sendMessage');
    }
    expect(calls[0].body.reply_markup?.inline_keyboard).toEqual([
      [
        {
          text: 'Next',
          callback_data: `page:devices:${telegramId}:2`,
        },
      ],
    ]);

    await env.DB.prepare(
      `DELETE FROM devices
       WHERE telegram_id = ? AND device_id = ?`,
    )
      .bind(telegramId, (20_010).toString(16).padStart(32, '0'))
      .run();

    await pressTelegramButton(telegramId, `page:devices:${telegramId}:2`);

    expect(calls).toHaveLength(3);
    expect(calls[1]).toEqual({
      method: 'answerCallbackQuery',
      body: { callback_query_id: 'callback-1' },
    });
    expect(calls[2]).toMatchObject({
      method: 'editMessageText',
      body: {
        chat_id: telegramId,
        message_id: 42,
        reply_markup: { inline_keyboard: [] },
      },
    });
    if (calls[2]?.method !== 'editMessageText') {
      assert.fail('expected editMessageText');
    }
    expect(calls[2].body.text).toContain('*Page*: `1 / 1`');
  });

  it('rejects device page callbacks for another regular user', async () => {
    const calls = mockTelegramMethods();

    await pressTelegramButton(12_001, 'page:devices:12002:2');

    expect(calls).toEqual([
      {
        method: 'answerCallbackQuery',
        body: {
          callback_query_id: 'callback-1',
          text: 'This button is not available.',
        },
      },
    ]);
  });

  it.each(['2027-02-30', '2099-08-09T12:00:00Z', '2099-8-9'])(
    'rejects invalid administrator expiry date %s',
    async (expiresAt) => {
      const messages = mockTelegram();
      const telegramId = 3199;

      await activatePairing(
        { id: 'unused' },
        1001,
        `/grant ${telegramId} 3 ${expiresAt}`,
      );

      expect(messages.at(-1)?.text).toContain('*Grant usage*');
      expect(messages.at(-1)?.text).toContain(
        '`/grant <telegram_id> [max_devices] [expires_at]`',
      );
      const entitlement = await env.DB.prepare(
        'SELECT telegram_id FROM entitlements WHERE telegram_id = ?',
      )
        .bind(telegramId)
        .first();
      expect(entitlement).toBeNull();
    },
  );
});

describe('device authorization', () => {
  it('preserves Telegram IDs beyond signed 32-bit range', async () => {
    const telegramId = 4_000_000_001;
    const { proof } = await authorizedDevice(telegramId);
    expect(proof.lease.telegramId).toBe(telegramId);
  });

  it('retries the same session rotation idempotently', async () => {
    const { identity, proof } = await authorizedDevice(4001);
    const { body } = await createRotationRequest(identity, proof);

    const responses = await Promise.all([
      request('/v1/license-leases', jsonRequest(body)),
      request('/v1/license-leases', jsonRequest(body)),
    ]);
    expect(responses.map((response) => response.status)).toEqual([201, 201]);
    const leases = await Promise.all(
      responses.map((response) => response.json<SignedLease>()),
    );
    expect(leases.map(({ lease }) => lease.generation)).toEqual([
      proof.lease.generation + 1,
      proof.lease.generation + 1,
    ]);

    const session = await env.DB.prepare(
      'SELECT generation FROM device_sessions WHERE device_id = ?',
    )
      .bind(identity.deviceId)
      .first<{ generation: number }>();
    expect(session?.generation).toBe(proof.lease.generation + 1);
  });

  it('allows only one clone to rotate the same session generation', async () => {
    const { identity, proof } = await authorizedDevice(4051);
    const first = await createRotationRequest(identity, proof);
    const second = await signRotationRequest(
      identity,
      proof,
      String(first.body.challenge),
    );

    const responses = await Promise.all([
      request('/v1/license-leases', jsonRequest(first.body)),
      request('/v1/license-leases', jsonRequest(second.body)),
    ]);
    expect(
      responses
        .map((response) => response.status)
        .sort((left, right) => left - right),
    ).toEqual([201, 409]);

    const winner = responses[0].status === 201 ? first : second;
    const session = await env.DB.prepare(
      `SELECT generation, refresh_token_hash AS refreshTokenHash,
              previous_refresh_token_hash AS previousRefreshTokenHash,
              last_rotation_id AS lastRotationId
       FROM device_sessions WHERE device_id = ?`,
    )
      .bind(identity.deviceId)
      .first<{
        generation: number;
        refreshTokenHash: string;
        previousRefreshTokenHash: string;
        lastRotationId: string;
      }>();
    expect(session).toMatchObject({
      generation: proof.lease.generation + 1,
      refreshTokenHash: winner.body.nextRefreshTokenHash,
      previousRefreshTokenHash: await sha256(identity.refreshToken),
      lastRotationId: winner.body.rotationId,
    });
  });

  it('rejects malformed device signatures without returning an internal error', async () => {
    const { identity, proof } = await authorizedDevice(4101);
    const { body } = await createRotationRequest(identity, proof);

    const response = await request(
      '/v1/license-leases',
      jsonRequest({ ...body, signature: 'not-valid-base64!' }),
    );
    expect(response.status).toBe(403);

    const retry = await request('/v1/license-leases', jsonRequest(body));
    expect(retry.status).toBe(201);
  });

  it('rejects a device immediately after the owner revokes it', async () => {
    const telegramId = 5001;
    const { identity, proof } = await authorizedDevice(telegramId);
    mockTelegram();
    await activatePairing(
      { id: 'unused' },
      telegramId,
      `/revoke_device ${identity.deviceId}`,
    );

    const response = await request(
      '/v1/license-challenges',
      jsonRequest({
        deviceId: identity.deviceId,
        sessionId: proof.lease.sessionId,
        generation: proof.lease.generation,
      }),
    );
    expect(response.status).toBe(403);
    await expect(
      response.json<{ error_code: string; detail: string }>(),
    ).resolves.toMatchObject({ error_code: 'license_device_unauthorized' });
  });

  it('returns an explicit code for a verified expired lease', async () => {
    const { identity, proof } = await authorizedDevice(5901);
    const now = Date.now();
    const lease = {
      ...proof.lease,
      issuedAt: new Date(now - 7 * 60 * 60_000).toISOString(),
      refreshAfter: new Date(now - 6.5 * 60 * 60_000).toISOString(),
      expiresAt: new Date(now - 60 * 60_000).toISOString(),
    };
    const expiredProof = {
      lease,
      signature: await signEd25519(
        env.SIGMO_LICENSE_PRIVATE_KEY,
        TEXT_ENCODER.encode(JSON.stringify(lease)),
      ),
    };
    const path = '/v1/configurations/current';
    const response = await request(path, {
      headers: await signedHeaders(identity, expiredProof, 'GET', path),
    });

    expect(response.status).toBe(401);
    await expect(
      response.json<{ error_code: string; detail: string }>(),
    ).resolves.toMatchObject({ error_code: 'license_lease_expired' });
  });

  it('rejects a lease that exceeds the protocol validity limits', async () => {
    const { identity, proof } = await authorizedDevice(5951);
    const issuedAt = Date.now() - 60_000;
    const path = '/v1/configurations/current';
    const invalidLeases = [
      {
        ...proof.lease,
        issuedAt: new Date(issuedAt).toISOString(),
        refreshAfter: new Date(issuedAt + 30 * 60_000 + 1_000).toISOString(),
        expiresAt: new Date(issuedAt + 6 * 60 * 60_000).toISOString(),
      },
      {
        ...proof.lease,
        issuedAt: new Date(issuedAt).toISOString(),
        refreshAfter: new Date(issuedAt + 30 * 60_000).toISOString(),
        expiresAt: new Date(issuedAt + 6 * 60 * 60_000 + 1_000).toISOString(),
      },
    ];

    for (const lease of invalidLeases) {
      const oversizedProof = {
        lease,
        signature: await signEd25519(
          env.SIGMO_LICENSE_PRIVATE_KEY,
          TEXT_ENCODER.encode(JSON.stringify(lease)),
        ),
      };
      const response = await request(path, {
        headers: await signedHeaders(identity, oversizedProof, 'GET', path),
      });

      expect(response.status).toBe(401);
      await expect(
        response.json<{ error_code: string; detail: string }>(),
      ).resolves.toMatchObject({ error_code: 'authorization_required' });
    }
  });

  it('authenticates configuration HEAD without marking the device seen', async () => {
    const { identity, proof } = await authorizedDevice(6001);
    const path = '/v1/configurations/current';
    const markSeen = vi.spyOn(Database.prototype, 'markDeviceSeen');

    const unauthorized = await request(path, {
      method: 'HEAD',
      headers: await signedHeaders(identity, proof, 'GET', path),
    });
    expect(unauthorized.status).toBe(401);

    const head = await request(path, {
      method: 'HEAD',
      headers: await signedHeaders(identity, proof, 'HEAD', path),
    });
    // Authentication succeeded, and the route now requires a recipient.
    expect(head.status).toBe(400);
    expect(await head.text()).toBe('');
    expect(markSeen).not.toHaveBeenCalled();

    const get = await request(path, {
      headers: await signedHeaders(identity, proof, 'GET', path),
    });
    expect(get.status).toBe(400);
    expect(markSeen).toHaveBeenCalledOnce();
  });
});

describe('request boundaries', () => {
  it('rejects oversized JSON bodies before parsing', async () => {
    const response = await request(
      '/v1/license-challenges',
      jsonRequest({ value: 'x'.repeat(70 * 1024) }),
    );
    expect(response.status).toBe(413);
    await expect(
      response.json<{ error_code: string; detail: string }>(),
    ).resolves.toMatchObject({
      error_code: 'request_body_too_large',
      detail: 'request body exceeds size limit',
    });
  });

  it('returns Problem Details for unknown resources', async () => {
    const response = await request('/v1/unknown-resource');
    expect(response.status).toBe(404);
    await expect(
      response.json<{ error_code: string; detail: string }>(),
    ).resolves.toMatchObject({
      error_code: 'resource_not_found',
      detail: 'resource not found',
    });
  });

  it('returns 405 and Allow for a known resource with the wrong method', async () => {
    const response = await request('/v1/license-pairings', { method: 'GET' });
    expect(response.status).toBe(405);
    expect(response.headers.get('allow')).toBe('POST, OPTIONS');
    await expect(
      response.json<{ error_code: string; detail: string }>(),
    ).resolves.toMatchObject({
      error_code: 'method_not_allowed',
      detail: 'This resource supports POST, OPTIONS.',
    });
  });

  it('uses numeric administrator IDs and secure random tokens', () => {
    expect(telegramAdmins(env)).toEqual(new Set([1001]));
    expect(randomToken()).toMatch(/^[A-Za-z0-9_-]+$/);
  });
});
