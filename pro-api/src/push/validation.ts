import type { PushDelivery, PushEnvelope } from './types';
import { isRecord } from '../http/validation';

export function isPushId(value: unknown): value is string {
  return typeof value === 'string' && /^[A-Za-z0-9_-]{16,128}$/.test(value);
}

export function isPushToken(value: unknown): value is string {
  return typeof value === 'string' && /^[A-Za-z0-9_-]{32,128}$/.test(value);
}

export function pushDelivery(value: unknown): PushDelivery | null {
  if (
    !isRecord(value) ||
    !isPushId(value.subscriptionId) ||
    typeof value.eventId !== 'string' ||
    !/^[A-Za-z0-9:_-]{1,160}$/.test(value.eventId) ||
    (value.kind !== 'sms' &&
      value.kind !== 'reminder' &&
      value.kind !== 'badge' &&
      value.kind !== 'call') ||
    typeof value.revision !== 'number' ||
    !Number.isSafeInteger(value.revision) ||
    value.revision < 0 ||
    typeof value.unreadCount !== 'number' ||
    !Number.isSafeInteger(value.unreadCount) ||
    value.unreadCount < 0 ||
    value.unreadCount > 1_000_000 ||
    typeof value.expiresAt !== 'number' ||
    !Number.isSafeInteger(value.expiresAt)
  ) {
    return null;
  }
  const kind = value.kind;
  const fields = {
    subscriptionId: value.subscriptionId,
    eventId: value.eventId,
    revision: value.revision,
    unreadCount: value.unreadCount,
    expiresAt: value.expiresAt,
  };
  if (kind === 'badge') {
    return { ...fields, kind };
  }
  const envelope = value.envelope;
  if (
    !isRecord(envelope) ||
    envelope.version !== 1 ||
    envelope.subscriptionId !== fields.subscriptionId ||
    envelope.eventId !== fields.eventId ||
    typeof envelope.nonce !== 'string' ||
    !/^[A-Za-z0-9+/]{16}$/.test(envelope.nonce) ||
    typeof envelope.ciphertext !== 'string' ||
    envelope.ciphertext.length > 2600 ||
    envelope.ciphertext.length < 24 ||
    !/^[A-Za-z0-9+/]+={0,2}$/.test(envelope.ciphertext) ||
    typeof value.threadId !== 'string' ||
    !/^[A-Za-z0-9_-]{1,64}$/.test(value.threadId)
  ) {
    return null;
  }
  const payload: PushEnvelope = {
    version: 1,
    subscriptionId: fields.subscriptionId,
    eventId: fields.eventId,
    nonce: envelope.nonce,
    ciphertext: envelope.ciphertext,
  };
  const encrypted = { ...fields, threadId: value.threadId, envelope: payload };
  if (kind === 'call') {
    if (
      typeof value.callUUID !== 'string' ||
      !/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(
        value.callUUID,
      )
    ) {
      return null;
    }
    return { ...encrypted, kind, callUUID: value.callUUID };
  }
  return { ...encrypted, kind };
}
