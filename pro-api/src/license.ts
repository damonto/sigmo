import {
  decodeBase64,
  encodeBase64,
  randomToken,
  secureEqual,
  sha256,
  signEd25519,
  textEncoder,
  verifyEd25519,
} from "./crypto";
import type { RequestContext } from "./context";
import type { Device, DeviceSession, Entitlement } from "./database";
import { apiError, json, nowISO, parseJSON } from "./http";
import type { Lease, SignedLease } from "./types";
import {
  challengeRequest,
  leaseRequest,
  pairingRequest,
  signedLease as parseSignedLease,
} from "./validation";

const minimumEntitlementLifetime = 5_000;
const maxOutstandingPairingsPerDevice = 3;
const leaseClockSkew = 5 * 60_000;
const maxLeaseRefreshDelay = 30 * 60_000;
const maxLeaseLifetime = 6 * 60 * 60_000;
const deviceIDPattern = /^[0-9a-f]{32}$/;
const telegramBotUsernamePattern = /^[A-Za-z][A-Za-z0-9_]{1,28}bot$/i;

type AuthorizationResult =
  | { ok: true; lease: Lease }
  | { ok: false; response: Response };

export function isActive(
  entitlement: Entitlement | null,
  now = new Date(),
): entitlement is Entitlement {
  return (
    entitlement !== null &&
    entitlement.status === "active" &&
    (!entitlement.expiresAt ||
      new Date(entitlement.expiresAt).getTime() - now.getTime() >=
        minimumEntitlementLifetime)
  );
}

export async function deviceID(publicKey: string): Promise<string> {
  const decoded = decodeBase64(publicKey);
  if (decoded.byteLength !== 32)
    throw new Error("Ed25519 public key must be 32 bytes");
  const digest = new Uint8Array(await crypto.subtle.digest("SHA-256", decoded));
  return Array.from(digest.slice(0, 16), (value) =>
    value.toString(16).padStart(2, "0"),
  ).join("");
}

export async function createPairing(
  request: Request,
  context: RequestContext,
): Promise<Response> {
  const { db, env } = context;
  const body = pairingRequest(await parseJSON(request));
  if (!body)
    return apiError(
      "license_pairing_invalid_request",
      "deviceId, publicKey, refreshTokenHash, and fingerprintHash are required",
      400,
    );
  let expectedID: string;
  try {
    expectedID = await deviceID(body.publicKey);
  } catch {
    return apiError(
      "device_public_key_invalid",
      "publicKey is not a valid Ed25519 public key",
      400,
    );
  }
  if (expectedID !== body.deviceId)
    return apiError(
      "device_id_mismatch",
      "deviceId does not match publicKey",
      400,
    );
  const botUsername = env.BOT_USERNAME.trim();
  if (!telegramBotUsernamePattern.test(botUsername))
    throw new Error("BOT_USERNAME is not a valid Telegram bot username");

  const pairingID = randomToken(12);
  const sessionID = randomToken(24);
  const pollToken = randomToken(32);
  const pollTokenHash = await sha256(pollToken);
  const createdAt = new Date();
  const expiresAt = new Date(createdAt.getTime() + 5 * 60_000);
  const createdAtISO = createdAt.toISOString();
  const expiresAtISO = expiresAt.toISOString();
  await db.deleteExpiredState(createdAtISO);
  const inserted = await db.insertPairing(
    {
      pairingId: pairingID,
      pollTokenHash,
      deviceId: body.deviceId,
      publicKey: body.publicKey,
      sessionId: sessionID,
      refreshTokenHash: body.refreshTokenHash,
      fingerprintHash: body.fingerprintHash,
      createdAt: createdAtISO,
      expiresAt: expiresAtISO,
    },
    maxOutstandingPairingsPerDevice,
  );
  if (!inserted)
    return apiError(
      "license_pairing_limit_exceeded",
      "too many active pairings for this device",
      429,
    );

  return json(
    {
      id: pairingID,
      pollToken,
      activationUrl: `https://t.me/${botUsername}?start=${pairingID}`,
      status: "pending",
      expiresAt: expiresAtISO,
    },
    201,
  );
}

export async function getPairing(
  request: Request,
  context: RequestContext,
  pairingID: string,
): Promise<Response> {
  const { db } = context;
  const token =
    request.headers.get("authorization")?.replace(/^Bearer\s+/i, "") ?? "";
  const row = await db.findPairing(pairingID);
  if (
    !row ||
    !token ||
    !(await secureEqual(await sha256(token), row.pollTokenHash))
  )
    return apiError("license_pairing_not_found", "pairing not found", 404);
  if (new Date(row.expiresAt) <= new Date()) {
    if (row.status === "pending") {
      await db.markPairingExpired(pairingID);
      return json({
        id: pairingID,
        status: "expired",
        expiresAt: row.expiresAt,
      });
    }
    return apiError("license_pairing_expired", "pairing expired", 410);
  }
  if (row.status !== "active" || row.telegramId === null) {
    return json({
      id: pairingID,
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
  )
    return apiError(
      "license_entitlement_inactive",
      "authorization revoked or expired",
      403,
    );
  return json({
    id: pairingID,
    status: "active",
    expiresAt: row.expiresAt,
    lease: await issueLease(context, entitlement, device, session),
  });
}

export async function createChallenge(
  request: Request,
  context: RequestContext,
): Promise<Response> {
  const { db } = context;
  const body = challengeRequest(await parseJSON(request));
  if (!body)
    return apiError(
      "license_challenge_invalid_request",
      "deviceId, sessionId, and generation are required",
      400,
    );
  const device = await db.findDevice(body.deviceId);
  if (!device || device.revokedAt)
    return apiError(
      "license_device_unauthorized",
      "device is not authorized",
      403,
    );
  const session = await db.findDeviceSession(body.deviceId);
  if (
    !session ||
    session.sessionId !== body.sessionId ||
    body.generation > session.generation ||
    body.generation < session.generation - 1
  )
    return apiError(
      "license_session_superseded",
      "device authorization session was replaced",
      409,
    );
  const entitlement = await db.findEntitlement(device.telegramId);
  if (!isActive(entitlement))
    return apiError(
      "license_entitlement_inactive",
      "authorization revoked or expired",
      403,
    );
  const value = randomToken(32);
  const valueHash = await sha256(value);
  const createdAt = new Date();
  const expiresAt = new Date(createdAt.getTime() + 5 * 60_000);
  const createdAtISO = createdAt.toISOString();
  await db.saveChallenge({
    challengeHash: valueHash,
    deviceId: body.deviceId,
    createdAt: createdAtISO,
    expiresAt: expiresAt.toISOString(),
  });
  return json({ challenge: value, expiresAt: expiresAt.toISOString() }, 201);
}

export async function createLease(
  request: Request,
  context: RequestContext,
): Promise<Response> {
  const { db } = context;
  const body = leaseRequest(await parseJSON(request));
  if (!body)
    return apiError(
      "license_lease_invalid_request",
      "device session rotation request is incomplete",
      400,
    );
  const challengeHash = await sha256(body.challenge);
  const challengeRow = await db.findChallenge(challengeHash, body.deviceId);
  if (!challengeRow)
    return apiError(
      "license_challenge_invalid",
      "challenge is invalid or already consumed",
      403,
    );
  if (new Date(challengeRow.expiresAt) <= new Date())
    return apiError("license_challenge_expired", "challenge expired", 410);
  const device = await db.findDevice(body.deviceId);
  if (!device || device.revokedAt)
    return apiError(
      "license_device_unauthorized",
      "device is not authorized",
      403,
    );
  const signatureValid = await verifyEd25519(
    device.publicKey,
    textEncoder.encode(rotationMessage(body)),
    body.signature,
  );
  if (!signatureValid)
    return apiError(
      "device_signature_invalid",
      "device signature is invalid",
      403,
    );
  const entitlement = await db.findEntitlement(device.telegramId);
  if (!isActive(entitlement))
    return apiError(
      "license_entitlement_inactive",
      "authorization revoked or expired",
      403,
    );
  const rotation = await db.rotateSession({
    deviceId: body.deviceId,
    sessionId: body.sessionId,
    generation: body.generation,
    refreshTokenHash: await sha256(body.refreshToken),
    nextRefreshTokenHash: body.nextRefreshTokenHash,
    rotationId: body.rotationId,
    fingerprintHash: body.fingerprintHash,
    challengeHash,
    timestamp: nowISO(),
  });
  if (rotation === "superseded")
    return apiError(
      "license_session_superseded",
      "device authorization session was replaced",
      409,
    );
  const session = await db.findDeviceSession(device.deviceId);
  if (!session) throw new Error("rotated device session is unavailable");
  return json(await issueLease(context, entitlement, device, session), 201);
}

type RotationMessageInput = {
  deviceId: string;
  sessionId: string;
  generation: number;
  challenge: string;
  rotationId: string;
  nextRefreshTokenHash: string;
  fingerprintHash: string;
};

export function rotationMessage(input: RotationMessageInput): string {
  return [
    "sigmo-license-v1",
    input.deviceId,
    input.sessionId,
    String(input.generation),
    input.challenge,
    input.rotationId,
    input.nextRefreshTokenHash,
    input.fingerprintHash,
  ].join("\n");
}

export async function issueLease(
  context: RequestContext,
  entitlement: Entitlement,
  device: Device,
  session: DeviceSession,
): Promise<SignedLease> {
  const { env } = context;
  const issuedAt = new Date();
  let expiresAt = new Date(issuedAt.getTime() + maxLeaseLifetime);
  const entitlementExpiresAt = entitlement.expiresAt
    ? new Date(entitlement.expiresAt)
    : undefined;
  if (entitlementExpiresAt && entitlementExpiresAt < expiresAt)
    expiresAt = entitlementExpiresAt;
  const lifetime = expiresAt.getTime() - issuedAt.getTime();
  if (lifetime < 2)
    throw new Error("authorization expires too soon to issue a lease");
  const refreshAfter = new Date(
    issuedAt.getTime() + Math.min(maxLeaseRefreshDelay, Math.floor(lifetime / 2)),
  );
  const telegramID = entitlement.telegramId;
  if (!Number.isSafeInteger(telegramID))
    throw new Error("Telegram ID exceeds JavaScript safe integer range");
  const lease: Lease = {
    schemaVersion: 1,
    deviceId: device.deviceId,
    sessionId: session.sessionId,
    generation: session.generation,
    telegramId: telegramID,
    status: "active",
    displayName: entitlement.displayName,
    username: entitlement.username || undefined,
    issuedAt: issuedAt.toISOString(),
    refreshAfter: refreshAfter.toISOString(),
    expiresAt: expiresAt.toISOString(),
    entitlementExpiresAt: entitlementExpiresAt?.toISOString(),
  };
  const encoded = textEncoder.encode(JSON.stringify(lease));
  return {
    lease,
    signature: await signEd25519(env.SIGMO_LICENSE_PRIVATE_KEY, encoded),
  };
}

function authorizationRequired(): AuthorizationResult {
  return {
    ok: false,
    response: apiError("authorization_required", "authorization required", 401),
  };
}

export async function authorizeRequest(
  request: Request,
  context: RequestContext,
): Promise<AuthorizationResult> {
  const { db, env } = context;
  const deviceIDHeader = request.headers.get("x-sigmo-device-id") ?? "";
  const proofValue = request.headers.get("x-sigmo-lease") ?? "";
  const timestamp = request.headers.get("x-sigmo-timestamp") ?? "";
  const signature = request.headers.get("x-sigmo-signature") ?? "";
  const unix = Number(timestamp);
  if (
    !deviceIDPattern.test(deviceIDHeader) ||
    !proofValue ||
    proofValue.length > 16 * 1024 ||
    !Number.isSafeInteger(unix) ||
    Math.abs(Math.floor(Date.now() / 1000) - unix) > 300 ||
    !signature ||
    signature.length > 256
  )
    return authorizationRequired();

  let proof: SignedLease | null;
  try {
    proof = parseSignedLease(
      JSON.parse(new TextDecoder().decode(decodeBase64(proofValue))) as unknown,
    );
  } catch {
    return authorizationRequired();
  }
  if (!proof) return authorizationRequired();

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
    proof.lease.deviceId !== deviceIDHeader ||
    issuedAt > now + leaseClockSkew ||
    issuedAt >= refreshAfter ||
    refreshAfter - issuedAt > maxLeaseRefreshDelay ||
    refreshAfter >= expiresAt ||
    expiresAt - issuedAt > maxLeaseLifetime ||
    (entitlementExpiresAt !== undefined &&
      (!Number.isFinite(entitlementExpiresAt) ||
        entitlementExpiresAt < expiresAt))
  )
    return authorizationRequired();
  if (
    !(await verifyEd25519(
      env.SIGMO_LICENSE_PUBLIC_KEY,
      textEncoder.encode(JSON.stringify(proof.lease)),
      proof.signature,
    ))
  )
    return authorizationRequired();
  if (expiresAt <= now)
    return {
      ok: false,
      response: apiError("license_lease_expired", "license lease expired", 401),
    };

  const device = await db.findDevice(deviceIDHeader);
  if (!device || device.revokedAt)
    return {
      ok: false,
      response: apiError(
        "license_device_unauthorized",
        "device is not authorized",
        403,
      ),
    };
  const session = await db.findDeviceSession(deviceIDHeader);
  if (
    !session ||
    session.sessionId !== proof.lease.sessionId ||
    session.generation !== proof.lease.generation
  )
    return {
      ok: false,
      response: apiError(
        "license_session_superseded",
        "device authorization session was replaced",
        409,
      ),
    };
  const requestURL = new URL(request.url);
  if (
    !(await verifyEd25519(
      device.publicKey,
      textEncoder.encode(
        `${request.method}\n${requestURL.pathname}${requestURL.search}\n${timestamp}`,
      ),
      signature,
    ))
  )
    return authorizationRequired();

  const entitlement = await db.findEntitlement(device.telegramId);
  if (!isActive(entitlement))
    return {
      ok: false,
      response: apiError(
        "license_entitlement_inactive",
        "authorization revoked or expired",
        403,
      ),
    };
  if (entitlement.telegramId !== proof.lease.telegramId)
    return authorizationRequired();
  await db.markDeviceSeen(deviceIDHeader, nowISO());
  return { ok: true, lease: proof.lease };
}
