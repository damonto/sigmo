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
import type { SignedLease } from "../src/types";

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

afterEach(() => {
  vi.restoreAllMocks();
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

function mockTelegram(): Array<{ chat_id: number; text: string }> {
  const messages: Array<{ chat_id: number; text: string }> = [];
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    const outgoing = new Request(input, init);
    if (
      outgoing.method !== "POST" ||
      new URL(outgoing.url).origin !== "https://api.telegram.org"
    ) {
      throw new Error(`unexpected outbound request: ${outgoing.url}`);
    }
    messages.push(await outgoing.json<{ chat_id: number; text: string }>());
    return Response.json({ ok: true });
  });
  return messages;
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

describe("download tickets", () => {
  it("binds the signed payload and rejects expiry", async () => {
    const secret = "s".repeat(32);
    const ticket = await createTicket(secret, {
      deviceId: "device-1",
      channel: "dev",
      version: "dev-01234567",
      target: "linux-amd64",
      path: "dev/versions/dev-01234567/sigmo-pro-linux-amd64",
      expiresAt: 200,
    });
    await expect(parseTicket(secret, ticket, 100_000)).resolves.toMatchObject({
      deviceId: "device-1",
      channel: "dev",
    });
    await expect(parseTicket(secret, ticket, 201_000)).resolves.toBeNull();
    await expect(parseTicket("wrong", ticket, 100_000)).resolves.toBeNull();
  });

  it("rejects a weak ticket signing secret", async () => {
    await expect(
      createTicket("short", {
        deviceId: "device-1",
        channel: "stable",
        version: "v1.0.0",
        target: "linux-amd64",
        path: "stable/versions/v1.0.0/sigmo-pro-linux-amd64",
        expiresAt: 200,
      }),
    ).rejects.toThrow("at least 32 characters");
  });
});

describe("Telegram pairing", () => {
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
      "Available commands: /devices, /revoke_device <device_id>",
    );
    expect(messages[0].text).not.toContain(String(entitlementId));
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
          name: "sigmo-pro-linux-amd64",
          size: 10,
          sha256: "0".repeat(64),
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
      "stable/versions/v2.0.0/sigmo-pro-linux-amd64",
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
