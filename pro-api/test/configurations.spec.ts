import { env } from 'cloudflare:workers';
import {
  createExecutionContext,
  waitOnExecutionContext,
} from 'cloudflare:test';
import { CompactEncrypt, compactDecrypt } from 'jose';
import { assert, expect, it } from 'vitest';

import worker from '../src/index';
import { deviceId, issueLease } from '../src/license/service';
import { Database } from '../src/storage/authorization';
import {
  decodeBase64,
  encodeBase64,
  TEXT_ENCODER,
  verifyEd25519,
} from '../src/security/crypto';

async function setup() {
  const signing = await crypto.subtle.generateKey('Ed25519', true, [
    'sign',
    'verify',
  ]);
  const recipient = await crypto.subtle.generateKey(
    { name: 'ECDH', namedCurve: 'P-256' },
    true,
    ['deriveBits'],
  );
  assert('privateKey' in signing && 'publicKey' in signing);
  assert('privateKey' in recipient && 'publicKey' in recipient);
  const rawPublicKey = await crypto.subtle.exportKey('raw', signing.publicKey);
  assert(rawPublicKey instanceof ArrayBuffer);
  const publicKey = encodeBase64(rawPublicKey);
  const id = await deviceId(publicKey);
  const now = new Date().toISOString();
  const telegramId = Number.parseInt(id.slice(0, 10), 16);
  await env.DB.prepare(
    `INSERT INTO entitlements
    (telegram_id,status,max_devices,display_name,username,created_at,updated_at)
    VALUES (?,'active',3,'Test','test',?,?)`,
  )
    .bind(telegramId, now, now)
    .run();
  await env.DB.prepare(
    `INSERT INTO devices
    (device_id,telegram_id,public_key,created_at,last_seen_at) VALUES (?,?,?,?,?)`,
  )
    .bind(id, telegramId, publicKey, now, now)
    .run();
  await env.DB.prepare(
    `INSERT INTO device_sessions
    (device_id,session_id,generation,refresh_token_hash,fingerprint_hash,updated_at)
    VALUES (?,?,1,'hash','fingerprint',?)`,
  )
    .bind(id, 'test-' + id, now)
    .run();
  const db = new Database(env.DB);
  const entitlement = await db.findEntitlement(telegramId);
  const device = await db.findDevice(id);
  const session = await db.findDeviceSession(id);
  if (!entitlement || !device || !session) {
    throw new Error('missing authorization fixture');
  }
  const context = createExecutionContext();
  const proof = await issueLease(
    { env, db, execution: context },
    entitlement,
    device,
    session,
  );
  const recipientBytes = await crypto.subtle.exportKey(
    'spki',
    recipient.publicKey,
  );
  assert(recipientBytes instanceof ArrayBuffer);
  const key = encodeBase64(recipientBytes, true);
  const path = `/v1/configurations/current?recipient=${key}`;
  const timestamp = String(Math.floor(Date.now() / 1000));
  const signature = encodeBase64(
    await crypto.subtle.sign(
      'Ed25519',
      signing.privateKey,
      TEXT_ENCODER.encode(`GET\n${path}\n${timestamp}`),
    ),
  );
  const headers = {
    'x-sigmo-device-id': id,
    'x-sigmo-lease': encodeBase64(
      TEXT_ENCODER.encode(JSON.stringify(proof)),
      true,
    ),
    'x-sigmo-timestamp': timestamp,
    'x-sigmo-signature': signature,
  };
  return { id, recipient, path, headers, proof };
}

async function request(path: string, headers: HeadersInit = {}) {
  const context = createExecutionContext();
  const response = await worker.fetch(
    new Request(`https://service.example${path}`, { headers }),
    env,
    context,
  );
  await waitOnExecutionContext(context);
  return response;
}

async function publish() {
  const files = { schemaVersion: 1, entitlement: [], wfc: [], ims: [] };
  const compressed = new Uint8Array(
    await new Response(
      new Blob([JSON.stringify(files)])
        .stream()
        .pipeThrough(new CompressionStream('gzip')),
    ).arrayBuffer(),
  );
  const hash = new Uint8Array(
    await crypto.subtle.digest('SHA-256', compressed),
  );
  const revision = Array.from(hash, (byte) =>
    byte.toString(16).padStart(2, '0'),
  ).join('');
  const encrypted = await new CompactEncrypt(compressed)
    .setProtectedHeader({ alg: 'dir', enc: 'A256GCM' })
    .encrypt(decodeBase64(env.SIGMO_CONFIG_ENCRYPTION_KEY));
  await env.BUCKET.put(`configs/releases/${revision}.enc`, encrypted);
  await env.BUCKET.put(
    'configs/current.json',
    JSON.stringify({ schemaVersion: 1, revision }),
  );
  return revision;
}

it('requires authorization before reading a catalog', async () => {
  expect((await request('/v1/configurations/current')).status).toBe(401);
});

it('encrypts and signs the complete snapshot for its authorized device', async () => {
  const identity = await setup();
  const revision = await publish();
  const response = await request(identity.path, identity.headers);
  expect(response.status).toBe(200);
  expect(response.headers.get('cache-control')).toBe('no-store');
  const body = await response.json<{ configuration: string }>();
  const decrypted = await compactDecrypt(
    body.configuration,
    identity.recipient.privateKey,
    {
      keyManagementAlgorithms: ['ECDH-ES'],
      contentEncryptionAlgorithms: ['A256GCM'],
    },
  );
  const signed = JSON.parse(new TextDecoder().decode(decrypted.plaintext)) as {
    payload: string;
    signature: string;
  };
  const payload = new TextDecoder().decode(decodeBase64(signed.payload));
  expect(
    await verifyEd25519(
      env.SIGMO_LICENSE_PUBLIC_KEY,
      TEXT_ENCODER.encode('sigmo-carrier-config-v1\n' + payload),
      signed.signature,
    ),
  ).toBe(true);
  const grant = JSON.parse(payload) as {
    deviceId: string;
    revision: string;
    expiresAt: string;
  };
  expect(grant.deviceId).toBe(identity.id);
  expect(grant.revision).toBe(revision);
  expect(grant.expiresAt).toBe(identity.proof.lease.expiresAt);
  const other = await crypto.subtle.generateKey(
    { name: 'ECDH', namedCurve: 'P-256' },
    true,
    ['deriveBits'],
  );
  assert('privateKey' in other && 'publicKey' in other);
  await expect(
    compactDecrypt(body.configuration, other.privateKey),
  ).rejects.toThrow();
  await expect(
    compactDecrypt(
      body.configuration.slice(0, -2) + 'AA',
      identity.recipient.privateKey,
    ),
  ).rejects.toThrow();
});

it('rejects recipient substitution and revoked devices', async () => {
  const identity = await setup();
  await publish();
  expect((await request(identity.path + 'A', identity.headers)).status).toBe(
    401,
  );
  await env.DB.prepare('UPDATE devices SET revoked_at = ? WHERE device_id = ?')
    .bind(new Date().toISOString(), identity.id)
    .run();
  expect((await request(identity.path, identity.headers)).status).toBe(403);
});

it('bounds repeat downloads across requests', async () => {
  const identity = await setup();
  await publish();
  for (let count = 0; count < 6; count++) {
    expect((await request(identity.path, identity.headers)).status).toBe(200);
  }
  const response = await request(identity.path, identity.headers);
  expect(response.status).toBe(429);
  expect(Number(response.headers.get('retry-after'))).toBeGreaterThan(0);
});
