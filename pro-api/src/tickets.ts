import {
  decodeBase64,
  encodeBase64,
  signHMAC,
  textEncoder,
  verifyHMAC,
} from "./crypto";
import type { DownloadTicket } from "./types";
import { downloadTicket } from "./validation";

const minimumSecretLength = 32;

export async function createTicket(
  secret: string,
  ticket: DownloadTicket,
): Promise<string> {
  if (secret.length < minimumSecretLength)
    throw new Error("download ticket secret must be at least 32 characters");
  const payload = encodeBase64(
    textEncoder.encode(JSON.stringify(ticket)),
    true,
  );
  return `${payload}.${await signHMAC(secret, payload)}`;
}

export async function parseTicket(
  secret: string,
  value: string,
  now = Date.now(),
): Promise<DownloadTicket | null> {
  if (secret.length < minimumSecretLength) return null;
  const [payload, signature, extra] = value.split(".");
  if (
    !payload ||
    !signature ||
    extra ||
    !(await verifyHMAC(secret, payload, signature))
  )
    return null;
  try {
    const ticket = downloadTicket(
      JSON.parse(new TextDecoder().decode(decodeBase64(payload))) as unknown,
    );
    if (!ticket || ticket.expiresAt <= Math.floor(now / 1000)) return null;
    return ticket;
  } catch {
    return null;
  }
}
