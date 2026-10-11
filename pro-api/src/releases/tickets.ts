import {
  decodeBase64,
  encodeBase64,
  signHmac,
  TEXT_ENCODER,
  verifyHmac,
} from '../security/crypto';
import type { DownloadTicket } from './types';
import { downloadTicket } from './validation';

const MINIMUM_SECRET_LENGTH = 32;

export async function createTicket(
  secret: string,
  ticket: DownloadTicket,
): Promise<string> {
  if (secret.length < MINIMUM_SECRET_LENGTH) {
    throw new Error('download ticket secret must be at least 32 characters');
  }
  const payload = encodeBase64(
    TEXT_ENCODER.encode(JSON.stringify(ticket)),
    true,
  );
  return `${payload}.${await signHmac(secret, payload)}`;
}

export async function parseTicket(
  secret: string,
  value: string,
  now = Date.now(),
): Promise<DownloadTicket | null> {
  if (secret.length < MINIMUM_SECRET_LENGTH) {
    return null;
  }
  const parts = value.split('.');
  if (parts.length !== 2) {
    return null;
  }
  const [payload, signature] = parts;
  if (
    !payload ||
    !signature ||
    !(await verifyHmac(secret, payload, signature))
  ) {
    return null;
  }
  try {
    const ticket = downloadTicket(
      JSON.parse(new TextDecoder().decode(decodeBase64(payload))) as unknown,
    );
    if (!ticket || ticket.expiresAt <= Math.floor(now / 1000)) {
      return null;
    }
    return ticket;
  } catch {
    return null;
  }
}
