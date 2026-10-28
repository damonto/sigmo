import type { Lease, SignedLease } from './types';
import {
  isRecord,
  isNonEmptyString,
  isSafeInteger,
  isRfc3339,
} from '../http/validation';

export function pairingRequest(value: unknown): {
  deviceId: string;
  publicKey: string;
  refreshTokenHash: string;
  fingerprintHash: string;
} | null {
  if (
    !isRecord(value) ||
    !isNonEmptyString(value.deviceId) ||
    !isNonEmptyString(value.publicKey) ||
    !isSha256(value.refreshTokenHash) ||
    !isSha256(value.fingerprintHash)
  ) {
    return null;
  }
  return {
    deviceId: value.deviceId,
    publicKey: value.publicKey,
    refreshTokenHash: value.refreshTokenHash,
    fingerprintHash: value.fingerprintHash,
  };
}

export function challengeRequest(value: unknown): {
  deviceId: string;
  sessionId: string;
  generation: number;
} | null {
  if (
    !isRecord(value) ||
    !isNonEmptyString(value.deviceId) ||
    !isNonEmptyString(value.sessionId) ||
    !isSafeInteger(value.generation) ||
    value.generation < 1
  ) {
    return null;
  }
  return {
    deviceId: value.deviceId,
    sessionId: value.sessionId,
    generation: value.generation,
  };
}

export function leaseRequest(value: unknown): {
  deviceId: string;
  sessionId: string;
  generation: number;
  challenge: string;
  refreshToken: string;
  nextRefreshTokenHash: string;
  rotationId: string;
  fingerprintHash: string;
  signature: string;
} | null {
  if (
    !isRecord(value) ||
    !isNonEmptyString(value.deviceId) ||
    !isNonEmptyString(value.sessionId) ||
    !isSafeInteger(value.generation) ||
    value.generation < 1 ||
    !isNonEmptyString(value.challenge) ||
    !isNonEmptyString(value.refreshToken) ||
    !isSha256(value.nextRefreshTokenHash) ||
    !isNonEmptyString(value.rotationId) ||
    !isSha256(value.fingerprintHash) ||
    !isNonEmptyString(value.signature)
  ) {
    return null;
  }
  return {
    deviceId: value.deviceId,
    sessionId: value.sessionId,
    generation: value.generation,
    challenge: value.challenge,
    refreshToken: value.refreshToken,
    nextRefreshTokenHash: value.nextRefreshTokenHash,
    rotationId: value.rotationId,
    fingerprintHash: value.fingerprintHash,
    signature: value.signature,
  };
}

function isSha256(value: unknown): value is string {
  return typeof value === 'string' && /^[A-Za-z0-9_-]{43}$/.test(value);
}

function isLease(value: unknown): value is Lease {
  return (
    isRecord(value) &&
    value.schemaVersion === 1 &&
    isNonEmptyString(value.deviceId) &&
    isNonEmptyString(value.sessionId) &&
    isSafeInteger(value.generation) &&
    value.generation > 0 &&
    isSafeInteger(value.telegramId) &&
    value.telegramId > 0 &&
    value.status === 'active' &&
    typeof value.displayName === 'string' &&
    (value.username === undefined || typeof value.username === 'string') &&
    isRfc3339(value.issuedAt) &&
    isRfc3339(value.refreshAfter) &&
    isRfc3339(value.expiresAt) &&
    (value.entitlementExpiresAt === undefined ||
      isRfc3339(value.entitlementExpiresAt))
  );
}

export function signedLease(value: unknown): SignedLease | null {
  if (
    !isRecord(value) ||
    !isNonEmptyString(value.signature) ||
    !isLease(value.lease)
  ) {
    return null;
  }
  return { lease: value.lease, signature: value.signature };
}
