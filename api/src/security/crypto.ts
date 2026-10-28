export const TEXT_ENCODER = new TextEncoder();

export function encodeBase64(
  bytes: ArrayBuffer | Uint8Array,
  urlSafe = false,
): string {
  const data = bytes instanceof Uint8Array ? bytes : new Uint8Array(bytes);
  let binary = '';
  for (const byte of data) {
    binary += String.fromCharCode(byte);
  }
  const encoded = btoa(binary).replace(/=+$/, '');
  return urlSafe ? encoded.replaceAll('+', '-').replaceAll('/', '_') : encoded;
}

export function decodeBase64(value: string): Uint8Array<ArrayBuffer> {
  const normalized = value.replaceAll('-', '+').replaceAll('_', '/');
  const padded = normalized + '='.repeat((4 - (normalized.length % 4)) % 4);
  const binary = atob(padded);
  return Uint8Array.from(binary, (character) => character.charCodeAt(0));
}

export async function sha256(
  value: string | Uint8Array<ArrayBufferLike>,
): Promise<string> {
  const input =
    typeof value === 'string'
      ? TEXT_ENCODER.encode(value)
      : new Uint8Array(value);
  return encodeBase64(await crypto.subtle.digest('SHA-256', input), true);
}

export async function secureEqual(
  provided: string,
  expected: string,
): Promise<boolean> {
  const [providedHash, expectedHash] = await Promise.all([
    crypto.subtle.digest('SHA-256', TEXT_ENCODER.encode(provided)),
    crypto.subtle.digest('SHA-256', TEXT_ENCODER.encode(expected)),
  ]);
  return crypto.subtle.timingSafeEqual(providedHash, expectedHash);
}

async function importEd25519PublicKey(value: string): Promise<CryptoKey> {
  return crypto.subtle.importKey(
    'raw',
    decodeBase64(value),
    { name: 'Ed25519' },
    false,
    ['verify'],
  );
}

async function importEd25519PrivateKey(value: string): Promise<CryptoKey> {
  return crypto.subtle.importKey(
    'pkcs8',
    decodeBase64(value),
    { name: 'Ed25519' },
    false,
    ['sign'],
  );
}

export async function verifyEd25519(
  publicKey: string,
  message: Uint8Array<ArrayBufferLike>,
  signature: string,
): Promise<boolean> {
  try {
    const key = await importEd25519PublicKey(publicKey);
    return await crypto.subtle.verify(
      'Ed25519',
      key,
      decodeBase64(signature),
      new Uint8Array(message),
    );
  } catch {
    return false;
  }
}

export async function signEd25519(
  privateKey: string,
  message: Uint8Array<ArrayBufferLike>,
): Promise<string> {
  const key = await importEd25519PrivateKey(privateKey);
  return encodeBase64(
    await crypto.subtle.sign('Ed25519', key, new Uint8Array(message)),
  );
}

export async function signHmac(secret: string, value: string): Promise<string> {
  const key = await crypto.subtle.importKey(
    'raw',
    TEXT_ENCODER.encode(secret),
    { name: 'HMAC', hash: 'SHA-256' },
    false,
    ['sign'],
  );
  return encodeBase64(
    await crypto.subtle.sign('HMAC', key, TEXT_ENCODER.encode(value)),
    true,
  );
}

export async function verifyHmac(
  secret: string,
  value: string,
  signature: string,
): Promise<boolean> {
  try {
    const key = await crypto.subtle.importKey(
      'raw',
      TEXT_ENCODER.encode(secret),
      { name: 'HMAC', hash: 'SHA-256' },
      false,
      ['verify'],
    );
    return await crypto.subtle.verify(
      'HMAC',
      key,
      decodeBase64(signature),
      TEXT_ENCODER.encode(value),
    );
  } catch {
    return false;
  }
}

export function randomToken(bytes = 24): string {
  return encodeBase64(crypto.getRandomValues(new Uint8Array(bytes)), true);
}
