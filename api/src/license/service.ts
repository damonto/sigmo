import {
  decodeBase64,
  randomToken,
  secureEqual,
  sha256,
  signEd25519,
  TEXT_ENCODER,
  verifyEd25519,
} from '../security/crypto';
import type { RequestContext } from '../http/context';
import type {
  Device,
  DeviceSession,
  Entitlement,
} from '../storage/authorization';
import { apiError, json } from '../http/response';
import { parseJson } from '../http/body';
import { bearerToken } from '../http/authorization';
import type { Lease, SignedLease } from './types';
import {
  challengeRequest,
  leaseRequest,
  pairingRequest,
  signedLease as parseSignedLease,
} from './validation';

const MINIMUM_ENTITLEMENT_LIFETIME = 5_000;
const MAX_OUTSTANDING_PAIRINGS_PER_DEVICE = 3;
const LEASE_CLOCK_SKEW = 5 * 60_000;
const MAX_LEASE_REFRESH_DELAY = 30 * 60_000;
const MAX_LEASE_LIFETIME = 6 * 60 * 60_000;
const DEVICE_ID_PATTERN = /^[0-9a-f]{32}$/;
const TELEGRAM_BOT_USERNAME_PATTERN = /^[A-Za-z][A-Za-z0-9_]{1,28}bot$/i;

type AuthorizationResult =
  { ok: true; lease: Lease } | { ok: false; response: Response };

export function isActive(
  entitlement: Entitlement | null,
  now = new Date(),
): entitlement is Entitlement {
  return (
    entitlement !== null &&
    entitlement.status === 'active' &&
    (!entitlement.expiresAt ||
      new Date(entitlement.expiresAt).getTime() - now.getTime() >=
        MINIMUM_ENTITLEMENT_LIFETIME)
  );
}

export async function deviceId(publicKey: string): Promise<string> {
  const decoded = decodeBase64(publicKey);
  if (decoded.byteLength !== 32) {
    throw new Error('Ed25519 public key must be 32 bytes');
  }
  const digest = new Uint8Array(await crypto.subtle.digest('SHA-256', decoded));
  return Array.from(digest.slice(0, 16), (value) =>
    value.toString(16).padStart(2, '0'),
  ).join('');
}

export async function createPairing(
  request: Request,
  context: RequestContext,
): Promise<Response> {
  const { db, env } = context;
  const body = pairingRequest(await parseJson(request));
  if (!body) {
    return apiError(
      'license_pairing_invalid_request',
      'deviceId, publicKey, refreshTokenHash, and fingerprintHash are required',
      422,
    );
  }
  let expectedId: string;
  try {
    expectedId = await deviceId(body.publicKey);
  } catch {
    return apiError(
      'device_public_key_invalid',
      'publicKey is not a valid Ed25519 public key',
      422,
    );
  }
  if (expectedId !== body.deviceId) {
    return apiError(
      'device_id_mismatch',
      'deviceId does not match publicKey',
      422,
    );
  }
  const botUsername = env.BOT_USERNAME.trim();
  if (!TELEGRAM_BOT_USERNAME_PATTERN.test(botUsername)) {
    throw new Error('BOT_USERNAME is not a valid Telegram bot username');
  }

  const pairingId = randomToken(12);
  const sessionId = randomToken(24);
  const pollToken = randomToken(32);
  const pollTokenHash = await sha256(pollToken);
  const createdAt = new Date();
  const expiresAt = new Date(createdAt.getTime() + 5 * 60_000);
  const createdAtIso = createdAt.toISOString();
  const expiresAtIso = expiresAt.toISOString();
  await db.deleteExpiredState(createdAtIso);
  const inserted = await db.insertPairing(
    {
      pairingId: pairingId,
      pollTokenHash,
      deviceId: body.deviceId,
      publicKey: body.publicKey,
      sessionId: sessionId,
      refreshTokenHash: body.refreshTokenHash,
      fingerprintHash: body.fingerprintHash,
      createdAt: createdAtIso,
      expiresAt: expiresAtIso,
    },
    MAX_OUTSTANDING_PAIRINGS_PER_DEVICE,
  );
  if (!inserted) {
    const response = apiError(
      'license_pairing_limit_exceeded',
      'too many active pairings for this device',
      429,
    );
    // All outstanding pairings expire within five minutes. This is a safe
    // upper bound even if the oldest pairing expires sooner.
    response.headers.set('retry-after', '300');
    return response;
  }

  return json(
    {
      id: pairingId,
      pollToken,
      activationUrl: `https://t.me/${botUsername}?start=${pairingId}`,
      status: 'pending',
      expiresAt: expiresAtIso,
    },
    201,
  );
}

export async function getPairing(
  request: Request,
  context: RequestContext,
  pairingId: string,
): Promise<Response> {
  const { db } = context;
  const token = bearerToken(request);
  const row = await db.findPairing(pairingId);
  if (
    !row ||
    !token ||
    !(await secureEqual(await sha256(token), row.pollTokenHash))
  ) {
    return apiError('license_pairing_not_found', 'pairing not found', 404);
  }
  if (new Date(row.expiresAt) <= new Date()) {
    if (row.status === 'pending') {
      if (request.method === 'HEAD') {
        return json(null);
      }
      await db.markPairingExpired(pairingId);
      return json({
        id: pairingId,
        status: 'expired',
        expiresAt: row.expiresAt,
      });
    }
    return apiError('license_pairing_expired', 'pairing expired', 410);
  }
  if (row.status !== 'active' || row.telegramId === null) {
    if (request.method === 'HEAD') {
      return json(null);
    }
    return json({
      id: pairingId,
      status: row.status,
      expiresAt: row.expiresAt,
    });
  }
  const entitlement = await db.findEntitlement(row.telegramId);
  const device = await db.findDevice(row.deviceId);
  const session = await db.findDeviceSession(row.deviceId);
  if (
    !isActive(entitlement) ||
    !device ||
    device.revokedAt ||
    device.telegramId !== row.telegramId ||
    device.publicKey !== row.publicKey ||
    !session ||
    session.sessionId !== row.sessionId ||
    session.fingerprintHash !== row.fingerprintHash
  ) {
    return apiError(
      'license_entitlement_inactive',
      'authorization revoked or expired',
      403,
    );
  }
  if (request.method === 'HEAD') {
    return json(null);
  }
  return json({
    id: pairingId,
    status: 'active',
    expiresAt: row.expiresAt,
    lease: await issueLease(context, entitlement, device, session),
  });
}

export async function createChallenge(
  request: Request,
  context: RequestContext,
): Promise<Response> {
  const { db } = context;
  const body = challengeRequest(await parseJson(request));
  if (!body) {
    return apiError(
      'license_challenge_invalid_request',
      'deviceId, sessionId, and generation are required',
      422,
    );
  }
  const device = await db.findDevice(body.deviceId);
  if (!device || device.revokedAt) {
    return apiError(
      'license_device_unauthorized',
      'device is not authorized',
      403,
    );
  }
  const session = await db.findDeviceSession(body.deviceId);
  if (
    !session ||
    session.sessionId !== body.sessionId ||
    body.generation > session.generation ||
    body.generation < session.generation - 1
  ) {
    return apiError(
      'license_session_superseded',
      'device authorization session was replaced',
      409,
    );
  }
  const entitlement = await db.findEntitlement(device.telegramId);
  if (!isActive(entitlement)) {
    return apiError(
      'license_entitlement_inactive',
      'authorization revoked or expired',
      403,
    );
  }
  const value = randomToken(32);
  const valueHash = await sha256(value);
  const createdAt = new Date();
  const expiresAt = new Date(createdAt.getTime() + 5 * 60_000);
  const createdAtIso = createdAt.toISOString();
  await db.saveChallenge({
    challengeHash: valueHash,
    deviceId: body.deviceId,
    createdAt: createdAtIso,
    expiresAt: expiresAt.toISOString(),
  });
  return json({ challenge: value, expiresAt: expiresAt.toISOString() }, 201);
}

export async function createLease(
  request: Request,
  context: RequestContext,
): Promise<Response> {
  const { db } = context;
  const body = leaseRequest(await parseJson(request));
  if (!body) {
    return apiError(
      'license_lease_invalid_request',
      'device session rotation request is incomplete',
      422,
    );
  }
  const challengeHash = await sha256(body.challenge);
  const challengeRow = await db.findChallenge(challengeHash, body.deviceId);
  if (!challengeRow) {
    return apiError(
      'license_challenge_invalid',
      'challenge is invalid or already consumed',
      403,
    );
  }
  if (new Date(challengeRow.expiresAt) <= new Date()) {
    return apiError('license_challenge_expired', 'challenge expired', 410);
  }
  const device = await db.findDevice(body.deviceId);
  if (!device || device.revokedAt) {
    return apiError(
      'license_device_unauthorized',
      'device is not authorized',
      403,
    );
  }
  const signatureValid = await verifyEd25519(
    device.publicKey,
    TEXT_ENCODER.encode(rotationMessage(body)),
    body.signature,
  );
  if (!signatureValid) {
    return apiError(
      'device_signature_invalid',
      'device signature is invalid',
      403,
    );
  }
  const entitlement = await db.findEntitlement(device.telegramId);
  if (!isActive(entitlement)) {
    return apiError(
      'license_entitlement_inactive',
      'authorization revoked or expired',
      403,
    );
  }
  const rotation = await db.rotateSession({
    deviceId: body.deviceId,
    sessionId: body.sessionId,
    generation: body.generation,
    refreshTokenHash: await sha256(body.refreshToken),
    nextRefreshTokenHash: body.nextRefreshTokenHash,
    rotationId: body.rotationId,
    fingerprintHash: body.fingerprintHash,
    challengeHash,
    timestamp: new Date().toISOString(),
  });
  if (rotation === 'superseded') {
    return apiError(
      'license_session_superseded',
      'device authorization session was replaced',
      409,
    );
  }
  const session = await db.findDeviceSession(device.deviceId);
  if (!session) {
    throw new Error('rotated device session is unavailable');
  }
  return json(await issueLease(context, entitlement, device, session), 201);
}

interface RotationMessageInput {
  deviceId: string;
  sessionId: string;
  generation: number;
  challenge: string;
  rotationId: string;
  nextRefreshTokenHash: string;
  fingerprintHash: string;
}

export function rotationMessage(input: RotationMessageInput): string {
  return [
    'sigmo-license-v1',
    input.deviceId,
    input.sessionId,
    String(input.generation),
    input.challenge,
    input.rotationId,
    input.nextRefreshTokenHash,
    input.fingerprintHash,
  ].join('\n');
}

export async function issueLease(
  context: RequestContext,
  entitlement: Entitlement,
  device: Device,
  session: DeviceSession,
): Promise<SignedLease> {
  const { env } = context;
  const issuedAt = new Date();
  let expiresAt = new Date(issuedAt.getTime() + MAX_LEASE_LIFETIME);
  const entitlementExpiresAt = entitlement.expiresAt
    ? new Date(entitlement.expiresAt)
    : undefined;
  if (entitlementExpiresAt && entitlementExpiresAt < expiresAt) {
    expiresAt = entitlementExpiresAt;
  }
  const lifetime = expiresAt.getTime() - issuedAt.getTime();
  if (lifetime < 2) {
    throw new Error('authorization expires too soon to issue a lease');
  }
  const refreshAfter = new Date(
    issuedAt.getTime() +
      Math.min(MAX_LEASE_REFRESH_DELAY, Math.floor(lifetime / 2)),
  );
  const telegramId = entitlement.telegramId;
  if (!Number.isSafeInteger(telegramId)) {
    throw new Error('Telegram ID exceeds JavaScript safe integer range');
  }
  const lease: Lease = {
    schemaVersion: 1,
    deviceId: device.deviceId,
    sessionId: session.sessionId,
    generation: session.generation,
    telegramId: telegramId,
    status: 'active',
    displayName: entitlement.displayName,
    username: entitlement.username || undefined,
    issuedAt: issuedAt.toISOString(),
    refreshAfter: refreshAfter.toISOString(),
    expiresAt: expiresAt.toISOString(),
    entitlementExpiresAt: entitlementExpiresAt?.toISOString(),
  };
  const encoded = TEXT_ENCODER.encode(JSON.stringify(lease));
  return {
    lease,
    signature: await signEd25519(env.SIGMO_LICENSE_PRIVATE_KEY, encoded),
  };
}

function authorizationRequired(): AuthorizationResult {
  return {
    ok: false,
    response: apiError('authorization_required', 'authorization required', 401),
  };
}

export async function authorizeRequest(
  request: Request,
  context: RequestContext,
): Promise<AuthorizationResult> {
  const { db, env } = context;
  const deviceIdHeader = request.headers.get('x-sigmo-device-id') ?? '';
  const proofValue = request.headers.get('x-sigmo-lease') ?? '';
  const timestamp = request.headers.get('x-sigmo-timestamp') ?? '';
  const signature = request.headers.get('x-sigmo-signature') ?? '';
  const unix = Number(timestamp);
  if (
    !DEVICE_ID_PATTERN.test(deviceIdHeader) ||
    !proofValue ||
    proofValue.length > 16 * 1024 ||
    !Number.isSafeInteger(unix) ||
    Math.abs(Math.floor(Date.now() / 1000) - unix) > 300 ||
    !signature ||
    signature.length > 256
  ) {
    return authorizationRequired();
  }

  let proof: SignedLease | null;
  try {
    proof = parseSignedLease(
      JSON.parse(new TextDecoder().decode(decodeBase64(proofValue))) as unknown,
    );
  } catch {
    return authorizationRequired();
  }
  if (!proof) {
    return authorizationRequired();
  }

  const issuedAt = Date.parse(proof.lease.issuedAt);
  const refreshAfter = Date.parse(proof.lease.refreshAfter);
  const expiresAt = Date.parse(proof.lease.expiresAt);
  const entitlementExpiresAt = proof.lease.entitlementExpiresAt
    ? Date.parse(proof.lease.entitlementExpiresAt)
    : undefined;
  const now = Date.now();
  if (
    !Number.isFinite(issuedAt) ||
    !Number.isFinite(refreshAfter) ||
    !Number.isFinite(expiresAt) ||
    proof.lease.deviceId !== deviceIdHeader ||
    issuedAt > now + LEASE_CLOCK_SKEW ||
    issuedAt >= refreshAfter ||
    refreshAfter - issuedAt > MAX_LEASE_REFRESH_DELAY ||
    refreshAfter >= expiresAt ||
    expiresAt - issuedAt > MAX_LEASE_LIFETIME ||
    (entitlementExpiresAt !== undefined &&
      (!Number.isFinite(entitlementExpiresAt) ||
        entitlementExpiresAt < expiresAt))
  ) {
    return authorizationRequired();
  }
  if (
    !(await verifyEd25519(
      env.SIGMO_LICENSE_PUBLIC_KEY,
      TEXT_ENCODER.encode(JSON.stringify(proof.lease)),
      proof.signature,
    ))
  ) {
    return authorizationRequired();
  }
  if (expiresAt <= now) {
    return {
      ok: false,
      response: apiError('license_lease_expired', 'license lease expired', 401),
    };
  }

  const device = await db.findDevice(deviceIdHeader);
  if (!device || device.revokedAt) {
    return {
      ok: false,
      response: apiError(
        'license_device_unauthorized',
        'device is not authorized',
        403,
      ),
    };
  }
  const session = await db.findDeviceSession(deviceIdHeader);
  if (
    !session ||
    session.sessionId !== proof.lease.sessionId ||
    session.generation !== proof.lease.generation
  ) {
    return {
      ok: false,
      response: apiError(
        'license_session_superseded',
        'device authorization session was replaced',
        409,
      ),
    };
  }
  const requestUrl = new URL(request.url);
  if (
    !(await verifyEd25519(
      device.publicKey,
      TEXT_ENCODER.encode(
        `${request.method}\n${requestUrl.pathname}${requestUrl.search}\n${timestamp}`,
      ),
      signature,
    ))
  ) {
    return authorizationRequired();
  }

  const entitlement = await db.findEntitlement(device.telegramId);
  if (!isActive(entitlement)) {
    return {
      ok: false,
      response: apiError(
        'license_entitlement_inactive',
        'authorization revoked or expired',
        403,
      ),
    };
  }
  if (entitlement.telegramId !== proof.lease.telegramId) {
    return authorizationRequired();
  }
  if (request.method !== 'HEAD') {
    await db.markDeviceSeen(deviceIdHeader, new Date().toISOString());
  }
  return { ok: true, lease: proof.lease };
}
