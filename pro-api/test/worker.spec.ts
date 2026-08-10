import { env } from "cloudflare:workers";
import {
  createExecutionContext,
  waitOnExecutionContext,
} from "cloudflare:test";
import { afterEach, assert, describe, expect, it, vi } from "vitest";

import {
  decodeBase64,
  encodeBase64,
  randomToken,
  signEd25519,
  textEncoder,
} from "../src/crypto";
import worker, { testExports } from "../src/index";
import { createTicket, parseTicket } from "../src/tickets";
import type {
  DownloadTicket,
  ReleaseChannel,
  SignedLease,
} from "../src/types";
import { manifest as parseManifest } from "../src/validation";

const IncomingRequest = Request<unknown, IncomingRequestCfProperties>;

interface DeviceIdentity {
  deviceId: string;
  publicKey: string;
  privateKey: CryptoKey;
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
  | { type: "all_private_chats" }
  | { type: "chat"; chat_id: number };

type TelegramCommandCall = {
  method: "setMyCommands" | "deleteMyCommands";
  commands?: Array<{ command: string; description: string }>;
  scope: TelegramCommandScope;
};

type TelegramAPIResponse = {
  ok: boolean;
  result?: boolean;
  description?: string;
};

type TelegramAPIResponder =
  | TelegramAPIResponse
  | ((call: TelegramCommandCall) => TelegramAPIResponse);

type TelegramMessage = {
  chat_id: number;
  text: string;
  reply_markup?: {
    inline_keyboard: Array<Array<{ text: string; url: string }>>;
  };
};

const bootstrapTargets = [
  "linux-amd64",
  "linux-amd64-musl",
  "linux-arm64",
  "linux-arm64-musl",
  "linux-arm",
  "linux-arm-musl",
] as const;

afterEach(async () => {
  vi.restoreAllMocks();
  await env.DB.prepare("DELETE FROM telegram_command_admins").run();
});

async function request(
  path: string,
  init: RequestInit<IncomingRequestCfProperties> = {},
): Promise<Response> {
  const ctx = createExecutionContext();
  const response = await worker.fetch(
    new IncomingRequest(new URL(path, "https://updates.example"), init),
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
  return {
    method: "POST",
    headers: { "content-type": "application/json", ...headers },
    body: JSON.stringify(value),
  };
}

async function createIdentity(): Promise<DeviceIdentity> {
  const pair = await crypto.subtle.generateKey("Ed25519", true, [
    "sign",
    "verify",
  ]);
  assert("privateKey" in pair && "publicKey" in pair);
  const exported = await crypto.subtle.exportKey("raw", pair.publicKey);
  assert(exported instanceof ArrayBuffer);
  const rawPublicKey = exported;
  const publicKey = encodeBase64(rawPublicKey);
  return {
    deviceId: await testExports.deviceID(publicKey),
    publicKey,
    privateKey: pair.privateKey,
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
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    const outgoing = new Request(input, init);
    const url = new URL(outgoing.url);
    if (
      outgoing.method !== "POST" ||
      url.origin !== "https://api.telegram.org"
    ) {
      throw new Error(`unexpected outbound request: ${outgoing.url}`);
    }
    if (!url.pathname.endsWith("/sendMessage"))
      throw new Error(`unexpected Telegram method: ${url.pathname}`);
    messages.push(await outgoing.json<TelegramMessage>());
    return Response.json({ ok: true });
  });
  return messages;
}

async function publishRelease(
  channel: ReleaseChannel,
  version: string,
  targets: readonly string[] = bootstrapTargets,
): Promise<void> {
  const commit = "0123456789abcdef0123456789abcdef01234567";
  const manifest = JSON.stringify({
    schemaVersion: 1,
    edition: "pro",
    channel,
    version,
    commit,
    publishedAt: "2026-08-09T12:00:00Z",
    notes: "release notes",
    artifacts: targets.map((target) => ({
      target,
      name: `sigmo-pro-${target}.gz`,
      compression: "gzip" as const,
      size: 10,
      sha256: "0".repeat(64),
      executableSize: 8,
      executableSha256: "1".repeat(64),
    })),
  });
  await env.sigmo_pro_updates.put(`${channel}/latest/manifest.json`, manifest);
}

function downloadButtons(message: TelegramMessage): Array<{
  text: string;
  url: string;
}> {
  return message.reply_markup?.inline_keyboard.flat() ?? [];
}

function tamperTicketSignature(ticket: string): string {
  const [payload, signature, extra] = ticket.split(".");
  assert(payload && signature && extra === undefined);
  const replacement = signature.startsWith("A") ? "B" : "A";
  return `${payload}.${replacement}${signature.slice(1)}`;
}

function mockTelegramCommands(
  responder: TelegramAPIResponder = { ok: true, result: true },
): TelegramCommandCall[] {
  const calls: TelegramCommandCall[] = [];
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    const outgoing = new Request(input, init);
    const url = new URL(outgoing.url);
    if (
      outgoing.method !== "POST" ||
      url.origin !== "https://api.telegram.org"
    ) {
      throw new Error(`unexpected outbound request: ${outgoing.url}`);
    }
    const method: TelegramCommandCall["method"] | null = url.pathname.endsWith(
      "/setMyCommands",
    )
      ? "setMyCommands"
      : url.pathname.endsWith("/deleteMyCommands")
        ? "deleteMyCommands"
        : null;
    if (!method) throw new Error(`unexpected Telegram method: ${url.pathname}`);
    const body = await outgoing.json<{
      commands?: Array<{ command: string; description: string }>;
      scope: TelegramCommandScope;
    }>();
    const call: TelegramCommandCall = { method, ...body };
    calls.push(call);
    const response =
      typeof responder === "function" ? responder(call) : responder;
    return Response.json(response);
  });
  return calls;
}

async function runScheduled(workerEnv: Env = env): Promise<void> {
  await worker.scheduled(
    {
      scheduledTime: Date.now(),
      cron: "0 * * * *",
      noRetry() {},
    },
    workerEnv,
    createExecutionContext(),
  );
}

async function telegramCommandAdminIDs(): Promise<number[]> {
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
    "/v1/license-pairings",
    jsonRequest({
      deviceId: identity.deviceId,
      publicKey: identity.publicKey,
    }),
  );
  expect(response.status).toBe(201);
  return response.json<PairingCreated>();
}

async function activatePairing(
  pairing: Pick<PairingCreated, "id">,
  telegramId: number,
  text = `/start ${pairing.id}`,
): Promise<void> {
  const response = await request(
    "/v1/telegram-updates",
    jsonRequest(
      {
        message: {
          chat: { id: telegramId, type: "private" },
          from: {
            id: telegramId,
            first_name: "Test",
            last_name: "User",
            username: `user${telegramId}`,
          },
          text,
        },
      },
      {
        "x-telegram-bot-api-secret-token": env.SIGMO_TELEGRAM_WEBHOOK_SECRET,
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
  expect(status.status).toBe("active");
  assert(status.lease);
  return { identity, proof: status.lease };
}

async function signedHeaders(
  identity: DeviceIdentity,
  proof: SignedLease,
  method: string,
  rawURL: string,
  additional: HeadersInit = {},
): Promise<Headers> {
  const url = new URL(rawURL, "https://updates.example");
  const timestamp = String(Math.floor(Date.now() / 1000));
  const message = `${method}\n${url.pathname}${url.search}\n${timestamp}`;
  const signature = await crypto.subtle.sign(
    "Ed25519",
    identity.privateKey,
    textEncoder.encode(message),
  );
  return new Headers({
    ...additional,
    "x-sigmo-device-id": identity.deviceId,
    "x-sigmo-lease": encodeBase64(
      textEncoder.encode(JSON.stringify(proof)),
      true,
    ),
    "x-sigmo-timestamp": timestamp,
    "x-sigmo-signature": encodeBase64(signature),
  });
}

describe("release manifests", () => {
  it("requires gzip artifacts and executable metadata", () => {
    const artifact = {
      target: "linux-amd64",
      name: "sigmo-pro-linux-amd64.gz",
      compression: "gzip",
      size: 10,
      sha256: "0".repeat(64),
      executableSize: 8,
      executableSha256: "1".repeat(64),
    };
    const manifest = {
      schemaVersion: 1,
      edition: "pro",
      channel: "stable",
      version: "v1.0.0",
      commit: "0123456789abcdef0123456789abcdef01234567",
      publishedAt: "2026-08-09T12:00:00Z",
      notes: "release notes",
      artifacts: [artifact],
    };
    expect(parseManifest(manifest)).not.toBeNull();

    const invalidArtifacts = [
      { ...artifact, compression: "none" },
      { ...artifact, name: "sigmo-pro-linux-amd64" },
      { ...artifact, executableSize: 0 },
      { ...artifact, executableSha256: "invalid" },
    ];
    for (const invalid of invalidArtifacts)
      expect(parseManifest({ ...manifest, artifacts: [invalid] })).toBeNull();
  });
});

describe("download tickets", () => {
  it("binds the signed payload and rejects expiry", async () => {
    const secret = "s".repeat(32);
    const ticket = await createTicket(secret, {
      deviceId: "device-1",
      channel: "dev",
      version: "dev-01234567",
      target: "linux-amd64",
      path: "dev/versions/dev-01234567/sigmo-pro-linux-amd64.gz",
      expiresAt: 200,
    });
    await expect(parseTicket(secret, ticket, 100_000)).resolves.toMatchObject({
      deviceId: "device-1",
      channel: "dev",
    });
    const [payload] = ticket.split(".");
    const wireTicket = JSON.parse(
      new TextDecoder().decode(decodeBase64(payload)),
    ) as Record<string, unknown>;
    expect(wireTicket).not.toHaveProperty("purpose");
    expect(wireTicket).not.toHaveProperty("telegramId");
    await expect(parseTicket(secret, ticket, 201_000)).resolves.toBeNull();
    await expect(parseTicket("wrong", ticket, 100_000)).resolves.toBeNull();
  });

  it("validates Bootstrap ticket signatures, expiry, and fields", async () => {
    const secret = "s".repeat(32);
    const fields = {
      purpose: "bootstrap" as const,
      telegramId: 1234,
      channel: "stable" as const,
      version: "v1.0.0",
      target: "linux-amd64",
      path: "stable/versions/v1.0.0/sigmo-pro-linux-amd64.gz",
      expiresAt: 200,
    };
    const ticket = await createTicket(secret, fields);

    await expect(parseTicket(secret, ticket, 100_000)).resolves.toEqual(fields);
    const tampered = tamperTicketSignature(ticket);
    await expect(parseTicket(secret, tampered, 100_000)).resolves.toBeNull();
    await expect(parseTicket(secret, ticket, 200_000)).resolves.toBeNull();

    const wrongPath = await createTicket(secret, {
      ...fields,
      path: "dev/versions/v1.0.0/sigmo-pro-linux-amd64.gz",
    });
    await expect(parseTicket(secret, wrongPath, 100_000)).resolves.toBeNull();
    const mixedIdentity = await createTicket(secret, {
      ...fields,
      deviceId: "device-1",
    } as unknown as DownloadTicket);
    await expect(
      parseTicket(secret, mixedIdentity, 100_000),
    ).resolves.toBeNull();
  });

  it("rejects a weak ticket signing secret", async () => {
    await expect(
      createTicket("short", {
        deviceId: "device-1",
        channel: "stable",
        version: "v1.0.0",
        target: "linux-amd64",
        path: "stable/versions/v1.0.0/sigmo-pro-linux-amd64.gz",
        expiresAt: 200,
      }),
    ).rejects.toThrow("at least 32 characters");
  });
});

describe("Telegram pairing", () => {
  it("registers described command menus for users and administrators", async () => {
    const calls = mockTelegramCommands();

    await runScheduled();

    expect(calls).toHaveLength(2);
    const userRegistration = calls.find(
      ({ method, scope }) =>
        method === "setMyCommands" && scope.type === "all_private_chats",
    );
    expect(
      userRegistration?.commands?.map(({ command }) => command),
    ).toEqual(["start", "download", "devices", "revoke_device"]);
    const adminRegistration = calls.find(
      ({ method, scope }) =>
        method === "setMyCommands" && scope.type === "chat",
    );
    expect(adminRegistration?.scope).toEqual({ type: "chat", chat_id: 1001 });
    expect(
      adminRegistration?.commands?.map(({ command }) => command),
    ).toEqual([
      "start",
      "download",
      "grant",
      "revoke",
      "status",
      "entitlements",
      "devices",
      "revoke_device",
    ]);
    expect(
      calls.flatMap(({ commands }) => commands ?? []).every(
        ({ command, description }) =>
          /^[a-z0-9_]{1,32}$/.test(command) &&
          description.length >= 1 &&
          description.length <= 256,
      ),
    ).toBe(true);
    await expect(telegramCommandAdminIDs()).resolves.toEqual([1001]);
  });

  it("deletes command scopes for removed administrators", async () => {
    await env.DB.batch([
      env.DB.prepare(
        "INSERT INTO telegram_command_admins (telegram_id) VALUES (?)",
      ).bind(1001),
      env.DB.prepare(
        "INSERT INTO telegram_command_admins (telegram_id) VALUES (?)",
      ).bind(1002),
    ]);
    const calls = mockTelegramCommands();

    await runScheduled();

    expect(calls).toContainEqual({
      method: "deleteMyCommands",
      scope: { type: "chat", chat_id: 1002 },
    });
    await expect(telegramCommandAdminIDs()).resolves.toEqual([1001]);
  });

  it("keeps the previous scope state when Telegram does not return true", async () => {
    await env.DB.prepare(
      "INSERT INTO telegram_command_admins (telegram_id) VALUES (?)",
    )
      .bind(1002)
      .run();
    mockTelegramCommands(({ method }) =>
      method === "deleteMyCommands"
        ? { ok: true, result: false }
        : { ok: true, result: true },
    );

    await expect(runScheduled()).rejects.toThrow(
      "Telegram deleteMyCommands did not return true",
    );
    await expect(telegramCommandAdminIDs()).resolves.toEqual([1002]);
  });

  it("does not reconcile command menus from /start", async () => {
    const methods: string[] = [];
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
      const outgoing = new Request(input, init);
      const method = new URL(outgoing.url).pathname.split("/").at(-1) ?? "";
      methods.push(method);
      return Response.json({ ok: true });
    });

    await activatePairing({ id: "unused" }, 3201, "/start");

    expect(methods).toEqual(["sendMessage"]);
  });

  it("shows the Telegram ID, entitlement status, and download commands from /start", async () => {
    const activeTelegramId = 3202;
    const inactiveTelegramId = 3203;
    await grant(activeTelegramId);
    const messages = mockTelegram();

    await activatePairing({ id: "unused" }, activeTelegramId, "/start");
    await activatePairing({ id: "unused" }, inactiveTelegramId, "/start");

    expect(messages).toHaveLength(2);
    expect(messages[0].text).toContain(`Telegram ID: ${activeTelegramId}`);
    expect(messages[0].text).toContain("Sigmo Pro entitlement: active");
    expect(messages[0].text).toContain("/download stable");
    expect(messages[0].text).toContain("/download dev");
    expect(messages[1].text).toContain(`Telegram ID: ${inactiveTelegramId}`);
    expect(messages[1].text).toContain("Sigmo Pro entitlement: inactive");
    expect(messages.every((message) => !message.reply_markup)).toBe(true);
  });

  it("returns Stable downloads in the fixed architecture and libc order", async () => {
    const telegramId = 3211;
    await grant(telegramId);
    await publishRelease("stable", "v3.0.0", [...bootstrapTargets].reverse());
    const messages = mockTelegram();
    const issuedAfter = Math.floor(Date.now() / 1000);

    await activatePairing({ id: "unused" }, telegramId, "/download");
    await activatePairing(
      { id: "unused" },
      telegramId,
      "/download stable",
    );

    expect(messages).toHaveLength(2);
    for (const message of messages) {
      expect(message.text).toContain("Channel: Stable");
      expect(message.text).toContain("Version: v3.0.0");
      expect(message.text).toContain("expire in 15 minutes");
      expect(message.text).toContain("gzip -d");
      expect(message.text).toContain("chmod +x");
      expect(
        message.reply_markup?.inline_keyboard.map((row) =>
          row.map(({ text }) => text),
        ),
      ).toEqual([
        ["amd64", "amd64 (musl)"],
        ["arm64", "arm64 (musl)"],
        ["arm", "arm (musl)"],
      ]);
    }

    const buttons = downloadButtons(messages[0]);
    expect(buttons).toHaveLength(6);
    const tickets = await Promise.all(
      buttons.map(async ({ url }) => {
        const downloadURL = new URL(url);
        expect(downloadURL.protocol).toBe("https:");
        expect(downloadURL.origin).toBe("https://updates.example");
        const ticketValue = downloadURL.pathname.split("/").at(-1) ?? "";
        return parseTicket(env.SIGMO_DOWNLOAD_TICKET_SECRET, ticketValue);
      }),
    );
    expect(tickets.map((ticket) => ticket?.target)).toEqual(bootstrapTargets);
    for (const ticket of tickets) {
      expect(ticket).toMatchObject({
        purpose: "bootstrap",
        telegramId,
        channel: "stable",
        version: "v3.0.0",
      });
      expect(ticket?.expiresAt).toBeGreaterThanOrEqual(issuedAfter + 899);
      expect(ticket?.expiresAt).toBeLessThanOrEqual(
        Math.floor(Date.now() / 1000) + 900,
      );
    }
  });

  it("returns Dev downloads to every active entitlement", async () => {
    const telegramId = 3212;
    await grant(telegramId);
    await publishRelease("dev", "dev-01234567");
    const messages = mockTelegram();

    await activatePairing({ id: "unused" }, telegramId, "/download dev");

    expect(messages).toHaveLength(1);
    expect(messages[0].text).toContain("Channel: Dev");
    expect(messages[0].text).toContain("Version: dev-01234567");
    expect(downloadButtons(messages[0])).toHaveLength(6);
  });

  it("returns one button for an exact channel and target", async () => {
    const telegramId = 3213;
    await grant(telegramId);
    await publishRelease("stable", "v3.1.0", ["linux-arm64-musl"]);
    const messages = mockTelegram();

    await activatePairing(
      { id: "unused" },
      telegramId,
      "/download stable linux-arm64-musl",
    );

    expect(messages).toHaveLength(1);
    expect(messages[0].reply_markup?.inline_keyboard).toEqual([
      [expect.objectContaining({ text: "arm64 (musl)" })],
    ]);
    const [button] = downloadButtons(messages[0]);
    const ticketValue = new URL(button.url).pathname.split("/").at(-1) ?? "";
    await expect(
      parseTicket(env.SIGMO_DOWNLOAD_TICKET_SECRET, ticketValue),
    ).resolves.toMatchObject({
      purpose: "bootstrap",
      telegramId,
      channel: "stable",
      target: "linux-arm64-musl",
    });
  });

  it("returns download usage and legal targets for unknown arguments", async () => {
    const messages = mockTelegram();

    await activatePairing({ id: "unused" }, 3214, "/download beta");
    await activatePairing(
      { id: "unused" },
      3214,
      "/download stable windows-amd64",
    );

    expect(messages).toHaveLength(2);
    for (const message of messages) {
      expect(message.text).toContain("Usage:");
      expect(message.text).toContain("stable|dev");
      expect(message.text).toContain(bootstrapTargets.join(", "));
      expect(message.reply_markup).toBeUndefined();
    }
  });

  it("does not mint downloads without an active entitlement", async () => {
    await publishRelease("stable", "v3.2.0");
    const messages = mockTelegram();

    await activatePairing({ id: "unused" }, 3215, "/download");

    expect(messages).toHaveLength(1);
    expect(messages[0].text).toContain("does not have an active Sigmo Pro entitlement");
    expect(messages[0].reply_markup).toBeUndefined();
  });

  it("does not mint downloads when a release is missing", async () => {
    const telegramId = 3216;
    await grant(telegramId);
    await env.sigmo_pro_updates.delete("dev/latest/manifest.json");
    const messages = mockTelegram();

    await activatePairing({ id: "unused" }, telegramId, "/download dev");

    expect(messages).toHaveLength(1);
    expect(messages[0].text).toContain("is not available yet");
    expect(messages[0].reply_markup).toBeUndefined();
  });

  it("does not mint a link when the requested target is missing", async () => {
    const telegramId = 3217;
    await grant(telegramId);
    await publishRelease("stable", "v3.3.0", ["linux-amd64"]);
    const messages = mockTelegram();

    await activatePairing(
      { id: "unused" },
      telegramId,
      "/download stable linux-arm-musl",
    );

    expect(messages).toHaveLength(1);
    expect(messages[0].text).toContain("does not include linux-arm-musl");
    expect(messages[0].reply_markup).toBeUndefined();
  });

  it("compares the webhook secret before processing an update", async () => {
    const outbound = vi.spyOn(globalThis, "fetch");
    const response = await request(
      "/v1/telegram-updates",
      jsonRequest(
        {
          message: {
            chat: { id: 1, type: "private" },
            text: "/devices",
          },
        },
        { "x-telegram-bot-api-secret-token": "wrong-secret" },
      ),
    );
    expect(response.status).toBe(403);
    await expect(
      response.json<{ error_code: string; message: string }>(),
    ).resolves.toEqual({
      error_code: "telegram_webhook_unauthorized",
      message: "invalid webhook secret",
    });
    expect(outbound).not.toHaveBeenCalled();
  });

  it("expires stale pairings without authorizing a device", async () => {
    const pairing = await createPairing(await createIdentity());
    await env.DB.prepare(
      "UPDATE pairings SET expires_at = ? WHERE pairing_id = ?",
    )
      .bind(new Date(Date.now() - 60_000).toISOString(), pairing.id)
      .run();

    const status = await pollPairing(pairing);
    expect(status.status).toBe("expired");
  });

  it("rejects device keys that are not raw Ed25519 public keys", async () => {
    const response = await request(
      "/v1/license-pairings",
      jsonRequest({
        deviceId: "not-a-device",
        publicKey: encodeBase64(new Uint8Array(31)),
      }),
    );
    expect(response.status).toBe(400);
    await expect(
      response.json<{ error_code: string; message: string }>(),
    ).resolves.toEqual({
      error_code: "device_public_key_invalid",
      message: "publicKey is not a valid Ed25519 public key",
    });
  });

  it("keeps an unentitled Telegram user pending", async () => {
    const pairing = await createPairing(await createIdentity());
    const messages = mockTelegram();
    await activatePairing(pairing, 2001);

    expect(messages).toHaveLength(1);
    expect(messages[0].text).toContain("does not have an active Sigmo Pro entitlement");
    const status = await pollPairing(pairing);
    expect(status.status).toBe("pending");
  });

  it("enforces the device limit inside the activation transaction", async () => {
    await grant(3001, 1);
    const first = await createPairing(await createIdentity());
    const second = await createPairing(await createIdentity());
    const messages = mockTelegram();

    await activatePairing(first, 3001);
    await activatePairing(second, 3001);

    expect(messages.at(-1)?.text).toContain("Device limit reached");
    expect((await pollPairing(first)).status).toBe("active");
    expect((await pollPairing(second)).status).toBe("pending");
  });

  it("does not rebind an active device to another Telegram user", async () => {
    await grant(3051);
    await grant(3052);
    const identity = await createIdentity();
    const first = await createPairing(identity);
    const second = await createPairing(identity);
    const messages = mockTelegram();

    await activatePairing(first, 3051);
    await activatePairing(second, 3052);

    expect(messages.at(-1)?.text).toContain("linked to another Telegram account");
    expect((await pollPairing(first)).status).toBe("active");
    expect((await pollPairing(second)).status).toBe("pending");
    const owner = await env.DB.prepare(
      "SELECT telegram_id FROM devices WHERE device_id = ?",
    )
      .bind(identity.deviceId)
      .first<{ telegram_id: number }>();
    expect(owner?.telegram_id).toBe(3051);
  });

  it("limits outstanding pairings per device", async () => {
    const identity = await createIdentity();
    for (let index = 0; index < 3; index += 1) await createPairing(identity);

    const response = await request(
      "/v1/license-pairings",
      jsonRequest({
        deviceId: identity.deviceId,
        publicKey: identity.publicKey,
      }),
    );
    expect(response.status).toBe(429);
    await expect(
      response.json<{ error_code: string; message: string }>(),
    ).resolves.toMatchObject({ error_code: "license_pairing_limit_exceeded" });
  });

  it("enforces the outstanding pairing limit under concurrency", async () => {
    const identity = await createIdentity();
    const responses = await Promise.all(
      Array.from({ length: 6 }, () =>
        request(
          "/v1/license-pairings",
          jsonRequest({
            deviceId: identity.deviceId,
            publicKey: identity.publicKey,
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

  it("ignores commands received from Telegram group chats", async () => {
    const messages = mockTelegram();
    const telegramId = 1001;
    const grantedId = 3099;
    const response = await request(
      "/v1/telegram-updates",
      jsonRequest(
        {
          message: {
            chat: { id: -100123, type: "supergroup" },
            from: {
              id: telegramId,
              first_name: "Admin",
            },
            text: `/grant ${grantedId}`,
          },
        },
        {
          "x-telegram-bot-api-secret-token": env.SIGMO_TELEGRAM_WEBHOOK_SECRET,
        },
      ),
    );

    expect(response.status).toBe(200);
    expect(messages).toHaveLength(0);
    const entitlement = await env.DB.prepare(
      "SELECT telegram_id FROM entitlements WHERE telegram_id = ?",
    )
      .bind(grantedId)
      .first();
    expect(entitlement).toBeNull();
  });

  it("ignores commands addressed to another Telegram bot", async () => {
    const messages = mockTelegram();
    const telegramId = 3088;

    await activatePairing(
      { id: "unused" },
      1001,
      `/grant@AnotherBot ${telegramId}`,
    );

    expect(messages).toHaveLength(0);
    const entitlement = await env.DB.prepare(
      "SELECT telegram_id FROM entitlements WHERE telegram_id = ?",
    )
      .bind(telegramId)
      .first();
    expect(entitlement).toBeNull();
  });

  it("executes administrator grant, status, and revoke commands", async () => {
    const messages = mockTelegram();
    const admin = 1001;
    const telegramId = 3101;
    const expiresAt = "2099-08-09";

    await activatePairing(
      { id: "unused" },
      admin,
      `/grant ${telegramId} 2 ${expiresAt}`,
    );
    const granted = await env.DB.prepare(
      "SELECT * FROM entitlements WHERE telegram_id = ?",
    )
      .bind(telegramId)
      .first<{ status: string; max_devices: number; expires_at: string }>();
    expect(granted).toMatchObject({
      status: "active",
      max_devices: 2,
      expires_at: `${expiresAt}T23:59:59.999Z`,
    });

    await activatePairing({ id: "unused" }, admin, `/status ${telegramId}`);
    expect(messages.at(-1)?.text).toContain(`${telegramId}: active`);

    await activatePairing({ id: "unused" }, admin, `/revoke ${telegramId}`);
    const revoked = await env.DB.prepare(
      "SELECT status FROM entitlements WHERE telegram_id = ?",
    )
      .bind(telegramId)
      .first<{ status: string }>();
    expect(revoked?.status).toBe("revoked");
  });

  it("lists only active unexpired entitlements for administrators", async () => {
    await env.DB.prepare("UPDATE entitlements SET status = 'revoked'").run();
    const activeId = 3151;
    const expiringId = 3152;
    const revokedId = 3153;
    const expiredId = 3154;
    await grant(activeId, 2);
    await grant(expiringId, 4, "2099-12-31T23:59:59.999Z");
    await grant(revokedId);
    await grant(expiredId, 3, "2000-01-01T23:59:59.999Z");
    const timestamp = new Date().toISOString();
    await env.DB.batch([
      env.DB.prepare(
        "UPDATE entitlements SET display_name = ?, username = ? WHERE telegram_id = ?",
      ).bind("Alice Admin", "alice", activeId),
      env.DB.prepare(
        "UPDATE entitlements SET status = 'revoked' WHERE telegram_id = ?",
      ).bind(revokedId),
      env.DB.prepare(
        `INSERT INTO devices
           (device_id, telegram_id, public_key, created_at, last_seen_at, revoked_at)
         VALUES (?, ?, ?, ?, ?, NULL), (?, ?, ?, ?, ?, ?)`,
      ).bind(
        "00000000000000000000000000003151",
        activeId,
        "public-key-1",
        timestamp,
        timestamp,
        "00000000000000000000000000003152",
        activeId,
        "public-key-2",
        timestamp,
        timestamp,
        timestamp,
      ),
    ]);
    const messages = mockTelegram();

    await activatePairing({ id: "unused" }, 1001, "/entitlements");

    expect(messages).toHaveLength(1);
    expect(messages[0].text).toContain("Active entitlements (2):");
    expect(messages[0].text).toContain(
      `${activeId} | Alice Admin @alice | devices 1/2 | expires never`,
    );
    expect(messages[0].text).toContain(
      `${expiringId} | User ${expiringId} @user${expiringId} | devices 0/4 | expires 2099-12-31`,
    );
    expect(messages[0].text).not.toContain(String(revokedId));
    expect(messages[0].text).not.toContain(String(expiredId));
  });

  it("does not expose entitlement lists to regular users", async () => {
    const entitlementId = 3161;
    await grant(entitlementId);
    const messages = mockTelegram();

    await activatePairing({ id: "unused" }, 3162, "/entitlements");

    expect(messages).toHaveLength(1);
    expect(messages[0].text).toBe(
      [
        "Available commands:",
        "/start [pairing_id] — Show Pro status or authorize a device",
        "/download [stable|dev] [target] — Download Sigmo Pro",
        "/devices — List linked devices",
        "/revoke_device <device_id> — Revoke a linked device by ID",
      ].join("\n"),
    );
    expect(messages[0].text).not.toContain(String(entitlementId));
  });

  it("shows administrator-specific command usage in help", async () => {
    const messages = mockTelegram();

    await activatePairing({ id: "unused" }, 1001, "/unknown");

    expect(messages).toHaveLength(1);
    expect(messages[0].text).toContain("/devices [telegram_id]");
    expect(messages[0].text).toContain("/revoke_device <device_id>");
    expect(messages[0].text).toContain(
      "/grant <telegram_id> [max_devices] [expires_at]",
    );
  });

  it("splits long entitlement lists into Telegram-sized messages", async () => {
    await env.DB.prepare("UPDATE entitlements SET status = 'revoked'").run();
    const telegramId = 3171;
    await grant(telegramId);
    await env.DB.prepare(
      "UPDATE entitlements SET display_name = ? WHERE telegram_id = ?",
    )
      .bind("A".repeat(5_000), telegramId)
      .run();
    const messages = mockTelegram();

    await activatePairing({ id: "unused" }, 1001, "/entitlements");

    expect(messages.length).toBeGreaterThan(1);
    expect(messages.every((message) => message.text.length <= 4_000)).toBe(
      true,
    );
    expect(messages.some((message) => message.text.includes(String(telegramId)))).toBe(
      true,
    );
  });

  it.each(["2027-02-30", "2099-08-09T12:00:00Z", "2099-8-9"])(
    "rejects invalid administrator expiry date %s",
    async (expiresAt) => {
      const messages = mockTelegram();
      const telegramId = 3199;

      await activatePairing(
        { id: "unused" },
        1001,
        `/grant ${telegramId} 3 ${expiresAt}`,
      );

      expect(messages.at(-1)?.text).toContain("Usage: /grant");
      const entitlement = await env.DB.prepare(
        "SELECT telegram_id FROM entitlements WHERE telegram_id = ?",
      )
        .bind(telegramId)
        .first();
      expect(entitlement).toBeNull();
    },
  );
});

describe("device authorization", () => {
  it("preserves Telegram IDs beyond signed 32-bit range", async () => {
    const telegramId = 4_000_000_001;
    const { proof } = await authorizedDevice(telegramId);
    expect(proof.lease.telegramId).toBe(telegramId);
  });

  it("consumes a startup challenge exactly once", async () => {
    const { identity } = await authorizedDevice(4001);
    const challengeResponse = await request(
      "/v1/license-challenges",
      jsonRequest({ deviceId: identity.deviceId }),
    );
    expect(challengeResponse.status).toBe(201);
    const challenge = await challengeResponse.json<{ challenge: string }>();
    const signature = await crypto.subtle.sign(
      "Ed25519",
      identity.privateKey,
      decodeBase64(challenge.challenge),
    );
    const body = {
      deviceId: identity.deviceId,
      challenge: challenge.challenge,
      signature: encodeBase64(signature),
    };

    const responses = await Promise.all([
      request("/v1/license-leases", jsonRequest(body)),
      request("/v1/license-leases", jsonRequest(body)),
    ]);
    expect(responses.map((response) => response.status).sort()).toEqual([
      201, 403,
    ]);
  });

  it("rejects malformed device signatures without returning an internal error", async () => {
    const { identity } = await authorizedDevice(4101);
    const challengeResponse = await request(
      "/v1/license-challenges",
      jsonRequest({ deviceId: identity.deviceId }),
    );
    const challenge = await challengeResponse.json<{ challenge: string }>();

    const response = await request(
      "/v1/license-leases",
      jsonRequest({
        deviceId: identity.deviceId,
        challenge: challenge.challenge,
        signature: "not-valid-base64!",
      }),
    );
    expect(response.status).toBe(403);

    const signature = await crypto.subtle.sign(
      "Ed25519",
      identity.privateKey,
      decodeBase64(challenge.challenge),
    );
    const retry = await request(
      "/v1/license-leases",
      jsonRequest({
        deviceId: identity.deviceId,
        challenge: challenge.challenge,
        signature: encodeBase64(signature),
      }),
    );
    expect(retry.status).toBe(201);
  });

  it("rejects a device immediately after the owner revokes it", async () => {
    const telegramId = 5001;
    const { identity } = await authorizedDevice(telegramId);
    mockTelegram();
    await activatePairing(
      { id: "unused" },
      telegramId,
      `/revoke_device ${identity.deviceId}`,
    );

    const response = await request(
      "/v1/license-challenges",
      jsonRequest({ deviceId: identity.deviceId }),
    );
    expect(response.status).toBe(403);
    await expect(
      response.json<{ error_code: string; message: string }>(),
    ).resolves.toMatchObject({ error_code: "license_device_unauthorized" });
  });
});

describe("private releases", () => {
  it("streams Bootstrap downloads and observes entitlement revocation immediately", async () => {
    const telegramId = 5801;
    await grant(telegramId);
    await publishRelease("stable", "v4.0.0", ["linux-amd64"]);
    await env.sigmo_pro_updates.put(
      "stable/versions/v4.0.0/sigmo-pro-linux-amd64.gz",
      "0123456789",
      {
        httpMetadata: {
          contentType: "application/octet-stream",
          contentEncoding: "gzip",
        },
      },
    );
    const messages = mockTelegram();
    await activatePairing(
      { id: "unused" },
      telegramId,
      "/download stable linux-amd64",
    );
    const [button] = downloadButtons(messages[0]);
    assert(button);

    const fullResponse = await request(button.url);
    expect(fullResponse.status).toBe(200);
    expect(fullResponse.headers.get("cache-control")).toBe("private, no-store");
    expect(fullResponse.headers.get("content-type")).toBe("application/gzip");
    expect(fullResponse.headers.get("content-encoding")).toBeNull();
    expect(fullResponse.headers.get("content-disposition")).toBe(
      'attachment; filename="sigmo-pro-linux-amd64.gz"',
    );
    expect(new TextDecoder().decode(await fullResponse.arrayBuffer())).toBe(
      "0123456789",
    );

    const rangeResponse = await request(button.url, {
      headers: { range: "bytes=3-6" },
    });
    expect(rangeResponse.status).toBe(206);
    expect(rangeResponse.headers.get("content-type")).toBe("application/gzip");
    expect(rangeResponse.headers.get("content-encoding")).toBeNull();
    expect(rangeResponse.headers.get("content-range")).toBe("bytes 3-6/10");
    expect(new TextDecoder().decode(await rangeResponse.arrayBuffer())).toBe(
      "3456",
    );

    await env.DB.prepare(
      "UPDATE entitlements SET status = 'revoked' WHERE telegram_id = ?",
    )
      .bind(telegramId)
      .run();
    const revokedResponse = await request(button.url);
    expect(revokedResponse.status).toBe(403);
    await expect(
      revokedResponse.json<{ error_code: string; message: string }>(),
    ).resolves.toMatchObject({
      error_code: "license_entitlement_inactive",
    });
  });

  it("rejects tampered, expired, and malformed Bootstrap tickets", async () => {
    const expiresAt = Math.floor(Date.now() / 1000) + 300;
    const fields = {
      purpose: "bootstrap" as const,
      telegramId: 5802,
      channel: "stable" as const,
      version: "v4.1.0",
      target: "linux-amd64",
      path: "stable/versions/v4.1.0/sigmo-pro-linux-amd64.gz",
      expiresAt,
    };
    const valid = await createTicket(
      env.SIGMO_DOWNLOAD_TICKET_SECRET,
      fields,
    );
    const tampered = tamperTicketSignature(valid);
    const expired = await createTicket(env.SIGMO_DOWNLOAD_TICKET_SECRET, {
      ...fields,
      expiresAt: Math.floor(Date.now() / 1000) - 1,
    });
    const malformed = await createTicket(
      env.SIGMO_DOWNLOAD_TICKET_SECRET,
      { ...fields, deviceId: "device-1" } as unknown as DownloadTicket,
    );

    for (const ticket of [tampered, expired, malformed]) {
      const response = await request(`/v1/downloads/${ticket}`);
      expect(response.status).toBe(403);
      await expect(
        response.json<{ error_code: string; message: string }>(),
      ).resolves.toMatchObject({ error_code: "download_ticket_invalid" });
    }
  });

  it("returns an explicit code for a verified expired lease", async () => {
    const { identity, proof } = await authorizedDevice(5901);
    const now = Date.now();
    const lease = {
      ...proof.lease,
      issuedAt: new Date(now - 73 * 60 * 60_000).toISOString(),
      refreshAfter: new Date(now - 49 * 60 * 60_000).toISOString(),
      expiresAt: new Date(now - 60 * 60_000).toISOString(),
    };
    const expiredProof = {
      lease,
      signature: await signEd25519(
        env.SIGMO_LICENSE_PRIVATE_KEY,
        textEncoder.encode(JSON.stringify(lease)),
      ),
    };
    const path =
      "/v1/release-channels/stable/releases/latest?target=linux-amd64";
    const response = await request(path, {
      headers: await signedHeaders(identity, expiredProof, "GET", path),
    });

    expect(response.status).toBe(401);
    await expect(
      response.json<{ error_code: string; message: string }>(),
    ).resolves.toMatchObject({ error_code: "license_lease_expired" });
  });

  it("rejects a lease that exceeds the protocol validity limits", async () => {
    const { identity, proof } = await authorizedDevice(5951);
    const issuedAt = Date.now() - 60_000;
    const path =
      "/v1/release-channels/stable/releases/latest?target=linux-amd64";
    const invalidLeases = [
      {
        ...proof.lease,
        issuedAt: new Date(issuedAt).toISOString(),
        refreshAfter: new Date(
          issuedAt + 24 * 60 * 60_000 + 1_000,
        ).toISOString(),
        expiresAt: new Date(issuedAt + 72 * 60 * 60_000).toISOString(),
      },
      {
        ...proof.lease,
        issuedAt: new Date(issuedAt).toISOString(),
        refreshAfter: new Date(issuedAt + 24 * 60 * 60_000).toISOString(),
        expiresAt: new Date(issuedAt + 72 * 60 * 60_000 + 1_000).toISOString(),
      },
    ];

    for (const lease of invalidLeases) {
      const oversizedProof = {
        lease,
        signature: await signEd25519(
          env.SIGMO_LICENSE_PRIVATE_KEY,
          textEncoder.encode(JSON.stringify(lease)),
        ),
      };
      const response = await request(path, {
        headers: await signedHeaders(identity, oversizedProof, "GET", path),
      });

      expect(response.status).toBe(401);
      await expect(
        response.json<{ error_code: string; message: string }>(),
      ).resolves.toMatchObject({ error_code: "authorization_required" });
    }
  });

  it("selects the latest version signature and streams byte ranges", async () => {
    const { identity, proof } = await authorizedDevice(6001);
    const manifest = JSON.stringify({
      schemaVersion: 1,
      edition: "pro",
      channel: "stable",
      version: "v2.0.0",
      commit: "0123456789abcdef0123456789abcdef01234567",
      publishedAt: "2026-08-09T12:00:00Z",
      notes: "release notes",
      artifacts: [
        {
          target: "linux-amd64",
          name: "sigmo-pro-linux-amd64.gz",
          compression: "gzip",
          size: 10,
          sha256: "0".repeat(64),
          executableSize: 8,
          executableSha256: "1".repeat(64),
        },
      ],
    });
    await env.sigmo_pro_updates.put("stable/latest/manifest.json", manifest);
    await env.sigmo_pro_updates.put(
      "stable/versions/v1.0.0/manifest.json.sig",
      "signature-v1",
    );
    await env.sigmo_pro_updates.put(
      "stable/versions/v2.0.0/manifest.json.sig",
      "signature-v2",
    );
    await env.sigmo_pro_updates.put(
      "stable/versions/v2.0.0/sigmo-pro-linux-amd64.gz",
      "0123456789",
    );

    const releasePath =
      "/v1/release-channels/stable/releases/latest?target=linux-amd64";
    const releaseResponse = await request(releasePath, {
      headers: await signedHeaders(identity, proof, "GET", releasePath),
    });
    expect(releaseResponse.status).toBe(200);
    const release = await releaseResponse.json<{
      manifest: string;
      signature: string;
      downloadUrl: string;
    }>();
    expect(release.manifest).toBe(manifest);
    expect(release.signature).toBe("signature-v2");
    const deviceTicketValue = new URL(release.downloadUrl).pathname
      .split("/")
      .at(-1);
    assert(deviceTicketValue);
    const deviceTicket = await parseTicket(
      env.SIGMO_DOWNLOAD_TICKET_SECRET,
      deviceTicketValue,
    );
    expect(deviceTicket).toMatchObject({
      deviceId: identity.deviceId,
      channel: "stable",
      version: "v2.0.0",
      target: "linux-amd64",
    });
    expect(deviceTicket && "purpose" in deviceTicket).toBe(false);

    const downloadResponse = await request(release.downloadUrl, {
      headers: await signedHeaders(
        identity,
        proof,
        "GET",
        release.downloadUrl,
        { range: "bytes=2-5" },
      ),
    });
    expect(downloadResponse.status).toBe(206);
    expect(downloadResponse.headers.get("content-range")).toBe("bytes 2-5/10");
    expect(downloadResponse.headers.get("cache-control")).toBe(
      "private, no-store",
    );
    expect(downloadResponse.headers.get("content-type")).toBe(
      "application/gzip",
    );
    expect(downloadResponse.headers.get("content-encoding")).toBeNull();
    expect(new TextDecoder().decode(await downloadResponse.arrayBuffer())).toBe(
      "2345",
    );
  });
});

describe("request boundaries", () => {
  it("rejects oversized JSON bodies before parsing", async () => {
    const response = await request(
      "/v1/license-challenges",
      jsonRequest({ value: "x".repeat(70 * 1024) }),
    );
    expect(response.status).toBe(413);
    await expect(
      response.json<{ error_code: string; message: string }>(),
    ).resolves.toEqual({
      error_code: "request_body_too_large",
      message: "request body exceeds size limit",
    });
  });

  it("returns the standard JSON error for unknown resources", async () => {
    const response = await request("/v1/unknown-resource");
    expect(response.status).toBe(404);
    await expect(
      response.json<{ error_code: string; message: string }>(),
    ).resolves.toEqual({
      error_code: "resource_not_found",
      message: "resource not found",
    });
  });

  it("returns 405 and Allow for a known resource with the wrong method", async () => {
    const response = await request("/v1/license-pairings", { method: "GET" });
    expect(response.status).toBe(405);
    expect(response.headers.get("allow")).toBe("POST");
    await expect(
      response.json<{ error_code: string; message: string }>(),
    ).resolves.toEqual({
      error_code: "method_not_allowed",
      message: "method not allowed",
    });
  });

  it("returns a channel-specific error for unknown release channels", async () => {
    const response = await request(
      "/v1/release-channels/nightly/releases/latest",
    );
    expect(response.status).toBe(404);
    await expect(
      response.json<{ error_code: string; message: string }>(),
    ).resolves.toMatchObject({ error_code: "release_channel_not_found" });
  });

  it("uses numeric administrator IDs and secure random tokens", () => {
    expect(testExports.admins(env)).toEqual(new Set([1001]));
    expect(randomToken()).toMatch(/^[A-Za-z0-9_-]+$/);
  });
});
